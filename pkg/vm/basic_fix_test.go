// Copyright (c) luaugo contributors. Licensed under the MIT License.

package vm_test

import (
	"io"
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/upstreamvm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// runOnLuaugo compiles src and runs it on luaugo's VM. Returns
// (status, msg) where status is "OK" or "RUNTIME_ERR" and msg is the
// error string (or stdout via output capture in `out`).
func runOnLuaugo(t *testing.T, src string) (string, string) {
	t.Helper()
	blob, err := compiler.CompileBinary("basic_fix_test.luau", []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(blob) > 0 && blob[0] == 0 {
		t.Fatalf("compile error: %s", string(blob[1:]))
	}
	s := vm.NewState()
	defer s.Close()
	prev := lib.Stdout
	defer func() { lib.Stdout = prev }()
	lib.Stdout = io.Discard
	lib.OpenAll(s)
	if err := s.Load("basic_fix_test.luau", blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	st := s.PCall(0, 0, 0)
	if st == vm.StatusOK {
		return "OK", ""
	}
	msg, _ := s.ToString(-1)
	return "RUNTIME_ERR", msg
}

// TestConcatNilErrorFormat reproduces basic.luau:123. The assertion
// matches a regex against the pcall error from `'1' .. nil .. '2'`.
// Upstream Luau formats this as
//
//	"attempt to concatenate nil with string"
//
// whereas the previous luaugo formatting was
//
//	"attempt to concatenate a nil value"
//
// which made the conformance fixture fail at line 123.
func TestConcatNilErrorFormat(t *testing.T) {
	src := `
local function f() return '1' .. nil .. '2' end
local ok, err = pcall(f)
if ok then error("expected pcall to fail") end
if not string.find(err, "attempt to concatenate nil with string") then
  error("unexpected error: " .. tostring(err))
end
`
	status, msg := runOnLuaugo(t, src)
	if status != "OK" {
		t.Fatalf("luaugo VM failed: %s", msg)
	}
}

// TestConcatNilUpstreamReference exercises the upstream VM to record
// the exact expected error message format. Skips if the harness is
// not built.
func TestConcatNilUpstreamReference(t *testing.T) {
	if !upstreamvm.Available() {
		t.Skip("upstream bcrunner not available")
	}
	src := []byte(`
local ok, err = pcall(function() return '1' .. nil .. '2' end)
print("A|" .. tostring(ok) .. "|" .. tostring(err))
local ok2, err2 = pcall(function() return '1' .. {} end)
print("B|" .. tostring(ok2) .. "|" .. tostring(err2))
local ok3, err3 = pcall(function() return {} .. '1' end)
print("C|" .. tostring(ok3) .. "|" .. tostring(err3))
local ok4, err4 = pcall(function() return nil .. true end)
print("D|" .. tostring(ok4) .. "|" .. tostring(err4))
local ok5, err5 = pcall(function() return 1 .. nil .. '2' end)
print("E|" .. tostring(ok5) .. "|" .. tostring(err5))
`)
	r, err := upstreamvm.RunSource(src)
	if err != nil {
		t.Fatalf("upstream RunSource: %v", err)
	}
	t.Logf("upstream stdout: %q", r.Stdout)
	t.Logf("upstream stderr: %q", r.Stderr)
	if !strings.Contains(r.Stdout, "attempt to concatenate") {
		t.Fatalf("unexpected upstream output: %q", r.Stdout)
	}
}

// TestBasicLine53 reproduces the originally-cited failure to verify
// it currently passes (this is a regression guard, not a fix target).
func TestBasicLine53(t *testing.T) {
	src := `
local r = (function() local a, b = 1, {} a, b[a] = 43, -1 return a + b[1] end)()
if r ~= 42 then error("expected 42, got " .. tostring(r)) end
`
	status, msg := runOnLuaugo(t, src)
	if status != "OK" {
		t.Fatalf("luaugo VM failed: %s", msg)
	}
}

// TestNumericForBackwardSimple checks for b=9,1,-2 (5 iterations,
// b takes values 9,7,5,3,1).
func TestNumericForBackwardSimple(t *testing.T) {
	src := `
local a = 1
local seen = {}
for b=9,1,-2 do
  a = a * 2
  seen[#seen+1] = b
end
if a ~= 32 then error("a expected 32, got " .. tostring(a) .. " seen=" .. table.concat(seen, ",")) end
`
	status, msg := runOnLuaugo(t, src)
	if status != "OK" {
		t.Fatalf("luaugo VM failed: %s", msg)
	}
}

// TestBasicLine188 reproduces basic.luau:188 verbatim.
//
// XFAIL: this is the next conformance fixture failure on basic.luau
// AFTER the concat-error-format fix above. The root cause is in the
// compiler, not the VM:
//
//	pkg/compiler/compiler.go (compileFor) declares it uses a 3-register
//	"limit, step, index" layout and binds the user-visible loop variable
//	to the index register (base+2). Upstream Luau (and our VM's
//	OpForNPrep/OpForNLoop) actually use a 4-register layout
//	[limit, step, index, var], where the loop body must reference R(A+3)
//	for the user variable and the compiler must emit a copy MOVE A+3,A+2
//	at the top of each iteration. With the upstream layout `b = nil`
//	stores into R(A+3) and the internal index at R(A+2) is preserved;
//	with our compiler's layout `b = nil` clobbers the index slot and
//	terminates the loop after one iteration.
//
// Cross-check: TestBasicLine188UpstreamCompileLuaugoVM verifies that
// the upstream-compiled bytecode runs correctly on our VM, isolating
// the bug to the compiler. This file's owned-files list does not
// include pkg/compiler/* so it is reported here and not fixed.
func TestBasicLine188(t *testing.T) {
	t.Skip("known compiler-side bug: pkg/compiler/compiler.go compileFor binds the loop var to the index register (3-register layout) instead of emitting upstream's 4-register layout with a per-iteration MOVE A+3,A+2; out of scope for this fix-basic agent (owned files are pkg/vm/{arith,compare,object}.go only)")
	src := `
local r = (function() local a = 1 for b=9,1,-2 do a = a * 2 b = nil end return a end)()
if r ~= 32 then error("expected 32, got " .. tostring(r)) end
`
	status, msg := runOnLuaugo(t, src)
	if status != "OK" {
		t.Fatalf("luaugo VM failed: %s", msg)
	}
}

// TestBasicLine188Upstream compares against the upstream VM.
func TestBasicLine188Upstream(t *testing.T) {
	if !upstreamvm.Available() {
		t.Skip("upstream bcrunner not available")
	}
	src := []byte(`
local r = (function() local a = 1 for b=9,1,-2 do a = a * 2 b = nil end return a end)()
print("r=" .. tostring(r))
local capt = {}
for b=9,1,-2 do
  table.insert(capt, tostring(b))
end
print("plain=" .. table.concat(capt, ","))
local capt2 = {}
for b=9,1,-2 do
  table.insert(capt2, tostring(b))
  b = nil
end
print("nild=" .. table.concat(capt2, ","))
`)
	r, err := upstreamvm.RunSource(src)
	if err != nil {
		t.Fatalf("upstream: %v", err)
	}
	t.Logf("upstream stdout: %q stderr: %q", r.Stdout, r.Stderr)
}

// TestBasicLine188CrossCheck: compile w/ luaugo, run on upstream VM.
// If THIS passes but TestBasicLine188 fails, the bug is in our VM.
func TestBasicLine188CrossCheck(t *testing.T) {
	if !upstreamvm.Available() {
		t.Skip("upstream bcrunner not available")
	}
	src := []byte(`
local r = (function() local a = 1 for b=9,1,-2 do a = a * 2 b = nil end return a end)()
print("r=" .. tostring(r))
`)
	blob, err := compiler.CompileBinary("repro.luau", src, compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	r, err := upstreamvm.Run(blob)
	if err != nil {
		t.Fatalf("upstream Run: %v", err)
	}
	t.Logf("luaugo-compile + upstream-VM: status=%d stdout=%q stderr=%q", r.Status, r.Stdout, r.Stderr)
}

// TestBasicLine188DiffBytecode dumps both compilers' bytecode for the
// failing fragment to a t.Log so we can inspect what differs.
func TestBasicLine188DiffBytecode(t *testing.T) {
	if !upstreamvm.Available() {
		t.Skip("upstream luau-compile not available")
	}
	src := []byte(`
local a = 1
for b=9,1,-2 do
  a = a * 2
  b = nil
end
return a
`)
	ours, err := compiler.CompileBinary("repro.luau", src, compiler.Defaults())
	if err != nil {
		t.Fatalf("our compile: %v", err)
	}
	theirs, err := upstreamvm.CompileSource(src)
	if err != nil {
		t.Skipf("upstream compile: %v", err)
	}
	t.Logf("ours bytes  (%d): % x", len(ours), ours)
	t.Logf("theirs bytes(%d): % x", len(theirs), theirs)
	if mod, err := bytecode.Decode(ours); err == nil {
		t.Logf("--- OURS disasm ---\n%s", bytecode.Disassemble(mod))
	} else {
		t.Logf("decode ours: %v", err)
	}
	if mod, err := bytecode.Decode(theirs); err == nil {
		t.Logf("--- THEIRS disasm ---\n%s", bytecode.Disassemble(mod))
	} else {
		t.Logf("decode theirs: %v", err)
	}
}

// TestBasicLine188UpstreamCompileLuaugoVM: upstream compile -> our VM.
// If THIS passes, the compiler emits the wrong bytecode and we can't fix
// it here. If THIS fails too, the bug is in our VM and we own the fix.
func TestBasicLine188UpstreamCompileLuaugoVM(t *testing.T) {
	if !upstreamvm.Available() {
		t.Skip("upstream luau-compile not available")
	}
	src := []byte(`
local r = (function() local a = 1 for b=9,1,-2 do a = a * 2 b = nil end return a end)()
return r
`)
	blob, err := upstreamvm.CompileSource(src)
	if err != nil {
		t.Skipf("upstream compile not available: %v", err)
	}
	s := vm.NewState()
	defer s.Close()
	prev := lib.Stdout
	defer func() { lib.Stdout = prev }()
	lib.Stdout = io.Discard
	lib.OpenAll(s)
	if err := s.Load("repro.luau", blob, 0); err != nil {
		t.Fatalf("our VM load: %v", err)
	}
	st := s.PCall(0, 1, 0)
	if st != vm.StatusOK {
		msg, _ := s.ToString(-1)
		t.Fatalf("our VM pcall failed: %s", msg)
	}
	num, _ := s.ToNumber(-1)
	t.Logf("upstream-compile + luaugo-VM: r=%v", num)
	if num != 32 {
		t.Fatalf("expected 32 got %v", num)
	}
}
