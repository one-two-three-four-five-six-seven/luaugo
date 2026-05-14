// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package lib

import (
	"math"
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// runVectorLuau spins up a fresh VM with the base + vector libraries
// and executes src as a Luau chunk. It mirrors runLuau (in
// table_test.go) but only opens the libraries vector tests need.
func runVectorLuau(t *testing.T, src string) (*vm.State, error) {
	t.Helper()
	s := vm.NewState()
	OpenBase(s)
	OpenVector(s)

	blob, err := compiler.CompileBinary("=vec_test", []byte(src), compiler.Defaults())
	if err != nil {
		s.Close()
		t.Fatalf("compile: %v", err)
	}
	if err := s.Load("=vec_test", blob, 0); err != nil {
		s.Close()
		t.Fatalf("load: %v", err)
	}
	if st := s.PCall(0, vm.MultRet, 0); st != vm.StatusOK {
		msg, _ := s.ToString(-1)
		s.Close()
		return nil, &runErr{msg: msg, status: st}
	}
	return s, nil
}

func mustRunVector(t *testing.T, src string) *vm.State {
	t.Helper()
	s, err := runVectorLuau(t, src)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	return s
}

// TestVectorMagnitudeProperty is a regression for the
// "vector.luau:99 attempt to index vector with 'Magnitude'" failure.
// Roblox-style vectors expose Magnitude as a computed read-only
// property; before the fix our vector type had no __index metamethod
// so the VM raised on access.
func TestVectorMagnitudeProperty(t *testing.T) {
	s := mustRunVector(t, `
		local v = vector.create(1, 2, 2)
		assert(v.Magnitude == 3)
		local z = vector.create(0, 0, 0)
		assert(z.Magnitude == 0)
		return v.Magnitude
	`)
	defer s.Close()
	got, ok := s.ToNumber(-1)
	if !ok || math.Abs(got-3) > 1e-6 {
		t.Fatalf("Magnitude: ok=%v got=%v want 3", ok, got)
	}
}

// TestVectorUnitProperty covers the .Unit (normalized) accessor.
func TestVectorUnitProperty(t *testing.T) {
	s := mustRunVector(t, `
		local u = vector.create(2, 0, 0).Unit
		assert(u == vector.create(1, 0, 0))
		-- Zero-magnitude vectors normalize to zero in luaugo (see
		-- contract in pkg/vm/lib/vector.go's vectorNormalize).
		local z = vector.create(0, 0, 0).Unit
		assert(z == vector.create(0, 0, 0))
	`)
	s.Close()
}

// TestVectorDotMethodNamecall covers the v:Dot(other) namecall path
// (line 104 of conformance/vector.luau). It must dispatch to the same
// implementation as vector.dot.
func TestVectorDotMethodNamecall(t *testing.T) {
	s := mustRunVector(t, `
		local a = vector.create(1, 2, 4)
		local b = vector.create(5, 6, 7)
		assert(a:Dot(b) == 45)
		assert(b:Dot(a) == 45)
	`)
	s.Close()
}

// TestVectorDotIndexedCall covers the v['Dot'](x, y) pattern
// (line 100 of conformance/vector.luau): looking up "Dot" on a
// vector via subscript and invoking the resulting bare function
// with two vector arguments (not using the original vector as self).
func TestVectorDotIndexedCall(t *testing.T) {
	s := mustRunVector(t, `
		local f = vector.create(0, 0, 0)['Dot']
		assert(f(vector.create(1, 2, 4), vector.create(5, 6, 7)) == 45)
	`)
	s.Close()
}

// TestVectorCrossNamecall confirms :Cross also dispatches correctly.
func TestVectorCrossNamecall(t *testing.T) {
	s := mustRunVector(t, `
		local r = vector.create(1, 0, 0):Cross(vector.create(0, 1, 0))
		assert(r == vector.create(0, 0, 1))
	`)
	s.Close()
}

// TestVectorComponentsBypassMetatable verifies the lowercase
// component accessors (.x, .y, .z) still resolve through the VM's
// fast path and are not shadowed by the metatable we installed.
func TestVectorComponentsBypassMetatable(t *testing.T) {
	s := mustRunVector(t, `
		local v = vector.create(11, 22, 33)
		assert(v.x == 11)
		assert(v.y == 22)
		assert(v.z == 33)
		assert(v.X == 11) -- uppercase aliases also accepted
		assert(v.Y == 22)
		assert(v.Z == 33)
	`)
	s.Close()
}

// TestVectorIndexUnknownNameRaises preserves the upstream error
// message for genuinely unknown property names. Without this, the
// metatable would simply return nil and scripts would see "attempt
// to call a nil value" elsewhere -- harder to debug than a clear
// index error at the point of misuse.
func TestVectorIndexUnknownNameRaises(t *testing.T) {
	_, err := runVectorLuau(t, `
		local v = vector.create(1, 2, 3)
		return v.NotARealProperty
	`)
	if err == nil {
		t.Fatalf("expected error for unknown vector property")
	}
	if !strings.Contains(err.Error(), "NotARealProperty") {
		t.Fatalf("error %q should mention the offending key", err.Error())
	}
	if !strings.Contains(err.Error(), "vector") {
		t.Fatalf("error %q should mention 'vector'", err.Error())
	}
}
