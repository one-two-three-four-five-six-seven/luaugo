// Copyright (c) luaugo contributors. Licensed under the MIT License.

package vm_test

import (
	"strings"
	"testing"
)

// These tests cover FORGPREP's __iter dispatch and the plain-table
// builtin iteration fast path. Both were missing prior to the
// pkg/vm/execute.go change that mirrors upstream Luau's LOP_FORGPREP
// semantics: when R(A) is a table (or userdata) without a function
// generator, FORGPREP must consult the __iter metamethod, fall back
// to leaving R(A) as-is when only __call is present, or set up the
// builtin (ra=nil, ra+1=table, ra+2=iterator-position) tuple that
// FORGLOOP's inline iteration consumes.

// TestIter_PlainTableGenericFor: `for k, v in t do ... end` over a
// plain table (no metatable). Without the FORGPREP fast-path,
// FORGLOOP would try to call the table as a function and raise
// "attempt to call a table value".
func TestIter_PlainTableGenericFor(t *testing.T) {
	ok, ret, errMsg := runIter(t, "plain_table_iter", `
		local t = {10, 20, 30}
		local s = 0
		for k, v in t do
			s = s + v
		end
		return tostring(s)
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	if ret != "60" {
		t.Fatalf("got %q, want %q", ret, "60")
	}
}

// TestIter_IterMetamethodTable: __iter on a table. FORGPREP must call
// __iter(t) and place its 3-tuple return at R(A..A+2).
func TestIter_IterMetamethodTable(t *testing.T) {
	ok, ret, errMsg := runIter(t, "iter_mm_table", `
		local f = {}
		setmetatable(f, { __iter = function(x)
			assert(f == x)
			return next, {1, 2, 3, 4}
		end })
		local x = 0
		for n in f do
			x = x + n
		end
		return tostring(x)
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	if ret != "10" {
		t.Fatalf("got %q, want %q", ret, "10")
	}
}

// TestIter_IterMetamethodUserdata: __iter on a userdata (newproxy(true)).
// The same FORGPREP path must work for userdata as for tables.
func TestIter_IterMetamethodUserdata(t *testing.T) {
	ok, ret, errMsg := runIter(t, "iter_mm_userdata", `
		local f = newproxy(true)
		getmetatable(f).__iter = function(x)
			assert(f == x)
			return next, {1, 2, 3, 4}
		end
		local x = 0
		for n in f do
			x = x + n
		end
		return tostring(x)
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	if ret != "10" {
		t.Fatalf("got %q, want %q", ret, "10")
	}
}

// TestIter_IterReturnsNonFunction: __iter that returns nothing leaves
// R(A) as nil. The subsequent FORGLOOP call should then raise the
// canonical "attempt to call a nil value" error (caught by pcall in
// the conformance fixture).
func TestIter_IterReturnsNonFunction(t *testing.T) {
	ok, ret, errMsg := runIter(t, "iter_mm_nil", `
		local obj = {}
		setmetatable(obj, { __iter = function() end })
		local ok, err = pcall(function() for x in obj do end end)
		return tostring(ok) .. "|" .. tostring(err)
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	if !strings.HasPrefix(ret, "false|") {
		t.Fatalf("expected pcall failure, got %q", ret)
	}
	if !strings.Contains(ret, "nil value") {
		t.Fatalf("expected 'nil value' in error message, got %q", ret)
	}
}

// TestIter_PlainTableSingleVar verifies `for k in t do` (one variable)
// over a plain table works the same as the two-variable form.
func TestIter_PlainTableSingleVar(t *testing.T) {
	ok, ret, errMsg := runIter(t, "plain_table_single", `
		local x = 0
		for k in {1, 2, 3, 4, 5} do
			x = x + k
		end
		return tostring(x)
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	if ret != "15" {
		t.Fatalf("got %q, want %q", ret, "15")
	}
}

// TestIter_PlainTableExtraVars verifies that when the user requests
// more variables than the iteration emits (only k, v), the extras
// must be set to nil each iteration. Mirrors the upstream LOP_FORGLOOP
// "clear extra variables" branch.
func TestIter_PlainTableExtraVars(t *testing.T) {
	ok, ret, errMsg := runIter(t, "plain_table_extra", `
		local x = 0
		for k,v,a,b,c,d,e in {1, 2, 3, 4, 5} do
			x = x + k
			assert(a == nil and b == nil and c == nil and d == nil and e == nil)
		end
		return tostring(x)
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	if ret != "15" {
		t.Fatalf("got %q, want %q", ret, "15")
	}
}

// TestIter_PlainTableMixed: dictionary + array entries. The builtin
// iteration must walk both the array part and the hash part.
func TestIter_PlainTableMixed(t *testing.T) {
	ok, ret, errMsg := runIter(t, "plain_table_mixed", `
		local x = 0
		for k, v in {1, 2, 3, nil, 5, a = 1, b = 2, c = 3} do
			x = x + v
		end
		return tostring(x)
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	if ret != "17" {
		t.Fatalf("got %q, want %q", ret, "17")
	}
}

// TestIter_NonIterableOverNumber: `for x in 42 do end` must raise
// the canonical "attempt to iterate over a number value" error.
func TestIter_NonIterableOverNumber(t *testing.T) {
	ok, ret, errMsg := runIter(t, "non_iter_num", `
		local ok, err = pcall(function() for x in 42 do end end)
		return tostring(ok) .. "|" .. tostring(err)
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	if !strings.HasPrefix(ret, "false|") {
		t.Fatalf("expected pcall failure, got %q", ret)
	}
	if !strings.Contains(ret, "iterate") {
		t.Fatalf("expected 'iterate' in error message, got %q", ret)
	}
}
