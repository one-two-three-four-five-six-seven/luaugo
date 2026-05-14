// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package bytecode

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/common"
)

// encode serializes a Module to the upstream Luau bytecode binary
// format. Mirrors BytecodeBuilder::finalize / writeFunction call order
// from .upstream/Bytecode/src/BytecodeBuilder.cpp.
//
// Byte-identity guarantee: for any Module produced by decode(blob),
// encode(m, opts) returns a blob equal to the original (provided opts
// don't request a different vector width than was decoded).
func encode(m *Module, opts EncodeOptions) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("bytecode: encode of nil module")
	}
	if m.Version < common.BytecodeVersionMin || m.Version > common.BytecodeVersionMax {
		return nil, fmt.Errorf("bytecode: version %d outside supported range [%d..%d]",
			m.Version, common.BytecodeVersionMin, common.BytecodeVersionMax)
	}
	if opts.VectorComponents == 0 {
		opts.VectorComponents = 3
	}
	if opts.VectorComponents != 3 && opts.VectorComponents != 4 {
		return nil, fmt.Errorf("bytecode: invalid VectorComponents=%d (must be 3 or 4)", opts.VectorComponents)
	}
	if int(m.MainProto) >= len(m.Protos) {
		return nil, fmt.Errorf("bytecode: MainProto=%d out of range (have %d protos)",
			m.MainProto, len(m.Protos))
	}

	out := make([]byte, 0, 64)
	out = append(out, m.Version)

	if m.Version >= 4 {
		out = append(out, m.TypeVersion)
	}

	// String table.
	out = varintAppend(out, uint32(len(m.Strings)))
	for _, s := range m.Strings {
		out = varintAppend(out, uint32(len(s)))
		out = append(out, s...)
	}

	// Userdata type name mapping is part of typesversion 3.
	if m.TypeVersion == 3 {
		for i, nameRef := range m.UserdataTypeNames {
			if nameRef == 0 {
				continue
			}
			out = append(out, byte(i+1))
			out = varintAppend(out, nameRef)
		}
		out = append(out, 0) // terminator
	}

	// Protos.
	out = varintAppend(out, uint32(len(m.Protos)))
	for i, p := range m.Protos {
		var err error
		out, err = encodeProto(out, p, m.Version, m.TypeVersion, opts)
		if err != nil {
			return nil, fmt.Errorf("bytecode: proto %d: %w", i, err)
		}
	}

	out = varintAppend(out, m.MainProto)
	return out, nil
}

func encodeProto(out []byte, p *Proto, version, typesversion uint8, opts EncodeOptions) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("nil proto")
	}

	out = append(out, p.MaxStackSize, p.NumParams, p.NumUpvalues, p.IsVararg)

	if version >= 4 {
		out = append(out, byte(p.Flags))
		// TypeInfo payload. Empty types: single varint 0. Non-empty: varint(len) + bytes.
		out = varintAppend(out, uint32(len(p.TypeInfo)))
		out = append(out, p.TypeInfo...)
	}

	// Instructions.
	out = varintAppend(out, uint32(len(p.Code)))
	for _, insn := range p.Code {
		out = binary.LittleEndian.AppendUint32(out, insn)
	}

	// Constants.
	out = varintAppend(out, uint32(len(p.Constants)))
	for i, c := range p.Constants {
		var err error
		out, err = encodeConstant(out, c, version)
		if err != nil {
			return nil, fmt.Errorf("constant %d: %w", i, err)
		}
	}

	// Child protos.
	out = varintAppend(out, uint32(len(p.Protos)))
	for _, fid := range p.Protos {
		out = varintAppend(out, fid)
	}

	out = varintAppend(out, p.LineDefined)
	out = varintAppend(out, p.DebugName)

	// Line info.
	if p.LineInfo != nil {
		if len(p.Code) == 0 {
			return nil, fmt.Errorf("line info present but Code is empty")
		}
		out = append(out, 1)
		out = append(out, p.LineInfo.LineGapLog2)
		intervals := int(((uint32(len(p.Code)) - 1) >> p.LineInfo.LineGapLog2) + 1)
		if len(p.LineInfo.LineInfo) != len(p.Code) {
			return nil, fmt.Errorf("LineInfo.LineInfo len=%d, want sizecode=%d",
				len(p.LineInfo.LineInfo), len(p.Code))
		}
		if len(p.LineInfo.AbsLineInfo) != intervals {
			return nil, fmt.Errorf("LineInfo.AbsLineInfo len=%d, want %d intervals",
				len(p.LineInfo.AbsLineInfo), intervals)
		}
		// Per-instruction bytes: in-memory holds "delta from interval
		// baseline" for each pc; wire format writes the byte-to-byte
		// delta of that running offset. Mirror upstream
		// BytecodeBuilder::writeLineInfo's third pass.
		var lastOffset uint8
		for _, b := range p.LineInfo.LineInfo {
			cur := uint8(b)
			out = append(out, cur-lastOffset)
			lastOffset = cur
		}
		// Absolute baselines: in-memory holds absolute lines; wire
		// writes the delta against the running last line.
		var lastLine int32
		for _, v := range p.LineInfo.AbsLineInfo {
			out = binary.LittleEndian.AppendUint32(out, uint32(v-lastLine))
			lastLine = v
		}
	} else {
		out = append(out, 0)
	}

	// Debug info.
	if p.DebugInfo != nil {
		out = append(out, 1)
		out = varintAppend(out, uint32(len(p.DebugInfo.Locals)))
		for _, l := range p.DebugInfo.Locals {
			out = varintAppend(out, l.Name)
			out = varintAppend(out, l.StartPC)
			out = varintAppend(out, l.EndPC)
			out = append(out, l.Reg)
		}
		out = varintAppend(out, uint32(len(p.DebugInfo.Upvalues)))
		for _, u := range p.DebugInfo.Upvalues {
			out = varintAppend(out, u)
		}
	} else {
		out = append(out, 0)
	}

	_ = typesversion
	_ = opts
	return out, nil
}

func encodeConstant(out []byte, c Constant, version uint8) ([]byte, error) {
	switch v := c.(type) {
	case ConstantNilEntry:
		out = append(out, byte(common.ConstantNil))

	case ConstantBooleanEntry:
		out = append(out, byte(common.ConstantBoolean))
		if v.Value {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}

	case ConstantNumberEntry:
		out = append(out, byte(common.ConstantNumber))
		out = binary.LittleEndian.AppendUint64(out, math.Float64bits(v.Value))

	case ConstantStringEntry:
		out = append(out, byte(common.ConstantString))
		out = varintAppend(out, v.Index)

	case ConstantImportEntry:
		out = append(out, byte(common.ConstantImport))
		out = binary.LittleEndian.AppendUint32(out, v.Packed)

	case ConstantTableEntry:
		out = append(out, byte(common.ConstantTable))
		out = varintAppend(out, uint32(len(v.Keys)))
		for _, k := range v.Keys {
			out = varintAppend(out, k)
		}

	case ConstantTableWithConstantsEntry:
		if version < 7 {
			return nil, fmt.Errorf("TABLE_WITH_CONSTANTS requires bytecode v7+")
		}
		out = append(out, byte(common.ConstantTableWithConstants))
		out = varintAppend(out, uint32(len(v.Pairs)))
		for _, pr := range v.Pairs {
			// Upstream wire format: varint key, int32 LE value.
			out = varintAppend(out, pr.Key)
			out = binary.LittleEndian.AppendUint32(out, pr.Value)
		}

	case ConstantClosureEntry:
		out = append(out, byte(common.ConstantClosure))
		out = varintAppend(out, v.ProtoIndex)

	case ConstantVectorEntry:
		if version < 5 {
			return nil, fmt.Errorf("VECTOR constants require bytecode v5+")
		}
		out = append(out, byte(common.ConstantVector))
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v.X))
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v.Y))
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v.Z))
		// Upstream encoder always emits 4 floats even in 3-wide mode (W=0).
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v.W))

	case ConstantIntegerEntry:
		if version < 8 {
			return nil, fmt.Errorf("INTEGER constants require bytecode v8+")
		}
		out = append(out, byte(common.ConstantInteger))
		// Upstream wire format: sign byte then varint64 of magnitude.
		// Negative: write 1 + varint64(two's-complement magnitude).
		// Non-negative: write 0 + varint64(value).
		if v.Value < 0 {
			out = append(out, 1)
			mag := ^uint64(v.Value) + 1
			out = varint64Append(out, mag)
		} else {
			out = append(out, 0)
			out = varint64Append(out, uint64(v.Value))
		}

	default:
		return nil, fmt.Errorf("unknown constant kind %T", c)
	}
	return out, nil
}
