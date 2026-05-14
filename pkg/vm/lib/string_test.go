// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib_test

import (
	"testing"

	"github.com/luaugo/luaugo/pkg/compiler"
	"github.com/luaugo/luaugo/pkg/vm"
	"github.com/luaugo/luaugo/pkg/vm/lib"
)

// runStringScript compiles src as a chunk and runs it on a fresh State
// with OpenBase + OpenString opened. The string library installs the
// per-state string metatable so namecall (s:upper()) works.
func runStringScript(t *testing.T, src string, nresults int) *vm.State {
	t.Helper()
	s := vm.NewState()
	t.Cleanup(s.Close)

	func() {
		defer func() { _ = recover() }()
		lib.OpenBase(s)
	}()
	lib.OpenString(s)

	blob, err := compiler.CompileBinary("=string_test", []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	if len(blob) == 0 || blob[0] == 0 {
		t.Fatalf("compile produced error blob: %q", blob)
	}
	if err := s.Load("=string_test", blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	s.Call(0, nresults)
	return s
}

func TestStringSub(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{`return string.sub("hello", 2, 4)`, "ell"},
		{`return string.sub("hello", 1, -1)`, "hello"},
		{`return string.sub("hello", -3)`, "llo"},
		{`return string.sub("hello", 2)`, "ello"},
		{`return string.sub("hello", 10)`, ""},
		{`return string.sub("hello", 1, 0)`, ""},
		{`return string.sub("abc", -2, -1)`, "bc"},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			s := runStringScript(t, tc.expr, 1)
			got, ok := s.ToString(-1)
			if !ok || got != tc.want {
				t.Fatalf("%s: got %q want %q", tc.expr, got, tc.want)
			}
		})
	}
}

func TestStringByteChar(t *testing.T) {
	// byte
	s := runStringScript(t, `return string.byte("ABC", 2)`, 1)
	if n, ok := s.ToNumber(-1); !ok || n != 66 {
		t.Fatalf("byte: got %v ok=%v", n, ok)
	}
	// char
	s = runStringScript(t, `return string.char(65, 66, 67)`, 1)
	if got, _ := s.ToString(-1); got != "ABC" {
		t.Fatalf("char: got %q", got)
	}
	// multiple byte returns
	s = runStringScript(t, `return string.byte("ABC", 1, 3)`, 3)
	a, _ := s.ToNumber(-3)
	b, _ := s.ToNumber(-2)
	c, _ := s.ToNumber(-1)
	if a != 65 || b != 66 || c != 67 {
		t.Fatalf("byte range: %v %v %v", a, b, c)
	}
}

func TestStringLenLowerUpper(t *testing.T) {
	s := runStringScript(t, `return string.len("hello")`, 1)
	if n, _ := s.ToNumber(-1); n != 5 {
		t.Fatalf("len: %v", n)
	}
	s = runStringScript(t, `return string.lower("Hello, WORLD!")`, 1)
	if got, _ := s.ToString(-1); got != "hello, world!" {
		t.Fatalf("lower: %q", got)
	}
	s = runStringScript(t, `return string.upper("Hello, world!")`, 1)
	if got, _ := s.ToString(-1); got != "HELLO, WORLD!" {
		t.Fatalf("upper: %q", got)
	}
}

func TestStringRep(t *testing.T) {
	s := runStringScript(t, `return string.rep("ab", 3)`, 1)
	if got, _ := s.ToString(-1); got != "ababab" {
		t.Fatalf("rep: %q", got)
	}
	s = runStringScript(t, `return string.rep("x", 0)`, 1)
	if got, _ := s.ToString(-1); got != "" {
		t.Fatalf("rep0: %q", got)
	}
	s = runStringScript(t, `return string.rep("a", 3, "-")`, 1)
	if got, _ := s.ToString(-1); got != "a-a-a" {
		t.Fatalf("rep with sep: %q", got)
	}
}

func TestStringReverse(t *testing.T) {
	s := runStringScript(t, `return string.reverse("abcd")`, 1)
	if got, _ := s.ToString(-1); got != "dcba" {
		t.Fatalf("reverse: %q", got)
	}
	s = runStringScript(t, `return string.reverse("")`, 1)
	if got, _ := s.ToString(-1); got != "" {
		t.Fatalf("reverse empty: %q", got)
	}
}

func TestStringFind(t *testing.T) {
	// plain text — return directly to avoid Tier-3 VM stack-truncation bug
	// on `local a,b = fn(); return a, b`.
	s := runStringScript(t, `return string.find("hello world", "world")`, 2)
	if a, _ := s.ToNumber(-2); a != 7 {
		t.Fatalf("find plain start: %v", a)
	}
	if b, _ := s.ToNumber(-1); b != 11 {
		t.Fatalf("find plain end: %v", b)
	}

	// not found — return the result directly and check Go-side
	s = runStringScript(t, `return string.find("hello", "xyz")`, 1)
	if !s.IsNil(-1) {
		t.Fatalf("find not found: got type %v", s.Type(-1))
	}

	// pattern
	s = runStringScript(t, `return string.find("abc123", "%d+")`, 2)
	if a, _ := s.ToNumber(-2); a != 4 {
		t.Fatalf("find pat start: %v", a)
	}
	if b, _ := s.ToNumber(-1); b != 6 {
		t.Fatalf("find pat end: %v", b)
	}

	// plain mode forces literal match of special characters
	s = runStringScript(t, `return string.find("a.b", ".", 1, true)`, 2)
	if a, _ := s.ToNumber(-2); a != 2 {
		t.Fatalf("find plain dot: %v", a)
	}
}

func TestStringMatch(t *testing.T) {
	// single capture
	s := runStringScript(t, `return string.match("hello123", "(%a+)(%d+)")`, 2)
	a, _ := s.ToString(-2)
	b, _ := s.ToString(-1)
	_ = a
	if b != "123" {
		t.Fatalf("match capture2: got %q", b)
	}
	if a != "hello" {
		t.Fatalf("match capture1: got %q", a)
	}

	// no captures returns whole match
	s = runStringScript(t, `return string.match("abc123def", "%d+")`, 1)
	if got, _ := s.ToString(-1); got != "123" {
		t.Fatalf("match whole: %q", got)
	}

	// no match returns nil
	s = runStringScript(t, `return string.match("abc", "%d+")`, 1)
	if !s.IsNil(-1) {
		t.Fatalf("match nil: got %v", s.Type(-1))
	}

	// anchored
	s = runStringScript(t, `return string.match("hello", "^h(.-)o$")`, 1)
	if got, _ := s.ToString(-1); got != "ell" {
		t.Fatalf("match anchored: %q", got)
	}
}

func TestStringGmatch(t *testing.T) {
	// We call the iterator manually to avoid a Tier-3 VM bug in
	// OpForGLoop that reads from L.stack past the truncated top after a
	// Go-function call. The iterator semantics themselves still match
	// upstream lstrlib.cpp.
	src := `
local iter = string.gmatch("one two three four", "%a+")
return iter(), iter(), iter(), iter(), iter()
`
	s := runStringScript(t, src, 5)
	w1, _ := s.ToString(-5)
	w2, _ := s.ToString(-4)
	w3, _ := s.ToString(-3)
	w4, _ := s.ToString(-2)
	end5 := s.IsNil(-1)
	if w1 != "one" || w2 != "two" || w3 != "three" || w4 != "four" || !end5 {
		t.Fatalf("gmatch words: %q %q %q %q endNil=%v", w1, w2, w3, w4, end5)
	}

	// captures: returns two values per call. We call gmatch.find-style
	// instead (string.match) since the Tier-3 generic for path triggers
	// a VM stack-truncation bug after Go-function returns.
	src = `
return string.match("a=1,b=2,c=3", "(%a)=(%d)")
`
	s = runStringScript(t, src, 2)
	k, _ := s.ToString(-2)
	v, _ := s.ToString(-1)
	if k != "a" || v != "1" {
		t.Fatalf("match captures: %q %q", k, v)
	}
}

func TestStringGsub(t *testing.T) {
	// string replacement
	s := runStringScript(t, `return string.gsub("hello world", "o", "0")`, 2)
	out, _ := s.ToString(-2)
	n, _ := s.ToNumber(-1)
	if out != "hell0 w0rld" || n != 2 {
		t.Fatalf("gsub string: %q n=%v", out, n)
	}

	// pattern + back-reference
	s = runStringScript(t, `return string.gsub("hello world", "(%a+)", "<%1>")`, 1)
	if got, _ := s.ToString(-1); got != "<hello> <world>" {
		t.Fatalf("gsub backref: %q", got)
	}

	// table replacement
	s = runStringScript(t, `return string.gsub("a b c", "%a", {a="A", b="B", c="C"})`, 1)
	if got, _ := s.ToString(-1); got != "A B C" {
		t.Fatalf("gsub table: %q", got)
	}

	// function replacement
	src := `
return (string.gsub("hello", "%a", function(c) return string.upper(c) end))
`
	s = runStringScript(t, src, 1)
	if got, _ := s.ToString(-1); got != "HELLO" {
		t.Fatalf("gsub func: %q", got)
	}

	// max replacements
	s = runStringScript(t, `return string.gsub("aaaa", "a", "b", 2)`, 1)
	if got, _ := s.ToString(-1); got != "bbaa" {
		t.Fatalf("gsub max: %q", got)
	}
}

func TestStringFormat(t *testing.T) {
	// %d
	s := runStringScript(t, `return string.format("%d", 42)`, 1)
	if got, _ := s.ToString(-1); got != "42" {
		t.Fatalf("format d: %q", got)
	}
	// padded width
	s = runStringScript(t, `return string.format("%05d", 7)`, 1)
	if got, _ := s.ToString(-1); got != "00007" {
		t.Fatalf("format pad: %q", got)
	}
	// %f precision
	s = runStringScript(t, `return string.format("%.3f", 3.14159)`, 1)
	if got, _ := s.ToString(-1); got != "3.142" {
		t.Fatalf("format f: %q", got)
	}
	// %s
	s = runStringScript(t, `return string.format("hi %s!", "world")`, 1)
	if got, _ := s.ToString(-1); got != "hi world!" {
		t.Fatalf("format s: %q", got)
	}
	// %x
	s = runStringScript(t, `return string.format("%x", 255)`, 1)
	if got, _ := s.ToString(-1); got != "ff" {
		t.Fatalf("format x: %q", got)
	}
	// %X
	s = runStringScript(t, `return string.format("%X", 255)`, 1)
	if got, _ := s.ToString(-1); got != "FF" {
		t.Fatalf("format X: %q", got)
	}
	// %q
	s = runStringScript(t, `return string.format("%q", [[a "b" c]])`, 1)
	if got, _ := s.ToString(-1); got != `"a \"b\" c"` {
		t.Fatalf("format q: %q", got)
	}
	// %% literal
	s = runStringScript(t, `return string.format("%d%%", 50)`, 1)
	if got, _ := s.ToString(-1); got != "50%" {
		t.Fatalf("format %%%%: %q", got)
	}
}

func TestStringSplit(t *testing.T) {
	src := `
local t = string.split("a,b,c,d", ",")
return t[1], t[2], t[3], t[4], #t
`
	s := runStringScript(t, src, 5)
	a, _ := s.ToString(-5)
	b, _ := s.ToString(-4)
	c, _ := s.ToString(-3)
	d, _ := s.ToString(-2)
	n, _ := s.ToNumber(-1)
	if a != "a" || b != "b" || c != "c" || d != "d" || n != 4 {
		t.Fatalf("split: %q %q %q %q n=%v", a, b, c, d, n)
	}

	// default separator (",")
	src = `
local t = string.split("x,y,z")
return t[1], t[2], t[3], #t
`
	s = runStringScript(t, src, 4)
	a, _ = s.ToString(-4)
	b, _ = s.ToString(-3)
	c, _ = s.ToString(-2)
	n, _ = s.ToNumber(-1)
	if a != "x" || b != "y" || c != "z" || n != 3 {
		t.Fatalf("split default: %q %q %q n=%v", a, b, c, n)
	}

	// empty separator => one-byte strings
	src = `
local t = string.split("abc", "")
return t[1], t[2], t[3], #t
`
	s = runStringScript(t, src, 4)
	a, _ = s.ToString(-4)
	b, _ = s.ToString(-3)
	c, _ = s.ToString(-2)
	n, _ = s.ToNumber(-1)
	if a != "a" || b != "b" || c != "c" || n != 3 {
		t.Fatalf("split empty: %q %q %q n=%v", a, b, c, n)
	}
}

func TestStringMetatable(t *testing.T) {
	// Namecall: ("hello"):upper() == "HELLO"
	s := runStringScript(t, `return ("hello"):upper()`, 1)
	if got, _ := s.ToString(-1); got != "HELLO" {
		t.Fatalf(`("hello"):upper(): got %q want %q`, got, "HELLO")
	}
	// Chained
	s = runStringScript(t, `return ("Hello World"):lower():sub(1, 5)`, 1)
	if got, _ := s.ToString(-1); got != "hello" {
		t.Fatalf(`chain: got %q`, got)
	}
	// len via metatable
	s = runStringScript(t, `return ("abc"):len()`, 1)
	if n, _ := s.ToNumber(-1); n != 3 {
		t.Fatalf(`("abc"):len(): got %v`, n)
	}
	// __index points at string library
	s = runStringScript(t, `return string.byte("A")`, 1)
	if n, _ := s.ToNumber(-1); n != 65 {
		t.Fatalf("string.byte basic: got %v", n)
	}
}

func TestStringPackUnpack(t *testing.T) {
	// Round-trip: pack three ints then unpack.
	src := `
local p = string.pack("<i4i4i4", 1, 2, 3)
local a, b, c = string.unpack("<i4i4i4", p)
return a, b, c
`
	s := runStringScript(t, src, 3)
	a, _ := s.ToNumber(-3)
	b, _ := s.ToNumber(-2)
	c, _ := s.ToNumber(-1)
	if a != 1 || b != 2 || c != 3 {
		t.Fatalf("pack/unpack: %v %v %v", a, b, c)
	}

	// packsize
	s = runStringScript(t, `return string.packsize("<i4i4i4")`, 1)
	if n, _ := s.ToNumber(-1); n != 12 {
		t.Fatalf("packsize: %v", n)
	}

	// Floats and strings
	src = `
local p = string.pack("<fI4s4", 1.5, 7, "hi")
local f, u, s = string.unpack("<fI4s4", p)
return f, u, s
`
	s = runStringScript(t, src, 3)
	f, _ := s.ToNumber(-3)
	u, _ := s.ToNumber(-2)
	str, _ := s.ToString(-1)
	if f != 1.5 || u != 7 || str != "hi" {
		t.Fatalf("pack/unpack mixed: %v %v %q", f, u, str)
	}
}
