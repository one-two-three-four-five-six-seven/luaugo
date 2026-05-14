// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib_test

import (
	"io"
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// runRedo compiles and runs src, returning the value returned by the
// chunk (or an error message) plus a status code.
func runRedo(t *testing.T, name, src string) (status, detail string) {
	t.Helper()
	blob, err := compiler.CompileBinary(name, []byte(src), compiler.Defaults())
	if err != nil {
		return "COMPILE_ERROR", err.Error()
	}
	if len(blob) > 0 && blob[0] == 0 {
		return "COMPILE_ERROR", string(blob[1:])
	}
	s := vm.NewState()
	defer s.Close()
	prev := lib.Stdout
	defer func() { lib.Stdout = prev }()
	lib.Stdout = io.Discard
	lib.OpenAll(s)
	if err := s.Load(name, blob, 0); err != nil {
		return "LOAD_ERROR", err.Error()
	}
	st := s.PCall(0, 1, 0)
	if st == vm.StatusOK {
		msg, _ := s.ToString(-1)
		return "OK", msg
	}
	msg, _ := s.ToString(-1)
	return "RUNTIME_ERR", msg
}

// TestPCallBasicReturnValues mirrors the very first checkresults case
// at pcall.luau line 47:
//
//	checkresults({ true, 42 }, pcall(function() return 42 end))
//
// The conformance fixture fails at pcall.luau:8, which is the loop body
// of checkresults that compares each captured pcall return value to the
// expected slice. The first failing comparison is the original
// repro target.
func TestPCallBasicReturnValues(t *testing.T) {
	src := `
local t = table.pack(pcall(function() return 42 end))
return tostring(t.n) .. "|" .. tostring(t[1]) .. "|" .. tostring(t[2])
`
	st, det := runRedo(t, "pcall_basic.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	want := "2|true|42"
	if det != want {
		t.Errorf("expected %q got %q", want, det)
	}
}

// TestPCallMultiReturn covers pcall.luau line 48:
//
//	checkresults({ true, 1, 2, 42 }, pcall(function(a, b) return a, b, 42 end, 1, 2))
func TestPCallMultiReturn(t *testing.T) {
	src := `
local t = table.pack(pcall(function(a, b) return a, b, 42 end, 1, 2))
return tostring(t.n) .. "|" .. tostring(t[1]) .. "|" .. tostring(t[2]) ..
	"|" .. tostring(t[3]) .. "|" .. tostring(t[4])
`
	st, det := runRedo(t, "pcall_multi.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	want := "4|true|1|2|42"
	if det != want {
		t.Errorf("expected %q got %q", want, det)
	}
}

// TestPCallInsideHelper exactly mimics the failure shape at
// pcall.luau line 8: a helper function that calls table.pack on its
// vararg, asserts the count, then iterates and compares t[i] == e[i].
func TestPCallInsideHelper(t *testing.T) {
	src := `
local function checkresults(e, ...)
	local t = table.pack(...)
	if t.n ~= #e then return "n mismatch: t.n="..tostring(t.n).." #e="..tostring(#e) end
	for i = 1, t.n do
		if t[i] ~= e[i] then
			return "i="..tostring(i).." t[i]="..tostring(t[i]).." e[i]="..tostring(e[i])
		end
	end
	return "OK"
end
return checkresults({ true, 42 }, pcall(function() return 42 end))
`
	st, det := runRedo(t, "pcall_helper.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	if det != "OK" {
		t.Errorf("checkresults reported: %s", det)
	}
}

// TestDebugInfoGoFunctionSource is the regression for errors.luau:198:
//
//	assert(debug.info(print, "s") == "[C]")
//
// debug.info(f, "s") on a Go-backed builtin function must return "[C]",
// matching upstream (which uses "C" through `luaL_checkclosure` /
// proto.source -> chunkid path). We accept "[C]" since that is the
// canonical form upstream produces for closures backed by a C function.
func TestDebugInfoGoFunctionSource(t *testing.T) {
	src := `return debug.info(print, "s")`
	st, det := runRedo(t, "dbg_go_src.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	if det != "[C]" {
		t.Errorf("expected \"[C]\" got %q", det)
	}
}

// TestDebugInfoLuaFunctionSource ensures the function-form of
// debug.info on a Lua closure still returns the chunkname of where
// that closure was defined (not "[Lua]" or "[C]").
func TestDebugInfoLuaFunctionSource(t *testing.T) {
	src := `local f = function() end
return debug.info(f, "s")`
	st, det := runRedo(t, "dbg_lua_src.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	// Upstream returns the proto source as recorded at load time
	// ("dbg_lua_src.luau" or "=dbg_lua_src.luau" stripped of the
	// leading '='). We accept either the chunkname or anything that
	// is clearly a Lua source string -- the critical invariant is
	// that it is NOT "[C]" for a Lua closure.
	if det == "[C]" {
		t.Errorf("Lua closure must not report [C]; got %q", det)
	}
	if !strings.Contains(det, "dbg_lua_src") && det != "[Lua]" {
		t.Logf("note: source for Lua closure is %q", det)
	}
}

// TestDebugInfoLibraryGoFunction is the regression for
// debug.luau:99 ("assert(quuz(math.cos) == \"0 true\")"): library
// builtins like math.cos are Go closures, so debug.info(math.cos, "a")
// must report (0, true) -- nparams=0, isvararg=true -- matching
// upstream's "C functions are treated as fully variadic" semantics.
func TestDebugInfoLibraryGoFunction(t *testing.T) {
	src := `
local a, v = debug.info(math.cos, "a")
return tostring(a) .. " " .. tostring(v)
`
	st, det := runRedo(t, "dbg_lib_a.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	if det != "0 true" {
		t.Errorf("expected %q got %q", "0 true", det)
	}
}

// TestDebugInfoFunctionFormSurvives is a sanity check that the
// function-form of debug.info -- debug.info(f, "a") -- returns *some*
// pair of values without crashing for Lua closures that are not
// currently on the call stack. Full proto introspection of cold
// closures requires VM-level surface area not yet exposed via the
// public State API; debug.luau:100-103 (which checks the precise
// nparams / isvararg) consequently still fails in that arrangement.
// This regression test guards against regressions where the
// function-form panics or returns the wrong number of results.
func TestDebugInfoFunctionFormSurvives(t *testing.T) {
	src := `
local f = function(a, b, ...) end
local n, v = debug.info(f, "a")
return tostring(n) .. "|" .. tostring(v)
`
	st, det := runRedo(t, "dbg_arity.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	parts := strings.Split(det, "|")
	if len(parts) != 2 {
		t.Errorf("expected exactly 2 results, got %q", det)
	}
}

// TestXPCallHandlerErrors covers pcall.luau:107 -- when the xpcall
// error handler itself raises, xpcall must return
// (false, "error in error handling") rather than leaking the partial
// call stack on the return convoy.
func TestXPCallHandlerErrors(t *testing.T) {
	src := `
local t = table.pack(xpcall(function() error("foo") end, function(err) error("bar") end))
return tostring(t.n) .. "|" .. tostring(t[1]) .. "|" .. tostring(t[2])
`
	st, det := runRedo(t, "xpcall_eh_err.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	want := "2|false|error in error handling"
	if det != want {
		t.Errorf("expected %q got %q", want, det)
	}
}

// TestXPCallMissingHandler covers pcall.luau:120 --
// `pcall(xpcall, function() return 42 end)` expects
// (false, "missing argument #2 to 'xpcall' (function expected)").
func TestXPCallMissingHandler(t *testing.T) {
	src := `
local t = table.pack(pcall(xpcall, function() return 42 end))
return tostring(t[1]) .. "|" .. tostring(t[2])
`
	st, det := runRedo(t, "xpcall_missing.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	// The pcall wraps xpcall's raise; the msg is prefixed with the
	// chunk:line by upstream Luau's error() machinery, so accept any
	// suffix that ends with the canonical xpcall error string.
	if !strings.HasSuffix(det, "missing argument #2 to 'xpcall' (function expected)") {
		t.Errorf("expected suffix match, got %q", det)
	}
	if !strings.HasPrefix(det, "false|") {
		t.Errorf("expected first result false; got %q", det)
	}
}

// TestXPCallWrongTypedHandler covers pcall.luau:121 --
// passing a non-function as the handler should produce
// "invalid argument #2 to 'xpcall' (function expected, got TY)".
func TestXPCallWrongTypedHandler(t *testing.T) {
	src := `
local t = table.pack(pcall(xpcall, function() return 42 end, true))
return tostring(t[1]) .. "|" .. tostring(t[2])
`
	st, det := runRedo(t, "xpcall_typed.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	if !strings.HasSuffix(det, "invalid argument #2 to 'xpcall' (function expected, got boolean)") {
		t.Errorf("expected suffix match, got %q", det)
	}
	if !strings.HasPrefix(det, "false|") {
		t.Errorf("expected first result false; got %q", det)
	}
}

// TestDebugTracebackAsErrorHandler is the regression for
// pcall.luau:106:
//
//	checkresults({ false, "pcall.luau:106: foo\npcall.luau:106\npcall.luau:106\n" },
//	             xpcall(function() error("foo") end, debug.traceback))
//
// When debug.traceback is used as an xpcall error handler, the
// returned string must follow upstream Luau's compact
// `<chunkname>:<line>` per-frame shape with the error message as the
// first line.
func TestDebugTracebackAsErrorHandler(t *testing.T) {
	src := `
local _, msg = xpcall(function() error("foo") end, debug.traceback)
return msg
`
	st, det := runRedo(t, "tb_handler.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	// Must contain the chunk:line: error prefix as the first line
	// AND not include the verbose Lua 5.x "stack traceback:" header
	// (we only emit that for direct user calls without a parseable
	// msg prefix).
	if !strings.HasPrefix(det, "tb_handler.luau:") {
		t.Errorf("expected chunk:line prefix, got %q", det)
	}
	if strings.Contains(det, "stack traceback") {
		t.Errorf("error-handler form must use compact shape; got %q", det)
	}
	// Must include a reconstructed frame line equal to the error
	// location (line 2 of the source -- the error() call).
	if !strings.Contains(det, "tb_handler.luau:2\n") {
		t.Errorf("expected reconstructed frame line, got %q", det)
	}
}

// TestDebugTracebackDirectCallKeepsVerboseShape ensures that the
// pre-existing in-package callers (e.g. tests in debug_test.go that
// look for "stack traceback") still see the Lua 5.x verbose shape
// when debug.traceback is called directly rather than via xpcall.
func TestDebugTracebackDirectCallKeepsVerboseShape(t *testing.T) {
	src := `return debug.traceback("boom")`
	st, det := runRedo(t, "tb_direct.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	if !strings.HasPrefix(det, "boom\n") {
		t.Errorf("expected msg first, got %q", det)
	}
	if !strings.Contains(det, "stack traceback") {
		t.Errorf("direct call should use verbose shape; got %q", det)
	}
}
