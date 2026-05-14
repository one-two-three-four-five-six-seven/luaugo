// Copyright (c) luaugo contributors. Licensed under the MIT License.
// debug-only test file: investigates individual conformance fixtures.

package vm_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// TestDebugRunFixture runs a single fixture identified by the env var
// LUAUGO_FIXTURE. Useful for printf-debugging during bug hunts.
func TestDebugRunFixture(t *testing.T) {
	name := os.Getenv("LUAUGO_FIXTURE")
	if name == "" {
		t.Skip("set LUAUGO_FIXTURE to a path to enable")
	}
	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	blob, err := compiler.CompileBinary(filepath.Base(name), src, compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(blob) > 0 && blob[0] == 0 {
		t.Fatalf("compile error blob: %s", string(blob[1:]))
	}
	s := vm.NewState()
	defer s.Close()

	var buf bytes.Buffer
	prev := lib.Stdout
	defer func() { lib.Stdout = prev }()
	lib.Stdout = io.MultiWriter(&buf, os.Stderr)
	lib.OpenAll(s)
	if err := s.Load(filepath.Base(name), blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	st := s.PCall(0, 0, 0)
	if st != vm.StatusOK {
		msg, _ := s.ToString(-1)
		t.Logf("RUNTIME_ERR: %s", msg)
		t.Logf("captured stdout:\n%s", buf.String())
		t.FailNow()
	}
}
