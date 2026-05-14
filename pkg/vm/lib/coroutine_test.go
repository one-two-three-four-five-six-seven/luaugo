// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib_test

import (
	"strings"
	"testing"

	"github.com/luaugo/luaugo/pkg/compiler"
	"github.com/luaugo/luaugo/pkg/vm"
	"github.com/luaugo/luaugo/pkg/vm/lib"
)

// newCoroutineState returns a fresh State with the base and coroutine
// libraries opened. Base is required for `type`, `pcall`, `error`, etc.
// used by the test chunks.
func newCoroutineState(t *testing.T) *vm.State {
	t.Helper()
	s := vm.NewState()
	t.Cleanup(s.Close)
	lib.OpenBase(s)
	lib.OpenCoroutine(s)
	return s
}

// runChunk compiles src as a chunk on s and invokes it via PCall(0,
// MultRet). It returns the call status. On non-OK status the error
// value is on top of s's stack.
func runChunk(t *testing.T, s *vm.State, src string) vm.Status {
	t.Helper()
	blob, err := compiler.CompileBinary("=coro_test", []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	if len(blob) == 0 || blob[0] == 0 {
		t.Fatalf("compile produced error blob: %q", blob)
	}
	if err := s.Load("=coro_test", blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	return s.PCall(0, vm.MultRet, 0)
}

// mustRunChunk wraps runChunk and fails fast on a non-OK status.
func mustRunChunk(t *testing.T, s *vm.State, src string) {
	t.Helper()
	if st := runChunk(t, s, src); st != vm.StatusOK {
		msg, _ := s.ToString(-1)
		t.Fatalf("chunk failed: status=%v err=%q", st, msg)
	}
}

// TestCoroutineYieldResume drives a coroutine through `yield 1`,
// `yield 2`, then `return 3`, then verifies that a fourth resume on
// the now-dead coroutine fails with (false, msg).
func TestCoroutineYieldResume(t *testing.T) {
	s := newCoroutineState(t)
	const src = `
		local co = coroutine.create(function()
			coroutine.yield(1)
			coroutine.yield(2)
			return 3
		end)
		local ok1, v1 = coroutine.resume(co)
		local ok2, v2 = coroutine.resume(co)
		local ok3, v3 = coroutine.resume(co)
		local ok4, v4 = coroutine.resume(co)
		return ok1, v1, ok2, v2, ok3, v3, ok4, v4
	`
	mustRunChunk(t, s, src)
	if s.Top() < 8 {
		t.Fatalf("expected 8 results on stack, got %d", s.Top())
	}
	expectBool := []bool{true, true, true, false}
	expectNum := []float64{1, 2, 3, 0}
	for i := 0; i < 4; i++ {
		boolIdx := 1 + 2*i
		numIdx := 2 + 2*i
		if got := s.ToBoolean(boolIdx); got != expectBool[i] {
			t.Errorf("ok%d: got %v want %v", i+1, got, expectBool[i])
		}
		if i < 3 {
			if got, _ := s.ToNumber(numIdx); got != expectNum[i] {
				t.Errorf("v%d: got %v want %v", i+1, got, expectNum[i])
			}
		} else {
			if !s.IsString(numIdx) {
				t.Errorf("v4: expected string error message, got type %v", s.Type(numIdx))
			}
		}
	}
}

// TestCoroutineStatus walks a coroutine through its lifecycle and
// verifies the user-observable status string at each transition, plus
// the value of coroutine.running() / coroutine.isyieldable() inside
// and outside a coroutine body.
func TestCoroutineStatus(t *testing.T) {
	s := newCoroutineState(t)
	const src = `
		local seen = {}
		local co = coroutine.create(function()
			seen.self_status = coroutine.status(coroutine.running())
			seen.is_thread   = type(coroutine.running()) == "thread"
			seen.yieldable   = coroutine.isyieldable()
			coroutine.yield()
		end)
		local before = coroutine.status(co)
		coroutine.resume(co)
		local mid   = coroutine.status(co)
		coroutine.resume(co)
		local after = coroutine.status(co)
		local main_running   = coroutine.running()
		local main_yieldable = coroutine.isyieldable()
		return before, seen.self_status, seen.is_thread, seen.yieldable,
		       mid, after, main_running, main_yieldable
	`
	mustRunChunk(t, s, src)
	if s.Top() < 8 {
		t.Fatalf("expected 8 results, got %d", s.Top())
	}

	check := func(idx int, label, want string) {
		if got, _ := s.ToString(idx); got != want {
			t.Errorf("%s: got %q want %q", label, got, want)
		}
	}
	check(1, "before", "suspended")
	check(2, "self_status (inside)", "running")
	if !s.ToBoolean(3) {
		t.Errorf("is_thread inside: got false want true")
	}
	if !s.ToBoolean(4) {
		t.Errorf("isyieldable inside: got false want true")
	}
	check(5, "mid (yielded)", "suspended")
	check(6, "after (returned)", "dead")
	if !s.IsNil(7) {
		t.Errorf("main running(): want nil, got type %v", s.Type(7))
	}
	if s.ToBoolean(8) {
		t.Errorf("main isyieldable: want false, got true")
	}
}

// TestCoroutineWrap exercises coroutine.wrap by calling the wrapper
// repeatedly to drain its yields and final return.
func TestCoroutineWrap(t *testing.T) {
	s := newCoroutineState(t)
	const src = `
		local gen = coroutine.wrap(function()
			for i = 1, 3 do
				coroutine.yield(i * 10)
			end
			return 99
		end)
		local a = gen()
		local b = gen()
		local c = gen()
		local d = gen()
		return a, b, c, d
	`
	mustRunChunk(t, s, src)
	if s.Top() < 4 {
		t.Fatalf("expected 4 results, got %d", s.Top())
	}
	want := []float64{10, 20, 30, 99}
	for i, w := range want {
		if got, _ := s.ToNumber(i + 1); got != w {
			t.Errorf("v%d: got %v want %v", i+1, got, w)
		}
	}
}

// TestCoroutineErrorPropagation verifies that errors in coroutines are
// reported via coroutine.resume as (false, err) AND that wrap re-raises
// the error (caught here at the Go layer via PCall on the chunk that
// invokes the wrapper).
func TestCoroutineErrorPropagation(t *testing.T) {
	// Sub-test 1: resume returns (false, err) on coroutine error.
	t.Run("resume_returns_false_err", func(t *testing.T) {
		s := newCoroutineState(t)
		const src = `
			local co = coroutine.create(function()
				error("boom-resume")
			end)
			local ok, err = coroutine.resume(co)
			local dead = coroutine.status(co)
			return ok, err, dead
		`
		mustRunChunk(t, s, src)
		if s.Top() < 3 {
			t.Fatalf("expected 3 results, got %d", s.Top())
		}
		if s.ToBoolean(1) {
			t.Errorf("resume ok: got true, want false")
		}
		msg, _ := s.ToString(2)
		if !strings.Contains(msg, "boom-resume") {
			t.Errorf("resume err: %q does not contain %q", msg, "boom-resume")
		}
		st, _ := s.ToString(3)
		if st != "dead" {
			t.Errorf("status after error: got %q want dead", st)
		}
	})

	// Sub-test 2: wrap re-raises the error as a Lua error caught by
	// the outer PCall on the chunk.
	t.Run("wrap_reraises", func(t *testing.T) {
		s := newCoroutineState(t)
		const src = `
			local w = coroutine.wrap(function()
				error("boom-wrap")
			end)
			return w()
		`
		st := runChunk(t, s, src)
		if st == vm.StatusOK {
			t.Fatalf("expected non-OK status from wrap-raising chunk, got OK")
		}
		msg, _ := s.ToString(-1)
		if !strings.Contains(msg, "boom-wrap") {
			t.Errorf("wrap err: %q does not contain %q", msg, "boom-wrap")
		}
	})
}
