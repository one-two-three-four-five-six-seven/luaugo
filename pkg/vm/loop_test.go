// Copyright (c) luaugo contributors. Licensed under the MIT License.

package vm_test

import (
	"io"
	"testing"
	"time"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// runScriptLP compiles and runs a Lua source under a per-test
// timeout. If execution exceeds the deadline we fail the test rather
// than hanging the suite, which is critical for loop tests since
// the failure mode of an inverted jump or wrong stop condition is
// an infinite loop.
func runScriptLP(t *testing.T, src string, deadline time.Duration) (string, vm.Status, string) {
	t.Helper()
	blob, err := compiler.CompileBinary("=loop_test", []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(blob) > 0 && blob[0] == 0 {
		t.Fatalf("compile error blob: %s", string(blob[1:]))
	}
	s := vm.NewState()
	defer s.Close()
	prev := lib.Stdout
	defer func() { lib.Stdout = prev }()
	lib.Stdout = io.Discard
	lib.OpenAll(s)
	if err := s.Load("=loop_test", blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	done := make(chan struct{})
	var st vm.Status
	var out, msg string
	go func() {
		defer close(done)
		st = s.PCall(0, 1, 0)
		if st != vm.StatusOK {
			msg, _ = s.ToString(-1)
			return
		}
		out, _ = s.ToString(-1)
	}()
	select {
	case <-done:
	case <-time.After(deadline):
		t.Fatalf("script exceeded %v deadline; suspected infinite loop", deadline)
	}
	return out, st, msg
}

// TestNumericForDescendingStep verifies that `for i=hi,lo,-1` halts
// correctly. This guards against OpForNLoop comparing wrong direction
// on negative steps.
func TestNumericForDescendingStep(t *testing.T) {
	src := `local s = 0
for i=10,1,-1 do s = s + i end
return tostring(s)`
	out, st, msg := runScriptLP(t, src, 2*time.Second)
	if st != vm.StatusOK {
		t.Fatalf("run: %v %q", st, msg)
	}
	if out != "55" {
		t.Fatalf("sum 10..1 by -1: got %q want 55", out)
	}
}

// TestNumericForLargeNegativeStep covers an irregular step that skips
// most of the range.
func TestNumericForLargeNegativeStep(t *testing.T) {
	src := `local s = 0
for i=100,1,-7 do s = s + i end
return tostring(s)`
	out, st, msg := runScriptLP(t, src, 2*time.Second)
	if st != vm.StatusOK {
		t.Fatalf("run: %v %q", st, msg)
	}
	// 100, 93, 86, ... 2: arithmetic series. 15 terms.
	// 100 + (15-1)*(-7) = 100 - 98 = 2. Sum = 15*(100+2)/2 = 765.
	if out != "765" {
		t.Fatalf("got %q want 765", out)
	}
}

// TestWhileTrueWithBreak verifies that `while true do break end` exits
// rather than looping forever. This depends on OpJumpBack being a
// backward branch and OpJumpIfNot / break-emission both being correct.
func TestWhileTrueWithBreak(t *testing.T) {
	src := `local n = 0
while true do
  n = n + 1
  if n == 5 then break end
end
return tostring(n)`
	out, st, msg := runScriptLP(t, src, 2*time.Second)
	if st != vm.StatusOK {
		t.Fatalf("run: %v %q", st, msg)
	}
	if out != "5" {
		t.Fatalf("got %q want 5", out)
	}
}

// TestRepeatUntilTrue verifies the repeat..until form, whose
// generated JUMPIFNOT must branch back to the loop top exactly when
// the condition is false.
func TestRepeatUntilTrue(t *testing.T) {
	src := `local n = 0
repeat
  n = n + 1
until n == 3
return tostring(n)`
	out, st, msg := runScriptLP(t, src, 2*time.Second)
	if st != vm.StatusOK {
		t.Fatalf("run: %v %q", st, msg)
	}
	if out != "3" {
		t.Fatalf("got %q want 3", out)
	}
}

// TestNestedLoops verifies nested numeric fors with break/continue
// patterns -- a common source of jump-offset miscalculations because
// the inner loop's backward jump must miss the outer's bookkeeping
// instructions.
func TestNestedLoops(t *testing.T) {
	src := `local total = 0
for i=1,5 do
  for j=1,5 do
    total = total + i * j
  end
end
return tostring(total)`
	out, st, msg := runScriptLP(t, src, 2*time.Second)
	if st != vm.StatusOK {
		t.Fatalf("run: %v %q", st, msg)
	}
	// sum(i)*sum(j) = 15*15 = 225.
	if out != "225" {
		t.Fatalf("got %q want 225", out)
	}
}

// TestDeepNestedRepeatUntil exercises three levels of repeat..until.
// A bug in OpJumpBack offset arithmetic typically blows up at depth 2
// or 3 because each loop's exit jump has to clear the bookkeeping of
// the enclosing levels.
func TestDeepNestedRepeatUntil(t *testing.T) {
	src := `local count = 0
local i = 0
repeat
  i = i + 1
  local j = 0
  repeat
    j = j + 1
    local k = 0
    repeat
      k = k + 1
      count = count + 1
    until k == 2
  until j == 3
until i == 4
return tostring(count)`
	out, st, msg := runScriptLP(t, src, 2*time.Second)
	if st != vm.StatusOK {
		t.Fatalf("run: %v %q", st, msg)
	}
	if out != "24" {
		t.Fatalf("got %q want 24", out)
	}
}

// TestRecursionStackOverflow exercises the call-depth guard added in
// do.go: an intentionally non-terminating recursion should raise a
// recoverable runtime error rather than running until the host OOMs
// or the Go stack explodes. Mirrors conformance/native.luau's
// fuzzfail3.
func TestRecursionStackOverflow(t *testing.T) {
	src := `local function f(n) return f(n+1) end
local ok, err = pcall(f, 0)
return tostring(ok)`
	out, st, msg := runScriptLP(t, src, 5*time.Second)
	if st != vm.StatusOK {
		t.Fatalf("run: %v %q", st, msg)
	}
	if out != "false" {
		t.Fatalf("got %q want false (pcall should catch overflow)", out)
	}
}
