// Copyright (c) luaugo contributors. Licensed under the MIT License.

package upstreamvm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGoldenBlob(t *testing.T) {
	RequireAvailable(t)

	// Use a tiny known-good fixture: the upstream-compiled "hello world"
	// equivalent. We construct it on the fly via RunSource to avoid
	// depending on any specific golden file.
	r, err := RunSource([]byte("print('hello'); return 1+2\n"))
	if err != nil {
		t.Fatalf("RunSource: %v", err)
	}
	if r.Status != StatusOK {
		t.Fatalf("status = %d, want StatusOK; stderr = %q", r.Status, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "hello") {
		t.Errorf("stdout missing 'hello': %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "3") {
		t.Errorf("stdout missing return value 3: %q", r.Stdout)
	}
}

func TestRunGoldenFile(t *testing.T) {
	RequireAvailable(t)

	// Run one of the small golden blobs to confirm harness file I/O.
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	candidates := []string{"safeenv.luac", "ndebug_upvalues.luac"}
	var blob []byte
	for _, name := range candidates {
		p := filepath.Join(repoRoot, "tests", "golden", name)
		if b, err := os.ReadFile(p); err == nil {
			blob = b
			break
		}
	}
	if blob == nil {
		t.Skip("no small golden fixture available")
	}
	r := MustRun(t, blob)
	if !r.LoadOK() {
		t.Errorf("blob did not load on upstream VM: status=%d stderr=%q",
			r.Status, r.Stderr)
	}
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
