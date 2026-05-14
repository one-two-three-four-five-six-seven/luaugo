// Copyright (c) luaugo contributors. Licensed under the MIT License.

package vm_test

import (
	"os"
	"testing"
)

// TestSort_ConformancePrefix exercises sort.luau lines 1..97 (the
// random/invert sort + pairs traversal block). It is the smallest
// prefix of the upstream sort fixture that reproduced the
// "invalid key to 'next'" panic prior to the FORGLOOP stack-staging
// fix (pkg/vm/execute.go). The bug was: OpForGLoop used L.top as the
// position to stage [generator, state, control] for the iterator
// call, but the loop body's last operation could leave L.top sitting
// inside R(A)..R(A+2). The subsequent push of R(A) into L.stack[L.top]
// clobbered R(A+2) (the iteration control) with R(A) (the generator
// function); on the next iteration FORGLOOP would call next(t, fn)
// and t.next() would raise "invalid key to 'next'" because a function
// is not a legal table key.
func TestSort_ConformancePrefix(t *testing.T) {
	src, err := os.ReadFile("../../tests/conformance/sort.luau")
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	full := string(src)
	// Truncate before the "non-trivial sort during sort" block.
	needle := "\"alo\", \"then this one\""
	i := -1
	for k := 0; k+len(needle) <= len(full); k++ {
		if full[k:k+len(needle)] == needle {
			i = k
			break
		}
	}
	if i < 0 {
		t.Fatalf("cut point not found")
	}
	for i > 0 && full[i-1] != '\n' {
		i--
	}
	src = []byte(full[:i] + "return \"OK\"\n")
	ok, _, errMsg := runIter(t, "sort_prefix", string(src))
	if !ok {
		t.Fatalf("sort prefix regressed: %s", errMsg)
	}
}

// TestSort_QuicksortKiller mirrors sort.luau's "force quicksort to
// degrade to heap sort" block. The block iterates a plain table with
// `for k in t do` (exercising the FORGPREP table-fast-path), mutates
// it during iteration, then runs sort with an adversarial comparator
// that depends on cumulative state. With both the FORGPREP __iter /
// builtin-table-iteration setup and the FORGLOOP register-window fix,
// this completes without raising.
func TestSort_QuicksortKiller(t *testing.T) {
	ok, _, errMsg := runIter(t, "sort_qs_killer", `
		local keys = {}
		local candidate = 0
		local nxt = 0
		local t = table.create(100, 0)
		for k in t do
			t[k] = k
		end
		table.sort(t, function (x, y)
			if keys[x] == nil and keys[y] == nil then
				if x == candidate then keys[x] = nxt else keys[y] = nxt end
				nxt += 1
			end
			if keys[x] == nil then
				candidate = x
				return true
			end
			if keys[y] == nil then
				candidate = y
				return false
			end
			return keys[x] < keys[y]
		end)
		return "OK"
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
}

// TestSort_FullFixtureSmoke runs the full sort.luau fixture and
// reports the outcome. Used to track sort.luau status; not a gate.
func TestSort_FullFixtureSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	src, err := os.ReadFile("../../tests/conformance/sort.luau")
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	ok, _, errMsg := runIter(t, "sort_full", string(src))
	t.Logf("sort.luau: ok=%v err=%q", ok, errMsg)
}

// TestSort_PairsAfterAdversarialSort guards against the regression
// where running pairs() after a sort that drove many register-window
// updates (the `i=i+1` global mutation in the comparator) raised
// "invalid key to 'next'".
func TestSort_PairsAfterAdversarialSort(t *testing.T) {
	ok, _, errMsg := runIter(t, "sort_pairs_after", `
		local limit = 30000
		local a = {}
		for i=1,limit do a[i] = math.random() end
		table.sort(a)
		table.sort(a)
		a = {}
		for i=1,limit do a[i] = math.random() end
		local k = 0
		table.sort(a, function(x,y) k=k+1; return y<x end)
		for i=1,limit do a[i] = false end
		table.sort(a, function(x,y) return nil end)
		for i,v in pairs(a) do assert(not v) end
		return "OK"
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
}
