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

	var results []outcome

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !(strings.HasSuffix(name, ".lua") || strings.HasSuffix(name, ".luau")) {
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
		case <-time.After(5 * time.Second):
			oc.Status = "TIMEOUT"
			oc.Detail = "exceeded 5s; possible infinite loop"
		}
		oc.Elapsed = time.Since(start)
		results = append(results, oc)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })

	// Report.
	var compileOK, loadOK, runOK, panics, timeouts int
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
		}
		t.Logf("%-40s %-13s %6dms   %s",
			truncate(oc.Name, 40),
			oc.Status,
			oc.Elapsed.Milliseconds(),
			truncate(oc.Detail, 80))
	}
	t.Logf("--------------------------------------------------------------------------")
	t.Logf("totals: %d fixtures", len(results))
	t.Logf("  compiled clean (luaugo compiler):                 %d / %d", compileOK, len(results))
	t.Logf("  loaded clean   (luaugo VM):                       %d / %d", loadOK, len(results))
	t.Logf("  main chunk ran to completion on luaugo VM:        %d / %d", runOK, len(results))
	t.Logf("  goroutine panics during luaugo VM execution:      %d", panics)
	t.Logf("  fixture timeouts (>5s):                           %d", timeouts)

	// Gate: every fixture must at least compile and load on our own VM.
	if compileOK < len(results) {
		t.Errorf("luaugo compiler regressed: only %d / %d fixtures compiled", compileOK, len(results))
	}
	if loadOK < len(results) {
		t.Errorf("luaugo VM loader regressed: only %d / %d fixtures loaded", loadOK, len(results))
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
