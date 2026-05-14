// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// readTpackFixture finds tests/conformance/tpack.luau by walking up
// from the test's current working directory. Used by both the focused
// regression tests in string_pack_test.go and the diagnostic trace
// test below.
func readTpackFixture() ([]byte, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for i := 0; i < 6; i++ {
		p := filepath.Join(dir, "tests", "conformance", "tpack.luau")
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		}
		dir = filepath.Dir(dir)
	}
	return nil, fmt.Errorf("tpack.luau not found")
}

// TestTpackCheckerTrace replays tpack.luau but rewrites its `checkerror`
// helper to surface WHICH checkerror call failed first (the original
// `assert(not status and string.find(err, msg))` just says "assertion
// failed!" at line 41 without telling you which of the 30+ call sites
// tripped it). This is a diagnostic harness — kept in tree so that
// future regressions in pack/unpack/packsize can be bisected quickly.
func TestTpackCheckerTrace(t *testing.T) {
	bytes, err := readTpackFixture()
	if err != nil {
		t.Skipf("fixture not found: %v", err)
		return
	}
	src := strings.ReplaceAll(string(bytes), "\r\n", "\n")

	old := "function checkerror (msg, f, ...)\n" +
		"  local status, err = pcall(f, ...)\n" +
		"  -- print(status, err, msg)\n" +
		"  assert(not status and string.find(err, msg))\n" +
		"end"
	rep := "local _check_idx = 0\n" +
		"function checkerror (msg, f, ...)\n" +
		"  _check_idx = _check_idx + 1\n" +
		"  local status, err = pcall(f, ...)\n" +
		"  local ok = (not status) and string.find(err, msg)\n" +
		"  if not ok then\n" +
		"    error(string.format(\"CHECK#%d FAIL msg=%q status=%s err=%s\", _check_idx, msg, tostring(status), tostring(err)), 0)\n" +
		"  end\n" +
		"end"
	src = strings.Replace(src, old, rep, 1)
	if !strings.Contains(src, "CHECK#%d FAIL") {
		t.Fatalf("checkerror trace replacement did not apply")
	}

	s := vm.NewState()
	defer s.Close()
	lib.OpenAll(s)

	blob, err := compiler.CompileBinary("=tpack_trace", []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(blob) == 0 || blob[0] == 0 {
		t.Fatalf("compile error blob: %q", blob)
	}
	if err := s.Load("=tpack_trace", blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	var rt string
	func() {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(error); ok {
					rt = e.Error()
				}
			}
		}()
		s.Call(0, 1)
	}()
	if rt != "" {
		t.Fatalf("tpack trace runtime err: %s", rt)
	}
	got, _ := s.ToString(-1)
	if got != "OK" {
		t.Fatalf("tpack trace returned %q, want OK", got)
	}
}
