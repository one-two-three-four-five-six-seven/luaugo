// Copyright (c) luaugo contributors. Licensed under the MIT License.

package common

import "testing"

func TestOpcodeNumericValues(t *testing.T) {
	// Spot check against upstream Bytecode.h enum LuauOpcode order.
	cases := []struct {
		op   Opcode
		want uint8
		name string
	}{
		{OpNop, 0, "NOP"},
		{OpLoadNil, 2, "LOADNIL"},
		{OpMove, 6, "MOVE"},
		{OpCall, 21, "CALL"},
		{OpReturn, 22, "RETURN"},
		{OpFastCall, 68, "FASTCALL"},
		{OpForGPrep, 76, "FORGPREP"},
		{OpJumpXEqKNil, 77, "JUMPXEQKNIL"},
		{OpIdiv, 81, "IDIV"},
		{OpIdivK, 82, "IDIVK"},
		{OpGetUdataKS, 83, "GETUDATAKS"},
		{OpSetUdataKS, 84, "SETUDATAKS"},
		{OpNameCallUdata, 85, "NAMECALLUDATA"},
	}
	for _, c := range cases {
		if uint8(c.op) != c.want {
			t.Errorf("%s: got numeric value %d, want %d", c.name, uint8(c.op), c.want)
		}
		if got := c.op.String(); got != c.name {
			t.Errorf("opcode %d: got name %q, want %q", uint8(c.op), got, c.name)
		}
	}
	if OpCount != 86 {
		t.Errorf("OpCount = %d, want 86", OpCount)
	}
}

func TestEncodeDecodeABC(t *testing.T) {
	insn := EncodeABC(OpAdd, 10, 20, 30)
	if InsnOp(insn) != OpAdd {
		t.Errorf("op: got %v, want OpAdd", InsnOp(insn))
	}
	if InsnA(insn) != 10 {
		t.Errorf("A: got %d, want 10", InsnA(insn))
	}
	if InsnB(insn) != 20 {
		t.Errorf("B: got %d, want 20", InsnB(insn))
	}
	if InsnC(insn) != 30 {
		t.Errorf("C: got %d, want 30", InsnC(insn))
	}
}

func TestEncodeDecodeAD(t *testing.T) {
	for _, d := range []int32{0, 1, -1, 32767, -32768, 100, -100} {
		insn := EncodeAD(OpLoadN, 5, d)
		if InsnOp(insn) != OpLoadN {
			t.Errorf("d=%d: op mismatch", d)
		}
		if InsnA(insn) != 5 {
			t.Errorf("d=%d: A mismatch", d)
		}
		if got := InsnD(insn); got != d {
			t.Errorf("d=%d: got back %d", d, got)
		}
	}
}

func TestEncodeDecodeE(t *testing.T) {
	for _, e := range []int32{0, 1, -1, 8388607, -8388608, 1000, -1000} {
		insn := EncodeE(OpJumpX, e)
		if InsnOp(insn) != OpJumpX {
			t.Errorf("e=%d: op mismatch", e)
		}
		if got := InsnE(insn); got != e {
			t.Errorf("e=%d: got back %d (insn=0x%08x)", e, got, insn)
		}
	}
}

func TestInsnAuxAccessors(t *testing.T) {
	aux := uint32(0x80000042) // NOT bit set, KV24 = 0x42
	if InsnAuxNot(aux) != 1 {
		t.Errorf("NOT bit: got %d, want 1", InsnAuxNot(aux))
	}
	if InsnAuxKV(aux) != 0x42 {
		t.Errorf("KV24: got 0x%x, want 0x42", InsnAuxKV(aux))
	}
	aux2 := uint32(0x12345678)
	if InsnAuxA(aux2) != 0x78 || InsnAuxB(aux2) != 0x56 {
		t.Errorf("AuxA/B: got 0x%x / 0x%x, want 0x78 / 0x56", InsnAuxA(aux2), InsnAuxB(aux2))
	}
	if InsnAuxKV16(aux2) != 0x5678 || InsnAuxSlot(aux2) != 0x1234 {
		t.Errorf("AuxKV16/Slot: got 0x%x / 0x%x", InsnAuxKV16(aux2), InsnAuxSlot(aux2))
	}
}
