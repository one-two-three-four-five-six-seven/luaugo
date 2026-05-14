// Copyright (c) luaugo contributors. Licensed under the MIT License.

package vm_test

import (
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// TestSetfenvIndexFallback reproduces conformance/events.luau:475-477.
//
// After `setfenv(1, setmetatable({}, {__index=_G}))` the chunk's
// environment is an empty table whose `__index` chains back to the
// real `_G`. The next bare-global read `X` (compiled to GETGLOBAL
// or GETIMPORT) must therefore honour `__index`. The conformance
// fixture aborts at this point with
//
//	"attempt to perform arithmetic (add) on a nil value"
//
// because our OpGetGlobal/OpGetImport/OpSetGlobal handlers in
// pkg/vm/execute.go do a *raw* env-table lookup (cl.env.getStr) and
// never consult the metatable, so `X` resolves to nil before the
// add ever runs.
//
// The fix lives in pkg/vm/execute.go (outside this agent's owned
// files); this test pins the contract so the integrator can verify
// the fix.
func TestSetfenvIndexFallback(t *testing.T) {
	out, st, msg := runScript(t, `
X = 20
local _G = getfenv()
setfenv(1, setmetatable({}, {__index=_G}))
X = X + 10
return tostring(X) .. "/" .. tostring(_G.X)
`)
	if st != vm.StatusOK {
		t.Fatalf("run: status=%v msg=%q", st, msg)
	}
	// X is read through __index (so 20+10=30); SETGLOBAL writes to
	// the *new* (overlay) env, leaving _G.X at 20.
	if out != "30/20" {
		t.Fatalf("got %q want %q", out, "30/20")
	}
}

// TestSetfenvSetGlobalRoutesThroughNewEnv ensures SETGLOBAL after
// setfenv writes to the new env (and does NOT poison _G). This is
// the second half of events.luau:477-484.
func TestSetfenvSetGlobalRoutesThroughNewEnv(t *testing.T) {
	out, st, msg := runScript(t, `
B = 30
local _G = getfenv()
setfenv(1, setmetatable({}, {__index=_G}))
B = false
local r1 = tostring(B)
B = nil
local r2 = tostring(B)
return r1 .. "," .. r2 .. "," .. tostring(_G.B)
`)
	if st != vm.StatusOK {
		t.Fatalf("run: status=%v msg=%q", st, msg)
	}
	// B is written into the new env (so reads as false), then nil-
	// erased there and falls through __index back to _G (30). _G.B
	// itself is untouched.
	if out != "false,30,30" {
		t.Fatalf("got %q want %q", out, "false,30,30")
	}
}

// TestArithUnchangedByBaseline confirms that the existing binary
// metamethod dispatch path is healthy (this is the area the
// coordinator hypothesised, but it turned out to be a red
// herring — see TestSetfenvIndexFallback for the real bug).
func TestArithMetamethodHealthy(t *testing.T) {
	out, st, msg := runScript(t, `
local mt = { __add = function(a, b)
    return (type(a) == "table" and a.v or a) + (type(b) == "table" and b.v or b)
end }
local t = setmetatable({v = 7}, mt)
return tostring(t + 10) .. "," .. tostring(3 + t) .. "," .. tostring(t + t)
`)
	if st != vm.StatusOK {
		t.Fatalf("run: status=%v msg=%q", st, msg)
	}
	if out != "17,10,14" {
		t.Fatalf("got %q want %q", out, "17,10,14")
	}
}
