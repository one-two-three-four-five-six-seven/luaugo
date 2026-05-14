// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import (
	"encoding/binary"
	"math"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// buffer.go: Luau's `buffer` standard library. Mirrors upstream
// VM/src/lbuflib.cpp. The Tier-2 vm.buffer GC type owns the byte slab
// and exposes it via vm.State.NewBuffer / LCheckBuffer; this file is
// just the Lua-callable shell: argument validation, little-endian
// codec, and bit-level read/write helpers.
//
// All multi-byte reads and writes are little-endian. Integer writers
// accept any number, wrapping through 32-bit two's complement (bit32
// semantics: (uint32)(int64)d) before truncating to the destination
// width. Float writers use IEEE-754 little-endian.

const maxBufferSize = 1 << 30 // mirrors vm.maxBufferSize

// openBufferImpl is the real entry point. The stub in stubs.go
// delegates here. Mirrors upstream luaopen_buffer (lbuflib.cpp).
func openBufferImpl(s *vm.State) {
	s.CreateTable(0, len(bufferLib))
	s.LRegisterList(bufferLib)
	s.SetGlobal("buffer")
}

// bufferLib is the ordered set of library entries (upstream order).
var bufferLib = []vm.LFnEntry{
	{Name: "create", Fn: bufferCreate},
	{Name: "fromstring", Fn: bufferFromString},
	{Name: "tostring", Fn: bufferToString},
	{Name: "readi8", Fn: bufferReadI8},
	{Name: "readu8", Fn: bufferReadU8},
	{Name: "readi16", Fn: bufferReadI16},
	{Name: "readu16", Fn: bufferReadU16},
	{Name: "readi32", Fn: bufferReadI32},
	{Name: "readu32", Fn: bufferReadU32},
	{Name: "readf32", Fn: bufferReadF32},
	{Name: "readf64", Fn: bufferReadF64},
	{Name: "writei8", Fn: bufferWriteI8},
	{Name: "writeu8", Fn: bufferWriteU8},
	{Name: "writei16", Fn: bufferWriteI16},
	{Name: "writeu16", Fn: bufferWriteU16},
	{Name: "writei32", Fn: bufferWriteI32},
	{Name: "writeu32", Fn: bufferWriteU32},
	{Name: "writef32", Fn: bufferWriteF32},
	{Name: "writef64", Fn: bufferWriteF64},
	{Name: "readstring", Fn: bufferReadString},
	{Name: "writestring", Fn: bufferWriteString},
	{Name: "len", Fn: bufferLen},
	{Name: "copy", Fn: bufferCopy},
	{Name: "fill", Fn: bufferFill},
	{Name: "readbits", Fn: bufferReadBits},
	{Name: "writebits", Fn: bufferWriteBits},
}

// ---------------------------------------------------------------------
// Argument helpers
// ---------------------------------------------------------------------

// bufCheckOffset reads an integer from argn. Negative offsets are
// tolerated here; bufRequireRange catches them as out-of-bounds.
func bufCheckOffset(s *vm.State, argn int) int {
	v := s.LCheckInteger(argn)
	return int(v)
}

// bufCheckUnsigned mirrors upstream's luaL_checkunsigned: any number,
// truncated via (unsigned)(long long)(d). Matches bit32 wrap.
func bufCheckUnsigned(s *vm.State, argn int) uint32 {
	d := s.LCheckNumber(argn)
	return uint32(int64(d))
}

// bufOutOfBounds raises the upstream-canonical out-of-bounds error.
func bufOutOfBounds(s *vm.State) {
	s.LError("buffer access out of bounds")
}

// bufRequireRange asserts that [off, off+n) is contained in
// [0, len(buf)). Mirrors upstream's isoutofbounds macro: a negative
// offset is rejected via the uint64 reinterpretation that yields a
// huge positive value, so a signed-arithmetic guard against negatives
// is equivalent.
func bufRequireRange(s *vm.State, buf []byte, off, n int) {
	if off < 0 || n < 0 || off > len(buf)-n {
		// off > len-n covers off+n > len without overflow because we've
		// already established off >= 0 and n >= 0.
		bufOutOfBounds(s)
	}
}

// ---------------------------------------------------------------------
// create / fromstring / tostring / len
// ---------------------------------------------------------------------

func bufferCreate(s *vm.State) int {
	size := s.LCheckInteger(1)
	if size < 0 {
		s.LArgError(1, "size")
	}
	if size > maxBufferSize {
		s.LError("buffer size too large")
	}
	s.NewBuffer(int(size))
	return 1
}

func bufferFromString(s *vm.State) int {
	str := s.LCheckString(1)
	if len(str) > maxBufferSize {
		s.LError("buffer size too large")
	}
	data := s.NewBuffer(len(str))
	copy(data, str)
	return 1
}

func bufferToString(s *vm.State) int {
	b := s.LCheckBuffer(1)
	s.PushString(string(b))
	return 1
}

func bufferLen(s *vm.State) int {
	b := s.LCheckBuffer(1)
	s.PushNumber(float64(len(b)))
	return 1
}

// ---------------------------------------------------------------------
// Integer read / write
// ---------------------------------------------------------------------

func bufferReadI8(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	bufRequireRange(s, b, off, 1)
	s.PushNumber(float64(int8(b[off])))
	return 1
}

func bufferReadU8(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	bufRequireRange(s, b, off, 1)
	s.PushNumber(float64(b[off]))
	return 1
}

func bufferReadI16(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	bufRequireRange(s, b, off, 2)
	s.PushNumber(float64(int16(binary.LittleEndian.Uint16(b[off:]))))
	return 1
}

func bufferReadU16(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	bufRequireRange(s, b, off, 2)
	s.PushNumber(float64(binary.LittleEndian.Uint16(b[off:])))
	return 1
}

func bufferReadI32(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	bufRequireRange(s, b, off, 4)
	s.PushNumber(float64(int32(binary.LittleEndian.Uint32(b[off:]))))
	return 1
}

func bufferReadU32(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	bufRequireRange(s, b, off, 4)
	s.PushNumber(float64(binary.LittleEndian.Uint32(b[off:])))
	return 1
}

func bufferWriteI8(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	val := bufCheckUnsigned(s, 3)
	bufRequireRange(s, b, off, 1)
	b[off] = byte(val)
	return 0
}

func bufferWriteU8(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	val := bufCheckUnsigned(s, 3)
	bufRequireRange(s, b, off, 1)
	b[off] = byte(val)
	return 0
}

func bufferWriteI16(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	val := bufCheckUnsigned(s, 3)
	bufRequireRange(s, b, off, 2)
	binary.LittleEndian.PutUint16(b[off:], uint16(val))
	return 0
}

func bufferWriteU16(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	val := bufCheckUnsigned(s, 3)
	bufRequireRange(s, b, off, 2)
	binary.LittleEndian.PutUint16(b[off:], uint16(val))
	return 0
}

func bufferWriteI32(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	val := bufCheckUnsigned(s, 3)
	bufRequireRange(s, b, off, 4)
	binary.LittleEndian.PutUint32(b[off:], val)
	return 0
}

func bufferWriteU32(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	val := bufCheckUnsigned(s, 3)
	bufRequireRange(s, b, off, 4)
	binary.LittleEndian.PutUint32(b[off:], val)
	return 0
}

// ---------------------------------------------------------------------
// Float read / write
// ---------------------------------------------------------------------

func bufferReadF32(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	bufRequireRange(s, b, off, 4)
	s.PushNumber(float64(math.Float32frombits(binary.LittleEndian.Uint32(b[off:]))))
	return 1
}

func bufferReadF64(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	bufRequireRange(s, b, off, 8)
	s.PushNumber(math.Float64frombits(binary.LittleEndian.Uint64(b[off:])))
	return 1
}

func bufferWriteF32(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	val := s.LCheckNumber(3)
	bufRequireRange(s, b, off, 4)
	binary.LittleEndian.PutUint32(b[off:], math.Float32bits(float32(val)))
	return 0
}

func bufferWriteF64(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	val := s.LCheckNumber(3)
	bufRequireRange(s, b, off, 8)
	binary.LittleEndian.PutUint64(b[off:], math.Float64bits(val))
	return 0
}

// ---------------------------------------------------------------------
// String read / write
// ---------------------------------------------------------------------

func bufferReadString(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	size := int(s.LCheckInteger(3))
	if size < 0 {
		s.LArgError(3, "size")
	}
	bufRequireRange(s, b, off, size)
	s.PushString(string(b[off : off+size]))
	return 1
}

func bufferWriteString(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	str := s.LCheckString(3)
	count := int(s.LOptInteger(4, int64(len(str))))
	if count < 0 {
		s.LArgError(4, "count")
	}
	if count > len(str) {
		s.LError("string length overflow")
	}
	bufRequireRange(s, b, off, count)
	copy(b[off:off+count], str)
	return 0
}

// ---------------------------------------------------------------------
// Copy / fill
// ---------------------------------------------------------------------

func bufferCopy(s *vm.State) int {
	dst := s.LCheckBuffer(1)
	dstOff := bufCheckOffset(s, 2)
	src := s.LCheckBuffer(3)
	srcOff := int(s.LOptInteger(4, 0))
	size := int(s.LOptInteger(5, int64(len(src)-srcOff)))
	if size < 0 {
		bufOutOfBounds(s)
	}
	bufRequireRange(s, src, srcOff, size)
	bufRequireRange(s, dst, dstOff, size)
	// Go's builtin copy is overlap-safe (memmove semantics) even when
	// src and dst alias, which matches upstream's memmove call.
	copy(dst[dstOff:dstOff+size], src[srcOff:srcOff+size])
	return 0
}

func bufferFill(s *vm.State) int {
	b := s.LCheckBuffer(1)
	off := bufCheckOffset(s, 2)
	val := bufCheckUnsigned(s, 3)
	size := int(s.LOptInteger(4, int64(len(b)-off)))
	if size < 0 {
		bufOutOfBounds(s)
	}
	bufRequireRange(s, b, off, size)
	fillByte := byte(val & 0xff)
	for i := 0; i < size; i++ {
		b[off+i] = fillByte
	}
	return 0
}

// ---------------------------------------------------------------------
// Bit-level read / write
// ---------------------------------------------------------------------

// readbits/writebits address a packed bit field [bitoffset, bitoffset +
// bitcount) within the buffer's little-endian byte sequence. bitcount
// must lie in [0,32]; bitoffset is unsigned (must be >= 0).
func bufferReadBits(s *vm.State) int {
	b := s.LCheckBuffer(1)
	bitOff := int64(s.LCheckNumber(2))
	bitCount := int(s.LCheckInteger(3))

	if bitOff < 0 {
		bufOutOfBounds(s)
	}
	if uint(bitCount) > 32 {
		s.LError("bit count is out of range of [0; 32]")
	}
	if uint64(bitOff)+uint64(bitCount) > uint64(len(b))*8 {
		bufOutOfBounds(s)
	}
	if bitCount == 0 {
		s.PushNumber(0)
		return 1
	}

	startByte := int(bitOff / 8)
	endByte := int((bitOff + int64(bitCount) + 7) / 8)
	var data uint64
	for i := endByte - 1; i >= startByte; i-- {
		data = (data << 8) | uint64(b[i])
	}
	sub := uint(bitOff & 7)
	mask := (uint64(1) << uint(bitCount)) - 1
	result := uint32((data >> sub) & mask)
	s.PushNumber(float64(result))
	return 1
}

func bufferWriteBits(s *vm.State) int {
	b := s.LCheckBuffer(1)
	bitOff := int64(s.LCheckNumber(2))
	bitCount := int(s.LCheckInteger(3))
	value := bufCheckUnsigned(s, 4)

	if bitOff < 0 {
		bufOutOfBounds(s)
	}
	if uint(bitCount) > 32 {
		s.LError("bit count is out of range of [0; 32]")
	}
	if uint64(bitOff)+uint64(bitCount) > uint64(len(b))*8 {
		bufOutOfBounds(s)
	}
	if bitCount == 0 {
		return 0
	}

	startByte := int(bitOff / 8)
	endByte := int((bitOff + int64(bitCount) + 7) / 8)
	var data uint64
	for i := endByte - 1; i >= startByte; i-- {
		data = (data << 8) | uint64(b[i])
	}
	sub := uint(bitOff & 7)
	mask := ((uint64(1) << uint(bitCount)) - 1) << sub
	data = (data &^ mask) | ((uint64(value) << sub) & mask)
	for i := startByte; i < endByte; i++ {
		b[i] = byte(data)
		data >>= 8
	}
	return 0
}
