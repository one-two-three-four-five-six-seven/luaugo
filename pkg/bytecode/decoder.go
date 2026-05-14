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

// reader is a thin cursor over an immutable bytecode blob. Every helper
// returns a *DecodeError carrying the byte offset that caused the failure
// so callers can pinpoint truncated or malformed inputs.
type reader struct {
	data []byte
	pos  int
}

func (r *reader) remaining() int { return len(r.data) - r.pos }

func (r *reader) byteAt() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, &DecodeError{Offset: uint64(r.pos), Msg: "bytecode: unexpected EOF reading byte"}
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *reader) bytes(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.data) {
		return nil, &DecodeError{
			Offset: uint64(r.pos),
			Msg:    fmt.Sprintf("bytecode: unexpected EOF reading %d bytes", n),
		}
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

func (r *reader) uint32LE() (uint32, error) {
	b, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (r *reader) int32LE() (int32, error) {
	v, err := r.uint32LE()
	return int32(v), err
}

func (r *reader) float32LE() (float32, error) {
	v, err := r.uint32LE()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(v), nil
}

func (r *reader) float64LE() (float64, error) {
	b, err := r.bytes(8)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b)), nil
}

func (r *reader) varint() (uint32, error) {
	v, n, err := varintRead(r.data, r.pos)
	if err != nil {
		return 0, err
	}
	r.pos += n
	return v, nil
}

func (r *reader) varint64() (uint64, error) {
	v, n, err := varint64Read(r.data, r.pos)
	if err != nil {
		return 0, err
	}
	r.pos += n
	return v, nil
}

// decode parses a full Luau bytecode blob. Mirrors lvmload.cpp's
// luau_load logic.
func decode(blob []byte) (*Module, error) {
	if len(blob) == 0 {
		return nil, &DecodeError{Offset: 0, Msg: "bytecode: empty blob"}
	}

	// version=0 means the rest of the blob is a textual compile error.
	if blob[0] == 0 {
		return nil, &DecodeError{Offset: 0, Msg: string(blob[1:])}
	}

	r := &reader{data: blob}
	version, err := r.byteAt()
	if err != nil {
		return nil, err
	}
	if version < common.BytecodeVersionMin || version > common.BytecodeVersionMax {
		return nil, &DecodeError{
			Offset: 0,
			Msg: fmt.Sprintf("bytecode: version %d outside supported range [%d..%d]",
				version, common.BytecodeVersionMin, common.BytecodeVersionMax),
		}
	}

	m := &Module{Version: version}

	if version >= 4 {
		tv, err := r.byteAt()
		if err != nil {
			return nil, err
		}
		if tv < common.TypeVersionMin || tv > common.TypeVersionMax {
			return nil, &DecodeError{
				Offset: uint64(r.pos - 1),
				Msg: fmt.Sprintf("bytecode: type version %d outside supported range [%d..%d]",
					tv, common.TypeVersionMin, common.TypeVersionMax),
			}
		}
		m.TypeVersion = tv
	}

	// String table.
	stringCount, err := r.varint()
	if err != nil {
		return nil, err
	}
	m.Strings = make([]string, stringCount)
	for i := uint32(0); i < stringCount; i++ {
		length, err := r.varint()
		if err != nil {
			return nil, err
		}
		raw, err := r.bytes(int(length))
		if err != nil {
			return nil, err
		}
		// Copy so the Module is independent of the input slice.
		s := make([]byte, len(raw))
		copy(s, raw)
		m.Strings[i] = string(s)
	}

	// Userdata type name mapping. Only present when typesversion == 3.
	if m.TypeVersion == 3 {
		// Mapping is terminated by a zero index byte. Each entry is
		// `[index_byte][varint name_ref]`. We store the name reference
		// keyed by (index-1); slots not referenced are left as zero.
		for {
			idx, err := r.byteAt()
			if err != nil {
				return nil, err
			}
			if idx == 0 {
				break
			}
			nameRef, err := r.varint()
			if err != nil {
				return nil, err
			}
			if int(idx) > len(m.UserdataTypeNames) {
				grown := make([]uint32, idx)
				copy(grown, m.UserdataTypeNames)
				m.UserdataTypeNames = grown
			}
			m.UserdataTypeNames[idx-1] = nameRef
		}
	}

	// Proto table.
	protoCount, err := r.varint()
	if err != nil {
		return nil, err
	}
	m.Protos = make([]*Proto, protoCount)
	for i := uint32(0); i < protoCount; i++ {
		p, err := decodeProto(r, version, m.TypeVersion)
		if err != nil {
			return nil, err
		}
		m.Protos[i] = p
	}

	mainID, err := r.varint()
	if err != nil {
		return nil, err
	}
	if mainID >= protoCount {
		return nil, &DecodeError{
			Offset: uint64(r.pos),
			Msg:    fmt.Sprintf("bytecode: main proto index %d out of range (have %d)", mainID, protoCount),
		}
	}
	m.MainProto = mainID

	return m, nil
}

func decodeProto(r *reader, version, typesversion uint8) (*Proto, error) {
	p := &Proto{}

	var err error
	if p.MaxStackSize, err = r.byteAt(); err != nil {
		return nil, err
	}
	if p.NumParams, err = r.byteAt(); err != nil {
		return nil, err
	}
	if p.NumUpvalues, err = r.byteAt(); err != nil {
		return nil, err
	}
	if p.IsVararg, err = r.byteAt(); err != nil {
		return nil, err
	}

	if version >= 4 {
		flagsByte, err := r.byteAt()
		if err != nil {
			return nil, err
		}
		p.Flags = common.ProtoFlag(flagsByte)

		typesize, err := r.varint()
		if err != nil {
			return nil, err
		}
		if typesize > 0 {
			raw, err := r.bytes(int(typesize))
			if err != nil {
				return nil, err
			}
			p.TypeInfo = make([]byte, len(raw))
			copy(p.TypeInfo, raw)
		}
	}

	// Instruction stream.
	sizecode, err := r.varint()
	if err != nil {
		return nil, err
	}
	p.Code = make([]uint32, sizecode)
	for j := uint32(0); j < sizecode; j++ {
		w, err := r.uint32LE()
		if err != nil {
			return nil, err
		}
		p.Code[j] = w
	}

	// Constant table.
	sizek, err := r.varint()
	if err != nil {
		return nil, err
	}
	p.Constants = make([]Constant, sizek)
	for j := uint32(0); j < sizek; j++ {
		c, err := decodeConstant(r, version)
		if err != nil {
			return nil, err
		}
		p.Constants[j] = c
	}

	// Child protos.
	sizep, err := r.varint()
	if err != nil {
		return nil, err
	}
	p.Protos = make([]uint32, sizep)
	for j := uint32(0); j < sizep; j++ {
		fid, err := r.varint()
		if err != nil {
			return nil, err
		}
		p.Protos[j] = fid
	}

	// linedefined is present for v2+. Our min is v3 so always read.
	if p.LineDefined, err = r.varint(); err != nil {
		return nil, err
	}
	if p.DebugName, err = r.varint(); err != nil {
		return nil, err
	}

	// Line info (optional).
	lineFlag, err := r.byteAt()
	if err != nil {
		return nil, err
	}
	if lineFlag != 0 {
		gapLog2, err := r.byteAt()
		if err != nil {
			return nil, err
		}
		if sizecode == 0 {
			return nil, &DecodeError{Offset: uint64(r.pos), Msg: "bytecode: line info present with zero-length code"}
		}
		intervals := int(((sizecode - 1) >> gapLog2) + 1)
		li := &LineInfo{LineGapLog2: gapLog2}

		// Per-instruction line bytes. The wire format is doubly delta-
		// encoded: each byte is the delta from the previous byte's
		// running offset, and the running offset is the delta from
		// the interval baseline. Upstream's lvmload sums the running
		// offset (lastoffset += byte) then stores that as
		// lineinfo[pc]; we do the same so the in-memory form is
		// "delta from baseline" (matching upstream Proto::lineinfo).
		rawDeltas, err := r.bytes(int(sizecode))
		if err != nil {
			return nil, err
		}
		li.LineInfo = make([]int8, sizecode)
		var lastOffset uint8
		for k, b := range rawDeltas {
			lastOffset += b
			li.LineInfo[k] = int8(lastOffset)
		}

		// Absolute line table is delta-encoded against running last
		// line. Upstream's lvmload sums these into absolute lines;
		// matching that, our in-memory AbsLineInfo holds the absolute
		// baseline for each interval (matching upstream Proto::abslineinfo).
		li.AbsLineInfo = make([]int32, intervals)
		var lastLine int32
		for k := 0; k < intervals; k++ {
			v, err := r.int32LE()
			if err != nil {
				return nil, err
			}
			lastLine += v
			li.AbsLineInfo[k] = lastLine
		}

		p.LineInfo = li
	}

	// Debug info (optional).
	dbgFlag, err := r.byteAt()
	if err != nil {
		return nil, err
	}
	if dbgFlag != 0 {
		di := &DebugInfo{}
		sizelocvars, err := r.varint()
		if err != nil {
			return nil, err
		}
		di.Locals = make([]DebugLocal, sizelocvars)
		for k := uint32(0); k < sizelocvars; k++ {
			name, err := r.varint()
			if err != nil {
				return nil, err
			}
			startpc, err := r.varint()
			if err != nil {
				return nil, err
			}
			endpc, err := r.varint()
			if err != nil {
				return nil, err
			}
			reg, err := r.byteAt()
			if err != nil {
				return nil, err
			}
			di.Locals[k] = DebugLocal{Name: name, StartPC: startpc, EndPC: endpc, Reg: reg}
		}
		sizeupvalues, err := r.varint()
		if err != nil {
			return nil, err
		}
		di.Upvalues = make([]uint32, sizeupvalues)
		for k := uint32(0); k < sizeupvalues; k++ {
			name, err := r.varint()
			if err != nil {
				return nil, err
			}
			di.Upvalues[k] = name
		}
		p.DebugInfo = di
	}

	_ = typesversion // future: parse v1 type info distinct from raw payload
	return p, nil
}

func decodeConstant(r *reader, version uint8) (Constant, error) {
	tagByte, err := r.byteAt()
	if err != nil {
		return nil, err
	}
	tag := common.ConstantTag(tagByte)
	switch tag {
	case common.ConstantNil:
		return ConstantNilEntry{}, nil

	case common.ConstantBoolean:
		b, err := r.byteAt()
		if err != nil {
			return nil, err
		}
		return ConstantBooleanEntry{Value: b != 0}, nil

	case common.ConstantNumber:
		v, err := r.float64LE()
		if err != nil {
			return nil, err
		}
		return ConstantNumberEntry{Value: v}, nil

	case common.ConstantString:
		idx, err := r.varint()
		if err != nil {
			return nil, err
		}
		return ConstantStringEntry{Index: idx}, nil

	case common.ConstantImport:
		v, err := r.uint32LE()
		if err != nil {
			return nil, err
		}
		return ConstantImportEntry{Packed: v}, nil

	case common.ConstantTable:
		keyCount, err := r.varint()
		if err != nil {
			return nil, err
		}
		keys := make([]uint32, keyCount)
		for i := uint32(0); i < keyCount; i++ {
			k, err := r.varint()
			if err != nil {
				return nil, err
			}
			keys[i] = k
		}
		return ConstantTableEntry{Keys: keys}, nil

	case common.ConstantClosure:
		idx, err := r.varint()
		if err != nil {
			return nil, err
		}
		return ConstantClosureEntry{ProtoIndex: idx}, nil

	case common.ConstantVector:
		if version < 5 {
			return nil, &DecodeError{
				Offset: uint64(r.pos - 1),
				Msg:    fmt.Sprintf("bytecode: VECTOR constant not valid in version %d", version),
			}
		}
		x, err := r.float32LE()
		if err != nil {
			return nil, err
		}
		y, err := r.float32LE()
		if err != nil {
			return nil, err
		}
		z, err := r.float32LE()
		if err != nil {
			return nil, err
		}
		w, err := r.float32LE()
		if err != nil {
			return nil, err
		}
		return ConstantVectorEntry{X: x, Y: y, Z: z, W: w}, nil

	case common.ConstantTableWithConstants:
		if version < 7 {
			return nil, &DecodeError{
				Offset: uint64(r.pos - 1),
				Msg:    fmt.Sprintf("bytecode: TABLE_WITH_CONSTANTS not valid in version %d", version),
			}
		}
		pairCount, err := r.varint()
		if err != nil {
			return nil, err
		}
		pairs := make([]ConstantTablePair, pairCount)
		for i := uint32(0); i < pairCount; i++ {
			// Upstream wire format: varint key, int32 LE value.
			key, err := r.varint()
			if err != nil {
				return nil, err
			}
			val, err := r.uint32LE()
			if err != nil {
				return nil, err
			}
			pairs[i] = ConstantTablePair{Key: key, Value: val}
		}
		return ConstantTableWithConstantsEntry{Pairs: pairs}, nil

	case common.ConstantInteger:
		if version < 8 {
			return nil, &DecodeError{
				Offset: uint64(r.pos - 1),
				Msg:    fmt.Sprintf("bytecode: INTEGER constant not valid in version %d", version),
			}
		}
		// Upstream wire format: 1-byte sign flag, then a varint64 magnitude.
		signByte, err := r.byteAt()
		if err != nil {
			return nil, err
		}
		mag, err := r.varint64()
		if err != nil {
			return nil, err
		}
		var v int64
		if signByte != 0 {
			v = int64(^mag + 1) // two's complement negation
		} else {
			v = int64(mag)
		}
		return ConstantIntegerEntry{Value: v}, nil

	default:
		return nil, &DecodeError{
			Offset: uint64(r.pos - 1),
			Msg:    fmt.Sprintf("bytecode: unknown constant tag %d", tag),
		}
	}
}
