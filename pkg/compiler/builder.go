// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package compiler

import (
	"fmt"
	"math"

	"github.com/luaugo/luaugo/internal/common"
	"github.com/luaugo/luaugo/pkg/bytecode"
)

// builder accumulates instructions and constants for a single proto and
// owns the shared module-level string table.
//
// This is a deliberately simplified counterpart to upstream's
// BytecodeBuilder. We do NOT attempt byte-identical output; we only
// need to produce a Module that, after bytecode.Encode, loads cleanly
// in the official Luau VM. The encoder validates structural soundness
// for us (out-of-range constant indices, missing TypeInfo on v4+, etc.).
type builder struct {
	module      *bytecode.Module
	stringIndex map[string]uint32 // 1-based indices into module.Strings
}

func newBuilder() *builder {
	return &builder{
		module: &bytecode.Module{
			Version:     common.BytecodeVersionTarget,
			TypeVersion: common.TypeVersionTarget,
		},
		stringIndex: make(map[string]uint32),
	}
}

// internString adds s to the module string table if absent and returns
// the 1-based index. Index 0 is reserved for "no string".
func (b *builder) internString(s string) uint32 {
	if idx, ok := b.stringIndex[s]; ok {
		return idx
	}
	b.module.Strings = append(b.module.Strings, s)
	idx := uint32(len(b.module.Strings)) // 1-based
	b.stringIndex[s] = idx
	return idx
}

// finalize sets MainProto and returns the assembled module.
func (b *builder) finalize(mainProto uint32) *bytecode.Module {
	b.module.MainProto = mainProto
	return b.module
}

// protoBuilder owns the per-function instruction stream, constant table,
// and bookkeeping needed to emit upstream-compatible bytecode for one
// AST function.
type protoBuilder struct {
	parent *builder

	code      []uint32
	constants []bytecode.Constant
	// constMap deduplicates constants by stable string key. The key
	// encodes both the tag and the payload so values with the same
	// numeric content but different tag (e.g. boolean vs nil) don't
	// collide.
	constMap map[string]uint32

	// Child protos (indices into module.Protos).
	childProtos []uint32

	// Register top tracks the next free register slot. Locals + temps
	// occupy [0, top); maxStack tracks the high-water mark.
	top      uint8
	maxStack uint8

	numParams   uint8
	isVararg    bool
	lineDefined uint32
	debugName   uint32
}

func newProtoBuilder(parent *builder) *protoBuilder {
	return &protoBuilder{
		parent:   parent,
		constMap: make(map[string]uint32),
	}
}

// reserveReg allocates count contiguous registers and returns the base.
// The caller is responsible for releasing them with freeReg when they
// fall out of scope (typically via a RegScope).
func (p *protoBuilder) reserveReg(count int) uint8 {
	if int(p.top)+count > 255 {
		panic(&CompileError{Msg: fmt.Sprintf("compiler: register overflow (top=%d, want=%d)", p.top, count)})
	}
	base := p.top
	p.top += uint8(count)
	if p.top > p.maxStack {
		p.maxStack = p.top
	}
	return base
}

// setTop sets the register top to t. Used to undo temporary
// allocations at the end of a sub-expression and to grow the top to
// account for multi-result calls that allocate "above" the current
// frame (e.g. CALL leaving N results at base..base+N-1).
func (p *protoBuilder) setTop(t uint8) {
	p.top = t
	if p.top > p.maxStack {
		p.maxStack = p.top
	}
}

// pc returns the current instruction word count.
func (p *protoBuilder) pc() int { return len(p.code) }

// emitABC appends an ABC-encoded instruction.
func (p *protoBuilder) emitABC(op common.Opcode, a, b, c uint8) {
	p.code = append(p.code, common.EncodeABC(op, a, b, c))
}

// emitAD appends an AD-encoded instruction.
func (p *protoBuilder) emitAD(op common.Opcode, a uint8, d int32) {
	p.code = append(p.code, common.EncodeAD(op, a, d))
}

// emitE appends an E-encoded instruction.
func (p *protoBuilder) emitE(op common.Opcode, e int32) {
	p.code = append(p.code, common.EncodeE(op, e))
}

// emitAux appends a raw AUX word that always follows the previous
// instruction.
func (p *protoBuilder) emitAux(w uint32) {
	p.code = append(p.code, w)
}

// patchD rewrites the D field of the instruction at pc to delta.
func (p *protoBuilder) patchD(pc int, delta int32) {
	if delta < math.MinInt16 || delta > math.MaxInt16 {
		panic(&CompileError{Msg: fmt.Sprintf("compiler: jump offset %d out of int16 range", delta)})
	}
	insn := p.code[pc]
	insn = (insn & 0x0000ffff) | uint32(uint16(int16(delta)))<<16
	p.code[pc] = insn
}

// patchE rewrites the E field of the instruction at pc to delta.
func (p *protoBuilder) patchE(pc int, delta int32) {
	insn := p.code[pc] & 0xff
	p.code[pc] = insn | uint32(uint32(delta<<8))
}

// addConstant interns a constant entry and returns its index.
func (p *protoBuilder) addConstant(key string, c bytecode.Constant) uint32 {
	if idx, ok := p.constMap[key]; ok {
		return idx
	}
	idx := uint32(len(p.constants))
	p.constants = append(p.constants, c)
	p.constMap[key] = idx
	return idx
}

func (p *protoBuilder) addNilConstant() uint32 {
	return p.addConstant("nil", bytecode.ConstantNilEntry{})
}

func (p *protoBuilder) addBoolConstant(v bool) uint32 {
	if v {
		return p.addConstant("b:1", bytecode.ConstantBooleanEntry{Value: true})
	}
	return p.addConstant("b:0", bytecode.ConstantBooleanEntry{Value: false})
}

func (p *protoBuilder) addNumberConstant(v float64) uint32 {
	bits := math.Float64bits(v)
	key := fmt.Sprintf("n:%x", bits)
	return p.addConstant(key, bytecode.ConstantNumberEntry{Value: v})
}

func (p *protoBuilder) addStringConstant(s string) uint32 {
	sidx := p.parent.internString(s)
	key := fmt.Sprintf("s:%d", sidx)
	return p.addConstant(key, bytecode.ConstantStringEntry{Index: sidx})
}

func (p *protoBuilder) addImportConstant(packed uint32) uint32 {
	key := fmt.Sprintf("i:%08x", packed)
	return p.addConstant(key, bytecode.ConstantImportEntry{Packed: packed})
}

func (p *protoBuilder) addClosureConstant(protoIdx uint32) uint32 {
	key := fmt.Sprintf("c:%d", protoIdx)
	return p.addConstant(key, bytecode.ConstantClosureEntry{ProtoIndex: protoIdx})
}

func (p *protoBuilder) addTableConstant(keys []uint32) uint32 {
	// Tables are identified structurally by their key list.
	key := "t:"
	for _, k := range keys {
		key += fmt.Sprintf("%d,", k)
	}
	return p.addConstant(key, bytecode.ConstantTableEntry{Keys: keys})
}

// emitLoadK emits LOADK if the constant index fits in 16 bits, otherwise
// LOADKX + AUX.
func (p *protoBuilder) emitLoadK(target uint8, cid uint32) {
	if cid < 32768 {
		p.emitAD(common.OpLoadK, target, int32(cid))
	} else {
		p.emitAD(common.OpLoadKX, target, 0)
		p.emitAux(cid)
	}
}

// emitReturn emits a RETURN A=base B=nvals+1 instruction.
func (p *protoBuilder) emitReturn(base uint8, nvals int) {
	p.emitABC(common.OpReturn, base, uint8(nvals+1), 0)
}

// build converts this builder's accumulated state into a finalized
// bytecode.Proto. typeInfo must be non-nil for bytecode v4+; pass an
// empty slice when no type info is desired.
func (p *protoBuilder) build(numUpvalues uint8, typeInfo []byte) *bytecode.Proto {
	if p.maxStack < p.numParams+1 {
		p.maxStack = p.numParams + 1
	}
	if p.maxStack == 0 {
		p.maxStack = 1
	}
	vararg := uint8(0)
	if p.isVararg {
		vararg = 1
	}
	if typeInfo == nil {
		typeInfo = []byte{}
	}
	return &bytecode.Proto{
		MaxStackSize: p.maxStack,
		NumParams:    p.numParams,
		NumUpvalues:  numUpvalues,
		IsVararg:     vararg,
		Flags:        0,
		TypeInfo:     typeInfo,
		Code:         p.code,
		Constants:    p.constants,
		Protos:       p.childProtos,
		LineDefined:  p.lineDefined,
		DebugName:    p.debugName,
	}
}
