// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

// coroutine.go implements Luau's `coroutine` library. Mirrors upstream
// VM/src/lcorolib.cpp. The underlying coroutine primitives (NewThread,
// Resume, Yield, Status) live in pkg/vm; this file wraps them as Lua
// callables on the `coroutine` global table.
//
// stubs.go declares `openCoroutine` as a thin delegate to
// `openCoroutineImpl` (this file's entry point); the parallel-dev
// stubs in zz_test_stubs.go must drop their copy of openCoroutineImpl
// once this file is integrated.
package lib

import "github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"

// openCoroutineImpl registers the `coroutine` global table and its
// functions. Mirrors upstream luaopen_coroutine.
func openCoroutineImpl(s *vm.State) {
	s.NewTable()

	s.LRegisterList([]vm.LFnEntry{
		{Name: "create", Fn: coCreate},
		{Name: "running", Fn: coRunning},
		{Name: "status", Fn: coStatus},
		{Name: "wrap", Fn: coWrap},
		{Name: "yield", Fn: coYield},
		{Name: "isyieldable", Fn: coIsYieldable},
		{Name: "close", Fn: coClose},
		{Name: "resume", Fn: coResume},
	})

	// Install as the `coroutine` global.
	s.SetGlobal("coroutine")
}

// coCreate implements coroutine.create(f). It allocates a fresh
// coroutine, places f as its body, and returns it. Upstream cocreate.
func coCreate(s *vm.State) int {
	s.LCheckType(1, vm.TFunction)
	co := s.NewThread()
	// NewThread leaves the thread on s's stack. Push a copy of the
	// function and move it onto the coroutine's stack so Resume can
	// find it as the function to call.
	s.PushValue(1)
	s.XMove(co, 1)
	return 1
}

// coRunning implements coroutine.running(). Returns the current thread
// for coroutines or nil on the main thread, matching upstream
// corunning's single-value return.
func coRunning(s *vm.State) int {
	if s.IsMainThread() {
		s.PushNil()
		return 1
	}
	s.PushThread()
	return 1
}

// coStatus implements coroutine.status(co).
func coStatus(s *vm.State) int {
	if s.Type(1) != vm.TThread {
		s.LTypeError(1, "thread")
	}
	co := s.ToThread(1)
	s.PushString(s.CoStatus(co))
	return 1
}

// coYield implements coroutine.yield(...). Suspends the current
// coroutine, passing every value on its top to the resumer; on resume
// the new values appear on the stack.
func coYield(s *vm.State) int {
	nargs := s.Top()
	return s.Yield(nargs)
}

// coIsYieldable implements coroutine.isyieldable().
func coIsYieldable(s *vm.State) int {
	s.PushBoolean(s.IsYieldable())
	return 1
}

// auxResume drives a Resume on co with nargs args from s's stack. The
// args occupy the top of s's stack and are consumed by Resume. The
// second return reports success.
//
// On success: results are on s's stack and the int is the result count.
// On error:   a single error value is on s's stack and the int is 1.
func auxResume(s *vm.State, co *vm.State, nargs int) (int, bool) {
	st := s.CoStatus(co)
	if st != "suspended" {
		s.PushFString("cannot resume %s coroutine", st)
		return 1, false
	}

	topBefore := s.Top()
	status := co.Resume(s, nargs)

	switch status {
	case vm.StatusOK, vm.StatusYield:
		nres := s.Top() - (topBefore - nargs)
		if nres < 0 {
			nres = 0
		}
		return nres, true
	default:
		nres := s.Top() - (topBefore - nargs)
		if nres < 1 {
			s.PushString("coroutine error")
			nres = 1
		}
		// Reduce to a single error value (the topmost is the error).
		for nres > 1 {
			s.Remove(-2)
			nres--
		}
		return 1, false
	}
}

// coResume implements coroutine.resume(co, ...). Returns
// (true, ...results) on success or (false, err) on error. Mirrors
// upstream coresumey.
func coResume(s *vm.State) int {
	if s.Type(1) != vm.TThread {
		s.LTypeError(1, "thread")
	}
	co := s.ToThread(1)
	nargs := s.Top() - 1
	// Strip the thread argument so only args remain on the stack.
	s.Remove(1)

	nres, ok := auxResume(s, co, nargs)

	// Prepend the boolean status: push it, then Insert it below the
	// nres results (negative idx is computed from absolute top, so
	// -(nres+1) lands at frame-relative index 1).
	s.PushBoolean(ok)
	s.Insert(-(nres + 1))
	return nres + 1
}

// coWrap implements coroutine.wrap(f). Returns a function that
// resumes the coroutine on each call and re-raises any error.
// Mirrors upstream cowrap + auxwrapy.
func coWrap(s *vm.State) int {
	s.LCheckType(1, vm.TFunction)
	co := s.NewThread()
	s.PushValue(1)
	s.XMove(co, 1)
	// Pop the thread that NewThread pushed; the Go-level closure
	// below captures `co` directly. The Go runtime keeps `co` alive
	// for as long as the returned wrapper function exists.
	s.Pop(1)

	wrapper := func(s *vm.State) int {
		nargs := s.Top()
		nres, ok := auxResume(s, co, nargs)
		if !ok {
			// Error value is on top of s; raise it as a Lua error
			// with the caller's source location prefix prepended
			// (matching upstream luaL_error semantics in cowrap).
			// If the error is a string, prepend "chunkname:line: ";
			// otherwise re-raise the value verbatim.
			if errStr, isStr := s.ToString(-1); isStr {
				s.Pop(1)
				s.LError("%s", errStr)
			} else {
				s.Error()
			}
		}
		return nres
	}
	s.PushGoFunction(wrapper, 0)
	return 1
}

// coClose implements coroutine.close(co). Returns (true) for a
// successfully closed coroutine or (false, err) for one that died with
// an error. Cannot close a running or normal coroutine.
//
// Tier-4 minimal: we cannot forcibly terminate a parked goroutine
// without a richer reset primitive in pkg/vm. For a suspended (yielded)
// coroutine we report success; the underlying goroutine remains
// blocked on its resume channel and is reclaimed by the Go runtime
// once nothing references the *State.
func coClose(s *vm.State) int {
	if s.Type(1) != vm.TThread {
		s.LTypeError(1, "thread")
	}
	co := s.ToThread(1)
	st := s.CoStatus(co)
	if st != "suspended" && st != "dead" {
		s.LError("cannot close %s coroutine", st)
	}
	switch co.Status() {
	case vm.StatusOK, vm.StatusYield:
		s.PushBoolean(true)
		return 1
	default:
		s.PushBoolean(false)
		if co.Top() > 0 {
			co.XMove(s, 1)
		} else {
			s.PushString("coroutine error")
		}
		return 2
	}
}
