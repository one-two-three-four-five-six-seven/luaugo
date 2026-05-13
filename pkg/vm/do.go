// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"fmt"
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
func (s *stateImpl) pushFrame(cl *closure, base int, numresults int, flags ciFlags) *callInfo {
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
		s.reserve(1)
		// Shift args right by 1 to make room for the receiver.
		copy(s.stack[funcIdx+1+1:s.top+1], s.stack[funcIdx+1:s.top])
		s.stack[funcIdx+1] = v
		s.stack[funcIdx] = mm
		s.top++
		s.stack = s.stack[:s.top]
		nargs++
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
	base := funcIdx + 1

	// Handle varargs: fixed params are at base..base+numparams-1; extra
	// args remain accessible via GETVARARGS but conceptually live
	// "before" base in upstream's layout. We model this by storing the
	// "extra args" base in the callInfo and copying fixed args after.
	numparams := int(p.NumParams)
	if nargs < numparams {
		// pad missing params with nil
		for i := nargs; i < numparams; i++ {
			if base+i >= len(s.stack) {
				s.reserve(base + i + 1 - s.top)
			}
			s.stack[base+i] = nilValue()
		}
		s.top = base + numparams
		s.stack = s.stack[:s.top]
		nargs = numparams
	}

	// Reserve stack room for the function's max stack size.
	needTop := base + int(p.MaxStackSize)
	if needTop > s.top {
		s.reserve(needTop - s.top)
		// pad new slots with nil (already done by reserve via slicing).
		for i := s.top; i < needTop; i++ {
			s.stack[i] = nilValue()
		}
		s.top = needTop
	}

	ci := s.pushFrame(cl, base, nresults, ciLua|ciFresh)
	ci.top = base + int(p.MaxStackSize)
	// For varargs functions, store the extras before `base`. Upstream
	// uses a "base" pointer that moves so fixed args are at [0..numparams-1]
	// while varargs sit at a lower index. We instead store the count
	// of varargs and their starting offset on the frame.
	if p.IsVararg != 0 {
		ci.numVararg = nargs - numparams
		ci.varargBase = base - ci.numVararg
		// Move fixed args down into [base..base+numparams-1] and
		// varargs into [varargBase..base-1].
		// Currently fixed args are at [base..base+nargs-1]. We want
		// fixed args at [base..base+numparams-1] and varargs at
		// [varargBase..base-1].
		// Shift fixed args left by 0 (they're already there). Place
		// extra args at varargBase. We need to insert space before
		// base for the varargs.
		extra := nargs - numparams
		if extra > 0 {
			// Need room before base. Easier approach: move fixed args
			// up to make space, then move extras into [base..base+extra-1]
			// and then shift fixed args back. Actually we'll keep the
			// upstream invariant by storing varargs ABOVE the fixed
			// stack (at base+numparams..base+nargs-1) and remember that
			// when GETVARARGS executes we copy from there. Reset:
			ci.numVararg = extra
			ci.varargBase = base + numparams
			// Reset fixed param area: it's already correct.
		} else {
			ci.numVararg = 0
			ci.varargBase = base
		}
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
