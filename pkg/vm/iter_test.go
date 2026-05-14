// Copyright (c) luaugo contributors. Licensed under the MIT License.

package vm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// runIter compiles src with luaugo's compiler, runs it on luaugo's
// own VM with the standard library opened. Returns (ok, lastReturn,
// errorMessage).
func runIter(t *testing.T, name, src string) (bool, string, string) {
	t.Helper()
	blob, err := compiler.CompileBinary(name, []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	s := vm.NewState()
	defer s.Close()
	prev := lib.Stdout
	defer func() { lib.Stdout = prev }()
	var buf bytes.Buffer
	lib.Stdout = &buf
	lib.OpenAll(s)
	if err := s.Load(name, blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	if st := s.PCall(0, 1, 0); st != vm.StatusOK {
		msg, _ := s.ToString(-1)
		return false, "", msg
	}
	ret, _ := s.ToString(-1)
	_ = buf
	return true, ret, ""
}

func TestIter_IpairsSum(t *testing.T) {
	ok, ret, errMsg := runIter(t, "ipairs_sum", `
		local t = {10, 20, 30}
		local s = 0
		for _, v in ipairs(t) do s = s + v end
		return tostring(s)
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	if ret != "60" {
		t.Fatalf("got %q, want %q", ret, "60")
	}
}

func TestIter_IpairsDebug(t *testing.T) {
	ok, ret, errMsg := runIter(t, "ipairs_debug", `
		local t = {10, 20, 30}
		local hits = 0
		local last_v = 999
		for i, v in ipairs(t) do
			hits = hits + 1
			last_v = v
		end
		return tostring(hits) .. "/" .. tostring(last_v)
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	if ret != "3/30" {
		t.Fatalf("got %q, want %q", ret, "3/30")
	}
}

func TestIter_PairsCount(t *testing.T) {
	ok, ret, errMsg := runIter(t, "pairs_count", `
		local t = {a = 1, b = 2, c = 3}
		local n = 0
		local sum = 0
		for k, v in pairs(t) do
			n = n + 1
			sum = sum + v
		end
		return tostring(n) .. "," .. tostring(sum)
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	if ret != "3,6" {
		t.Fatalf("got %q, want %q", ret, "3,6")
	}
}

func TestIter_ConformanceSnippet(t *testing.T) {
	// Reproduces the first ~50 lines of tests/conformance/iter.luau
	// inline so we can pin down which generic-for use case still
	// breaks if any.
	ok, _, errMsg := runIter(t, "conf_iter", `
		-- basic for loop tests
		do
		  local a
		  for a,b in pairs{} do error("not here") end
		  for i=1,0 do error("not here") end
		  for i=0,1,-1 do error("not here") end
		  a = nil; for i=1,1 do assert(not a); a=1 end; assert(a)
		  a = nil; for i=1,1,-1 do assert(not a); a=1 end; assert(a)
		  a = 0; for i=0, 1, 0.1 do a=a+1 end; assert(a==11)
		end
		-- generic for with function iterators
		do
		  local function f (n, p)
		    local t = {}; for i=1,p do t[i] = i*10 end
		    return function (_,n)
		             if n > 0 then
		               n = n-1
		               return n, unpack(t)
		             end
		           end, nil, n
		  end
		  local x = 0
		  for n,a,b,c,d in f(5,3) do
		    x = x+1
		    assert(a == 10 and b == 20 and c == 30 and d == nil)
		  end
		  assert(x == 5)
		end
		return "OK"
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
}

func TestIter_CallMetamethodTable(t *testing.T) {
	// Generic-for with a non-function generator backed by __call. The
	// FORGLOOP must invoke the table's __call metamethod each iteration.
	ok, ret, errMsg := runIter(t, "call_mt", `
		local f = {}
		setmetatable(f, { __call = function(_, _, n) if n > 0 then return n - 1 end end })
		local x = 0
		for n in f, nil, 5 do
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

func TestIter_NonIterableErrors(t *testing.T) {
	// FORGPREP* must raise a Lua error with the canonical "attempt to
	// iterate over a <type> value" message when the generator slot is
	// not a function (and not a table/userdata that could carry __iter
	// or __call). The test wraps the for-loops in pcall and checks
	// the error string the way tests/conformance/iter_fenv.luau does.
	ok, ret, errMsg := runIter(t, "non_iterable", `
		local results = {}
		local function check(v, label)
			local ok, err = pcall(function() for _ in v do end end)
			results[#results+1] = (ok and "ok" or err)
			results[#results+1] = label
		end
		check("hello", "string")
		check(42, "number")
		check(true, "bool")
		local all = table.concat(results, "|")
		return all
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	// Each pcall must fail with "attempt to iterate over a <type> value".
	for _, want := range []string{"iterate over a string", "iterate over a number", "iterate over a boolean"} {
		if !strings.Contains(ret, want) {
			t.Fatalf("got %q, missing %q", ret, want)
		}
	}
}

func TestIter_CustomIterator(t *testing.T) {
	// Custom iterator: returns a step counter value plus a state-control.
	// This exercises the generic for path through Lua-level iterators.
	ok, ret, errMsg := runIter(t, "custom_iter", `
		local function iter(_, i)
			i = i + 1
			if i <= 4 then
				return i, i * 10
			end
		end
		local s = ""
		for i, v in iter, nil, 0 do
			s = s .. tostring(i) .. "=" .. tostring(v) .. ";"
		end
		return s
	`)
	if !ok {
		t.Fatalf("runtime error: %s", errMsg)
	}
	want := "1=10;2=20;3=30;4=40;"
	if ret != want {
		t.Fatalf("got %q, want %q", ret, want)
	}
	if strings.Contains(ret, "nil") {
		t.Fatalf("unexpected nil in result: %q", ret)
	}
}
