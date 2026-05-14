// Copyright (c) luaugo contributors. Licensed under the MIT License.

package main

import (
	"bytes"
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

func TestTextModeProducesDisassembly(t *testing.T) {
	// --text is the default and emits human-readable bytecode. The
	// minimum we assert is that an opcode mnemonic appears.
	p := writeTemp(t, "smoke.luau", "local x = 42\nprint(x)\n")
	code, out, errOut := runWith(t, []string{"luau-compile", p})
	if code != 0 {
		t.Fatalf("--text exit %d, stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "LOADN") {
		t.Fatalf("disassembly missing LOADN: %q", out)
	}
}

func TestBinaryModeProducesValidBlob(t *testing.T) {
	p := writeTemp(t, "smoke.luau", "local x = 42\nprint(x)\n")
	code, out, errOut := runWith(t, []string{"luau-compile", "--binary", p})
	if code != 0 {
		t.Fatalf("--binary exit %d, stderr=%q", code, errOut)
	}
	if len(out) == 0 {
		t.Fatalf("--binary wrote nothing")
	}
	// Byte 0 of a valid bytecode blob is the version number, which
	// is non-zero. A leading 0 byte signals a compile-error blob.
	if out[0] == 0 {
		t.Fatalf("--binary produced a compile-error blob: %q", out[1:])
	}
}

func TestNullModePrintsSummary(t *testing.T) {
	p := writeTemp(t, "smoke.luau", "local x = 42\n")
	code, out, _ := runWith(t, []string{"luau-compile", "--null", p})
	if code != 0 {
		t.Fatalf("--null exit %d", code)
	}
	if !strings.Contains(out, "Compiled") {
		t.Fatalf("--null missing summary line: %q", out)
	}
}

func TestBadOptLevelRejected(t *testing.T) {
	p := writeTemp(t, "smoke.luau", "return\n")
	code, _, errOut := runWith(t, []string{"luau-compile", "-O", "9", p})
	if code == 0 {
		t.Fatalf("bad -O accepted, want failure (stderr=%q)", errOut)
	}
}

func TestCompileErrorSurfacedToStderr(t *testing.T) {
	p := writeTemp(t, "bad.luau", "local x = !!@\n")
	code, _, errOut := runWith(t, []string{"luau-compile", p})
	if code == 0 {
		t.Fatalf("compile error accepted, want failure")
	}
	if errOut == "" {
		t.Fatalf("compile error produced no stderr")
	}
}
