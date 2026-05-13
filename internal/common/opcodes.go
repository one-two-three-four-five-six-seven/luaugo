// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

// Package common holds shared constants for the luaugo project: opcodes,
// bytecode version numbers, builtin function ids, capture types, and proto
// flags. The values are baked into the on-disk bytecode and therefore
// MUST match upstream Luau exactly. Source of truth:
// .upstream/Common/include/Luau/Bytecode.h
package common

// Opcode is the operation field of a Luau instruction word. The opcode
// occupies the least significant byte of the 32-bit instruction header.
type Opcode uint8

// Luau opcode enumeration. The numeric values are part of the bytecode
// format and must not be reordered. See upstream Bytecode.h enum
// LuauOpcode for the canonical specification.
const (
	OpNop          Opcode = 0  // noop
	OpBreak        Opcode = 1  // debugger break
	OpLoadNil      Opcode = 2  // R(A) = nil
	OpLoadB        Opcode = 3  // R(A) = (B != 0); pc += C
	OpLoadN        Opcode = 4  // R(A) = D (int16 literal)
	OpLoadK        Opcode = 5  // R(A) = K(D)
	OpMove         Opcode = 6  // R(A) = R(B)
	OpGetGlobal    Opcode = 7  // R(A) = Globals[K(AUX)], C is cached slot
	OpSetGlobal    Opcode = 8  // Globals[K(AUX)] = R(A), C is cached slot
	OpGetUpval     Opcode = 9  // R(A) = Upvalue(B)
	OpSetUpval     Opcode = 10 // Upvalue(B) = R(A)
	OpCloseUpvals  Opcode = 11 // close upvalues for stack slots >= R(A)
	OpGetImport    Opcode = 12 // R(A) = imported global path encoded in K(D)+AUX
	OpGetTable     Opcode = 13 // R(A) = R(B)[R(C)]
	OpSetTable     Opcode = 14 // R(B)[R(C)] = R(A)
	OpGetTableKS   Opcode = 15 // R(A) = R(B)[K(AUX)], C is cached slot
	OpSetTableKS   Opcode = 16 // R(B)[K(AUX)] = R(A), C is cached slot
	OpGetTableN    Opcode = 17 // R(A) = R(B)[C+1]
	OpSetTableN    Opcode = 18 // R(B)[C+1] = R(A)
	OpNewClosure   Opcode = 19 // R(A) = closure of child proto D; followed by CAPTURE instrs
	OpNameCall     Opcode = 20 // prepare method call by name: R(A) = R(B):K(AUX); R(A+1) = R(B)
	OpCall         Opcode = 21 // R(A), ... = R(A)(R(A+1), ..., R(A+B-1)); B=0 means MULTRET in args
	OpReturn       Opcode = 22 // return R(A), ..., R(A+B-2); B=0 means return values up to top
	OpJump         Opcode = 23 // pc += D
	OpJumpBack     Opcode = 24 // pc += D (safepoint, used by while/repeat loops)
	OpJumpIf       Opcode = 25 // if R(A) is truthy then pc += D
	OpJumpIfNot    Opcode = 26 // if R(A) is falsy then pc += D
	OpJumpIfEq     Opcode = 27 // if R(A) == R(AUX) then pc += D
	OpJumpIfLe     Opcode = 28 // if R(A) <= R(AUX) then pc += D
	OpJumpIfLt     Opcode = 29 // if R(A) < R(AUX) then pc += D
	OpJumpIfNotEq  Opcode = 30 // if R(A) != R(AUX) then pc += D
	OpJumpIfNotLe  Opcode = 31 // if R(A) > R(AUX) then pc += D
	OpJumpIfNotLt  Opcode = 32 // if R(A) >= R(AUX) then pc += D
	OpAdd          Opcode = 33 // R(A) = R(B) + R(C)
	OpSub          Opcode = 34 // R(A) = R(B) - R(C)
	OpMul          Opcode = 35 // R(A) = R(B) * R(C)
	OpDiv          Opcode = 36 // R(A) = R(B) / R(C)
	OpMod          Opcode = 37 // R(A) = R(B) % R(C)
	OpPow          Opcode = 38 // R(A) = R(B) ^ R(C)
	OpAddK         Opcode = 39 // R(A) = R(B) + K(C)
	OpSubK         Opcode = 40 // R(A) = R(B) - K(C)
	OpMulK         Opcode = 41 // R(A) = R(B) * K(C)
	OpDivK         Opcode = 42 // R(A) = R(B) / K(C)
	OpModK         Opcode = 43 // R(A) = R(B) % K(C)
	OpPowK         Opcode = 44 // R(A) = R(B) ^ K(C)
	OpAnd          Opcode = 45 // R(A) = R(B) and R(C)
	OpOr           Opcode = 46 // R(A) = R(B) or R(C)
	OpAndK         Opcode = 47 // R(A) = R(B) and K(C)
	OpOrK          Opcode = 48 // R(A) = R(B) or K(C)
	OpConcat       Opcode = 49 // R(A) = concat(R(B), ..., R(C))
	OpNot          Opcode = 50 // R(A) = not R(B)
	OpMinus        Opcode = 51 // R(A) = -R(B)
	OpLength       Opcode = 52 // R(A) = #R(B)
	OpNewTable     Opcode = 53 // R(A) = new table; B is hash-size hint, AUX is array size
	OpDupTable     Opcode = 54 // R(A) = duplicate of table template K(D)
	OpSetList      Opcode = 55 // R(A)[AUX..] = R(B), ..., R(B+C-2); C=0 means up to top
	OpForNPrep     Opcode = 56 // numeric for prep; layout [limit, step, index, var]
	OpForNLoop     Opcode = 57 // numeric for step; jumps to D if loop continues
	OpForGLoop     Opcode = 58 // generic for step; layout [gen, state, idx, vars...]
	OpForGPrepInext Opcode = 59 // generic for prep specialized for ipairs (luaB_inext)
	OpFastCall3    Opcode = 60 // fastcall builtin with 3 register args; AUX has reg2, reg3
	OpForGPrepNext Opcode = 61 // generic for prep specialized for next (luaB_next)
	OpNativeCall   Opcode = 62 // pseudo-opcode for native code dispatch (never emitted)
	OpGetVarargs   Opcode = 63 // R(A), ... = varargs; B=0 means MULTRET
	OpDupClosure   Opcode = 64 // R(A) = closure of pre-created function in K(D)
	OpPrepVarargs  Opcode = 65 // prepare stack for variadic functions; A is # fixed args
	OpLoadKX       Opcode = 66 // R(A) = K(AUX); used when constant index exceeds 16 bits
	OpJumpX        Opcode = 67 // pc += E (24-bit signed offset)
	OpFastCall     Opcode = 68 // fastcall builtin; C is jump offset over following CALL
	OpCoverage     Opcode = 69 // increment instruction hit counter encoded in E
	OpCapture      Opcode = 70 // capture local/upval for NEWCLOSURE; A is LuauCaptureType
	OpSubRK        Opcode = 71 // R(A) = K(B) - R(C); for constant-on-left subtraction
	OpDivRK        Opcode = 72 // R(A) = K(B) / R(C); for constant-on-left division
	OpFastCall1    Opcode = 73 // fastcall builtin with 1 register arg
	OpFastCall2    Opcode = 74 // fastcall builtin with 2 register args; AUX has reg2
	OpFastCall2K   Opcode = 75 // fastcall builtin with 1 register arg + 1 constant; AUX has K index
	OpForGPrep     Opcode = 76 // generic for prep (general case); jumps to backedge
	OpJumpXEqKNil  Opcode = 77 // jump if R(A) == nil (or != if AUX NOT bit set)
	OpJumpXEqKB    Opcode = 78 // jump if R(A) == bool(AUX low bit) (or != if NOT bit set)
	OpJumpXEqKN    Opcode = 79 // jump if R(A) == K(AUX low 24) numeric; NOT bit flips result
	OpJumpXEqKS    Opcode = 80 // jump if R(A) == K(AUX low 24) string; NOT bit flips result
	OpIdiv         Opcode = 81 // R(A) = floor(R(B) / R(C))
	OpIdivK        Opcode = 82 // R(A) = floor(R(B) / K(C))
	OpGetUdataKS   Opcode = 83 // userdata field load by atom (v9)
	OpSetUdataKS   Opcode = 84 // userdata field store by atom (v9)
	OpNameCallUdata Opcode = 85 // userdata method call by atom (v9)

	// OpCount is the number of defined opcodes; not a valid opcode itself.
	OpCount Opcode = 86
)

// String returns a short uppercase mnemonic for the opcode, matching the
// names used by the upstream Luau disassembler.
func (op Opcode) String() string {
	if int(op) < len(opcodeNames) && opcodeNames[op] != "" {
		return opcodeNames[op]
	}
	return "OP_INVALID"
}

var opcodeNames = [OpCount]string{
	OpNop:           "NOP",
	OpBreak:         "BREAK",
	OpLoadNil:       "LOADNIL",
	OpLoadB:         "LOADB",
	OpLoadN:         "LOADN",
	OpLoadK:         "LOADK",
	OpMove:          "MOVE",
	OpGetGlobal:     "GETGLOBAL",
	OpSetGlobal:     "SETGLOBAL",
	OpGetUpval:      "GETUPVAL",
	OpSetUpval:      "SETUPVAL",
	OpCloseUpvals:   "CLOSEUPVALS",
	OpGetImport:     "GETIMPORT",
	OpGetTable:      "GETTABLE",
	OpSetTable:      "SETTABLE",
	OpGetTableKS:    "GETTABLEKS",
	OpSetTableKS:    "SETTABLEKS",
	OpGetTableN:     "GETTABLEN",
	OpSetTableN:     "SETTABLEN",
	OpNewClosure:    "NEWCLOSURE",
	OpNameCall:      "NAMECALL",
	OpCall:          "CALL",
	OpReturn:        "RETURN",
	OpJump:          "JUMP",
	OpJumpBack:      "JUMPBACK",
	OpJumpIf:        "JUMPIF",
	OpJumpIfNot:     "JUMPIFNOT",
	OpJumpIfEq:      "JUMPIFEQ",
	OpJumpIfLe:      "JUMPIFLE",
	OpJumpIfLt:      "JUMPIFLT",
	OpJumpIfNotEq:   "JUMPIFNOTEQ",
	OpJumpIfNotLe:   "JUMPIFNOTLE",
	OpJumpIfNotLt:   "JUMPIFNOTLT",
	OpAdd:           "ADD",
	OpSub:           "SUB",
	OpMul:           "MUL",
	OpDiv:           "DIV",
	OpMod:           "MOD",
	OpPow:           "POW",
	OpAddK:          "ADDK",
	OpSubK:          "SUBK",
	OpMulK:          "MULK",
	OpDivK:          "DIVK",
	OpModK:          "MODK",
	OpPowK:          "POWK",
	OpAnd:           "AND",
	OpOr:            "OR",
	OpAndK:          "ANDK",
	OpOrK:           "ORK",
	OpConcat:        "CONCAT",
	OpNot:           "NOT",
	OpMinus:         "MINUS",
	OpLength:        "LENGTH",
	OpNewTable:      "NEWTABLE",
	OpDupTable:      "DUPTABLE",
	OpSetList:       "SETLIST",
	OpForNPrep:      "FORNPREP",
	OpForNLoop:      "FORNLOOP",
	OpForGLoop:      "FORGLOOP",
	OpForGPrepInext: "FORGPREP_INEXT",
	OpFastCall3:     "FASTCALL3",
	OpForGPrepNext:  "FORGPREP_NEXT",
	OpNativeCall:    "NATIVECALL",
	OpGetVarargs:    "GETVARARGS",
	OpDupClosure:    "DUPCLOSURE",
	OpPrepVarargs:   "PREPVARARGS",
	OpLoadKX:        "LOADKX",
	OpJumpX:         "JUMPX",
	OpFastCall:      "FASTCALL",
	OpCoverage:      "COVERAGE",
	OpCapture:       "CAPTURE",
	OpSubRK:         "SUBRK",
	OpDivRK:         "DIVRK",
	OpFastCall1:     "FASTCALL1",
	OpFastCall2:     "FASTCALL2",
	OpFastCall2K:    "FASTCALL2K",
	OpForGPrep:      "FORGPREP",
	OpJumpXEqKNil:   "JUMPXEQKNIL",
	OpJumpXEqKB:     "JUMPXEQKB",
	OpJumpXEqKN:     "JUMPXEQKN",
	OpJumpXEqKS:     "JUMPXEQKS",
	OpIdiv:          "IDIV",
	OpIdivK:         "IDIVK",
	OpGetUdataKS:    "GETUDATAKS",
	OpSetUdataKS:    "SETUDATAKS",
	OpNameCallUdata: "NAMECALLUDATA",
}

// HasAux reports whether the given opcode is always followed by a single
// auxiliary 32-bit word. This is encoding-static for every opcode except
// OpNewClosure (which is followed by a variable number of OpCapture
// instructions, not by an AUX word).
func (op Opcode) HasAux() bool {
	switch op {
	case OpGetGlobal, OpSetGlobal,
		OpGetImport,
		OpGetTableKS, OpSetTableKS,
		OpNameCall,
		OpJumpIfEq, OpJumpIfLe, OpJumpIfLt,
		OpJumpIfNotEq, OpJumpIfNotLe, OpJumpIfNotLt,
		OpNewTable,
		OpSetList,
		OpForGLoop,
		OpFastCall2, OpFastCall2K, OpFastCall3,
		OpJumpXEqKNil, OpJumpXEqKB, OpJumpXEqKN, OpJumpXEqKS,
		OpLoadKX,
		OpGetUdataKS, OpSetUdataKS, OpNameCallUdata:
		return true
	}
	return false
}

// Instruction word field accessors. These mirror the LUAU_INSN_* macros
// from upstream Bytecode.h byte-for-byte.

// InsnOp returns the opcode (low 8 bits) of an instruction word.
func InsnOp(insn uint32) Opcode { return Opcode(insn & 0xff) }

// InsnA returns the A field (bits 8..15) of an instruction word.
func InsnA(insn uint32) uint8 { return uint8((insn >> 8) & 0xff) }

// InsnB returns the B field (bits 16..23) of an instruction word.
func InsnB(insn uint32) uint8 { return uint8((insn >> 16) & 0xff) }

// InsnC returns the C field (bits 24..31) of an instruction word.
func InsnC(insn uint32) uint8 { return uint8((insn >> 24) & 0xff) }

// InsnD returns the D field (signed 16-bit, bits 16..31) of an
// instruction word using AD encoding.
func InsnD(insn uint32) int32 { return int32(insn) >> 16 }

// InsnE returns the E field (signed 24-bit, bits 8..31) of an instruction
// word using E encoding.
func InsnE(insn uint32) int32 { return int32(insn) >> 8 }

// InsnAuxA returns the low byte of an AUX word (FASTCALL3 source reg 2).
func InsnAuxA(aux uint32) uint8 { return uint8(aux & 0xff) }

// InsnAuxB returns the second byte of an AUX word (FASTCALL3 source reg 3).
func InsnAuxB(aux uint32) uint8 { return uint8((aux >> 8) & 0xff) }

// InsnAuxKV returns the unsigned 24-bit constant index encoded in an AUX
// word (used by LOP_JUMPXEQK* instructions).
func InsnAuxKV(aux uint32) uint32 { return aux & 0xffffff }

// InsnAuxKB returns the 1-bit constant value encoded in an AUX word
// (used by LOP_JUMPXEQKB).
func InsnAuxKB(aux uint32) uint32 { return aux & 0x1 }

// InsnAuxNot returns the negation flag (top bit) encoded in an AUX word
// (used by LOP_JUMPXEQK*).
func InsnAuxNot(aux uint32) uint32 { return aux >> 31 }

// InsnAuxKV16 returns the low 16-bit constant index encoded in an AUX
// word used by the v9 userdata accessor opcodes.
func InsnAuxKV16(aux uint32) uint32 { return aux & 0xffff }

// InsnAuxSlot returns the cached slot value encoded in the high 16 bits
// of an AUX word used by the v9 userdata accessor opcodes.
func InsnAuxSlot(aux uint32) uint32 { return aux >> 16 }

// EncodeABC packs an ABC-encoded instruction word.
func EncodeABC(op Opcode, a, b, c uint8) uint32 {
	return uint32(op) | uint32(a)<<8 | uint32(b)<<16 | uint32(c)<<24
}

// EncodeAD packs an AD-encoded instruction word. d is sign-extended into
// the high 16 bits.
func EncodeAD(op Opcode, a uint8, d int32) uint32 {
	return uint32(op) | uint32(a)<<8 | uint32(uint16(int16(d)))<<16
}

// EncodeE packs an E-encoded instruction word. e is sign-extended into
// the high 24 bits.
func EncodeE(op Opcode, e int32) uint32 {
	return uint32(op) | uint32(uint32(e<<8))
}
