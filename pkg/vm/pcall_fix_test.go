// Copyright (c) luaugo contributors. Licensed under the MIT License.
//
// Regression tests for the pcall.luau:66 and errors.luau:125 fixes:
//   - pushFrame off-by-one (allowed one extra recursive call beyond
//     LUAI_MAXCALLS, producing #res == 10001 instead of the upstream
//     10000 for `pcall(stackover)` recursion).
//   - missing "<chunk>:<line>: " prefix on errors raised by OP_FORNPREP
//     (the executor doesn't update ci.savedpc before raising for the
//     numeric-for type check, so addErrorWhere reads a stale PC and
//     skips the prefix).

package vm_test

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// runChunk compiles+runs a Lua chunk and returns the script's return
// value (as string) or an error message. For repro tests only.
func runChunk(t *testing.T, name, src string) (status string, detail string) {
	t.Helper()
	blob, err := compiler.CompileBinary(name, []byte(src), compiler.Defaults())
	if err != nil {
		return "COMPILE_ERROR", err.Error()
	}
	if len(blob) > 0 && blob[0] == 0 {
		return "COMPILE_ERROR", string(blob[1:])
	}
	s := vm.NewState()
	defer s.Close()
	prev := lib.Stdout
	defer func() { lib.Stdout = prev }()
	lib.Stdout = io.Discard
	lib.OpenAll(s)
	if err := s.Load(name, blob, 0); err != nil {
		return "LOAD_ERROR", err.Error()
	}
	st := s.PCall(0, 1, 0)
	if st == vm.StatusOK {
		msg, _ := s.ToString(-1)
		return "OK", msg
	}
	msg, _ := s.ToString(-1)
	return "RUNTIME_ERR", msg
}

// TestPCallStackOverRecursion mirrors pcall.luau lines 60-67. The
// script self-recurses through pcall; each Lua recursion consumes
// one Lua frame for stackover and one for pcall (the C entry). Once
// we hit maxCallDepth, the innermost call raises "stack overflow"
// and each pcall surface unwinds to the caller, leaving {false, err}
// on the stack. The outermost pcall's result table has #res == 10000
// (matching upstream LUAI_MAXCALLS-based budget).
func TestPCallStackOverRecursion(t *testing.T) {
	src := `
		function stackover() return pcall(stackover) end
		local res = {pcall(stackover)}
		return #res
	`
	st, det := runChunk(t, "pcall_repro.luau", src)
	if st != "OK" {
		t.Fatalf("expected OK, got %s: %s", st, det)
	}
	if det != "10000" {
		t.Errorf("expected #res == 10000, got %q", det)
	}
}

// TestPCallRecurseSucceedsAtLimitMinus3 covers pcall.luau:162. A
// pcall'd self-recursive function must succeed when its depth budget
// fits inside maxCallDepth-2 (main+pcall headers). Regression guard
// against making the pushFrame limit too tight.
func TestPCallRecurseSucceedsAtLimitMinus3(t *testing.T) {
	src := `
		local calllimit = 20000
		local function recurse(n)
			return n <= 1 and 1 or recurse(n-1) + 1
		end
		local ok, val = pcall(recurse, calllimit - 3)
		return tostring(ok)..","..tostring(val)
	`
	st, det := runChunk(t, "pcall_limit_ok.luau", src)
	if st != "OK" {
		t.Fatalf("expected OK, got %s: %s", st, det)
	}
	want := "true,19997"
	if det != want {
		t.Errorf("expected %q, got %q", want, det)
	}
}

// TestPCallRecurseFailsAtLimitMinus2 covers pcall.luau:163. The same
// recursion at depth calllimit-2 must overflow.
func TestPCallRecurseFailsAtLimitMinus2(t *testing.T) {
	src := `
		local calllimit = 20000
		local function recurse(n)
			return n <= 1 and 1 or recurse(n-1) + 1
		end
		local ok, err = pcall(recurse, calllimit - 2)
		return tostring(ok)..":"..type(err)
	`
	st, det := runChunk(t, "pcall_limit_overflow.luau", src)
	if st != "OK" {
		t.Fatalf("expected OK, got %s: %s", st, det)
	}
	if det != "false:string" {
		t.Errorf("expected false:string, got %q", det)
	}
}

// TestForLoopBadLimitLine mirrors errors.luau line 125. A for-loop
// whose limit is a non-numeric string must raise an error that
// includes the source line where the for began (line 2 here), so
// lineerror() in errors.luau can extract "2".
func TestForLoopBadLimitLine(t *testing.T) {
	src := "local a\n for i=1,'a' do \n print(i) \n end"
	full := fmt.Sprintf(`local s = %q
		local fn = loadstring(s)
		local ok, err = pcall(fn)
		return tostring(err)
	`, src)
	st, det := runChunk(t, "errors_repro.luau", full)
	if st != "OK" {
		t.Fatalf("expected OK, got %s: %s", st, det)
	}
	// The chunk-name embedded in the loaded source is "=" + chunkname
	// (loadstring's default), but the only requirement from
	// errors.luau:125 is that `string.match(msg, ":(%d+):")` captures
	// "2". So we just check for ":2:".
	if !strings.Contains(det, ":2:") {
		t.Errorf("expected ':2:' in error message, got %q", det)
	}
}
