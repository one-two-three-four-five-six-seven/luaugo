// Copyright (c) luaugo contributors. Licensed under the MIT License.

package tests

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// TestLuaugoVMSuite is the companion of TestLuauConformanceSuite: every
// fixture in tests/conformance/ is compiled by luaugo's compiler and
// executed by luaugo's OWN virtual machine (not the upstream VM).
//
// This is the in-process correctness gate. It exercises:
//   - the AST parser on the full upstream corpus,
//   - the compiler producing bytecode that our VM accepts,
//   - the VM's interpreter, GC, and standard library implementations.
//
// Per-fixture outcome:
//   COMPILE_ERROR : pkg/compiler refused the source.
//   LOAD_ERROR    : pkg/vm.State.Load rejected the bytecode.
//   PANIC         : the VM goroutine panicked at runtime (a Go panic,
//                   not a Luau error -- almost always our bug).
//   RUNTIME_ERR   : pkg/vm.State.PCall returned non-OK status. Most
//                   conformance fixtures hit this because they assert
//                   on harness-only globals like Tester() that
//                   bcrunner / our VM do not provide.
//   OK            : main chunk returned cleanly.
//
// The hard pass criterion is the COMPILE+LOAD success rate, which
// measures the COMPILER and BYTECODE LOADER specifically (independent
// of any stdlib gap). Runtime success is reported but not gated since
// many fixtures need the upstream conformance harness.
func TestLuaugoVMSuite(t *testing.T) {
	conformDir := "conformance"
	entries, err := os.ReadDir(conformDir)
	if err != nil {
		t.Fatalf("read %s: %v", conformDir, err)
	}

	type outcome struct {
		Name    string
		Status  string
		Detail  string
		Elapsed time.Duration
	}

	// skipFixtures mirrors upstream's TEST_CASE gating: fixtures that
	// upstream itself only runs when specific feature flags are on or
	// when native codegen is enabled. luaugo is a pure-VM port with no
	// native codegen and does not implement the experimental integer
	// type, so these tests are structurally inapplicable; reporting
	// them as failures would be misleading. The justification per
	// fixture is documented inline below and mirrors the upstream
	// gating in tests/Conformance.test.cpp.
	skipFixtures := map[string]string{
		// Conformance.test.cpp:1214 gates "integers.luau" behind
		// FFlag::LuauIntegerType && FFlag::LuauIntegerLibrary. These
		// flags introduce a separate TInteger value tag and an
		// `integer` global library; both are experimental and not
		// present in the stable Luau VM that luaugo targets.
		"integers.luau": "upstream gate: FFlag::LuauIntegerType && FFlag::LuauIntegerLibrary (experimental integer value type)",
		// Conformance.test.cpp:1224 additionally gates
		// "integers_regspill.luau" behind `codegen &&
		// luau_codegen_supported()` -- it depends on the native
		// codegen register-spill path, which luaugo does not have.
		"integers_regspill.luau": "upstream gate: FFlag::LuauIntegerType + native codegen (register spill paths)",
		// Conformance.test.cpp:3866 gates "native_types.luau" behind
		// `codegen && luau_codegen_supported()`, with the comment
		// "This tests requires code to run natively, otherwise all
		// 'is_native' checks will fail". The test asserts on runtime
		// type guards emitted by the native code generator on
		// function entry; luaugo's interpreter has no such guards.
		"native_types.luau": "upstream gate: codegen && luau_codegen_supported() (native entry type guards)",
	}

	var results []outcome

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !(strings.HasSuffix(name, ".lua") || strings.HasSuffix(name, ".luau")) {
			continue
		}
		if reason, ok := skipFixtures[name]; ok {
			results = append(results, outcome{Name: name, Status: "SKIP", Detail: reason})
			continue
		}
		src, err := os.ReadFile(filepath.Join(conformDir, name))
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}

		oc := outcome{Name: name}
		start := time.Now()
		// Run each fixture with its own per-test timeout so a hang on
		// one fixture doesn't pin the whole suite. We do this by
		// running the body in a goroutine guarded by a select.
		done := make(chan struct{})
		var s, d string
		go func() {
			defer close(done)
			s, d = runOnLuaugoVM(name, src)
		}()
		select {
		case <-done:
			oc.Status, oc.Detail = s, d
		case <-time.After(30 * time.Second):
			oc.Status = "TIMEOUT"
			oc.Detail = "exceeded 30s; possible infinite loop"
		}
		oc.Elapsed = time.Since(start)
		results = append(results, oc)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })

	// Report.
	var compileOK, loadOK, runOK, panics, timeouts, skipped int
	t.Logf("")
	t.Logf("luaugo compiler + luaugo VM, per conformance fixture")
	t.Logf("--------------------------------------------------------------------------")
	t.Logf("%-40s %-13s %-9s  %s", "fixture", "status", "ms", "detail")
	t.Logf("--------------------------------------------------------------------------")
	for _, oc := range results {
		switch oc.Status {
		case "OK":
			compileOK++
			loadOK++
			runOK++
		case "RUNTIME_ERR":
			compileOK++
			loadOK++
		case "PANIC":
			compileOK++
			loadOK++
			panics++
		case "LOAD_ERROR":
			compileOK++
		case "TIMEOUT":
			compileOK++
			loadOK++
			timeouts++
		case "SKIP":
			skipped++
		}
		t.Logf("%-40s %-13s %6dms   %s",
			truncate(oc.Name, 40),
			oc.Status,
			oc.Elapsed.Milliseconds(),
			truncate(oc.Detail, 80))
	}
	// Denominators exclude SKIPs: those fixtures aren't applicable to
	// our pure-VM runtime (see skipFixtures map above for upstream
	// gating justification).
	denom := len(results) - skipped
	t.Logf("--------------------------------------------------------------------------")
	t.Logf("totals: %d fixtures (%d applicable, %d skipped as native-only / flagged)", len(results), denom, skipped)
	t.Logf("  compiled clean (luaugo compiler):                 %d / %d", compileOK, denom)
	t.Logf("  loaded clean   (luaugo VM):                       %d / %d", loadOK, denom)
	t.Logf("  main chunk ran to completion on luaugo VM:        %d / %d", runOK, denom)
	t.Logf("  goroutine panics during luaugo VM execution:      %d", panics)
	t.Logf("  fixture timeouts (>5s):                           %d", timeouts)

	// Gate: every applicable fixture must at least compile and load.
	if compileOK < denom {
		t.Errorf("luaugo compiler regressed: only %d / %d applicable fixtures compiled", compileOK, denom)
	}
	if loadOK < denom {
		t.Errorf("luaugo VM loader regressed: only %d / %d applicable fixtures loaded", loadOK, denom)
	}
}

// runOnLuaugoVM compiles src with luaugo's compiler and runs the
// resulting bytecode on luaugo's VM. Returns a coarse status code and
// a single-line detail string.
//
// All Go panics inside the VM are caught so a single bad fixture does
// not fail the rest of the suite.
func runOnLuaugoVM(name string, src []byte) (status, detail string) {
	defer func() {
		if r := recover(); r != nil {
			status = "PANIC"
			one := strings.SplitN(fmt.Sprint(r), "\n", 2)[0]
			// First non-runtime stack frame is most informative.
			loc := firstUserFrame(debug.Stack())
			if loc != "" {
				detail = one + " @ " + loc
			} else {
				detail = one
			}
		}
	}()

	blob, err := compiler.CompileBinary(name, src, compiler.Defaults())
	if err != nil {
		return "COMPILE_ERROR", err.Error()
	}
	if len(blob) > 0 && blob[0] == 0 {
		return "COMPILE_ERROR", "compile error blob: " + string(blob[1:])
	}

	s := vm.NewState()
	defer s.Close()

	// Redirect lib.Stdout so we don't pollute test output if anything
	// the script calls writes there. Sink to /dev/null equivalent.
	prev := lib.Stdout
	defer func() { lib.Stdout = prev }()
	lib.Stdout = io.Discard

	lib.OpenAll(s)
	installConformanceShims(s)

	// Sandbox the thread so the fixture runs in a fresh writable
	// globals table backed by the real globals via __index. Upstream's
	// conformance harness does the same (luaL_sandbox +
	// luaL_sandboxthread in Conformance.test.cpp:310-313). Several
	// fixtures depend on this -- notably tables.luau:249-263 which
	// clears most of `_G` and then expects to keep reading bit32,
	// io, etc. through the __index fall-through.
	s.SandboxThread()
	// Re-bind `_G` to the now-sandboxed globals table. OpenBase
	// installed the original direct binding before SandboxThread
	// swapped the live globals out from under it; without this update
	// `_G[k] = nil` would mutate the underlying parent globals rather
	// than the sandbox copy and shadowing wouldn't work.
	s.PushGlobalsTable()
	s.SetGlobal("_G")

	if err := s.Load(name, blob, 0); err != nil {
		return "LOAD_ERROR", err.Error()
	}
	st := s.PCall(0, 0, 0)
	if st == vm.StatusOK {
		return "OK", ""
	}
	msg, _ := s.ToString(-1)
	return "RUNTIME_ERR", oneline(msg)
}

func oneline(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

func firstUserFrame(stack []byte) string {
	// Pick the first "luaugo/pkg/..." source line out of the stack
	// trace -- the most useful hint when a fixture panics.
	scanner := bytes.NewReader(stack)
	buf := make([]byte, 0, len(stack))
	tmp := make([]byte, 256)
	for {
		n, err := scanner.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	for _, line := range strings.Split(string(buf), "\n") {
		l := strings.TrimSpace(line)
		if strings.Contains(l, "luaugo/pkg/vm") || strings.Contains(l, "luaugo/pkg/compiler") {
			// Want only the "/path/file.go:NN" half.
			if idx := strings.LastIndex(l, " "); idx >= 0 {
				return l[idx+1:]
			}
			return l
		}
	}
	return ""
}
