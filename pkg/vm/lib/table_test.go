// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import (
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// runLuau compiles src as a Luau chunk and runs it with the base and
// table libraries installed. Returns the VM (so the caller may inspect
// results) or an error.
func runLuau(t *testing.T, src string) (*vm.State, error) {
	t.Helper()
	s := vm.NewState()
	OpenBase(s)
	OpenTable(s)

	blob, err := compiler.CompileBinary("=test", []byte(src), compiler.Defaults())
	if err != nil {
		s.Close()
		t.Fatalf("compile: %v", err)
	}
	if err := s.Load("=test", blob, 0); err != nil {
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

type runErr struct {
	msg    string
	status vm.Status
}

func (e *runErr) Error() string { return e.msg }

// mustRun runs src and fails the test on error.
func mustRun(t *testing.T, src string) *vm.State {
	t.Helper()
	s, err := runLuau(t, src)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	return s
}

// expectErr runs src and expects a runtime error whose message
// contains want.
func expectErr(t *testing.T, src, want string) {
	t.Helper()
	s, err := runLuau(t, src)
	if s != nil {
		s.Close()
	}
	if err == nil {
		t.Fatalf("expected error containing %q, got success", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}

// ----------------------------------------------------------------------
// table.insert / table.remove
// ----------------------------------------------------------------------

func TestTableInsertRemove(t *testing.T) {
	src := `
		local t = {10, 20, 30}
		-- append
		table.insert(t, 40)
		assert(#t == 4 and t[4] == 40)
		-- insert at position 2: shift 20,30,40 -> positions 3,4,5
		table.insert(t, 2, 99)
		assert(#t == 5)
		assert(t[1] == 10 and t[2] == 99 and t[3] == 20 and t[4] == 30 and t[5] == 40)
		-- remove default (last)
		local removed = table.remove(t)
		assert(removed == 40 and #t == 4)
		-- remove at position 2
		local r2 = table.remove(t, 2)
		assert(r2 == 99)
		assert(#t == 3)
		assert(t[1] == 10 and t[2] == 20 and t[3] == 30)
		-- remove on empty table returns nothing
		local empty = {}
		local got = table.remove(empty)
		assert(got == nil)
	`
	mustRun(t, src).Close()
}

// ----------------------------------------------------------------------
// table.concat
// ----------------------------------------------------------------------

func TestTableConcat(t *testing.T) {
	mustRun(t, `
		local t = {"a", "b", "c", "d"}
		assert(table.concat(t) == "abcd")
		assert(table.concat(t, ",") == "a,b,c,d")
		assert(table.concat(t, "-", 2) == "b-c-d")
		assert(table.concat(t, "-", 2, 3) == "b-c")
		assert(table.concat({}, ",") == "")
		assert(table.concat({1, 2, 3}, " ") == "1 2 3")
		-- empty range
		assert(table.concat(t, ",", 3, 2) == "")
	`).Close()
	// Error case: non-stringable value.
	expectErr(t, `table.concat({1, true, 3}, ",")`, "concat")
}

// ----------------------------------------------------------------------
// table.sort
// ----------------------------------------------------------------------

func TestTableSort(t *testing.T) {
	// Default sort (ascending) on a multi-duplicate list.
	mustRun(t, `
		local t = {3, 1, 4, 1, 5, 9, 2, 6, 5, 3, 5}
		table.sort(t)
		local expected = {1, 1, 2, 3, 3, 4, 5, 5, 5, 6, 9}
		for i = 1, #expected do
			assert(t[i] == expected[i], "mismatch at " .. tostring(i))
		end
	`).Close()
	// Custom comparator (descending).
	mustRun(t, `
		local t = {1, 2, 3, 4, 5}
		table.sort(t, function(a, b) return a > b end)
		assert(t[1] == 5 and t[5] == 1)
	`).Close()
	// String sort.
	mustRun(t, `
		local t = {"banana", "apple", "cherry"}
		table.sort(t)
		assert(t[1] == "apple")
		assert(t[2] == "banana")
		assert(t[3] == "cherry")
	`).Close()
	// Empty / single element edge cases.
	mustRun(t, `
		local t = {}
		table.sort(t)
		assert(#t == 0)
		local s = {42}
		table.sort(s)
		assert(s[1] == 42)
	`).Close()
	// Larger pseudo-random input must end up non-decreasing.
	mustRun(t, `
		local t = {}
		local rng = 12345
		for i = 1, 200 do
			rng = (rng * 1103515245 + 12345) % 2147483648
			t[i] = rng % 1000
		end
		table.sort(t)
		for i = 2, #t do
			assert(t[i-1] <= t[i], "not sorted at " .. tostring(i))
		end
	`).Close()
	// Bad comparator: returns true unconditionally. Must raise
	// "invalid order function" rather than loop forever, since the
	// partition iterators never converge.
	expectErr(t, `
		local t = {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		table.sort(t, function(a, b) return true end)
	`, "invalid order function")
}

// ----------------------------------------------------------------------
// table.pack / table.unpack
// ----------------------------------------------------------------------

func TestTablePackUnpack(t *testing.T) {
	mustRun(t, `
		local p = table.pack("a", "b", "c")
		assert(p.n == 3)
		assert(p[1] == "a" and p[2] == "b" and p[3] == "c")

		-- pack zero args
		local q = table.pack()
		assert(q.n == 0)

		-- unpack returns multiple values
		local a, b, c = table.unpack(p)
		assert(a == "a" and b == "b" and c == "c")

		-- unpack with i,j
		local x, y = table.unpack({10, 20, 30, 40}, 2, 3)
		assert(x == 20 and y == 30)

		-- unpack empty range
		local r = table.unpack({1, 2, 3}, 4)
		assert(r == nil)

		-- unpack is also available as a global alias in Lua 5.1
		local u, v = unpack({"x", "y"})
		assert(u == "x" and v == "y")
	`).Close()
}

// ----------------------------------------------------------------------
// table.move (including overlap)
// ----------------------------------------------------------------------

func TestTableMove(t *testing.T) {
	// Basic move to another table.
	mustRun(t, `
		local src = {1, 2, 3, 4, 5}
		local dst = {0, 0, 0, 0, 0}
		local r = table.move(src, 2, 4, 1, dst)
		assert(r == dst)
		assert(dst[1] == 2 and dst[2] == 3 and dst[3] == 4)
		-- positions 4,5 are unchanged
		assert(dst[4] == 0 and dst[5] == 0)
	`).Close()

	// Move within same table, no overlap.
	mustRun(t, `
		local t = {1, 2, 3, 4, 5}
		table.move(t, 1, 3, 5)
		-- t[5..7] = old t[1..3]
		assert(t[5] == 1 and t[6] == 2 and t[7] == 3)
	`).Close()

	// Overlap, destination > source range (must copy descending so
	// the source bytes are not stomped before they are read).
	// t={1,2,3,4,5}, move 1..3 to 2 -> {1,1,2,3,5}.
	mustRun(t, `
		local t = {1, 2, 3, 4, 5}
		table.move(t, 1, 3, 2)
		assert(t[1] == 1, "t[1]=" .. tostring(t[1]))
		assert(t[2] == 1, "t[2]=" .. tostring(t[2]))
		assert(t[3] == 2, "t[3]=" .. tostring(t[3]))
		assert(t[4] == 3, "t[4]=" .. tostring(t[4]))
		assert(t[5] == 5, "t[5]=" .. tostring(t[5]))
	`).Close()

	// Overlap, destination < source (shift left): t={1,2,3,4,5}, move
	// 3..5 to 1 -> {3,4,5,4,5}.
	mustRun(t, `
		local t = {1, 2, 3, 4, 5}
		table.move(t, 3, 5, 1)
		assert(t[1] == 3, "t[1]=" .. tostring(t[1]))
		assert(t[2] == 4, "t[2]=" .. tostring(t[2]))
		assert(t[3] == 5, "t[3]=" .. tostring(t[3]))
	`).Close()

	// e < f -> nothing to move; returns destination unchanged.
	mustRun(t, `
		local s = {1, 2, 3}
		local d = {9, 9, 9}
		local r = table.move(s, 2, 1, 1, d)
		assert(r == d)
		assert(d[1] == 9 and d[2] == 9 and d[3] == 9)
	`).Close()

	// Frozen destination rejects writes.
	expectErr(t, `
		local s = {1, 2, 3}
		local d = table.freeze({0, 0, 0})
		table.move(s, 1, 3, 1, d)
	`, "readonly")
}

// ----------------------------------------------------------------------
// table.create
// ----------------------------------------------------------------------

func TestTableCreate(t *testing.T) {
	mustRun(t, `
		local t = table.create(5)
		assert(type(t) == "table")
		-- nil-filled array has length 0 by Luau semantics
		assert(#t == 0)
		local f = table.create(3, "x")
		assert(f[1] == "x" and f[2] == "x" and f[3] == "x")
		assert(#f == 3)
		local z = table.create(0)
		assert(#z == 0)
	`).Close()
	expectErr(t, `table.create(-1)`, "out of range")
}

// ----------------------------------------------------------------------
// table.find
// ----------------------------------------------------------------------

func TestTableFind(t *testing.T) {
	mustRun(t, `
		local t = {"a", "b", "c", "b", "d"}
		assert(table.find(t, "a") == 1)
		assert(table.find(t, "b") == 2)
		assert(table.find(t, "b", 3) == 4)
		assert(table.find(t, "z") == nil)
		assert(table.find(t, "a", 5) == nil)
		assert(table.find({10, 20, 30}, 20) == 2)
	`).Close()
	expectErr(t, `table.find({1}, 1, 0)`, "out of range")
}

// ----------------------------------------------------------------------
// table.freeze / table.isfrozen / table.clone
// ----------------------------------------------------------------------

func TestTableFreezeClone(t *testing.T) {
	mustRun(t, `
		local t = {1, 2, 3}
		assert(table.isfrozen(t) == false)
		local r = table.freeze(t)
		assert(r == t)
		assert(table.isfrozen(t) == true)

		local c = table.clone(t)
		assert(c ~= t)
		assert(table.isfrozen(c) == false)
		assert(#c == 3)
		assert(c[1] == 1 and c[2] == 2 and c[3] == 3)
		-- Mutating clone must not touch original.
		c[1] = 99
		assert(t[1] == 1)
		assert(c[1] == 99)
	`).Close()
	// Writing to a frozen table raises.
	expectErr(t, `
		local t = table.freeze({1, 2, 3})
		t[1] = 99
	`, "readonly")
	// Double-freeze raises.
	expectErr(t, `
		local t = table.freeze({1})
		table.freeze(t)
	`, "already frozen")
	// Clone preserves the metatable but is not frozen, and the
	// metatable's __index keeps working through the clone.
	mustRun(t, `
		local mt = {__index = function() return "mt" end}
		local t = setmetatable({}, mt)
		table.freeze(t)
		local c = table.clone(t)
		assert(getmetatable(c) == mt)
		assert(table.isfrozen(c) == false)
		assert(c.foo == "mt")
	`).Close()
}

// ----------------------------------------------------------------------
// table.clear
// ----------------------------------------------------------------------

func TestTableClear(t *testing.T) {
	mustRun(t, `
		local t = {1, 2, 3, 4, 5}
		t.x = "hello"
		table.clear(t)
		assert(#t == 0)
		assert(t[1] == nil)
		assert(t.x == nil)
		-- metatable preserved
		local mt = {__index = function() return 42 end}
		local u = setmetatable({1, 2, 3}, mt)
		table.clear(u)
		assert(#u == 0)
		assert(u.anything == 42)
	`).Close()
	expectErr(t, `
		local t = table.freeze({1, 2, 3})
		table.clear(t)
	`, "readonly")
}

// ----------------------------------------------------------------------
// Sanity checks for the deprecated foreach/foreachi/getn/maxn entries
// so the registration code does not regress.
// ----------------------------------------------------------------------

func TestTableDeprecatedEntries(t *testing.T) {
	mustRun(t, `
		-- getn equivalent to #
		assert(table.getn({1, 2, 3}) == 3)
		-- maxn finds the largest positive integer key
		assert(table.maxn({[1]=1, [3]=3, [10]=10}) == 10)
		assert(table.maxn({}) == 0)
		-- foreachi visits sequence positions in order
		local seen = {}
		table.foreachi({"a","b","c"}, function(i, v) seen[i] = v end)
		assert(seen[1] == "a" and seen[3] == "c")
		-- foreach can early-exit by returning non-nil
		local r = table.foreach({1, 2, 3}, function(k, v)
			if v == 2 then return "stop" end
			return nil
		end)
		assert(r == "stop")
		-- foreachi early-exits too
		local r2 = table.foreachi({10, 20, 30}, function(i, v)
			if v == 20 then return i end
			return nil
		end)
		assert(r2 == 2)
	`).Close()
}
