// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

// bufferapi.go: public *State methods for creating and inspecting
// buffer values. These are the surface used by the buffer stdlib
// (pkg/vm/lib/buffer.go) and by user Go code that wants to hand a Lua
// program a fresh byte buffer.
//
// The Tier-2 buffer type already implements the read/write primitives;
// the methods here only deal with stack plumbing (push / fetch / type
// check). Out-of-range allocations and accesses raise Lua runtime
// errors that propagate through pcall via the standard luaError panic.

// NewBuffer allocates a new buffer of size bytes (zero-initialized) and
// pushes it onto the stack. size must satisfy 0 <= size <= 1 GiB; both
// limits mirror upstream's MAX_BUFFER_SIZE. Out-of-range sizes raise a
// Lua runtime error.
//
// The returned slice aliases the buffer's storage; mutating it changes
// the buffer's contents. The slice is valid until the buffer becomes
// unreachable.
func (s *State) NewBuffer(size int) []byte {
	b := newBuffer(s.impl.gs, size)
	s.impl.push(bufferValue(b))
	return b.data
}

// IsBuffer reports whether the value at idx is a buffer.
func (s *State) IsBuffer(idx int) bool { return s.Type(idx) == TBuffer }

// ToBuffer returns the buffer storage at idx and true, or (nil,false)
// if idx does not hold a buffer. The returned slice aliases the live
// buffer.
func (s *State) ToBuffer(idx int) ([]byte, bool) {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		return nil, false
	}
	v := si.stack[i]
	if v.tag != TBuffer {
		return nil, false
	}
	return v.gc.(*buffer).data, true
}

// LCheckBuffer returns the buffer storage at argn or raises a
// type-mismatch error. Counterpart of luaL_checkbuffer.
func (s *State) LCheckBuffer(argn int) []byte {
	b, ok := s.ToBuffer(argn)
	if !ok {
		s.LTypeError(argn, "buffer")
	}
	return b
}
