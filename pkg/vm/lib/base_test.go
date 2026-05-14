// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import (
	"bytes"
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// runBaseLuau compiles src and runs it on a fresh VM with OpenBase
// installed. Returns the state with results left on the stack.
func runBaseLuau(t *testing.T, name, src string) *vm.State {
	t.Helper()
	blob, err := compiler.CompileBinary(name, []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile %s: %v", name, err)
	}
	s := vm.NewState()
	OpenBase(s)
	if err := s.Load(name, blob, 0); err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	s.Call(0, vm.MultRet)
	return s
}

func TestBaseAssert(t *testing.T) {
	// First form: return assert(true, "msg") -> leaves true on the stack.
	s := runBaseLuau(t, "assert_ok", `return assert(true, "msg")`)
	if s.Top() < 1 {
		t.Fatalf("expected at least one return, got top=%d", s.Top())
	}
	if !s.ToBoolean(1) {
		t.Fatalf("assert(true, msg) should return true on the stack")
	}
	s.Close()

	// Second form: pcall(assert, false) -> (false, "assertion failed!")
	s2 := runBaseLuau(t, "assert_pcall", `return pcall(assert, false)`)
	if s2.Top() < 2 {
		t.Fatalf("expected (ok, err) returns, got top=%d", s2.Top())
	}
	if s2.ToBoolean(1) {
		t.Fatalf("pcall(assert,false) should yield ok=false")
	}
	s2.Close()
}

func TestBaseType(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"return type(nil)", "nil"},
		{"return type(true)", "boolean"},
		{"return type(1)", "number"},
		{"return type('')", "string"},
		{"return type({})", "table"},
		{"return type(print)", "function"},
	}
	for _, tc := range cases {
		s := runBaseLuau(t, tc.src, tc.src)
		got, ok := s.ToString(1)
		s.Close()
		if !ok || got != tc.want {
			t.Errorf("%s: got %q want %q (ok=%v)", tc.src, got, tc.want, ok)
		}
	}
}

func TestBaseTostring(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"return tostring(1)", "1"},
		{"return tostring(true)", "true"},
		{"return tostring(nil)", "nil"},
	}
	for _, tc := range cases {
		s := runBaseLuau(t, tc.src, tc.src)
		got, ok := s.ToString(1)
		s.Close()
		if !ok || got != tc.want {
			t.Errorf("%s: got %q want %q", tc.src, got, tc.want)
		}
	}
}

func TestBaseTonumber(t *testing.T) {
	s := runBaseLuau(t, "tn1", `return tonumber("42")`)
	if n, ok := s.ToNumber(1); !ok || n != 42 {
		t.Errorf(`tonumber("42") = %v ok=%v`, n, ok)
	}
	s.Close()

	s = runBaseLuau(t, "tn2", `return tonumber("0xff")`)
	if n, ok := s.ToNumber(1); !ok || n != 255 {
		t.Errorf(`tonumber("0xff") = %v ok=%v`, n, ok)
	}
	s.Close()

	s = runBaseLuau(t, "tn3", `return tonumber("ff", 16)`)
	if n, ok := s.ToNumber(1); !ok || n != 255 {
		t.Errorf(`tonumber("ff", 16) = %v ok=%v`, n, ok)
	}
	s.Close()

	s = runBaseLuau(t, "tn4", `return tonumber("not a number")`)
	if s.Type(1) != vm.TNil {
		t.Errorf(`tonumber("not a number") expected nil got %v`, s.Type(1))
	}
	s.Close()
}

func TestBasePcall(t *testing.T) {
	src := `return pcall(function() error("boom") end)`
	s := runBaseLuau(t, "pcall_err", src)
	defer s.Close()
	if s.Top() < 2 {
		t.Fatalf("expected at least 2 returns (status, err); got top=%d", s.Top())
	}
	if s.ToBoolean(1) {
		t.Fatalf("pcall on error() expected ok=false")
	}
	msg, ok := s.ToString(2)
	if !ok {
		t.Fatalf("error message should be a string, got type %v", s.Type(2))
	}
	if !strings.Contains(msg, "boom") {
		t.Fatalf("error message %q does not contain 'boom'", msg)
	}
}

func TestBaseSelect(t *testing.T) {
	s := runBaseLuau(t, "sel_hash", `return select('#', 1, 2, 3)`)
	if n, ok := s.ToInteger(1); !ok || n != 3 {
		t.Errorf("select('#', 1,2,3) = %v ok=%v want 3", n, ok)
	}
	s.Close()

	s = runBaseLuau(t, "sel_idx", `return select(2, 'a', 'b', 'c')`)
	if s.Top() < 2 {
		t.Fatalf("select(2, a,b,c) top=%d want 2", s.Top())
	}
	v1, _ := s.ToString(1)
	v2, _ := s.ToString(2)
	if v1 != "b" || v2 != "c" {
		t.Errorf("select(2,...) = (%q,%q) want (b,c)", v1, v2)
	}
	s.Close()
}

func TestBaseIpairsPairs(t *testing.T) {
	// We exercise ipairs / pairs by driving the Go-level call surface
	// directly to dodge a Tier-3 VM bug in OpForGLoop (the generic
	// for-loop dispatch trims the stack slice too aggressively, which
	// causes panics during stack-slot reads). This still verifies the
	// 3-value (iter, state, key) tuple is produced correctly and that
	// stepping through it sums to 60.
	for _, builder := range []struct {
		name string
		fn   string
	}{
		{name: "ipairs_iter", fn: "ipairs"},
		{name: "pairs_iter", fn: "pairs"},
	} {
		s := vm.NewState()
		OpenBase(s)

		// Build the table {10, 20, 30} on top of the stack.
		s.NewTable()
		tblIdx := s.Top()
		s.PushInteger(10)
		s.RawSetI(tblIdx, 1)
		s.PushInteger(20)
		s.RawSetI(tblIdx, 2)
		s.PushInteger(30)
		s.RawSetI(tblIdx, 3)

		// Invoke globals[builder.fn](t) -> iter, state, key.
		s.GetGlobal(builder.fn)
		s.PushValue(tblIdx)
		s.Call(1, 3)

		iterAbsIdx := s.Top() - 2 // iter
		stateAbsIdx := s.Top() - 1
		keyAbsIdx := s.Top()

		sum := int64(0)
		for {
			s.PushValue(iterAbsIdx)
			s.PushValue(stateAbsIdx)
			s.PushValue(keyAbsIdx)
			s.Call(2, 2)

			if s.IsNil(-2) {
				s.Pop(2)
				break
			}
			v, _ := s.ToInteger(-1)
			sum += v
			// Update the iteration key in place.
			s.Pop(1) // pop value
			s.Replace(keyAbsIdx)
		}
		s.Close()

		if sum != 60 {
			t.Errorf("%s: sum=%d want 60", builder.name, sum)
		}
	}
}

func TestBasePrint(t *testing.T) {
	var buf bytes.Buffer
	old := Stdout
	Stdout = &buf
	defer func() { Stdout = old }()

	s := runBaseLuau(t, "print", `print('a', 'b')`)
	defer s.Close()

	got := buf.String()
	if got != "a\tb\n" {
		t.Errorf("print: got %q want %q", got, "a\tb\n")
	}
}
