// Copyright (c) luaugo contributors. Licensed under the MIT License.

package compiler

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/upstreamvm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"
)

// compileAndEncode is a test helper: compile source → encode → return blob.
func compileAndEncode(t *testing.T, source string) []byte {
	t.Helper()
	blob, err := CompileBinary("test", []byte(source), Defaults())
	if err != nil {
		t.Fatalf("CompileBinary(%q) failed: %v", source, err)
	}
	return blob
}

func runOnUpstream(t *testing.T, source string) upstreamvm.Result {
	t.Helper()
	upstreamvm.RequireAvailable(t)
	blob := compileAndEncode(t, source)
	return upstreamvm.MustRun(t, blob)
}

func TestCompileEmptyChunk(t *testing.T) {
	m, err := CompileSource("test", []byte(""), Defaults())
	if err != nil {
		t.Fatalf("CompileSource: %v", err)
	}
	if len(m.Protos) == 0 {
		t.Fatalf("expected at least one proto")
	}
	main := m.Protos[m.MainProto]
	if main == nil {
		t.Fatalf("nil main proto")
	}
	if _, err := bytecode.Encode(m, bytecode.EncodeOptions{}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
}

func TestCompileSimpleReturn(t *testing.T) {
	res := runOnUpstream(t, "return 1")
	if !res.LoadOK() {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "1") {
		t.Fatalf("stdout=%q want contains 1", res.Stdout)
	}
}

func TestCompilePrint(t *testing.T) {
	res := runOnUpstream(t, `print("hello")`)
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "hello") {
		t.Fatalf("stdout=%q want contains hello", res.Stdout)
	}
}

func TestCompileLocal(t *testing.T) {
	res := runOnUpstream(t, "local x = 1 return x")
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "1") {
		t.Fatalf("stdout=%q want contains 1", res.Stdout)
	}
}

func TestCompileArith(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"return 1+2", "3"},
		{"return (1+2)*3", "9"},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			res := runOnUpstream(t, tc.src)
			if res.Status != upstreamvm.StatusOK {
				t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
			}
			if !strings.Contains(res.Stdout, tc.want) {
				t.Fatalf("stdout=%q want %q", res.Stdout, tc.want)
			}
		})
	}
}

func TestCompileBuiltinFastcall(t *testing.T) {
	res := runOnUpstream(t, "return math.abs(-5)")
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "5") {
		t.Fatalf("stdout=%q want contains 5", res.Stdout)
	}
}

func TestCompileTableConstruct(t *testing.T) {
	res := runOnUpstream(t, "local t = {1,2,3}; return t[1]+t[2]+t[3]")
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "6") {
		t.Fatalf("stdout=%q want 6", res.Stdout)
	}
}

func TestCompileFunctionCall(t *testing.T) {
	res := runOnUpstream(t, "local function f(x) return x*2 end; return f(21)")
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "42") {
		t.Fatalf("stdout=%q want 42", res.Stdout)
	}
}

func TestCompileIfElse(t *testing.T) {
	res := runOnUpstream(t, `if 1 < 2 then return "less" else return "not" end`)
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "less") {
		t.Fatalf("stdout=%q want less", res.Stdout)
	}
}

func TestCompileLoops(t *testing.T) {
	res := runOnUpstream(t, "local s = 0; for i = 1, 10 do s = s + i end; return s")
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "55") {
		t.Fatalf("stdout=%q want 55", res.Stdout)
	}
}

func TestCompileWhile(t *testing.T) {
	res := runOnUpstream(t, "local i = 1; while i < 5 do i = i + 1 end; return i")
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "5") {
		t.Fatalf("stdout=%q want 5", res.Stdout)
	}
}

func TestCompileGenericFor(t *testing.T) {
	res := runOnUpstream(t, "local t = {10,20,30}; local s = 0; for _, v in ipairs(t) do s = s + v end; return s")
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "60") {
		t.Fatalf("stdout=%q want 60", res.Stdout)
	}
}

func TestCompileClosure(t *testing.T) {
	res := runOnUpstream(t, "local function adder(n) return function(x) return x+n end end; return adder(7)(35)")
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "42") {
		t.Fatalf("stdout=%q want 42", res.Stdout)
	}
}

func TestCompileStringConcat(t *testing.T) {
	res := runOnUpstream(t, `return "hello" .. " " .. "world"`)
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "hello world") {
		t.Fatalf("stdout=%q want hello world", res.Stdout)
	}
}

func TestCompileMethodCall(t *testing.T) {
	res := runOnUpstream(t, `local s = "Hello"; return s:lower()`)
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q", res.Status, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "hello") {
		t.Fatalf("stdout=%q want hello", res.Stdout)
	}
}

// ----------------------------------------------------------------------
// Conformance corpus walkers
// ----------------------------------------------------------------------

func conformancePaths(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("..", "..", "tests", "conformance", "*.luau"))
	if err != nil || len(matches) == 0 {
		// alt path
		matches, err = filepath.Glob(filepath.Join("../..", "tests", "conformance", "*.luau"))
		if err != nil {
			t.Fatalf("glob: %v", err)
		}
	}
	sort.Strings(matches)
	return matches
}

func TestCompileAllConformance(t *testing.T) {
	paths := conformancePaths(t)
	if len(paths) == 0 {
		t.Skip("no conformance fixtures present")
	}
	type result struct {
		name string
		ok   bool
		msg  string
	}
	results := make([]result, 0, len(paths))
	for _, p := range paths {
		name := filepath.Base(p)
		data, err := os.ReadFile(p)
		if err != nil {
			results = append(results, result{name, false, "read: " + err.Error()})
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					results = append(results, result{name, false, "panic: " + asString(r) + "\n" + string(debug.Stack())})
				}
			}()
			if _, err := CompileSource(name, data, Defaults()); err != nil {
				results = append(results, result{name, false, "compile: " + err.Error()})
				return
			}
			results = append(results, result{name, true, ""})
		}()
	}
	clean := 0
	for _, r := range results {
		if r.ok {
			clean++
		}
	}
	t.Logf("Compile-clean: %d / %d", clean, len(results))
	for _, r := range results {
		if !r.ok {
			msg := r.msg
			if len(msg) > 200 {
				msg = msg[:200] + "..."
			}
			t.Logf("  FAIL %s: %s", r.name, msg)
		}
	}
	if clean < 45 {
		t.Errorf("compile-clean count %d < required 45", clean)
	}
}

func TestRunAllConformance(t *testing.T) {
	upstreamvm.RequireAvailable(t)
	paths := conformancePaths(t)
	if len(paths) == 0 {
		t.Skip("no conformance fixtures present")
	}
	type result struct {
		name   string
		status string
		msg    string
		loadOk bool
	}
	results := make([]result, 0, len(paths))
	for _, p := range paths {
		name := filepath.Base(p)
		data, err := os.ReadFile(p)
		if err != nil {
			results = append(results, result{name, "READ_FAIL", err.Error(), false})
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					results = append(results, result{name, "COMPILE_PANIC", asString(r), false})
				}
			}()
			blob, err := CompileBinary(name, data, Defaults())
			if err != nil {
				results = append(results, result{name, "COMPILE_ERR", err.Error(), false})
				return
			}
			res, err := upstreamvm.Run(blob)
			if err != nil {
				results = append(results, result{name, "RUN_ERR", err.Error(), false})
				return
			}
			var status string
			switch res.Status {
			case upstreamvm.StatusOK:
				status = "OK"
			case upstreamvm.StatusLoadError:
				status = "LOAD_ERR"
			case upstreamvm.StatusRuntimeError:
				status = "RUNTIME_ERR"
			default:
				status = "UNKNOWN"
			}
			results = append(results, result{name, status, res.Stderr, res.LoadOK()})
		}()
	}
	loadOK := 0
	for _, r := range results {
		if r.loadOk {
			loadOK++
		}
	}
	t.Logf("Load-OK: %d / %d", loadOK, len(results))
	for _, r := range results {
		msg := r.msg
		if len(msg) > 200 {
			msg = msg[:200] + "..."
		}
		t.Logf("  %-25s %s  %s", r.name, r.status, msg)
	}
	if loadOK < 40 {
		t.Errorf("load-OK count %d < required 40", loadOK)
	}
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}
