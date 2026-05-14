// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib

import (
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// runAllLibs compiles src and runs it on a fresh VM with every
// standard-library module installed. Used by tests that exercise
// non-base libraries (e.g. table.move).
func runAllLibs(t *testing.T, name, src string) *vm.State {
	t.Helper()
	blob, err := compiler.CompileBinary(name, []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile %s: %v", name, err)
	}
	s := vm.NewState()
	OpenAll(s)
	if err := s.Load(name, blob, 0); err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	s.Call(0, vm.MultRet)
	return s
}

// missing_globals_test.go documents the global/library behaviour gaps
// found during the conformance-suite bug-fix sweep and locks in the
// expected outputs so future refactors notice when they regress.
//
// Two clusters drove this file:
//
//   1. loadstring -- previously stubbed to (nil, "loadstring disabled").
//      Several upstream fixtures (calls.luau, literals.luau, locals.luau,
//      basic.luau via dostring, ...) cannot make progress without it.
//      Fix: pkg/vm/lib/base.go::baseLoadString now delegates to
//      pkg/compiler.CompileBinary and vm.State.Load.
//
//   2. table.move bounds -- the upstream tmove (ltablib.cpp:199)
//      validates `f > 0 || e < INT_MAX + f` and `d <= INT_MAX - n + 1`
//      *before* iterating. Without these guards table.move({},0,maxI,1)
//      runs for ~2 billion iterations, which the conformance corpus
//      relies on rejecting fast ("too many elements to move",
//      "destination wrap around"). Fix: pkg/vm/lib/table.go::tableMove.
//
// The tests below are the smallest reproductions that surface each
// fix. They live in the same package as the implementations so they
// can drive runBaseLuau (from base_test.go).

// TestLoadstringExists is a smoke test: the previous implementation
// always failed; this confirms basic compile-and-call works. Detailed
// coverage lives in loadstring_test.go.
func TestLoadstringExists(t *testing.T) {
	s := runBaseLuau(t, "loadstring_exists", `
		local f = loadstring("return 7")
		assert(type(f) == "function", "loadstring must return a function")
		return f()
	`)
	defer s.Close()
	n, _ := s.ToInteger(-1)
	if n != 7 {
		t.Fatalf("expected 7, got %d", n)
	}
}

// TestTableMoveTooManyElements verifies the upstream `"too many
// elements to move"` argcheck fires before any iteration happens.
// Without this guard the call would loop for ~INT_MAX iterations.
func TestTableMoveTooManyElements(t *testing.T) {
	s := runAllLibs(t, "tmove_toomany", `
		local maxI = 2147483647
		local ok, err = pcall(table.move, {}, 0, maxI, 1)
		assert(not ok)
		return err
	`)
	defer s.Close()
	msg, _ := s.ToString(-1)
	if !strings.Contains(msg, "too many") {
		t.Fatalf("expected 'too many' in error, got %q", msg)
	}
}

// TestTableMoveDestinationWrapAround verifies the upstream `"destination
// wrap around"` argcheck fires when the destination index plus the
// move range exceeds INT_MAX.
func TestTableMoveDestinationWrapAround(t *testing.T) {
	s := runAllLibs(t, "tmove_wrap", `
		local maxI = 2147483647
		local ok, err = pcall(table.move, {}, 1, maxI, 2)
		assert(not ok)
		return err
	`)
	defer s.Close()
	msg, _ := s.ToString(-1)
	if !strings.Contains(msg, "wrap around") {
		t.Fatalf("expected 'wrap around' in error, got %q", msg)
	}
}

// TestTableMoveBoundaryIntMin: move.luau::62 expects
// table.move({[maxI] = 100}, maxI, maxI, minI) to succeed; minI is the
// 32-bit signed minimum so a too-tight upper-bound check would reject
// it.
func TestTableMoveBoundaryIntMin(t *testing.T) {
	s := runAllLibs(t, "tmove_minI", `
		local maxI = 2147483647
		local minI = -2147483648
		local a = table.move({[maxI] = 100}, maxI, maxI, minI)
		return a[minI]
	`)
	defer s.Close()
	n, _ := s.ToInteger(-1)
	if n != 100 {
		t.Fatalf("expected 100, got %d", n)
	}
}

// TestStackDepthAllowsHundredFrames documents the call-depth headroom
// bump. The recursion factor in conformance/pm.luau builds a 256-arg
// call to string.char via `range(0,255)`, which would have overflowed
// at the previous depth cap of 200.
func TestStackDepthAllowsHundredFrames(t *testing.T) {
	s := runAllLibs(t, "depth", `
		local function range(i, j)
			if i <= j then return i, range(i+1, j) end
		end
		return string.char(range(0, 255))
	`)
	defer s.Close()
	str, _ := s.ToString(-1)
	if len(str) != 256 {
		t.Fatalf("expected 256-char string, got %d", len(str))
	}
}
