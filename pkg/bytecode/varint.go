// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package bytecode

// Luau variable-length integer codec. Encoding uses 7 data bits per byte
// with the MSB set on every non-final byte. The encoding is unsigned and
// little-endian: the first byte carries the least significant 7 bits.
//
// Source of truth: writeVarInt / readVarInt in
// .upstream/Bytecode/src/BytecodeBuilder.cpp and
// .upstream/VM/src/lvmload.cpp.

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

// varint64Append encodes a 64-bit unsigned value. Used only by the
// INTEGER constant payload. The upstream wire format is identical to the
// 32-bit varint, just with up to 10 bytes.
func varint64Append(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v|0x80))
		v >>= 7
	}
	return append(dst, byte(v))
}

func varint64Read(src []byte, pos int) (uint64, int, error) {
	var v uint64
	var shift uint
	start := pos
	for pos < len(src) {
		b := src[pos]
		pos++
		v |= uint64(b&0x7f) << shift
		if b < 0x80 {
			return v, pos - start, nil
		}
		shift += 7
		if shift >= 70 {
			return 0, 0, &DecodeError{Offset: uint64(start), Msg: "bytecode: varint64 too large"}
		}
	}
	return 0, 0, &DecodeError{Offset: uint64(start), Msg: "bytecode: truncated varint64"}
}
