// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package lib

import (
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// TestTableInsertWholeFloatPos is a regression for the
// "tables.luau:388 invalid argument #2 to 'insert' (integer expected,
// got number)" failure. Upstream table.insert accepts any numeric
// position argument: whole-valued floats (2.0), out-of-range floats
// (-1e9), and even NaN. Before the fix our LCheckInteger rejected
// NaN (asInteger requires an exact integer representation) which
// raised at the call site instead of silently doing nothing.
func TestTableInsertWholeFloatPos(t *testing.T) {
	// 2.0 (whole-valued float) accepted as position.
	s := mustRun(t, `
		local a = {10, 20, 30}
		table.insert(a, 2.0, 99)
		assert(a[1] == 10)
		assert(a[2] == 99)
		assert(a[3] == 20)
		assert(a[4] == 30)
	`)
	s.Close()

	// -10^9 (large negative float, integral) accepted; lands in
	// the negative-index region without shifting the array part.
	s = mustRun(t, `
		local a = {1, 2, 3}
		table.insert(a, -10^9, 42)
		assert(a[1] == 1 and a[2] == 2 and a[3] == 3)
		assert(a[-1000000000] == 42)
	`)
	s.Close()

	// 0/0 (NaN) must not raise. This is the conformance fixture's
	// case ("platform-dependent behavior atm so hard to validate"):
	// upstream accepts NaN by truncating with C semantics; we treat
	// it as out-of-range so neither the shift nor the assertion
	// after fires an error.
	s = mustRun(t, `
		local a = {1, 2, 3}
		table.insert(a, 0/0, 42)
		assert(a[1] == 1 and a[2] == 2 and a[3] == 3)
	`)
	s.Close()
}

// TestTableInsertRejectsNonNumber confirms we still reject genuinely
// non-numeric position arguments rather than silently misbehaving.
func TestTableInsertRejectsNonNumber(t *testing.T) {
	_, err := runLuau(t, `
		local a = {}
		table.insert(a, "x", 1)
	`)
	if err == nil {
		t.Fatalf("expected runtime error for string position, got nil")
	}
}

// TestTableInsertPositiveInfinity covers another finite-but-extreme
// boundary: +Inf truncates to MaxInt and lands out of the array part
// without crashing.
func TestTableInsertPositiveInfinity(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenBase(s)
	OpenTable(s)

	src := `
		local a = {1, 2, 3}
		table.insert(a, 1/0, 99)
		assert(a[1] == 1 and a[2] == 2 and a[3] == 3)
		return true
	`
	if _, err := runLuau(t, src); err != nil {
		t.Fatalf("table.insert(a, +Inf, 99) must not raise: %v", err)
	}
}
