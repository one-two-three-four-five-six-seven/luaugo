// Copyright (c) luaugo contributors. Licensed under the MIT License.

package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConformanceFixturesPresent verifies the conformance test corpus
// mirrored from upstream is intact. The expected count is updated as
// part of `tools/sync-upstream.ps1`; mismatches indicate a broken sync
// or accidental fixture loss.
func TestConformanceFixturesPresent(t *testing.T) {
	const wantCount = 53
	dir := filepath.Join("conformance")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read conformance dir: %v", err)
	}
	var count int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".lua") || strings.HasSuffix(name, ".luau") {
			count++
		}
	}
	if count != wantCount {
		t.Errorf("conformance fixture count = %d, want %d (sync upstream or update wantCount)", count, wantCount)
	}
}
