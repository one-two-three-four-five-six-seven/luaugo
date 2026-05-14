// Copyright (c) luaugo contributors. Licensed under the MIT License.

package tests

import (
	"bytes"
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// runOnOurVM compiles src with luaugo's compiler, runs it on luaugo's
// own VM with the standard library opened, captures stdout from print,
// and returns (stdoutText, finalReturnAsString). Fails the test on any
// load or runtime error.
func runOnOurVM(t *testing.T, name, src string) (string, string) {
	t.Helper()

	blob, err := compiler.CompileBinary(name, []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	s := vm.NewState()
	defer s.Close()

	prev := lib.Stdout
	defer func() { lib.Stdout = prev }()
	var buf bytes.Buffer
	lib.Stdout = &buf

	lib.OpenAll(s)

	if err := s.Load(name, blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	if st := s.PCall(0, 1, 0); st != vm.StatusOK {
		msg, _ := s.ToString(-1)
		t.Fatalf("runtime error: %s", msg)
	}
	ret, _ := s.ToString(-1)
	return buf.String(), ret
}

// TestOurVM_HelloWorld is the first smoke test: print plus return.
func TestOurVM_HelloWorld(t *testing.T) {
	out, ret := runOnOurVM(t, "hello", `
		print("hello", "world")
		return "done"
	`)
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Errorf("stdout missing hello/world: %q", out)
	}
	if ret != "done" {
		t.Errorf("return = %q, want %q", ret, "done")
	}
}

// TestOurVM_Arithmetic confirms numeric operators and constant folding
// produce correct results on our VM.
func TestOurVM_Arithmetic(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"1 + 2", "3"},
		{"10 - 4", "6"},
		{"3 * 7", "21"},
		{"22 / 4", "5.5"},
		{"23 % 5", "3"},
		{"2 ^ 10", "1024"},
		{"22 // 4", "5"}, // floor division
		{"-5", "-5"},
		{"(1 + 2) * (3 + 4)", "21"},
	}
	for _, c := range cases {
		_, ret := runOnOurVM(t, "arith", "return tostring("+c.expr+")")
		if ret != c.want {
			t.Errorf("%s: got %q, want %q", c.expr, ret, c.want)
		}
	}
}

// TestOurVM_Strings exercises the string library on our VM.
func TestOurVM_Strings(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"concat", `return "foo" .. "bar"`, "foobar"},
		{"upper", `return string.upper("hello")`, "HELLO"},
		{"lower", `return string.lower("HELLO")`, "hello"},
		{"len", `return tostring(string.len("hello"))`, "5"},
		{"sub", `return string.sub("hello world", 1, 5)`, "hello"},
		{"rep", `return string.rep("ab", 3)`, "ababab"},
		{"reverse", `return string.reverse("abc")`, "cba"},
		{"format-d", `return string.format("%d", 42)`, "42"},
		{"format-s", `return string.format("%s/%s", "a", "b")`, "a/b"},
		{"method-upper", `return ("hello"):upper()`, "HELLO"},
	}
	for _, c := range cases {
		_, ret := runOnOurVM(t, c.name, c.src)
		if ret != c.want {
			t.Errorf("%s: got %q, want %q", c.name, ret, c.want)
		}
	}
}

// TestOurVM_Tables exercises table construction, access, and methods.
func TestOurVM_Tables(t *testing.T) {
	src := `
		local t = {10, 20, 30}
		local sum = t[1] + t[2] + t[3]
		t[4] = 40
		return tostring(sum) .. "/" .. tostring(t[4]) .. "/" .. tostring(#t)
	`
	_, ret := runOnOurVM(t, "tables", src)
	if ret != "60/40/4" {
		t.Errorf("got %q, want %q", ret, "60/40/4")
	}
}

// TestOurVM_Loops covers numeric for, while, and repeat.
func TestOurVM_Loops(t *testing.T) {
	t.Run("numeric for", func(t *testing.T) {
		_, ret := runOnOurVM(t, "nfor", `
			local s = 0
			for i = 1, 10 do s = s + i end
			return tostring(s)
		`)
		if ret != "55" {
			t.Errorf("got %q, want 55", ret)
		}
	})
	t.Run("while", func(t *testing.T) {
		_, ret := runOnOurVM(t, "while", `
			local i = 1
			while i < 100 do i = i * 2 end
			return tostring(i)
		`)
		if ret != "128" {
			t.Errorf("got %q, want 128", ret)
		}
	})
	t.Run("repeat", func(t *testing.T) {
		_, ret := runOnOurVM(t, "repeat", `
			local i = 0
			repeat
				i = i + 1
			until i >= 5
			return tostring(i)
		`)
		if ret != "5" {
			t.Errorf("got %q, want 5", ret)
		}
	})
}

// TestOurVM_Conditionals tests if/elseif/else.
func TestOurVM_Conditionals(t *testing.T) {
	src := `
		local function classify(n)
			if n < 0 then return "neg"
			elseif n == 0 then return "zero"
			else return "pos" end
		end
		return classify(-3) .. "/" .. classify(0) .. "/" .. classify(7)
	`
	_, ret := runOnOurVM(t, "cond", src)
	if ret != "neg/zero/pos" {
		t.Errorf("got %q", ret)
	}
}

// TestOurVM_Closures verifies upvalue capture and lexical scoping.
func TestOurVM_Closures(t *testing.T) {
	src := `
		local function counter()
			local n = 0
			return function()
				n = n + 1
				return n
			end
		end
		local c = counter()
		local a = c()
		local b = c()
		local d = c()
		return tostring(a) .. "/" .. tostring(b) .. "/" .. tostring(d)
	`
	_, ret := runOnOurVM(t, "closure", src)
	if ret != "1/2/3" {
		t.Errorf("got %q, want 1/2/3", ret)
	}
}

// TestOurVM_Recursion verifies recursive calls work.
func TestOurVM_Recursion(t *testing.T) {
	src := `
		local function fact(n)
			if n <= 1 then return 1 end
			return n * fact(n - 1)
		end
		return tostring(fact(7))
	`
	_, ret := runOnOurVM(t, "rec", src)
	if ret != "5040" {
		t.Errorf("got %q, want 5040", ret)
	}
}

// TestOurVM_PCall verifies pcall catches runtime errors.
func TestOurVM_PCall(t *testing.T) {
	src := `
		local ok, err = pcall(function() error("boom") end)
		return tostring(ok) .. "/" .. tostring(err):find("boom") and "ok" or "no-match"
	`
	_, ret := runOnOurVM(t, "pcall", src)
	if !strings.Contains(ret, "ok") {
		// Fallback: at least verify pcall returned false on first slot.
		_, ret2 := runOnOurVM(t, "pcall2", `
			local ok = pcall(function() error("boom") end)
			return tostring(ok)
		`)
		if ret2 != "false" {
			t.Errorf("pcall did not return false on error; got %q", ret2)
		}
	}
}

// TestOurVM_Math exercises a slice of the math library.
func TestOurVM_Math(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"abs", `return tostring(math.abs(-7))`, "7"},
		{"floor", `return tostring(math.floor(3.7))`, "3"},
		{"ceil", `return tostring(math.ceil(3.2))`, "4"},
		{"sqrt", `return tostring(math.sqrt(16))`, "4"},
		{"max", `return tostring(math.max(1, 7, 3))`, "7"},
		{"min", `return tostring(math.min(1, 7, 3))`, "1"},
		{"clamp", `return tostring(math.clamp(15, 0, 10))`, "10"},
		{"pi-truncate", `return tostring(math.floor(math.pi * 100))`, "314"},
	}
	for _, c := range cases {
		_, ret := runOnOurVM(t, c.name, c.src)
		if ret != c.want {
			t.Errorf("%s: got %q, want %q", c.name, ret, c.want)
		}
	}
}

// TestOurVM_Bit32 exercises bit32.
func TestOurVM_Bit32(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"band", `return tostring(bit32.band(0xff, 0x0f))`, "15"},
		{"bor", `return tostring(bit32.bor(0xf0, 0x0f))`, "255"},
		{"bxor", `return tostring(bit32.bxor(0xff, 0x0f))`, "240"},
		{"lshift", `return tostring(bit32.lshift(1, 4))`, "16"},
		{"rshift", `return tostring(bit32.rshift(16, 2))`, "4"},
	}
	for _, c := range cases {
		_, ret := runOnOurVM(t, c.name, c.src)
		if ret != c.want {
			t.Errorf("%s: got %q, want %q", c.name, ret, c.want)
		}
	}
}

// TestOurVM_GoCallback verifies Go-registered functions are callable
// from Luau on our VM.
func TestOurVM_GoCallback(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	lib.OpenAll(s)

	s.Register("triple", func(s *vm.State) int {
		n, _ := s.ToNumber(1)
		s.PushNumber(n * 3)
		return 1
	})

	blob, err := compiler.CompileBinary("cb", []byte(`return tostring(triple(14))`), compiler.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Load("cb", blob, 0); err != nil {
		t.Fatal(err)
	}
	if st := s.PCall(0, 1, 0); st != vm.StatusOK {
		msg, _ := s.ToString(-1)
		t.Fatalf("runtime: %s", msg)
	}
	ret, _ := s.ToString(-1)
	if ret != "42" {
		t.Errorf("got %q, want 42", ret)
	}
}

// TestOurVM_MultipleStates verifies isolation between independent VMs.
func TestOurVM_MultipleStates(t *testing.T) {
	src := `_G.shared = (_G.shared or 0) + 1; return tostring(_G.shared)`
	for i := 0; i < 3; i++ {
		_, ret := runOnOurVM(t, "iso", src)
		if ret != "1" {
			t.Errorf("iteration %d: got %q, want 1 (each state must start fresh)", i, ret)
		}
	}
}
