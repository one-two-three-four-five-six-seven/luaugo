// Copyright (c) luaugo contributors. Licensed under the MIT License.

package vm_test

import (
	"io"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// runScript compiles src with luaugo's compiler and executes it on
// luaugo's VM. Returns (firstReturnValueAsString, status, errMsg).
// nresults is the number of return values to fetch.
func runScript(t *testing.T, src string) (string, vm.Status, string) {
	t.Helper()
	blob, err := compiler.CompileBinary("=test", []byte(src), compiler.Defaults())
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
	if err := s.Load("=test", blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	st := s.PCall(0, 1, 0)
	if st != vm.StatusOK {
		msg, _ := s.ToString(-1)
		return "", st, msg
	}
	out, _ := s.ToString(-1)
	return out, st, ""
}

// TestArithOnReturnedNumber checks that we can perform arithmetic on a
// value that arrives via a function-call return slot. This used to
// fail with "attempt to perform arithmetic (add) on a nil value" when
// a register-allocation or stack-restore bug left the return slot
// reading nil after the call.
func TestArithOnReturnedNumber(t *testing.T) {
	out, st, msg := runScript(t, `local function f() return 5 end
return tostring(f() + 3)
`)
	if st != vm.StatusOK {
		t.Fatalf("run: status=%v msg=%q", st, msg)
	}
	if out != "8" {
		t.Fatalf("got %q want %q", out, "8")
	}
}

// TestConcatWithMethodCall checks that we can perform arithmetic on
// the return value of a NAMECALL / method invocation.
func TestConcatWithMethodCall(t *testing.T) {
	out, st, msg := runScript(t, `local s = "abc"
return tostring(s:len() + s:len())
`)
	if st != vm.StatusOK {
		t.Fatalf("run: status=%v msg=%q", st, msg)
	}
	if out != "6" {
		t.Fatalf("got %q want %q", out, "6")
	}
}

// TestCompareReturnValue checks that comparisons consume the call
// return value, not nil.
func TestCompareReturnValue(t *testing.T) {
	out, st, msg := runScript(t, `local function f() return 7 end
return tostring(f() < 10)
`)
	if st != vm.StatusOK {
		t.Fatalf("run: status=%v msg=%q", st, msg)
	}
	if out != "true" {
		t.Fatalf("got %q want %q", out, "true")
	}
}
