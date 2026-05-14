// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"testing"

	"github.com/luaugo/luaugo/internal/common"
	"github.com/luaugo/luaugo/pkg/bytecode"
)

// fastcallSandbox ensures the global env is safeenv so the FASTCALL
// fast path is taken (mirrors upstream where stdlib registration
// implies a safeenv).
func fastcallSandbox(s *State) {
	s.impl.globals.safeenv = true
}

// buildFastcall1Module hand-builds a Proto whose code is:
//
//	LOADN  0, arg
//	FASTCALL1 <builtin>, 0, skip=2
//	GETGLOBAL 0, <fname>   ; only executed on fallback
//	  AUX (kv index)
//	CALL   0, 2, 2         ; nargs=1, nresults=1
//	RETURN 0, 2            ; return R(0)
//
// On the fast path FASTCALL writes R(0) and jumps past the CALL.
//
// arg is loaded as a signed 16-bit number via LOADN.
func buildFastcall1Module(builtin common.Builtin, fname string, arg int16) *bytecode.Module {
	// Constant 0 is the function name string used by GETGLOBAL fallback.
	consts := []bytecode.Constant{
		bytecode.ConstantStringEntry{Index: 1},
	}
	strs := []string{fname}
	code := []uint32{
		common.EncodeAD(common.OpLoadN, 0, int32(arg)),
		// FASTCALL1: A=builtin, B=reg0 (R(0)=arg), C=skip past CALL.
		// Skip count is "number of insns from FASTCALL1+1 to the CALL".
		// Layout: [FASTCALL1] [GETGLOBAL] [AUX] [CALL]; so skip = 2
		// (advances past GETGLOBAL+AUX, lands at CALL).
		common.EncodeABC(common.OpFastCall1, uint8(builtin), 0, 2),
		common.EncodeABC(common.OpGetGlobal, 0, 0, 0),
		0, // AUX = constant index 0 (the function name)
		common.EncodeABC(common.OpCall, 0, 2, 2),
		common.EncodeABC(common.OpReturn, 0, 2, 0),
	}
	return buildModule(code, consts, strs, 4)
}

// buildFastcall2Module hand-builds a Proto whose code is:
//
//	LOADN  0, arg0
//	LOADN  1, arg1
//	FASTCALL2 <builtin>, 0, skip=3, AUX=reg1
//	GETGLOBAL 0, <fname>   ; only executed on fallback
//	  AUX (kv index)
//	MOVE 1, 1              ; placeholder to keep arg1 in R(1) for CALL fallback
//	CALL   0, 3, 2         ; nargs=2, nresults=1
//	RETURN 0, 2
//
// Layout from FASTCALL2 (with AUX consumed):
//
//	pc points after FASTCALL2+AUX, at GETGLOBAL.
//	skip = C-1 means jump (skip) insns from there to CALL.
func buildFastcall2Module(builtin common.Builtin, fname string, a0, a1 int16) *bytecode.Module {
	consts := []bytecode.Constant{
		bytecode.ConstantStringEntry{Index: 1},
	}
	strs := []string{fname}
	// We need to lay out:
	//   FASTCALL2 ; AUX(reg1)
	//   GETGLOBAL ; AUX(kv)
	//   MOVE 1,1                       ; harmless filler
	//   CALL 0,3,2
	// After FASTCALL2 consumes AUX, pc points at GETGLOBAL. skip = C-1
	// must equal 3 (GETGLOBAL, AUX, MOVE) so pc+skip lands on CALL.
	// So C = 4.
	code := []uint32{
		common.EncodeAD(common.OpLoadN, 0, int32(a0)),
		common.EncodeAD(common.OpLoadN, 1, int32(a1)),
		common.EncodeABC(common.OpFastCall2, uint8(builtin), 0, 4),
		uint32(1), // AUX = reg index for second arg (R(1))
		common.EncodeABC(common.OpGetGlobal, 0, 0, 0),
		0, // AUX = constant 0
		common.EncodeABC(common.OpMove, 1, 1, 0),
		common.EncodeABC(common.OpCall, 0, 3, 2),
		common.EncodeABC(common.OpReturn, 0, 2, 0),
	}
	return buildModule(code, consts, strs, 5)
}

// ----------------------------------------------------------------------
// FASTCALL1: math.abs
// ----------------------------------------------------------------------

func TestFastcallMathAbs(t *testing.T) {
	s := NewState()
	defer s.Close()
	fastcallSandbox(s)

	m := buildFastcall1Module(common.BuiltinMathAbs, "__noexist_abs", -7)
	if err := s.LoadModule("=mathabs", m, 0); err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	s.Call(0, 1)
	got, ok := s.ToNumber(-1)
	if !ok || got != 7 {
		t.Fatalf("math.abs(-7) via FASTCALL1: got %v ok=%v", got, ok)
	}
}

// ----------------------------------------------------------------------
// FASTCALL1: string.len (via LOADK because the arg is a string)
// ----------------------------------------------------------------------

func TestFastcallStringLen(t *testing.T) {
	s := NewState()
	defer s.Close()
	fastcallSandbox(s)

	// We need to seed R(0) with a string. LOADK reads from Constants.
	consts := []bytecode.Constant{
		bytecode.ConstantStringEntry{Index: 1}, // "hello"
		bytecode.ConstantStringEntry{Index: 2}, // "__noexist_len"
	}
	strs := []string{"hello", "__noexist_len"}
	code := []uint32{
		common.EncodeAD(common.OpLoadK, 0, 0), // R(0)="hello"
		common.EncodeABC(common.OpFastCall1, uint8(common.BuiltinStringLen), 0, 2),
		common.EncodeABC(common.OpGetGlobal, 0, 0, 0),
		1, // AUX = constant 1 ("__noexist_len")
		common.EncodeABC(common.OpCall, 0, 2, 2),
		common.EncodeABC(common.OpReturn, 0, 2, 0),
	}
	m := buildModule(code, consts, strs, 3)
	if err := s.LoadModule("=strlen", m, 0); err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	s.Call(0, 1)
	got, ok := s.ToNumber(-1)
	if !ok || got != 5 {
		t.Fatalf("string.len('hello') via FASTCALL1: got %v ok=%v", got, ok)
	}
}

// ----------------------------------------------------------------------
// FASTCALL1: type(...)
// ----------------------------------------------------------------------

func TestFastcallType(t *testing.T) {
	s := NewState()
	defer s.Close()
	fastcallSandbox(s)

	consts := []bytecode.Constant{
		bytecode.ConstantStringEntry{Index: 1}, // "__noexist_type"
	}
	strs := []string{"__noexist_type"}
	code := []uint32{
		common.EncodeAD(common.OpLoadN, 0, 42), // R(0)=42 (number)
		common.EncodeABC(common.OpFastCall1, uint8(common.BuiltinType), 0, 2),
		common.EncodeABC(common.OpGetGlobal, 0, 0, 0),
		0, // AUX
		common.EncodeABC(common.OpCall, 0, 2, 2),
		common.EncodeABC(common.OpReturn, 0, 2, 0),
	}
	m := buildModule(code, consts, strs, 3)
	if err := s.LoadModule("=type", m, 0); err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	s.Call(0, 1)
	got, ok := s.ToString(-1)
	if !ok || got != "number" {
		t.Fatalf("type(42) via FASTCALL1: got %q ok=%v", got, ok)
	}
}

// ----------------------------------------------------------------------
// FASTCALL2: bit32.band
// ----------------------------------------------------------------------

func TestFastcallBit32Band(t *testing.T) {
	s := NewState()
	defer s.Close()
	fastcallSandbox(s)

	// 0xff & 0x0f = 0x0f = 15.
	m := buildFastcall2Module(common.BuiltinBit32Band, "__noexist_band", 0xff, 0x0f)
	if err := s.LoadModule("=band", m, 0); err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	s.Call(0, 1)
	got, ok := s.ToNumber(-1)
	if !ok || got != 15 {
		t.Fatalf("bit32.band(0xff, 0x0f) via FASTCALL2: got %v ok=%v", got, ok)
	}
}

// ----------------------------------------------------------------------
// Unit tests bypassing FASTCALL: exercise builtinTable entries
// directly.
// ----------------------------------------------------------------------

func TestBuiltinTableMathFloor(t *testing.T) {
	s := NewState()
	defer s.Close()
	L := s.impl
	L.reserve(2)
	L.stack[0] = numberValue(3.7)
	L.stack[1] = nilValue()
	n, ok := dispatchFastcall(L, common.BuiltinMathFloor, 1, L.stack[0], nil, 1, 1)
	if !ok || n != 1 {
		t.Fatalf("dispatch: ok=%v n=%d", ok, n)
	}
	if got := L.stack[1].num; got != 3.0 {
		t.Fatalf("math.floor(3.7): got %v", got)
	}
}

func TestBuiltinTableMathCeil(t *testing.T) {
	s := NewState()
	defer s.Close()
	L := s.impl
	L.reserve(2)
	L.stack[0] = numberValue(3.2)
	L.stack[1] = nilValue()
	n, ok := dispatchFastcall(L, common.BuiltinMathCeil, 1, L.stack[0], nil, 1, 1)
	if !ok || n != 1 || L.stack[1].num != 4.0 {
		t.Fatalf("math.ceil(3.2): ok=%v n=%d v=%v", ok, n, L.stack[1].num)
	}
}

func TestBuiltinTableMathClamp(t *testing.T) {
	s := NewState()
	defer s.Close()
	L := s.impl
	L.reserve(4)
	L.stack[0] = numberValue(5)
	args := []value{numberValue(0), numberValue(3)}
	n, ok := dispatchFastcall(L, common.BuiltinMathClamp, 3, L.stack[0], args, 1, 3)
	if !ok || n != 1 || L.stack[3].num != 3 {
		t.Fatalf("clamp(5,0,3)=3: ok=%v n=%d v=%v", ok, n, L.stack[3].num)
	}
}

func TestBuiltinTableMathSign(t *testing.T) {
	s := NewState()
	defer s.Close()
	L := s.impl
	L.reserve(2)
	L.stack[0] = numberValue(-3.0)
	n, ok := dispatchFastcall(L, common.BuiltinMathSign, 1, L.stack[0], nil, 1, 1)
	if !ok || n != 1 || L.stack[1].num != -1.0 {
		t.Fatalf("sign(-3): ok=%v n=%d v=%v", ok, n, L.stack[1].num)
	}
	L.stack[0] = numberValue(0)
	dispatchFastcall(L, common.BuiltinMathSign, 1, L.stack[0], nil, 1, 1)
	if L.stack[1].num != 0 {
		t.Fatalf("sign(0): %v", L.stack[1].num)
	}
}

func TestBuiltinTableBit32Bnot(t *testing.T) {
	s := NewState()
	defer s.Close()
	L := s.impl
	L.reserve(2)
	L.stack[0] = numberValue(0)
	n, ok := dispatchFastcall(L, common.BuiltinBit32Bnot, 1, L.stack[0], nil, 1, 1)
	if !ok || n != 1 {
		t.Fatalf("bnot(0): ok=%v n=%d", ok, n)
	}
	if got := L.stack[1].num; got != 4294967295 {
		t.Fatalf("bnot(0)=0xFFFFFFFF: got %v", got)
	}
}

func TestBuiltinTableBit32Shift(t *testing.T) {
	s := NewState()
	defer s.Close()
	L := s.impl
	L.reserve(4)
	L.stack[0] = numberValue(1)
	args := []value{numberValue(4)}
	if _, ok := dispatchFastcall(L, common.BuiltinBit32LShift, 2, L.stack[0], args, 1, 2); !ok {
		t.Fatalf("lshift dispatch failed")
	}
	if L.stack[2].num != 16 {
		t.Fatalf("1<<4 = 16: got %v", L.stack[2].num)
	}
	// arshift: -1 >> 1 should stay all-ones since the top bit replicates.
	L.stack[0] = numberValue(float64(uint32(0x80000000)))
	args[0] = numberValue(1)
	if _, ok := dispatchFastcall(L, common.BuiltinBit32ArShift, 2, L.stack[0], args, 1, 2); !ok {
		t.Fatalf("arshift dispatch failed")
	}
	if got := uint32(L.stack[2].num); got != 0xC0000000 {
		t.Fatalf("arshift(0x80000000,1)=0xC0000000: got 0x%x", got)
	}
}

func TestBuiltinTableStringByteChar(t *testing.T) {
	s := NewState()
	defer s.Close()
	L := s.impl
	L.reserve(4)
	hs := L.gs.intern("hello")
	L.stack[0] = stringValue(hs)
	args := []value{numberValue(1)}
	if n, ok := dispatchFastcall(L, common.BuiltinStringByte, 1, L.stack[0], args, 1, 2); !ok || n != 1 {
		t.Fatalf("string.byte('hello',1): ok=%v n=%d", ok, n)
	}
	if L.stack[1].num != 'h' {
		t.Fatalf("string.byte('hello',1)=104: got %v", L.stack[1].num)
	}
	// string.char(65,66) -> "AB"
	L.stack[0] = numberValue(65)
	args2 := []value{numberValue(66)}
	if n, ok := dispatchFastcall(L, common.BuiltinStringChar, 2, L.stack[0], args2, 1, 2); !ok || n != 1 {
		t.Fatalf("string.char(65,66): ok=%v n=%d", ok, n)
	}
	if got, _ := L.stack[2].asString(); got != "AB" {
		t.Fatalf("string.char(65,66)='AB': got %q", got)
	}
}

func TestBuiltinTableRawEqLen(t *testing.T) {
	s := NewState()
	defer s.Close()
	L := s.impl
	L.reserve(4)
	a := stringValue(L.gs.intern("foo"))
	b := stringValue(L.gs.intern("foo"))
	L.stack[0] = a
	args := []value{b}
	if _, ok := dispatchFastcall(L, common.BuiltinRawEqual, 2, L.stack[0], args, 1, 2); !ok {
		t.Fatalf("rawequal dispatch failed")
	}
	if !L.stack[2].bool_ {
		t.Fatalf("rawequal('foo','foo')=true: got false")
	}
	// rawlen of "foo"
	L.stack[0] = a
	if _, ok := dispatchFastcall(L, common.BuiltinRawLen, 1, L.stack[0], nil, 1, 1); !ok {
		t.Fatalf("rawlen dispatch failed")
	}
	if L.stack[1].num != 3 {
		t.Fatalf("rawlen('foo')=3: got %v", L.stack[1].num)
	}
}

func TestBuiltinTableRawGetSet(t *testing.T) {
	s := NewState()
	defer s.Close()
	L := s.impl
	L.reserve(4)
	tbl := newTable(L.gs, 0, 0)
	tv := tableValue(tbl)
	key := stringValue(L.gs.intern("k"))
	val := numberValue(42)
	L.stack[0] = tv
	args := []value{key, val}
	if _, ok := dispatchFastcall(L, common.BuiltinRawSet, 3, L.stack[0], args, 1, 3); !ok {
		t.Fatalf("rawset dispatch failed")
	}
	// rawget should yield 42.
	L.stack[0] = tv
	args2 := []value{key}
	if _, ok := dispatchFastcall(L, common.BuiltinRawGet, 3, L.stack[0], args2, 1, 2); !ok {
		t.Fatalf("rawget dispatch failed")
	}
	if L.stack[3].num != 42 {
		t.Fatalf("rawget returned %v", L.stack[3])
	}
}

func TestBuiltinTableToNumberToString(t *testing.T) {
	s := NewState()
	defer s.Close()
	L := s.impl
	L.reserve(4)
	// tonumber("3.5") -> 3.5
	str := stringValue(L.gs.intern("3.5"))
	if _, ok := dispatchFastcall(L, common.BuiltinToNumber, 1, str, nil, 1, 1); !ok {
		t.Fatalf("tonumber dispatch failed")
	}
	if L.stack[1].num != 3.5 {
		t.Fatalf("tonumber('3.5'): got %v", L.stack[1].num)
	}
	// tostring(true) -> "true"
	bv := booleanValue(true)
	if _, ok := dispatchFastcall(L, common.BuiltinToString, 1, bv, nil, 1, 1); !ok {
		t.Fatalf("tostring dispatch failed")
	}
	if got, _ := L.stack[1].asString(); got != "true" {
		t.Fatalf("tostring(true): got %q", got)
	}
}

func TestBuiltinTableTableUnpack(t *testing.T) {
	s := NewState()
	defer s.Close()
	L := s.impl
	L.reserve(8)
	tbl := newTable(L.gs, 3, 0)
	tbl.setNum(L.gs, 1, numberValue(10))
	tbl.setNum(L.gs, 2, numberValue(20))
	tbl.setNum(L.gs, 3, numberValue(30))
	tv := tableValue(tbl)
	n, ok := dispatchFastcall(L, common.BuiltinTableUnpack, 1, tv, nil, MultRet, 1)
	if !ok || n != 3 {
		t.Fatalf("table.unpack: ok=%v n=%d", ok, n)
	}
	if L.stack[1].num != 10 || L.stack[2].num != 20 || L.stack[3].num != 30 {
		t.Fatalf("unpack values: %v %v %v", L.stack[1], L.stack[2], L.stack[3])
	}
}
