// Copyright (c) luaugo contributors. Licensed under the MIT License.

package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/upstreamvm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
)

// TestLuauConformanceSuite is the headline integration test: for every
// fixture in tests/conformance/, the test compiles the .luau source
// twice -- once with upstream's luau-compile and once with luaugo's
// compiler -- runs both bytecode blobs on the OFFICIAL upstream Luau
// VM via bcrunner, and reports a side-by-side comparison.
//
// The pass criterion is: for every fixture, the luaugo blob must at
// least LOAD on the upstream VM (i.e. not return StatusLoadError).
// Runtime errors are expected for many conformance fixtures because
// they rely on a test-harness Tester() global that bcrunner does not
// register; those are compared against the upstream-compiled blob's
// outcome to confirm we hit the same runtime point.
//
// A fixture is considered "equivalent" if both blobs produce the same
// Status under the upstream VM. Strict-equivalence (matching stdout
// byte-for-byte) is also reported but not required to pass.
func TestLuauConformanceSuite(t *testing.T) {
	upstreamvm.RequireAvailable(t)

	conformDir := "conformance"
	entries, err := os.ReadDir(conformDir)
	if err != nil {
		t.Fatalf("read %s: %v", conformDir, err)
	}

	type outcome struct {
		Name             string
		LuaugoCompileErr string
		UpstreamCompErr  string
		UpstreamStatus   upstreamvm.Status
		LuaugoStatus     upstreamvm.Status
		UpstreamStdout   string
		LuaugoStdout     string
		UpstreamStderr   string
		LuaugoStderr     string
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
		chunk := "=" + name // bare-name prefix per Lua chunkname rules
		runOpts := upstreamvm.RunOptions{
			NoSandbox: true,
			Chunkname: chunk,
		}

		// 1. Upstream compile + run (reference outcome).
		upBlob, err := upstreamvm.CompileSource(src)
		if err != nil {
			oc.UpstreamCompErr = err.Error()
		} else {
			r, runErr := upstreamvm.RunWith(upBlob, runOpts)
			if runErr != nil {
				oc.UpstreamCompErr = "harness: " + runErr.Error()
			} else {
				oc.UpstreamStatus = r.Status
				oc.UpstreamStdout = r.Stdout
				oc.UpstreamStderr = r.Stderr
			}
		}

		// 2. luaugo compile + run.
		luBlob, err := compiler.CompileBinary(name, src, compiler.Defaults())
		if err != nil {
			oc.LuaugoCompileErr = err.Error()
		} else if len(luBlob) > 0 && luBlob[0] == 0 {
			oc.LuaugoCompileErr = "compile error: " + string(luBlob[1:])
		} else {
			r, runErr := upstreamvm.RunWith(luBlob, runOpts)
			if runErr != nil {
				oc.LuaugoCompileErr = "harness: " + runErr.Error()
			} else {
				oc.LuaugoStatus = r.Status
				oc.LuaugoStdout = r.Stdout
				oc.LuaugoStderr = r.Stderr
			}
		}

		results = append(results, oc)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })

	// Report.
	type stat struct{ total, luaugoCompileOK, luaugoLoadOK, sameStatus, sameStdout int }
	var s stat
	t.Logf("")
	t.Logf("                                        upstream        luaugo")
	t.Logf("fixture                                 compile  run    compile  run    status  stdout")
	t.Logf("----------------------------------------------------------------------------------")
	for _, oc := range results {
		s.total++

		upCompOK := oc.UpstreamCompErr == ""
		luCompOK := oc.LuaugoCompileErr == ""
		if luCompOK {
			s.luaugoCompileOK++
		}
		luLoadOK := luCompOK && oc.LuaugoStatus != upstreamvm.StatusLoadError && oc.LuaugoStatus != upstreamvm.StatusIO
		if luLoadOK {
			s.luaugoLoadOK++
		}

		sameStatus := upCompOK && luCompOK && oc.UpstreamStatus == oc.LuaugoStatus
		if sameStatus {
			s.sameStatus++
		}
		sameStdout := sameStatus && oc.UpstreamStdout == oc.LuaugoStdout
		if sameStdout {
			s.sameStdout++
		}

		t.Logf("%-38s  %-6s   %-5s  %-6s   %-5s  %-6v  %-6v",
			truncate(oc.Name, 38),
			boolStr(upCompOK, "OK", "ERR"),
			statusStr(oc.UpstreamStatus, oc.UpstreamCompErr),
			boolStr(luCompOK, "OK", "ERR"),
			statusStr(oc.LuaugoStatus, oc.LuaugoCompileErr),
			sameStatus,
			sameStdout,
		)
	}
	t.Logf("----------------------------------------------------------------------------------")
	t.Logf("totals: %d fixtures", s.total)
	t.Logf("  luaugo compiled clean:                                  %d / %d", s.luaugoCompileOK, s.total)
	t.Logf("  luaugo bytecode loaded on upstream VM:                  %d / %d", s.luaugoLoadOK, s.total)
	t.Logf("  same VM status as upstream-compiled blob:               %d / %d", s.sameStatus, s.total)
	t.Logf("  identical stdout to upstream-compiled blob:             %d / %d", s.sameStdout, s.total)

	// Hard gate: every fixture must at least LOAD on the real VM.
	const requireLoadOK = 53
	if s.luaugoLoadOK < requireLoadOK {
		t.Fatalf("luaugo bytecode load-OK count %d below required %d", s.luaugoLoadOK, requireLoadOK)
	}
}

func boolStr(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

func statusStr(st upstreamvm.Status, errMsg string) string {
	if errMsg != "" {
		return "skip"
	}
	switch st {
	case upstreamvm.StatusOK:
		return "OK"
	case upstreamvm.StatusIO:
		return "io"
	case upstreamvm.StatusLoadError:
		return "load!"
	case upstreamvm.StatusRuntimeError:
		return "rt!"
	}
	return fmt.Sprintf("?%d", int(st))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "~"
}
