// Copyright (c) luaugo contributors. Licensed under the MIT License.

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func runWith(t *testing.T, argv []string, stdin string) (int, string, string) {
	t.Helper()
	var so, se bytes.Buffer
	si := strings.NewReader(stdin)
	code := run(argv, si, &so, &se)
	return code, so.String(), se.String()
}

func TestParsedAstIsValidJSON(t *testing.T) {
	p := writeTemp(t, "smoke.luau", "local x = 42\nprint(x)\n")
	code, out, errOut := runWith(t, []string{"luau-ast", p}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, errOut)
	}
	// Top-level shape is { "root": ..., "commentLocations": [...] }.
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	root, ok := doc["root"].(map[string]any)
	if !ok {
		t.Fatalf("missing 'root' object: %v", doc)
	}
	if root["type"] != "AstStatBlock" {
		t.Fatalf("root.type = %v, want AstStatBlock", root["type"])
	}
	if _, ok := doc["commentLocations"]; !ok {
		t.Errorf("missing commentLocations key")
	}
}

func TestStdinIsAccepted(t *testing.T) {
	// `-` means "read source from stdin", matching upstream.
	code, out, errOut := runWith(t, []string{"luau-ast", "-"}, "return 1\n")
	if code != 0 {
		t.Fatalf("stdin path exit %d, stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "AstStatBlock") {
		t.Fatalf("unexpected output for stdin source: %q", out)
	}
}

func TestSyntaxErrorReportedAndNonZero(t *testing.T) {
	p := writeTemp(t, "bad.luau", "local x = !!@\n")
	code, _, errOut := runWith(t, []string{"luau-ast", p}, "")
	if code == 0 {
		t.Fatalf("syntax error returned 0, want non-zero")
	}
	if !strings.Contains(errOut, "Parse errors") {
		t.Fatalf("missing 'Parse errors' diagnostic in stderr: %q", errOut)
	}
}

func TestUsageWhenNoArgs(t *testing.T) {
	code, _, errOut := runWith(t, []string{"luau-ast"}, "")
	if code == 0 {
		t.Fatalf("no args accepted, want non-zero")
	}
	if !strings.Contains(errOut, "Usage:") {
		t.Fatalf("missing usage banner: %q", errOut)
	}
}
