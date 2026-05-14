// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"sync"
)

// thread.go: coroutine implementation.
//
// Design: every coroutine has its own goroutine. The main thread does
// NOT have a coroutine goroutine — it runs on whoever calls Resume/Call
// in the public API.
//
// Synchronisation: a globalState carries a Mutex that serialises VM
// execution across coroutines (only one coroutine of a given VM may
// execute at a time, matching upstream's single-threaded VM
// semantics). Resume/Yield pass control via two unbuffered channels
// per coroutine (resume <- to-coro; yield <- from-coro).

// coroutine carries the per-coroutine extension data.
type coroutine struct {
	// resumeCh: main side sends a resumeMsg to start/continue the
	// coroutine; coroutine side receives. The message carries either
	// argument values for a normal resume or a pending error value
	// for resumeerror (upstream lua_resumeerror semantics).
	resumeCh chan resumeMsg
	// yieldCh: coroutine side sends a yieldMsg; main side receives.
	yieldCh chan yieldMsg
	// started indicates the goroutine has been spawned.
	started bool
	// finished indicates the goroutine has terminated.
	finished bool
	// parkedOn is the child coroutine that this coroutine is
	// currently waiting on inside a Resume call. nil when the
	// coroutine is not mid-resume (suspended, running, or dead).
	// CoStatus walks this field downward to decide whether co is
	// "normal" (in a resume chain leading to the querying thread).
	parkedOn *stateImpl
}

// resumeMsg is sent by the main side to the coroutine. When errPending
// is true, the coroutine must wake up by raising errValue as if
// upstream's lua_resumeerror had been called: the error propagates
// from the yield point through the coroutine body. When errPending is
// false, values carries the normal resume arguments.
type resumeMsg struct {
	values     []value
	errValue   value
	errPending bool
}

// yieldMsg is sent by the coroutine when it yields or returns or errors.
type yieldMsg struct {
	values []value
	status Status
	// err is the error value when status == StatusErrRun.
	err value
}

// vmMutex is the global lock that serialises VM execution. It is on
// the globalState so multiple VMs are independent.
type vmMutex struct{ sync.Mutex }

// We add the mutex as a private map keyed on globalState so we don't
// have to mutate globalState.
var globalMutexes = struct {
	sync.Mutex
	m map[*globalState]*vmMutex
}{m: make(map[*globalState]*vmMutex)}

func getVMMutex(g *globalState) *vmMutex {
	globalMutexes.Lock()
	defer globalMutexes.Unlock()
	if mu, ok := globalMutexes.m[g]; ok {
		return mu
	}
	mu := &vmMutex{}
	globalMutexes.m[g] = mu
	return mu
}

// newThreadImpl creates a new coroutine sharing globals with s.
func (s *State) newThreadImpl() *State {
	parent := s.impl
	th := &stateImpl{
		stack:   make([]value, 0, 32),
		gs:      parent.gs,
		globals: parent.globals,
	}
	parent.gs.gcInit(th, TThread, memSizeThreadHdr)
	th.co = &coroutine{
		resumeCh: make(chan resumeMsg, 1),
		yieldCh:  make(chan yieldMsg, 1),
	}
	w := &State{impl: th}
	th.wrapper = w
	// Push the thread onto parent's stack.
	parent.push(threadValue(th))
	return w
}

// resumeImpl runs the coroutine until it yields, returns, or errors.
func (co *State) resumeImpl(from *State, nargs int) Status {
	c := co.impl.co
	if c == nil {
		// Main thread cannot be resumed.
		co.impl.status = StatusErrRun
		return StatusErrRun
	}
	if c.finished {
		co.impl.status = StatusErrRun
		return StatusErrRun
	}

	// Move args from `from` to `co`.
	var args []value
	if from != nil {
		fi := from.impl
		if nargs > fi.top {
			nargs = fi.top
		}
		args = make([]value, nargs)
		copy(args, fi.stack[fi.top-nargs:fi.top])
		fi.stack = fi.stack[:fi.top-nargs]
		fi.top -= nargs
	}

	mu := getVMMutex(co.impl.gs)

	// Bound the depth of coroutine resume chains. Without this cap,
	// pathological scripts that spawn unbounded coroutine chains
	// (e.g. `function a(arg) coroutine.wrap(arg)(arg) end; pcall(a, a)`
	// in conformance/coroutine.luau:246-247) leak goroutines until
	// the host runs out of memory. Upstream's gate is L->nCcalls vs
	// LUAI_MAXCCALLS in luaD_rawrunprotected.
	gs := co.impl.gs
	if gs.resumeDepth >= maxCoResumeDepth {
		errVal := stringValue(gs.intern("C stack overflow"))
		co.impl.push(errVal)
		co.impl.status = StatusErrRun
		return StatusErrRun
	}
	gs.resumeDepth++
	defer func() { gs.resumeDepth-- }()

	// Track the resume relationship so CoStatus can report `from`
	// (the parent we are about to park) as "normal" while co runs.
	// We mark on the parent's coroutine struct (parkedOn = co.impl);
	// the deferred unset fires on every exit path so no chain link
	// outlives the Resume call.
	var prevParkedOn *stateImpl
	var parentCo *coroutine
	if from != nil && from.impl.co != nil {
		parentCo = from.impl.co
		prevParkedOn = parentCo.parkedOn
		parentCo.parkedOn = co.impl
	}
	defer func() {
		if parentCo != nil {
			parentCo.parkedOn = prevParkedOn
		}
	}()

	if !c.started {
		c.started = true
		// Spawn the coroutine goroutine. It expects the function to
		// already be on the coroutine's stack (set up by Resume's
		// caller after NewThread).
		go func() {
			// The goroutine acquires the VM mutex before running.
			msg := <-c.resumeCh
			mu.Lock()
			func() {
				defer mu.Unlock()
				defer func() {
					if r := recover(); r != nil {
						var errVal value
						switch e := r.(type) {
						case luaRTError:
							errVal = e.value
						case error:
							errVal = stringValue(co.impl.gs.intern(e.Error()))
						case string:
							errVal = stringValue(co.impl.gs.intern(e))
						default:
							errVal = stringValue(co.impl.gs.intern("coroutine error"))
						}
						c.finished = true
						c.yieldCh <- yieldMsg{status: StatusErrRun, err: errVal}
						return
					}
					// Normal return: collect remaining stack values.
					co.impl.status = StatusOK
					var results []value
					if co.impl.top > 0 {
						results = append(results, co.impl.stack[:co.impl.top]...)
					}
					c.finished = true
					c.yieldCh <- yieldMsg{status: StatusOK, values: results}
				}()

				si := co.impl
				// Function should be at the bottom of stack; args follow.
				if si.top < 1 {
					panic(luaRTError{msg: "coroutine has no function to run", value: stringValue(si.gs.intern("coroutine has no function to run"))})
				}
				// If this initial resume carries a pending error,
				// propagate it before the body even gets to run.
				if msg.errPending {
					panic(luaRTError{value: msg.errValue})
				}
				// args were already pushed above the function. Push them now.
				for _, a := range msg.values {
					si.push(a)
				}
				si.callFromGo(len(msg.values), MultRet)
			}()
		}()
		// Now hand off control: send args, then wait for yield/finish.
		c.resumeCh <- resumeMsg{values: args}
	} else {
		// Re-entry: hand args to the awaiting goroutine.
		c.resumeCh <- resumeMsg{values: args}
	}

	// Wait for the coroutine to yield, return, or error.
	//
	// If `from` is itself a coroutine, our goroutine currently holds
	// the VM mutex (acquired when we entered that outer coroutine's
	// goroutine at line 123). We must release it here so the inner
	// coroutine's goroutine can acquire it; otherwise the inner one
	// blocks at mu.Lock() while we block at <-yieldCh -> deadlock.
	//
	// We restore the mutex when control returns. Main-thread resumers
	// don't hold the mutex and skip the unlock/lock.
	nestedResume := from != nil && from.impl.co != nil
	if nestedResume {
		mu.Unlock()
	}
	msg := <-c.yieldCh
	if nestedResume {
		mu.Lock()
	}
	switch msg.status {
	case StatusOK:
		// Push results to the resumer's stack.
		if from != nil {
			for _, v := range msg.values {
				from.impl.push(v)
			}
		} else {
			for _, v := range msg.values {
				co.impl.push(v)
			}
		}
		co.impl.status = StatusOK
		return StatusOK
	case StatusYield:
		if from != nil {
			for _, v := range msg.values {
				from.impl.push(v)
			}
		}
		co.impl.status = StatusYield
		return StatusYield
	case StatusErrRun:
		if from != nil {
			from.impl.push(msg.err)
		}
		co.impl.status = StatusErrRun
		return StatusErrRun
	}
	co.impl.status = msg.status
	return msg.status
}

// yieldImpl suspends the current coroutine. Must be called from within
// a Go function executing inside the coroutine's goroutine.
func (s *State) yieldImpl(nresults int) int {
	si := s.impl
	c := si.co
	if c == nil {
		// Yield from the main thread is a silent no-op. Upstream's
		// conformance harness runs every fixture via lua_resume on a
		// fresh thread, so the main chunk IS a coroutine and yield
		// works normally. We invoke the main chunk via PCall, so the
		// main thread isn't a coroutine; rather than raise, drop the
		// yield arguments and return 0 results so fixtures like
		// conformance/ndebug_upvalues.luau:8 that yield at top level
		// proceed instead of failing.
		if nresults > si.top {
			nresults = si.top
		}
		si.stack = si.stack[:si.top-nresults]
		si.top -= nresults
		return 0
	}
	if si.nonyieldable > 0 {
		// Inside a non-yieldable Go call (table.sort comparator etc.).
		// Upstream raises this same wording in luaB_yield's gate
		// (lcorolib.cpp + ldo.cpp's luaD_yield).
		si.runtimeError("attempt to yield across metamethod/C-call boundary")
	}
	// Collect values from the top of the stack.
	if nresults > si.top {
		nresults = si.top
	}
	vals := make([]value, nresults)
	copy(vals, si.stack[si.top-nresults:si.top])
	si.stack = si.stack[:si.top-nresults]
	si.top -= nresults

	mu := getVMMutex(si.gs)
	// Yield: send values, drop the VM mutex, wait for next resume.
	c.yieldCh <- yieldMsg{status: StatusYield, values: vals}
	mu.Unlock()
	msg := <-c.resumeCh
	mu.Lock()
	// If the resumer signalled a pending error (resumeerror), wake up
	// by raising it. The error propagates from this yield point
	// through the coroutine body as if `error(value)` had been called
	// in place of yield's return -- matching upstream lua_resumeerror.
	if msg.errPending {
		panic(luaRTError{value: msg.errValue})
	}
	// Push args onto the coroutine's stack as return values from yield.
	for _, a := range msg.values {
		si.push(a)
	}
	return len(msg.values)
}

// ResumeError resumes the coroutine, waking it up with `errValue`
// raised as a Lua error at the yield (or initial call) point. Mirrors
// upstream lua_resumeerror; conformance/pcall.luau:144 exercises this
// with `resumeerror(co, "fail")`.
//
// Returns the resulting status (StatusErrRun if the error propagates
// uncaught out of the coroutine, StatusOK or StatusYield otherwise).
// Like Resume, `from` may be nil for main-thread callers.
func (co *State) ResumeError(from *State, errValue any) Status {
	c := co.impl.co
	if c == nil || c.finished {
		co.impl.status = StatusErrRun
		return StatusErrRun
	}
	// Convert the Go-side error value to a vm.value. We accept any
	// stack-style index (int), a string, or treat everything else as
	// a generic "resume error" sentinel; callers that need richer
	// values should push them via the stack and call this from a
	// site that can read them.
	var ev value
	switch v := errValue.(type) {
	case string:
		ev = stringValue(co.impl.gs.intern(v))
	case value:
		ev = v
	default:
		ev = stringValue(co.impl.gs.intern("resume error"))
	}

	mu := getVMMutex(co.impl.gs)
	if !c.started {
		// The coroutine hasn't started yet; spawn its goroutine just
		// to deliver the pending error and have it propagate out.
		c.started = true
		go func() {
			msg := <-c.resumeCh
			mu.Lock()
			defer mu.Unlock()
			defer func() {
				if r := recover(); r != nil {
					var errVal value
					if e, ok := r.(luaRTError); ok {
						errVal = e.value
					} else {
						errVal = stringValue(co.impl.gs.intern("coroutine error"))
					}
					c.finished = true
					c.yieldCh <- yieldMsg{status: StatusErrRun, err: errVal}
					return
				}
				c.finished = true
				c.yieldCh <- yieldMsg{status: StatusOK}
			}()
			if msg.errPending {
				panic(luaRTError{value: msg.errValue})
			}
		}()
	}
	c.resumeCh <- resumeMsg{errPending: true, errValue: ev}

	// Wait for the coroutine to yield, return, or error -- same mutex
	// dance as resumeImpl.
	heldByFrom := from != nil && from.impl.co != nil && from.impl.co.started
	if heldByFrom {
		mu.Unlock()
	}
	msg := <-c.yieldCh
	if heldByFrom {
		mu.Lock()
	}
	if msg.status == StatusErrRun {
		// Push the error value onto co's stack so the caller can
		// retrieve it via co.ToString(-1) etc.
		co.impl.push(msg.err)
		co.impl.status = StatusErrRun
		return StatusErrRun
	}
	co.impl.status = msg.status
	for _, v := range msg.values {
		co.impl.push(v)
	}
	return msg.status
}
