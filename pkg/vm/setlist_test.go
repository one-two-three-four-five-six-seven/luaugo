// Copyright (c) luaugo contributors. Licensed under the MIT License.

package vm_test

import (
	"io"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// runScriptSL is a local script-runner duplicate (arith_test.go has
// one named runScript) so the setlist tests can stand on their own
// even if arith_test.go evolves.
func runScriptSL(t *testing.T, src string) (string, vm.Status, string) {
	t.Helper()
	blob, err := compiler.CompileBinary("=setlist_test", []byte(src), compiler.Defaults())
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
	if err := s.Load("=setlist_test", blob, 0); err != nil {
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

// TestSetListLargeTableInitializer exercises the SETLIST opcode by
// constructing a table with more inline-initialised elements than the
// compiler is willing to emit via individual SETTABLE_N writes. Past
// the inline-init threshold the compiler emits NEWTABLE + GETVARARGS
// (or a string of stores) followed by SETLIST, which is exactly the
// pattern that previously failed with
//
//	SETLIST: target is not a table
//
// on conformance/constructs.luau.
func TestSetListLargeTableInitializer(t *testing.T) {
	src := `local t = {1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20}
return tostring(t[20])`
	out, st, msg := runScriptSL(t, src)
	if st != vm.StatusOK {
		t.Fatalf("run: status=%v msg=%q", st, msg)
	}
	if out != "20" {
		t.Fatalf("t[20]: got %q want %q", out, "20")
	}
}

// TestSetListVarargPackWithNils stresses SETLIST + GETVARARGS over a
// vararg pack containing interior and trailing nils. This used to
// loop forever inside table.set() because a numeric key at
// sizearray+1 with a nil value would rehash repeatedly without ever
// growing the array part (the load-factor heuristic refuses to grow
// when most of the new slots would be nil).
func TestSetListVarargPackWithNils(t *testing.T) {
	src := `local function f(a, ...)
  local arg = {...}
  return tostring(#arg)
end
return f("first", "a", nil, 45, "x", nil)`
	out, st, msg := runScriptSL(t, src)
	if st != vm.StatusOK {
		t.Fatalf("run: status=%v msg=%q", st, msg)
	}
	// The boundary semantics of `#` on a table with holes are
	// implementation-defined; we just require that the call returns
	// SOMETHING (i.e. doesn't hang or panic). A specific value would
	// over-fit the test to a particular boundary policy.
	if out == "" {
		t.Fatalf("expected non-empty tostring(#arg), got %q", out)
	}
}

// TestSetListThenIndexAllSlots makes sure every slot a SETLIST writes
// is actually readable via the regular indexing path afterwards.
func TestSetListThenIndexAllSlots(t *testing.T) {
	src := `local t = {10,20,30,40,50,60,70,80,90,100,110,120,130,140,150,160,170,180,190,200}
local sum = 0
for i=1,20 do sum = sum + t[i] end
return tostring(sum)`
	out, st, msg := runScriptSL(t, src)
	if st != vm.StatusOK {
		t.Fatalf("run: status=%v msg=%q", st, msg)
	}
	if out != "2100" {
		t.Fatalf("sum: got %q want %q", out, "2100")
	}
}
