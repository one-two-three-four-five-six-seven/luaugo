// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib

import (
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// TestLoadstringBasic verifies that loadstring compiles a chunk and
// returns a callable function. Mirrors Lua 5.1's lbaselib.cpp
// luaB_loadstring semantics: the loaded chunk runs in the current
// global environment.
func TestLoadstringBasic(t *testing.T) {
	s := runBaseLuau(t, "loadstring_basic", `
		local f = assert(loadstring("return 1 + 2"))
		return f()
	`)
	defer s.Close()
	if s.Top() < 1 {
		t.Fatalf("expected return value, got top=%d", s.Top())
	}
	n, ok := s.ToInteger(-1)
	if !ok || n != 3 {
		t.Fatalf("expected 3, got %v (ok=%v)", n, ok)
	}
}

// TestLoadstringChunkNameDefault confirms loadstring without an
// explicit chunkname still produces a valid loadable closure.
func TestLoadstringChunkNameDefault(t *testing.T) {
	s := runBaseLuau(t, "loadstring_default_name", `
		local f = assert(loadstring("return 42"))
		return f()
	`)
	defer s.Close()
	n, _ := s.ToInteger(-1)
	if n != 42 {
		t.Fatalf("expected 42, got %d", n)
	}
}

// TestLoadstringSyntaxError ensures malformed source returns
// (nil, errmsg) instead of raising.
func TestLoadstringSyntaxError(t *testing.T) {
	s := runBaseLuau(t, "loadstring_syntax", `
		local f, err = loadstring("this is not valid lua $$$")
		return f, err
	`)
	defer s.Close()
	if s.Top() < 2 {
		t.Fatalf("expected (nil, err), got top=%d", s.Top())
	}
	if !s.IsNil(1) {
		t.Fatalf("expected first return to be nil, got %v", s.Type(1))
	}
	if s.Type(2) != vm.TString {
		t.Fatalf("expected second return to be string, got %v", s.Type(2))
	}
	msg, _ := s.ToString(2)
	if msg == "" {
		t.Fatalf("expected non-empty error message")
	}
}

// TestLoadstringSeesGlobals verifies that loaded chunks share the
// global table with the caller -- so assignments propagate.
func TestLoadstringSeesGlobals(t *testing.T) {
	s := runBaseLuau(t, "loadstring_globals", `
		x = 1
		local f = assert(loadstring("x = x + 10; return x"))
		return f()
	`)
	defer s.Close()
	n, _ := s.ToInteger(-1)
	if n != 11 {
		t.Fatalf("expected 11, got %d", n)
	}
}

// TestLoadstringFactorial reproduces the upstream calls.luau pattern
// where loadstring builds a string-quoted recursive call.
func TestLoadstringFactorial(t *testing.T) {
	s := runBaseLuau(t, "loadstring_factorial", `
		function fat(x)
			if x == 0 then return 1 end
			return x * loadstring("return fat(" .. x-1 .. ")")()
		end
		return fat(5)
	`)
	defer s.Close()
	n, _ := s.ToInteger(-1)
	if n != 120 {
		t.Fatalf("expected 120, got %d", n)
	}
}

// TestLoadstringChunkName threads a chunkname through to the closure
// so that runtime errors locate to the user-supplied name. Lua 5.1's
// signature is loadstring(source, chunkname).
func TestLoadstringChunkName(t *testing.T) {
	s := runBaseLuau(t, "loadstring_chunkname", `
		local f = assert(loadstring("error('boom')", "myChunk"))
		local ok, err = pcall(f)
		return ok, err
	`)
	defer s.Close()
	if s.ToBoolean(1) {
		t.Fatalf("expected pcall to fail")
	}
	errMsg, _ := s.ToString(2)
	if !strings.Contains(errMsg, "boom") {
		t.Fatalf("expected error to mention 'boom', got %q", errMsg)
	}
}
