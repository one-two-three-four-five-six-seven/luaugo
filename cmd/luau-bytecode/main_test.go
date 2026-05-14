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

func runWith(t *testing.T, argv []string) (int, string, string) {
	t.Helper()
	var so, se bytes.Buffer
	code := run(argv, &so, &se)
	return code, so.String(), se.String()
}

func TestSummaryIsValidJSON(t *testing.T) {
	// Emit a summary into a temp file and confirm it parses as JSON
	// with the upstream-defined top-level shape: a single key per
	// input file mapping to an array of function records.
	p := writeTemp(t, "smoke.luau", "local x = 42\nprint(x)\n")
	tmp := t.TempDir()
	out := filepath.Join(tmp, "summary.json")
	code, _, errOut := runWith(t, []string{"luau-bytecode", "--summary-file", out, p})
	if code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, errOut)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	var doc map[string][]map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("summary is not valid JSON: %v\n%s", err, raw)
	}
	if len(doc) != 1 {
		t.Fatalf("summary should have one file key, got %d", len(doc))
	}
	for _, fns := range doc {
		if len(fns) == 0 {
			t.Fatalf("summary file entry has no functions")
		}
		first := fns[0]
		// Spot-check the keys upstream guarantees.
		for _, want := range []string{"source", "name", "line", "nestingLimit", "counts"} {
			if _, ok := first[want]; !ok {
				t.Errorf("function record missing field %q", want)
			}
		}
	}
}

func TestRejectsBadOptLevel(t *testing.T) {
	p := writeTemp(t, "smoke.luau", "return\n")
	code, _, errOut := runWith(t, []string{"luau-bytecode", "-O", "9", p})
	if code == 0 {
		t.Fatalf("bad -O accepted (stderr=%q)", errOut)
	}
}

func TestRequiresInputFiles(t *testing.T) {
	code, _, errOut := runWith(t, []string{"luau-bytecode"})
	if code == 0 {
		t.Fatalf("no input accepted, want failure")
	}
	if !strings.Contains(errOut, "no input") {
		t.Fatalf("expected 'no input' in stderr: %q", errOut)
	}
}
