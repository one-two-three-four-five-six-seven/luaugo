// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib_test

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

func runErrChunk(t *testing.T, name, src string) (status, detail string) {
	t.Helper()
	blob, err := compiler.CompileBinary(name, []byte(src), compiler.Defaults())
	if err != nil {
		return "COMPILE_ERROR", err.Error()
	}
	if len(blob) > 0 && blob[0] == 0 {
		return "COMPILE_ERROR", string(blob[1:])
	}
	s := vm.NewState()
	defer s.Close()
	prev := lib.Stdout
	defer func() { lib.Stdout = prev }()
	lib.Stdout = io.Discard
	lib.OpenAll(s)
	if err := s.Load(name, blob, 0); err != nil {
		return "LOAD_ERROR", err.Error()
	}
	st := s.PCall(0, 1, 0)
	if st == vm.StatusOK {
		msg, _ := s.ToString(-1)
		return "OK", msg
	}
	msg, _ := s.ToString(-1)
	return "RUNTIME_ERR", msg
}

// TestErrorPrefixesChunkAndLine mirrors pcall.luau line 80 and
// errors.luau lineerror cases: `error("foo")` from a Lua function
// must produce "<chunkname>:<line>: foo".
func TestErrorPrefixesChunkAndLine(t *testing.T) {
	src := `
local ok, err = pcall(function()
	error("foo")
end)
return tostring(err)
`
	st, det := runErrChunk(t, "errprefix.luau", src)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	// error() is at line 3.
	want := "errprefix.luau:3: foo"
	if det != want {
		t.Errorf("expected %q got %q", want, det)
	}
}

// TestForLoopLinePrefix: errors.luau:125 lineerror requires
// "<chunk>:<line>: ..." on errors raised by the for-loop type check.
func TestForLoopLinePrefix(t *testing.T) {
	src := "local a\n for i=1,'a' do \n print(i) \n end"
	full := fmt.Sprintf(`local s = %q
local fn, msg = loadstring(s)
if not fn then return tostring(msg) end
local ok, err = pcall(fn)
return tostring(err)
`, src)
	st, det := runErrChunk(t, "forline.luau", full)
	if st != "OK" {
		t.Fatalf("got %s: %s", st, det)
	}
	if !strings.Contains(det, ":2:") {
		t.Errorf("expected ':2:' in %q", det)
	}
}
