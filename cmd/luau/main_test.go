// Copyright (c) luaugo contributors. Licensed under the MIT License.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp drops a Lua source file into the test's temp directory
// and returns its path. The file is deleted automatically when the
// test ends.
func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// runWith executes `run` with synthetic argv and captures stdout +
// stderr for inspection.
func runWith(t *testing.T, argv []string, stdin string) (int, string, string) {
	t.Helper()
	var so, se bytes.Buffer
	si := strings.NewReader(stdin)
	code := run(argv, si, &so, &se)
	return code, so.String(), se.String()
}

func TestVersionFlag(t *testing.T) {
	code, out, _ := runWith(t, []string{"luau", "--version"}, "")
	if code != 0 {
		t.Fatalf("--version: exit %d, want 0", code)
	}
	if !strings.Contains(out, "luaugo") {
		t.Fatalf("--version output missing 'luaugo': %q", out)
	}
}

func TestHelpFlag(t *testing.T) {
	code, _, errOut := runWith(t, []string{"luau", "--help"}, "")
	if code != 0 {
		t.Fatalf("--help: exit %d, want 0", code)
	}
	if !strings.Contains(errOut, "Usage:") {
		t.Fatalf("--help missing Usage banner: %q", errOut)
	}
}

func TestRunScriptWithProgramArgs(t *testing.T) {
	// Verify that `-a` separates program arguments and that they
	// arrive as varargs to the main chunk. Also confirms the
	// per-script sandbox can reach the base library's `print`.
	src := `local a, b = ...
print("got:", a, b, type(a))
`
	p := writeTemp(t, "smoke.luau", src)
	code, out, errOut := runWith(t, []string{"luau", p, "-a", "alpha", "42"}, "")
	if code != 0 {
		t.Fatalf("run script: exit %d, stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "got:\talpha\t42\tstring") {
		t.Fatalf("script output unexpected: %q", out)
	}
}

func TestRunScriptSyntaxError(t *testing.T) {
	p := writeTemp(t, "bad.luau", "local x = !!@\n")
	code, _, errOut := runWith(t, []string{"luau", p}, "")
	if code == 0 {
		t.Fatalf("syntax error: exit 0, want non-zero (stderr=%q)", errOut)
	}
}

func TestREPLEvaluatesExpression(t *testing.T) {
	// Feed an expression and a statement to the REPL on stdin and
	// confirm both produce visible output. The trailing newline
	// after the statement is what flushes its print().
	stdin := "1 + 1\nprint('hi')\n"
	code, out, _ := runWith(t, []string{"luau"}, stdin)
	if code != 0 {
		t.Fatalf("REPL exit %d, want 0", code)
	}
	if !strings.Contains(out, "2") {
		t.Fatalf("REPL didn't pretty-print expression: %q", out)
	}
	if !strings.Contains(out, "hi") {
		t.Fatalf("REPL didn't echo print('hi'): %q", out)
	}
}

func TestBadOptLevel(t *testing.T) {
	code, _, errOut := runWith(t, []string{"luau", "-O", "9"}, "")
	if code == 0 {
		t.Fatalf("bad -O level: exit 0, want non-zero")
	}
	if !strings.Contains(errOut, "Optimization level") {
		t.Fatalf("error message didn't mention optimization level: %q", errOut)
	}
}
