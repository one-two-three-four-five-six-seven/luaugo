// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib

import (
	"math"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// runBit32 compiles `source`, runs it, and returns the single numeric
// result it leaves on the stack. The chunk runs under OpenBase + OpenBit32.
func runBit32(t *testing.T, source string) float64 {
	t.Helper()
	s := vm.NewState()
	defer s.Close()
	OpenBase(s)
	OpenBit32(s)
	m, err := compiler.CompileSource("=bit32_test", []byte(source), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := s.LoadModule("=bit32_test", m, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	s.Call(0, 1)
	v, ok := s.ToNumber(-1)
	if !ok {
		t.Fatalf("expected numeric result, got %v", s.Type(-1))
	}
	return v
}

// runBit32Bool is the boolean counterpart of runBit32.
func runBit32Bool(t *testing.T, source string) bool {
	t.Helper()
	s := vm.NewState()
	defer s.Close()
	OpenBase(s)
	OpenBit32(s)
	m, err := compiler.CompileSource("=bit32_test", []byte(source), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := s.LoadModule("=bit32_test", m, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	s.Call(0, 1)
	return s.ToBoolean(-1)
}

// callBit32 invokes bit32.<name>(args...) via the Go API and returns
// the resulting float64. Args are pushed as numbers; all bit32 inputs
// are unsigned-32 anyway.
func callBit32(t *testing.T, name string, args ...float64) float64 {
	t.Helper()
	s := vm.NewState()
	defer s.Close()
	OpenBit32(s)
	s.GetGlobal("bit32")
	if s.Type(-1) != vm.TTable {
		t.Fatalf("bit32 global not a table after OpenBit32: got %v", s.Type(-1))
	}
	s.GetField(-1, name)
	for _, a := range args {
		s.PushNumber(a)
	}
	s.Call(len(args), 1)
	v, ok := s.ToNumber(-1)
	if !ok {
		t.Fatalf("bit32.%s: non-numeric result", name)
	}
	return v
}

func callBit32Bool(t *testing.T, name string, args ...float64) bool {
	t.Helper()
	s := vm.NewState()
	defer s.Close()
	OpenBit32(s)
	s.GetGlobal("bit32")
	s.GetField(-1, name)
	for _, a := range args {
		s.PushNumber(a)
	}
	s.Call(len(args), 1)
	return s.ToBoolean(-1)
}

// pcallBit32 invokes bit32.<name>(args...) under PCall and returns the
// resulting status (with the error message if non-OK).
func pcallBit32(t *testing.T, name string, args ...float64) (vm.Status, string) {
	t.Helper()
	s := vm.NewState()
	defer s.Close()
	OpenBit32(s)
	s.GetGlobal("bit32")
	s.GetField(-1, name)
	for _, a := range args {
		s.PushNumber(a)
	}
	st := s.PCall(len(args), 1, 0)
	if st != vm.StatusOK {
		msg, _ := s.ToString(-1)
		return st, msg
	}
	return st, ""
}

// ----------------------------------------------------------------------
// TestBit32Band
// ----------------------------------------------------------------------

func TestBit32Band(t *testing.T) {
	cases := []struct {
		args []float64
		want float64
	}{
		{[]float64{}, math.MaxUint32},                 // empty -> ~0
		{[]float64{0xFF}, 0xFF},                       // single arg
		{[]float64{0xF0F0F0F0, 0x0F0F0F0F}, 0},        // complementary
		{[]float64{0xFFFF, 0xF0F0, 0x0FF0}, 0x00F0},   // chain
		{[]float64{-1}, math.MaxUint32},               // negative -> 0xFFFFFFFF
		{[]float64{float64(0x123456789)}, 0x23456789}, // value > 2^32 truncated
	}
	for _, c := range cases {
		got := callBit32(t, "band", c.args...)
		if got != c.want {
			t.Errorf("band(%v) = %v, want %v", c.args, got, c.want)
		}
	}

	if got := runBit32(t, "return bit32.band(0xFFFF, 0xF0F0, 0x0FF0)"); got != 0x00F0 {
		t.Errorf("e2e band: got %v want 240", got)
	}
}

// ----------------------------------------------------------------------
// TestBit32Bor
// ----------------------------------------------------------------------

func TestBit32Bor(t *testing.T) {
	cases := []struct {
		args []float64
		want float64
	}{
		{[]float64{}, 0},
		{[]float64{0xFF}, 0xFF},
		{[]float64{0xF0F0F0F0, 0x0F0F0F0F}, math.MaxUint32},
		{[]float64{0x00FF, 0xFF00}, 0xFFFF},
		{[]float64{0, 0, 0}, 0},
	}
	for _, c := range cases {
		got := callBit32(t, "bor", c.args...)
		if got != c.want {
			t.Errorf("bor(%v) = %v, want %v", c.args, got, c.want)
		}
	}
	if got := runBit32(t, "return bit32.bor(0xF0, 0x0F)"); got != 0xFF {
		t.Errorf("e2e bor: got %v want 255", got)
	}
}

// ----------------------------------------------------------------------
// TestBit32Bxor
// ----------------------------------------------------------------------

func TestBit32Bxor(t *testing.T) {
	cases := []struct {
		args []float64
		want float64
	}{
		{[]float64{}, 0},
		{[]float64{0xFF}, 0xFF},
		{[]float64{0xFFFF, 0xFFFF}, 0},
		{[]float64{0xF0F0F0F0, 0x0F0F0F0F}, math.MaxUint32},
		{[]float64{0xAA, 0x55, 0xFF}, 0x00},
	}
	for _, c := range cases {
		got := callBit32(t, "bxor", c.args...)
		if got != c.want {
			t.Errorf("bxor(%v) = %v, want %v", c.args, got, c.want)
		}
	}
	if got := runBit32(t, "return bit32.bxor(0xAA, 0xFF)"); got != 0x55 {
		t.Errorf("e2e bxor: got %v want 85", got)
	}
}

// ----------------------------------------------------------------------
// TestBit32Bnot
// ----------------------------------------------------------------------

func TestBit32Bnot(t *testing.T) {
	cases := []struct {
		arg, want float64
	}{
		{0, math.MaxUint32},
		{math.MaxUint32, 0},
		{0xFFFF, 0xFFFF0000},
		{0xF0F0F0F0, 0x0F0F0F0F},
		{-1, 0},
	}
	for _, c := range cases {
		got := callBit32(t, "bnot", c.arg)
		if got != c.want {
			t.Errorf("bnot(%v) = %v, want %v", c.arg, got, c.want)
		}
	}
	if got := runBit32(t, "return bit32.bnot(0)"); got != math.MaxUint32 {
		t.Errorf("e2e bnot: got %v want %v", got, float64(math.MaxUint32))
	}
}

// ----------------------------------------------------------------------
// TestBit32Btest
// ----------------------------------------------------------------------

func TestBit32Btest(t *testing.T) {
	cases := []struct {
		args []float64
		want bool
	}{
		{[]float64{}, true},
		{[]float64{0}, false},
		{[]float64{0xFF}, true},
		{[]float64{0xF0, 0x0F}, false},
		{[]float64{0xFF, 0xF0}, true},
		{[]float64{math.MaxUint32, math.MaxUint32}, true},
	}
	for _, c := range cases {
		got := callBit32Bool(t, "btest", c.args...)
		if got != c.want {
			t.Errorf("btest(%v) = %v, want %v", c.args, got, c.want)
		}
	}
	if got := runBit32Bool(t, "return bit32.btest(0xFF, 0xF0)"); !got {
		t.Errorf("e2e btest: expected true")
	}
}

// ----------------------------------------------------------------------
// TestBit32Lshift / TestBit32Rshift / TestBit32Arshift
// ----------------------------------------------------------------------

func TestBit32Lshift(t *testing.T) {
	cases := []struct {
		val, n, want float64
	}{
		{1, 0, 1},
		{1, 1, 2},
		{1, 31, 1 << 31},
		{1, 32, 0},
		{1, 100, 0},
		{0xFF, 8, 0xFF00},
		{0xFFFFFFFF, 4, 0xFFFFFFF0},
		{1, -1, 0},
		{0xFF, -4, 0x0F},
	}
	for _, c := range cases {
		got := callBit32(t, "lshift", c.val, c.n)
		if got != c.want {
			t.Errorf("lshift(%v, %v) = %v, want %v", c.val, c.n, got, c.want)
		}
	}
	if got := runBit32(t, "return bit32.lshift(1, 8)"); got != 256 {
		t.Errorf("e2e lshift: got %v want 256", got)
	}
}

func TestBit32Rshift(t *testing.T) {
	cases := []struct {
		val, n, want float64
	}{
		{0x100, 0, 0x100},
		{0x100, 8, 1},
		{0x80000000, 31, 1},
		{0xFFFFFFFF, 32, 0},
		{1, 100, 0},
		{1, -1, 2},
		{0xFFFFFFFF, 4, 0x0FFFFFFF},
	}
	for _, c := range cases {
		got := callBit32(t, "rshift", c.val, c.n)
		if got != c.want {
			t.Errorf("rshift(%v, %v) = %v, want %v", c.val, c.n, got, c.want)
		}
	}
	if got := runBit32(t, "return bit32.rshift(256, 8)"); got != 1 {
		t.Errorf("e2e rshift: got %v want 1", got)
	}
}

func TestBit32Arshift(t *testing.T) {
	cases := []struct {
		val, n, want float64
	}{
		{0x40000000, 1, 0x20000000},  // non-negative: same as rshift
		{0x80000000, 1, 0xC0000000},  // negative: sign-fill
		{0x80000000, 31, 0xFFFFFFFF}, // long shift
		{0x80000000, 32, 0xFFFFFFFF}, // i >= 32 on negative -> all ones
		{0x80000000, 100, 0xFFFFFFFF},
		{0xFFFFFFFF, 4, 0xFFFFFFFF},
		{0x80000000, -1, 0}, // negative i: shift left, sign bit shifted out
		{0x00FFFFFF, 4, 0x000FFFFF},
		{0, 5, 0},
	}
	for _, c := range cases {
		got := callBit32(t, "arshift", c.val, c.n)
		if got != c.want {
			t.Errorf("arshift(%v, %v) = %v, want %v", c.val, c.n, got, c.want)
		}
	}
	if got := runBit32(t, "return bit32.arshift(0x80000000, 4)"); got != 0xF8000000 {
		t.Errorf("e2e arshift: got %v want %v", got, float64(0xF8000000))
	}
}

// ----------------------------------------------------------------------
// TestBit32Extract / TestBit32Replace
// ----------------------------------------------------------------------

func TestBit32Extract(t *testing.T) {
	cases := []struct {
		val, field float64
		width      []float64 // optional
		want       float64
	}{
		{0xDEADBEEF, 0, nil, 1},
		{0xDEADBEEF, 0, []float64{4}, 0xF},
		{0xDEADBEEF, 4, []float64{4}, 0xE},
		{0xDEADBEEF, 8, []float64{8}, 0xBE},
		{0xDEADBEEF, 16, []float64{16}, 0xDEAD},
		{0xFFFFFFFF, 0, []float64{32}, 0xFFFFFFFF},
		{0, 31, nil, 0},
		{0x80000000, 31, nil, 1},
	}
	for _, c := range cases {
		var got float64
		if c.width != nil {
			got = callBit32(t, "extract", c.val, c.field, c.width[0])
		} else {
			got = callBit32(t, "extract", c.val, c.field)
		}
		if got != c.want {
			t.Errorf("extract(%v, %v, %v) = %v, want %v", c.val, c.field, c.width, got, c.want)
		}
	}

	// Error cases.
	bad := []struct{ args []float64 }{
		{[]float64{0, -1}},    // negative field
		{[]float64{0, 0, 0}},  // zero width
		{[]float64{0, 0, -1}}, // negative width
		{[]float64{0, 31, 2}}, // overflow: 31+2=33
		{[]float64{0, 0, 33}}, // width alone > 32
	}
	for _, b := range bad {
		st, _ := pcallBit32(t, "extract", b.args...)
		if st == vm.StatusOK {
			t.Errorf("extract(%v): expected error, got OK", b.args)
		}
	}

	if got := runBit32(t, "return bit32.extract(0xDEADBEEF, 16, 16)"); got != 0xDEAD {
		t.Errorf("e2e extract: got %v want %v", got, float64(0xDEAD))
	}
}

func TestBit32Replace(t *testing.T) {
	cases := []struct {
		val, v, field float64
		width         []float64
		want          float64
	}{
		{0, 1, 0, nil, 1},
		{0xFFFFFFFF, 0, 0, nil, 0xFFFFFFFE},
		{0, 0xFF, 8, []float64{8}, 0xFF00},
		{0xDEADBEEF, 0xCAFE, 16, []float64{16}, 0xCAFEBEEF},
		{0xFFFFFFFF, 0, 0, []float64{32}, 0},
		{0x12345678, 0xABCD, 8, []float64{16}, 0x12ABCD78}, // bits[8..23] = 0xABCD
	}
	for _, c := range cases {
		var got float64
		if c.width != nil {
			got = callBit32(t, "replace", c.val, c.v, c.field, c.width[0])
		} else {
			got = callBit32(t, "replace", c.val, c.v, c.field)
		}
		if got != c.want {
			t.Errorf("replace(%#x, %#x, %v, %v) = %#x, want %#x",
				uint32(c.val), uint32(c.v), c.field, c.width, uint32(got), uint32(c.want))
		}
	}

	// Bits outside `width` in v must be ignored (masked off).
	if got := callBit32(t, "replace", 0, 0xFFFFFFFF, 0, 4); got != 0xF {
		t.Errorf("replace mask: got %v want 15", got)
	}

	// Error cases.
	bad := []struct{ args []float64 }{
		{[]float64{0, 0, -1}},    // negative field
		{[]float64{0, 0, 0, 0}},  // zero width
		{[]float64{0, 0, 31, 2}}, // overflow
	}
	for _, b := range bad {
		st, _ := pcallBit32(t, "replace", b.args...)
		if st == vm.StatusOK {
			t.Errorf("replace(%v): expected error, got OK", b.args)
		}
	}

	if got := runBit32(t, "return bit32.replace(0xDEADBEEF, 0xCAFE, 16, 16)"); got != 0xCAFEBEEF {
		t.Errorf("e2e replace: got %v want %v", got, float64(0xCAFEBEEF))
	}
}

// ----------------------------------------------------------------------
// TestBit32Rotate covers lrotate + rrotate.
// ----------------------------------------------------------------------

func TestBit32Rotate(t *testing.T) {
	// lrotate
	lcases := []struct {
		val, n, want float64
	}{
		{0x12345678, 0, 0x12345678},
		{0x12345678, 4, 0x23456781},
		{0x12345678, 32, 0x12345678}, // full cycle
		{0x12345678, 36, 0x23456781}, // 36 mod 32 == 4
		{0x80000000, 1, 1},           // top bit -> bottom
		{1, -1, 0x80000000},          // negative lrotate = rrotate
		{0x12345678, -4, 0x81234567},
	}
	for _, c := range lcases {
		got := callBit32(t, "lrotate", c.val, c.n)
		if got != c.want {
			t.Errorf("lrotate(%#x, %v) = %#x, want %#x",
				uint32(c.val), c.n, uint32(got), uint32(c.want))
		}
	}

	// rrotate
	rcases := []struct {
		val, n, want float64
	}{
		{0x12345678, 0, 0x12345678},
		{0x12345678, 4, 0x81234567},
		{0x12345678, 32, 0x12345678},
		{1, 1, 0x80000000},
		{0x80000000, -1, 1},
		{0x12345678, -4, 0x23456781},
	}
	for _, c := range rcases {
		got := callBit32(t, "rrotate", c.val, c.n)
		if got != c.want {
			t.Errorf("rrotate(%#x, %v) = %#x, want %#x",
				uint32(c.val), c.n, uint32(got), uint32(c.want))
		}
	}

	// Composition: lrotate then rrotate restores the value.
	roundtrip := callBit32(t, "rrotate", callBit32(t, "lrotate", 0xDEADBEEF, 13), 13)
	if roundtrip != 0xDEADBEEF {
		t.Errorf("lrotate/rrotate roundtrip: got %v want %v", roundtrip, float64(0xDEADBEEF))
	}

	if got := runBit32(t, "return bit32.lrotate(0x12345678, 4)"); got != 0x23456781 {
		t.Errorf("e2e lrotate: got %v want %v", got, float64(0x23456781))
	}
	if got := runBit32(t, "return bit32.rrotate(0x12345678, 4)"); got != 0x81234567 {
		t.Errorf("e2e rrotate: got %v want %v", got, float64(0x81234567))
	}
}

// ----------------------------------------------------------------------
// TestBit32Countlz / TestBit32Countrz
// ----------------------------------------------------------------------

func TestBit32Countlz(t *testing.T) {
	cases := []struct {
		val, want float64
	}{
		{0, 32},
		{1, 31},
		{0x80000000, 0},
		{0xFFFFFFFF, 0},
		{0x00010000, 15},
		{2, 30},
	}
	for _, c := range cases {
		got := callBit32(t, "countlz", c.val)
		if got != c.want {
			t.Errorf("countlz(%#x) = %v, want %v", uint32(c.val), got, c.want)
		}
	}
	if got := runBit32(t, "return bit32.countlz(0x00010000)"); got != 15 {
		t.Errorf("e2e countlz: got %v want 15", got)
	}
}

func TestBit32Countrz(t *testing.T) {
	cases := []struct {
		val, want float64
	}{
		{0, 32},
		{1, 0},
		{2, 1},
		{0x80000000, 31},
		{0xFFFFFFFF, 0},
		{0x00010000, 16},
	}
	for _, c := range cases {
		got := callBit32(t, "countrz", c.val)
		if got != c.want {
			t.Errorf("countrz(%#x) = %v, want %v", uint32(c.val), got, c.want)
		}
	}
	if got := runBit32(t, "return bit32.countrz(0x80000000)"); got != 31 {
		t.Errorf("e2e countrz: got %v want 31", got)
	}
}

// ----------------------------------------------------------------------
// TestBit32Byteswap
// ----------------------------------------------------------------------

func TestBit32Byteswap(t *testing.T) {
	cases := []struct {
		val, want float64
	}{
		{0x00000000, 0x00000000},
		{0xFFFFFFFF, 0xFFFFFFFF},
		{0x12345678, 0x78563412},
		{0xAABBCCDD, 0xDDCCBBAA},
		{0x000000FF, 0xFF000000},
		{0xFF000000, 0x000000FF},
	}
	for _, c := range cases {
		got := callBit32(t, "byteswap", c.val)
		if got != c.want {
			t.Errorf("byteswap(%#x) = %#x, want %#x",
				uint32(c.val), uint32(got), uint32(c.want))
		}
	}
	if got := runBit32(t, "return bit32.byteswap(0x12345678)"); got != 0x78563412 {
		t.Errorf("e2e byteswap: got %v want %v", got, float64(0x78563412))
	}
}

// ----------------------------------------------------------------------
// TestBit32StringCoercion
//
// Upstream Luau's bit32.* operations consume their numeric arguments
// via luaL_checkunsigned, which calls lua_tointeger, which falls back
// to luaO_str2d for string operands. That lexer accepts decimal, `0x`
// hex, and `0b` binary literals (with optional sign / whitespace). Our
// VM had only ParseFloat in value.asNumber(), which silently fails on
// hex strings and caused tests/conformance/bitwise.luau to die at the
// first `bit32.lrotate("0x12345678", 4)` assertion.
//
// This test locks in the corrected behaviour: bit32.* must accept
// numeric strings in every shape Luau's number lexer accepts.
// ----------------------------------------------------------------------

func TestBit32StringCoercion(t *testing.T) {
	cases := []struct {
		src  string
		want float64
	}{
		// Hex strings (the original regression).
		{`return bit32.lrotate("0x12345678", 4)`, 0x23456781},
		{`return bit32.rrotate("0x12345678", -4)`, 0x23456781},
		{`return bit32.band("0xff", "0x0f")`, 0x0f},
		{`return bit32.bor("0xf0", "0x0f")`, 0xff},
		{`return bit32.bxor("0xff", "0x0f")`, 0xf0},
		{`return bit32.byteswap("0xa1b2c3d4")`, 0xd4c3b2a1},
		// Plain decimal strings still work.
		{`return bit32.bnot("1")`, 0xfffffffe},
		{`return bit32.band("1", 3)`, 1},
		{`return bit32.band(1, "3")`, 1},
		{`return bit32.band(1, 3, "5")`, 1},
		{`return bit32.bor("1", 2)`, 3},
		{`return bit32.bxor("1", 3)`, 2},
		{`return bit32.countlz("42")`, 26},
		{`return bit32.countrz("42")`, 1},
		{`return bit32.extract("42", 1, 3)`, 5},
		// Signed decimal: arshift("-1", n) - "-1" -> 0xffffffff after
		// unsigned reduction, then the sign bit forces an arithmetic
		// shift filling with 1s.
		{`return bit32.arshift("-1", 1)`, 0xffffffff},
		{`return bit32.arshift("-1", 32)`, 0xffffffff},
		// Binary literal coercion (Luau extension).
		{`return bit32.band("0b1100", "0b1010")`, 0b1000},
	}
	for _, c := range cases {
		got := runBit32(t, c.src)
		if got != c.want {
			t.Errorf("%s: got %v (%#x), want %v (%#x)",
				c.src, got, uint32(got), c.want, uint32(c.want))
		}
	}
	// btest with a string operand should still return a boolean.
	if !runBit32Bool(t, `return bit32.btest("1", 3)`) {
		t.Error(`bit32.btest("1", 3): expected true`)
	}
	if !runBit32Bool(t, `return bit32.btest(1, "3")`) {
		t.Error(`bit32.btest(1, "3"): expected true`)
	}
}
