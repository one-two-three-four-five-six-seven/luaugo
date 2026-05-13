// Copyright (c) luaugo contributors. Licensed under the MIT License.

package bytecode

// Tier-1 placeholders for Tier 2's bytecode codec agent.

func decode(blob []byte) (*Module, error) {
	panic("bytecode: decoder not yet implemented (Tier 2 codec agent)")
}

func encode(m *Module, opts EncodeOptions) ([]byte, error) {
	panic("bytecode: encoder not yet implemented (Tier 2 codec agent)")
}

func disassemble(m *Module) string {
	panic("bytecode: Disassemble not yet implemented (Tier 2 codec agent)")
}

func disassembleProto(m *Module, p *Proto) string {
	panic("bytecode: DisassembleProto not yet implemented (Tier 2 codec agent)")
}

func varintAppend(dst []byte, v uint32) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v|0x80))
		v >>= 7
	}
	return append(dst, byte(v))
}

func varintRead(src []byte, pos int) (uint32, int, error) {
	var v uint32
	var shift uint
	start := pos
	for pos < len(src) {
		b := src[pos]
		pos++
		v |= uint32(b&0x7f) << shift
		if b < 0x80 {
			return v, pos - start, nil
		}
		shift += 7
		if shift >= 35 {
			return 0, 0, &DecodeError{Offset: uint64(start), Msg: "bytecode: varint too large"}
		}
	}
	return 0, 0, &DecodeError{Offset: uint64(start), Msg: "bytecode: truncated varint"}
}
