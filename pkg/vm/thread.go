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
	// resumeCh: main side sends a slice of args to start/continue the
	// coroutine; coroutine side receives.
	resumeCh chan []value
	// yieldCh: coroutine side sends a yieldMsg; main side receives.
	yieldCh chan yieldMsg
	// started indicates the goroutine has been spawned.
	started bool
	// finished indicates the goroutine has terminated.
	finished bool
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
		resumeCh: make(chan []value, 1),
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

	if !c.started {
		c.started = true
		// Spawn the coroutine goroutine. It expects the function to
		// already be on the coroutine's stack (set up by Resume's
		// caller after NewThread).
		go func() {
			// The goroutine acquires the VM mutex before running.
			args := <-c.resumeCh
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
				// args were already pushed above the function. Push them now.
				for _, a := range args {
					si.push(a)
				}
				si.callFromGo(len(args), MultRet)
			}()
		}()
		// Now hand off control: send args, then wait for yield/finish.
		c.resumeCh <- args
	} else {
		// Re-entry: hand args to the awaiting goroutine.
		c.resumeCh <- args
	}

	// Wait for the coroutine to yield, return, or error.
	msg := <-c.yieldCh
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
		si.runtimeError("attempt to yield from outside a coroutine")
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
	args := <-c.resumeCh
	mu.Lock()
	// Push args onto the coroutine's stack as return values from yield.
	for _, a := range args {
		si.push(a)
	}
	return len(args)
}
