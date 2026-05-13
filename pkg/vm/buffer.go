// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"encoding/binary"
	"math"
)

// buffer mirrors upstream LuauBuffer (lbuffer.h): a fixed-size byte
// slab interpreted as little-endian primitive values by the
// buffer.read*/write* library functions.
//
// Bounds checks here intentionally mirror upstream behaviour: they
// PANIC instead of returning errors, so the surrounding VM can convert
// the panic into a Lua runtime error via pcall.
type buffer struct {
	gcHeader
	data []byte
}

const maxBufferSize = 1 << 30

func newBuffer(g *globalState, size int) *buffer {
	if size < 0 || size > maxBufferSize {
		panic(luaError{message: "buffer size out of range"})
	}
	b := &buffer{data: make([]byte, size)}
	g.gcInit(b, TBuffer, memSizeBufferHdr+size)
	return b
}

// Len returns the buffer's size in bytes.
func (b *buffer) Len() int { return len(b.data) }

func (b *buffer) checkRange(off, n int) {
	if off < 0 || n < 0 || off+n > len(b.data) {
		panic(luaError{message: "buffer access out of bounds"})
	}
}

// Read primitives. Naming mirrors Lua's `buffer.readu8`, etc.

func (b *buffer) ReadI8(off int) int8 {
	b.checkRange(off, 1)
	return int8(b.data[off])
}
func (b *buffer) ReadU8(off int) uint8 {
	b.checkRange(off, 1)
	return b.data[off]
}
func (b *buffer) ReadI16(off int) int16 {
	b.checkRange(off, 2)
	return int16(binary.LittleEndian.Uint16(b.data[off:]))
}
func (b *buffer) ReadU16(off int) uint16 {
	b.checkRange(off, 2)
	return binary.LittleEndian.Uint16(b.data[off:])
}
func (b *buffer) ReadI32(off int) int32 {
	b.checkRange(off, 4)
	return int32(binary.LittleEndian.Uint32(b.data[off:]))
}
func (b *buffer) ReadU32(off int) uint32 {
	b.checkRange(off, 4)
	return binary.LittleEndian.Uint32(b.data[off:])
}
func (b *buffer) ReadI64(off int) int64 {
	b.checkRange(off, 8)
	return int64(binary.LittleEndian.Uint64(b.data[off:]))
}
func (b *buffer) ReadU64(off int) uint64 {
	b.checkRange(off, 8)
	return binary.LittleEndian.Uint64(b.data[off:])
}
func (b *buffer) ReadF32(off int) float32 {
	b.checkRange(off, 4)
	return math.Float32frombits(binary.LittleEndian.Uint32(b.data[off:]))
}
func (b *buffer) ReadF64(off int) float64 {
	b.checkRange(off, 8)
	return math.Float64frombits(binary.LittleEndian.Uint64(b.data[off:]))
}

// Write primitives.

func (b *buffer) WriteI8(off int, v int8) {
	b.checkRange(off, 1)
	b.data[off] = byte(v)
}
func (b *buffer) WriteU8(off int, v uint8) {
	b.checkRange(off, 1)
	b.data[off] = v
}
func (b *buffer) WriteI16(off int, v int16) {
	b.checkRange(off, 2)
	binary.LittleEndian.PutUint16(b.data[off:], uint16(v))
}
func (b *buffer) WriteU16(off int, v uint16) {
	b.checkRange(off, 2)
	binary.LittleEndian.PutUint16(b.data[off:], v)
}
func (b *buffer) WriteI32(off int, v int32) {
	b.checkRange(off, 4)
	binary.LittleEndian.PutUint32(b.data[off:], uint32(v))
}
func (b *buffer) WriteU32(off int, v uint32) {
	b.checkRange(off, 4)
	binary.LittleEndian.PutUint32(b.data[off:], v)
}
func (b *buffer) WriteI64(off int, v int64) {
	b.checkRange(off, 8)
	binary.LittleEndian.PutUint64(b.data[off:], uint64(v))
}
func (b *buffer) WriteU64(off int, v uint64) {
	b.checkRange(off, 8)
	binary.LittleEndian.PutUint64(b.data[off:], v)
}
func (b *buffer) WriteF32(off int, v float32) {
	b.checkRange(off, 4)
	binary.LittleEndian.PutUint32(b.data[off:], math.Float32bits(v))
}
func (b *buffer) WriteF64(off int, v float64) {
	b.checkRange(off, 8)
	binary.LittleEndian.PutUint64(b.data[off:], math.Float64bits(v))
}

// Copy copies n bytes from src@srcOff to dst@dstOff. Out-of-bounds
// triggers a Lua error.
func (b *buffer) Copy(dstOff int, src *buffer, srcOff, n int) {
	b.checkRange(dstOff, n)
	src.checkRange(srcOff, n)
	copy(b.data[dstOff:dstOff+n], src.data[srcOff:srcOff+n])
}

// Fill writes n copies of val starting at off.
func (b *buffer) Fill(off, n int, val byte) {
	b.checkRange(off, n)
	for i := 0; i < n; i++ {
		b.data[off+i] = val
	}
}

// Bytes returns the underlying slice (live; mutations affect the
// buffer). Mostly for testing.
func (b *buffer) Bytes() []byte { return b.data }
