// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

// fenv.go: low-level helpers that expose function environments
// (closure.env) and the thread's globals table to the stdlib. These
// back the implementations of setfenv / getfenv in pkg/vm/lib/base.go.
//
// We mirror upstream lbaselib.cpp::luaB_setfenv / luaB_getfenv. There
// the level form walks the call stack and the function form pokes the
// closure's `env` field directly. Both operations are inexpressible
// through the existing public API, so they live here.

// ClosureEnvAt returns the environment table of the function value at
// the given stack index. Returns nil when the slot is empty, holds a
// non-function value, or holds a closure with no environment.
//
// The returned value is the *table reference held inside the closure,
// not a copy, so callers must not mutate it directly; instead, push
// it onto the stack with PushClosureEnvAt or replace it wholesale
// with SetClosureEnvAt.
func (s *State) ClosureEnvAt(idx int) (envIdx int, ok bool) {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		return 0, false
	}
	v := si.stack[i]
	if v.tag != TFunction || v.gc == nil {
		return 0, false
	}
	cl, ok := v.gc.(*closure)
	if !ok || cl == nil || cl.env == nil {
		return 0, false
	}
	si.push(tableValue(cl.env))
	return si.top - 1, true
}

// SetClosureEnvAt sets the environment of the function at funcIdx to
// the table at envIdx. Returns false if funcIdx does not hold a
// function or envIdx does not hold a table. Neither slot is popped.
func (s *State) SetClosureEnvAt(funcIdx, envIdx int) bool {
	si := s.impl
	fi := si.absIndex(funcIdx)
	ei := si.absIndex(envIdx)
	if fi < 0 || fi >= si.top || ei < 0 || ei >= si.top {
		return false
	}
	fv := si.stack[fi]
	ev := si.stack[ei]
	if fv.tag != TFunction || fv.gc == nil {
		return false
	}
	if ev.tag != TTable || ev.gc == nil {
		return false
	}
	cl, ok := fv.gc.(*closure)
	if !ok || cl == nil {
		return false
	}
	cl.env = ev.gc.(*table)
	return true
}

// ClosureAtLevel returns true if there is a call frame at the given
// stack level (1 = caller of this Go function, 2 = caller of that,
// etc.) and pushes that frame's closure onto the stack at the
// returned index. Returns 0, false otherwise.
//
// Mirrors the level-form of upstream luaB_setfenv: a positive level
// walks the call stack; level 0 means "the thread itself" and is
// signalled to the caller by SetThreadGlobals / PushThreadGlobals
// rather than this function.
func (s *State) ClosureAtLevel(level int) (closureIdx int, ok bool) {
	si := s.impl
	if level < 1 {
		return 0, false
	}
	n := len(si.frames)
	// frames[n-1] is the innermost frame, which is the Go function
	// that called this helper. Upstream counts that as level 1.
	idx := n - level
	if idx < 0 || idx >= n {
		return 0, false
	}
	ci := si.frames[idx]
	if ci == nil || ci.cl == nil {
		return 0, false
	}
	si.push(closureValue(ci.cl))
	return si.top - 1, true
}

// PushThreadGlobals pushes the thread's globals table. Distinct from
// PushGlobalsTable only in name, kept for symmetry with the level-0
// form of setfenv/getfenv.
func (s *State) PushThreadGlobals() {
	s.impl.push(tableValue(s.impl.globals))
}

// SetThreadGlobals replaces the thread's globals table with the table
// at envIdx. Returns false if envIdx is not a table. The slot is not
// popped.
//
// This is the level-0 form of setfenv: `setfenv(0, t)` makes `t` the
// thread's globals table, which all subsequent `_G` reads see.
func (s *State) SetThreadGlobals(envIdx int) bool {
	si := s.impl
	ei := si.absIndex(envIdx)
	if ei < 0 || ei >= si.top {
		return false
	}
	ev := si.stack[ei]
	if ev.tag != TTable || ev.gc == nil {
		return false
	}
	si.globals = ev.gc.(*table)
	return true
}
