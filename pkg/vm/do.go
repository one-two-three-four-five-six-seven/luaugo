// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"fmt"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/vmlog"
)

// do.go: call frame management and protected-call entry points.
// Mirrors upstream ldo.cpp.
//
// A call frame (callInfo) describes one suspended Lua function call.
// The interpreter pushes a frame on entry, runs the function's
// bytecode, then pops the frame on return. C/Go functions are invoked
// inline (no interpreter loop) but still get a frame for traceback.

// MultRet is the special "all return values" sentinel used by lua_call
// and friends.
const MultRet = -1

// maxCallDepth caps Lua's call stack depth to prevent unbounded
// recursion from running until Go's own stack overflows (or, on the
// heap-based frame stack used here, until we OOM). Mirrors upstream
// Luau's LUAI_MAXCALLS (luaconf.h: 20000). When exceeded we raise a
// normal Lua runtime error so pcall can recover it -- exactly what
// fixtures like native.luau's fuzzfail3 and conformance/pm.luau's
// `range(0, 255)` expect.
//
// Note: upstream distinguishes LUAI_MAXCALLS (overall, 20000) from
// LUAI_MAXCCALLS (native stack guard, 200). We use a single limit
// because our frame stack lives on the Go heap so we don't need a
// separate guard for native-stack exhaustion.
const maxCallDepth = 20000

// callInfo describes one Lua call frame. Mirrors upstream CallInfo
// (lstate.h).
type callInfo struct {
	// closure being invoked.
	cl *closure

	// base is the index in the thread stack of the function's R(0).
	// The stack layout from base is:
	//   [R(0)..R(MaxStackSize-1)] -- registers/locals
	base int

	// top is the high-water mark for this frame (one past the last
	// register currently in use). The interpreter writes its current
	// stack top here so OpReturn knows where to stop copying results.
	top int

	// savedpc is the program counter (instruction index in cl.proto.Code)
	// at which to resume. Captured before nested calls.
	savedpc int

	// numresults is the number of results the caller expects, or
	// MultRet for "as many as the callee returns". Set by the caller.
	numresults int

	// flags is per-frame bookkeeping.
	flags ciFlags

	// Vararg bookkeeping (only meaningful for Lua frames whose proto
	// has IsVararg != 0).
	numVararg  int
	varargBase int
}

type ciFlags uint8

const (
	// ciLua marks this frame as running a Lua function (vs Go).
	ciLua ciFlags = 1 << 0
	// ciFresh: a fresh invocation of `Execute`, so OpReturn must yield
	// control back to the caller instead of resuming the parent frame.
	ciFresh ciFlags = 1 << 1
)

// pushFrame pushes a new call frame onto the state's frame stack.
//
// Raises a runtime error (recoverable by pcall) when the call depth
// exceeds maxCallDepth, mirroring upstream Luau's "stack overflow"
// behaviour. Without this guard, deeply recursive scripts loop until
// the host OOMs (see native.luau::fuzzfail3, which intentionally
// recurses to test pcall's overflow recovery).
func (s *stateImpl) pushFrame(cl *closure, base int, numresults int, flags ciFlags) *callInfo {
	if len(s.frames) >= maxCallDepth {
		s.runtimeError("stack overflow")
	}
	ci := &callInfo{
		cl:         cl,
		base:       base,
		top:        base,
		numresults: numresults,
		flags:      flags,
	}
	s.frames = append(s.frames, ci)
	return ci
}

func (s *stateImpl) popFrame() {
	n := len(s.frames)
	if n == 0 {
		return
	}
	s.frames[n-1] = nil
	s.frames = s.frames[:n-1]
}

// currentFrame returns the innermost call frame, or nil.
func (s *stateImpl) currentFrame() *callInfo {
	n := len(s.frames)
	if n == 0 {
		return nil
	}
	return s.frames[n-1]
}

// callValue invokes the function at stack[funcIdx] with `nargs`
// arguments above it. After the call, `nresults` values occupy the
// slots starting at funcIdx (the function value is replaced by the
// first return). MultRet leaves all returns on the stack.
//
// callValue handles both Lua and Go (C) closures and dispatches
// __call when funcIdx holds a non-callable value with a metatable.
func (s *stateImpl) callValue(funcIdx, nargs, nresults int) {
	v := s.stack[funcIdx]
	for v.tag != TFunction {
		// Resolve __call recursively.
		mm := s.gs.getTagMethodForValue(v, TMCall)
		if mm.tag == TNil {
			s.runtimeError("attempt to call a " + v.tag.String() + " value")
		}
		// Insert mm before funcIdx, push old value as first arg.
		// The shift moves the existing args block
		//   [funcIdx+1 .. funcIdx+1+nargs)
		// up by one slot. We must size the operation against `nargs`
		// (the authoritative arg count for fixed-B calls) rather
		// than against s.top, because OpCall does not always
		// position s.top at funcIdx+1+nargs for fixed-B calls
		// (events.luau:292 -- nested __call chains with fixed args).
		argsEnd := funcIdx + 1 + nargs
		need := argsEnd + 1
		if need > len(s.stack) {
			s.reserve(need - s.top)
		}
		copy(s.stack[funcIdx+2:argsEnd+1], s.stack[funcIdx+1:argsEnd])
		s.stack[funcIdx+1] = v
		s.stack[funcIdx] = mm
		nargs++
		if argsEnd+1 > s.top {
			s.top = argsEnd + 1
		}
		v = mm
	}
	cl := v.gc.(*closure)
	if cl.isGo {
		s.callGo(cl, funcIdx, nargs, nresults)
		return
	}
	s.callLua(cl, funcIdx, nargs, nresults)
}

// callGo invokes a Go closure. The function value is at funcIdx with
// nargs above it; on return the results occupy funcIdx..funcIdx+r-1.
func (s *stateImpl) callGo(cl *closure, funcIdx, nargs, nresults int) {
	base := funcIdx + 1
	// Build a fresh frame for traceback.
	ci := s.pushFrame(cl, base, nresults, 0)
	ci.top = base + nargs
	// The Go function sees a stack whose index 1 is its first arg.
	// We achieve this by shrinking the visible stack to base..top so
	// absIndex(1) == base.
	savedBase := s.callBase
	savedFnIdx := s.callFunc
	s.callBase = base
	s.callFunc = funcIdx
	// Hide everything below base so that ToBoolean(1) etc. resolve
	// to base+0. We do this by temporarily shifting top to be
	// relative to base.
	prevTop := s.top
	s.top = base + nargs
	s.stack = s.stack[:s.top]

	defer func() {
		s.callBase = savedBase
		s.callFunc = savedFnIdx
	}()

	// Construct a *State view (the same one for the current thread).
	// Tier-2 design wraps each stateImpl in exactly one *State; we
	// pass that to the Go function by reusing the public State on the
	// main thread or by minting a fresh wrapper here.
	wrapper := &State{impl: s}
	got := cl.goFn(wrapper)

	// got is the count of return values the Go function pushed.
	if got < 0 {
		got = 0
	}
	// Results are at s.top-got .. s.top-1; move them to funcIdx..
	resultBase := s.top - got
	if resultBase < base {
		got = s.top - base
		resultBase = base
	}

	// Determine how many to keep.
	want := nresults
	keep := got
	if want != MultRet && got > want {
		keep = want
	}
	for i := 0; i < keep; i++ {
		s.stack[funcIdx+i] = s.stack[resultBase+i]
	}
	if want != MultRet {
		// Pad with nil if needed.
		for i := keep; i < want; i++ {
			if funcIdx+i >= len(s.stack) {
				s.reserve(funcIdx + i + 1 - s.top)
			}
			s.stack[funcIdx+i] = nilValue()
		}
		s.top = funcIdx + want
	} else {
		s.top = funcIdx + keep
	}
	s.stack = s.stack[:s.top]
	_ = prevTop
	s.popFrame()
}

// callLua invokes a Lua closure by running the interpreter loop.
func (s *stateImpl) callLua(cl *closure, funcIdx, nargs, nresults int) {
	p := cl.proto
	numparams := int(p.NumParams)

	// Pad missing fixed params with nil so callees always see a full
	// fixed-arg block. Arguments currently live at funcIdx+1..funcIdx+nargs.
	if nargs < numparams {
		for i := nargs; i < numparams; i++ {
			idx := funcIdx + 1 + i
			if idx >= len(s.stack) {
				s.reserve(idx + 1 - s.top)
			}
			s.stack[idx] = nilValue()
		}
		s.top = funcIdx + 1 + numparams
		s.stack = s.stack[:s.top]
		nargs = numparams
	}

	if vmlog.Enabled("call") {
		vmlog.V("call", "callLua funcIdx=%d nargs=%d numparams=%d isVararg=%v maxStack=%d",
			funcIdx, nargs, numparams, p.IsVararg != 0, p.MaxStackSize)
	}

	// For vararg functions we follow the upstream layout: the function
	// closure stays at funcIdx, then the EXTRA args (varargs) occupy
	// the slots immediately above it, then the FIXED args, then
	// scratch. The frame's `base` is set to the slot AFTER the extras
	// so that R(0)..R(numparams-1) are the fixed args. GETVARARGS then
	// reads from [base-extra..base-1].
	//
	// Concretely we shift the nargs values up by `extra` so that the
	// last `extra` of them sit immediately above funcIdx (the varargs)
	// while the first `numparams` end up at the new base.
	base := funcIdx + 1
	extra := 0
	if p.IsVararg != 0 && nargs > numparams {
		extra = nargs - numparams
		// Make room: ensure we have extra more slots above the current
		// arg block. Current args occupy [funcIdx+1 .. funcIdx+nargs];
		// after the shift, the fixed args will be at
		// [funcIdx+1+extra .. funcIdx+1+extra+numparams-1].
		needTopShift := funcIdx + 1 + extra + numparams
		if needTopShift > s.top {
			s.reserve(needTopShift - s.top)
			s.top = needTopShift
			s.stack = s.stack[:s.top]
		}
		// Move the FIXED args (the first numparams of the nargs block)
		// up by `extra`. The trailing `extra` values stay at the
		// original positions and become the varargs.
		// Original: [funcIdx+1 .. funcIdx+1+numparams-1] = fixed
		//           [funcIdx+1+numparams .. funcIdx+1+nargs-1] = extras
		// Target:   [funcIdx+1 .. funcIdx+1+extra-1]          = extras (kept)
		//           [funcIdx+1+extra .. funcIdx+1+extra+numparams-1] = fixed
		// So we need to swap the two blocks. Simplest:
		//   1. snapshot the fixed args
		//   2. move the extras down into [funcIdx+1 .. funcIdx+extra]
		//   3. copy the snapshotted fixed args into the new fixed
		//      window.
		// But the extras already are at [funcIdx+1+numparams .. ];
		// we want them at [funcIdx+1 ..]. And the fixed already are at
		// [funcIdx+1 ..]; we want them at [funcIdx+1+extra ..]. That's
		// a rotation. We can implement with two scratch buffers.
		fixed := make([]value, numparams)
		extras := make([]value, extra)
		for i := 0; i < numparams; i++ {
			fixed[i] = s.stack[funcIdx+1+i]
		}
		for i := 0; i < extra; i++ {
			extras[i] = s.stack[funcIdx+1+numparams+i]
		}
		for i := 0; i < extra; i++ {
			s.stack[funcIdx+1+i] = extras[i]
		}
		for i := 0; i < numparams; i++ {
			s.stack[funcIdx+1+extra+i] = fixed[i]
		}
		base = funcIdx + 1 + extra
		// Bump s.top to past the fixed-arg slots: the subsequent
		// nil-fill that grows up to base+MaxStackSize would otherwise
		// overwrite the just-shifted fixed args (which still need to
		// be visible to the callee as its R(0)..R(numparams-1)).
		if afterFixed := base + numparams; afterFixed > s.top {
			s.top = afterFixed
			s.stack = s.stack[:s.top]
		}
	}

	// Ensure L.top covers the fixed-arg window before we nil-pad the
	// scratch area. Opcodes that placed the call's arguments (MOVE,
	// LOADK, LOADN, ...) write directly to register slots without
	// raising L.top, so the caller's recorded top can sit BELOW the
	// first argument. The nil-padding loop below clears every slot
	// from s.top up to base+MaxStackSize, so if we leave s.top there
	// the freshly-placed arguments get clobbered with nil before the
	// callee can read them. (This was the root cause of the
	// "attempt to compare nil with number" failure on constructs.luau:
	// f(12) ran with R0 = nil.)
	//
	// Fixed args occupy [base, base+numparams) in both the vararg and
	// non-vararg layouts (after the shift above). For non-vararg
	// callees with nargs > numparams the extra args sit at
	// [base+numparams, base+nargs) and are discarded by the callee, so
	// nilling them is fine and we don't need to preserve them.
	argTop := base + numparams
	if p.IsVararg == 0 && nargs > numparams {
		argTop = base + nargs
	}
	if argTop > s.top {
		if argTop > len(s.stack) {
			s.reserve(argTop - s.top)
		}
		s.top = argTop
		s.stack = s.stack[:s.top]
	}

	// Reserve stack room for the function's max stack size at the new
	// base.
	needTop := base + int(p.MaxStackSize)
	if needTop > s.top {
		s.reserve(needTop - s.top)
		for i := s.top; i < needTop; i++ {
			s.stack[i] = nilValue()
		}
		s.top = needTop
	}

	ci := s.pushFrame(cl, base, nresults, ciLua|ciFresh)
	ci.top = base + int(p.MaxStackSize)
	if p.IsVararg != 0 {
		ci.numVararg = extra
		ci.varargBase = base - extra
	}

	// Run interpreter.
	executeProto(s, ci)
}

// callBase / callFunc are set by callGo so the Go function's view of
// the stack uses base-relative indices via absIndex.
// These fields live on stateImpl (see state.go additions in this file).

// raiseErrorImpl raises a Lua error with the value on top of the stack.
func (s *stateImpl) raiseErrorImpl() {
	if s.top == 0 {
		s.runtimeError("error")
	}
	v := s.stack[s.top-1]
	s.runtimeErrorValue(v)
}

// callFromGo wraps callValue with a Go-side defer to convert luaRTError
// into Go-level panics that the outer pcall handles. Used by Call().
func (s *stateImpl) callFromGo(nargs, nresults int) {
	funcIdx := s.top - nargs - 1
	if funcIdx < 0 {
		s.runtimeError("call: not enough values on the stack")
	}
	s.callValue(funcIdx, nargs, nresults)
}

// pcallFromGo runs a protected call. Returns Status and leaves either
// the results or the error value on the stack.
func (s *stateImpl) pcallFromGo(nargs, nresults, errfunc int) (st Status) {
	funcIdx := s.top - nargs - 1
	if funcIdx < 0 {
		s.runtimeError("pcall: not enough values on the stack")
	}
	// Translate errfunc (Lua stack index) into an absolute index.
	var ef int = -1
	if errfunc != 0 {
		ef = s.absIndex(errfunc)
	}

	// Save call stack depth so we can restore on error.
	savedFrames := len(s.frames)
	savedTop := s.top

	defer func() {
		if r := recover(); r != nil {
			// Restore call stack.
			for len(s.frames) > savedFrames {
				s.popFrame()
			}
			// Close any upvalues that escaped during the abort.
			s.closeUpvalsTo(savedTop)
			// Restore top to the function slot.
			if savedTop > cap(s.stack) {
				s.reserve(savedTop - s.top)
			}
			s.stack = s.stack[:funcIdx+1]
			s.top = funcIdx + 1
			// Build an error value at stack[funcIdx].
			var errVal value
			switch e := r.(type) {
			case luaRTError:
				errVal = e.value
			case Error:
				lv := e.LuaValue()
				errVal = goAnyToValue(s.gs, lv)
			case error:
				errVal = stringValue(s.gs.intern(e.Error()))
			case string:
				errVal = stringValue(s.gs.intern(e))
			default:
				errVal = stringValue(s.gs.intern(fmt.Sprintf("%v", r)))
			}
			// If errfunc is set, call it with the error value.
			if ef >= 0 && ef < s.top && s.stack[ef].tag == TFunction {
				// Run errfunc(errVal) but protect from further error.
				func() {
					defer func() {
						if rr := recover(); rr != nil {
							st = StatusErrErr
						}
					}()
					efBase := s.top
					s.push(s.stack[ef])
					s.push(errVal)
					s.callValue(efBase, 1, 1)
					errVal = s.stack[efBase]
					s.stack = s.stack[:efBase]
					s.top = efBase
				}()
				if st == StatusErrErr {
					// keep errVal as-is and report ErrErr
					s.stack[funcIdx] = errVal
					return
				}
			}
			s.stack[funcIdx] = errVal
			st = StatusErrRun
		}
	}()

	// Pad results.
	s.callValue(funcIdx, nargs, nresults)
	return StatusOK
}

// closeUpvalsTo closes any open upvalues whose stack index is >= level.
func (s *stateImpl) closeUpvalsTo(level int) {
	prev := (**upVal)(&s.openUpvals)
	for *prev != nil {
		u := *prev
		if u.stackIndex < level {
			break
		}
		*prev = u.openNext
		u.close()
	}
}

// goAnyToValue converts a Go value (returned by Error.LuaValue) to a
// Lua value.
func goAnyToValue(g *globalState, v any) value {
	switch x := v.(type) {
	case nil:
		return nilValue()
	case bool:
		return booleanValue(x)
	case string:
		return stringValue(g.intern(x))
	case int:
		return numberValue(float64(x))
	case int64:
		return numberValue(float64(x))
	case float64:
		return numberValue(x)
	case float32:
		return numberValue(float64(x))
	}
	return stringValue(g.intern(fmt.Sprintf("%v", v)))
}
