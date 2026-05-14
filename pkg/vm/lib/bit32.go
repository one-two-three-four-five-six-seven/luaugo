// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import (
	"math"
	"math/bits"

	"github.com/luaugo/luaugo/pkg/vm"
)

// bit32.go: Tier-4 implementation of the bit32 standard library.
// Mirrors upstream Luau's VM/src/lbitlib.cpp. All operands are
// interpreted modulo 2^32 (uint32).

// bit32NBits is the operand width. bit32 is fixed at 32 bits.
const bit32NBits = 32

// bit32CheckU32 reads argn as a number and reduces it modulo 2^32,
// matching upstream's luaL_checkunsigned + luai_num2unsigned
// ((unsigned)(long long)(n)).
func bit32CheckU32(s *vm.State, argn int) uint32 {
	n, ok := s.ToNumber(argn)
	if !ok {
		s.LTypeError(argn, "number")
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		s.LArgError(argn, "number has no integer representation")
	}
	return uint32(int64(n))
}

// bit32CheckInt reads argn as a signed integer (shift / rotate amount).
func bit32CheckInt(s *vm.State, argn int) int {
	v, ok := s.ToInteger(argn)
	if !ok {
		s.LTypeError(argn, "integer")
	}
	return int(v)
}

// bit32OptInt reads argn as an integer or returns def if absent.
func bit32OptInt(s *vm.State, argn int, def int) int {
	if s.IsNoneOrNil(argn) {
		return def
	}
	return bit32CheckInt(s, argn)
}

// bit32FieldArgs validates the (field, width) pair starting at argument
// farg. Returns the field offset f and width w. Width defaults to 1.
// Raises if f < 0, w <= 0, or f + w > 32.
func bit32FieldArgs(s *vm.State, farg int) (f, w int) {
	f = bit32CheckInt(s, farg)
	w = bit32OptInt(s, farg+1, 1)
	if f < 0 {
		s.LArgError(farg, "field cannot be negative")
	}
	if w <= 0 {
		s.LArgError(farg+1, "width must be positive")
	}
	if f+w > bit32NBits {
		s.LError("trying to access non-existent bits")
	}
	return f, w
}

// bit32Mask returns a value with the low w bits set (1 <= w <= 32).
func bit32Mask(w int) uint32 {
	if w >= bit32NBits {
		return ^uint32(0)
	}
	return (uint32(1) << uint32(w)) - 1
}

// ----------------------------------------------------------------------
// bitwise primitives
// ----------------------------------------------------------------------

func bit32Band(s *vm.State) int {
	n := s.Top()
	r := ^uint32(0)
	for i := 1; i <= n; i++ {
		r &= bit32CheckU32(s, i)
	}
	s.PushNumber(float64(r))
	return 1
}

func bit32Bor(s *vm.State) int {
	n := s.Top()
	var r uint32
	for i := 1; i <= n; i++ {
		r |= bit32CheckU32(s, i)
	}
	s.PushNumber(float64(r))
	return 1
}

func bit32Bxor(s *vm.State) int {
	n := s.Top()
	var r uint32
	for i := 1; i <= n; i++ {
		r ^= bit32CheckU32(s, i)
	}
	s.PushNumber(float64(r))
	return 1
}

func bit32Bnot(s *vm.State) int {
	r := ^bit32CheckU32(s, 1)
	s.PushNumber(float64(r))
	return 1
}

func bit32Btest(s *vm.State) int {
	n := s.Top()
	r := ^uint32(0)
	for i := 1; i <= n; i++ {
		r &= bit32CheckU32(s, i)
	}
	s.PushBoolean(r != 0)
	return 1
}

// ----------------------------------------------------------------------
// shifts and rotates
// ----------------------------------------------------------------------

// bit32Shift implements a logical shift. Positive i = left, negative =
// right. |i| >= 32 yields 0.
func bit32Shift(r uint32, i int) uint32 {
	if i < 0 {
		i = -i
		if i >= bit32NBits {
			return 0
		}
		return r >> uint(i)
	}
	if i >= bit32NBits {
		return 0
	}
	return r << uint(i)
}

func bit32Lshift(s *vm.State) int {
	r := bit32CheckU32(s, 1)
	i := bit32CheckInt(s, 2)
	s.PushNumber(float64(bit32Shift(r, i)))
	return 1
}

func bit32Rshift(s *vm.State) int {
	r := bit32CheckU32(s, 1)
	i := bit32CheckInt(s, 2)
	s.PushNumber(float64(bit32Shift(r, -i)))
	return 1
}

func bit32Arshift(s *vm.State) int {
	r := bit32CheckU32(s, 1)
	i := bit32CheckInt(s, 2)
	if i < 0 || (r&(uint32(1)<<(bit32NBits-1))) == 0 {
		s.PushNumber(float64(bit32Shift(r, -i)))
		return 1
	}
	if i >= bit32NBits {
		s.PushNumber(float64(^uint32(0)))
		return 1
	}
	shifted := r >> uint(i)
	signfill := ^(^uint32(0) >> uint(i))
	s.PushNumber(float64(shifted | signfill))
	return 1
}

// bit32Rot rotates r by i bits. Positive = left, negative = right.
func bit32Rot(r uint32, i int) uint32 {
	i &= bit32NBits - 1
	if i == 0 {
		return r
	}
	return (r << uint(i)) | (r >> uint(bit32NBits-i))
}

func bit32Lrotate(s *vm.State) int {
	r := bit32CheckU32(s, 1)
	i := bit32CheckInt(s, 2)
	s.PushNumber(float64(bit32Rot(r, i)))
	return 1
}

func bit32Rrotate(s *vm.State) int {
	r := bit32CheckU32(s, 1)
	i := bit32CheckInt(s, 2)
	s.PushNumber(float64(bit32Rot(r, -i)))
	return 1
}

// ----------------------------------------------------------------------
// field manipulation
// ----------------------------------------------------------------------

func bit32Extract(s *vm.State) int {
	r := bit32CheckU32(s, 1)
	f, w := bit32FieldArgs(s, 2)
	res := (r >> uint(f)) & bit32Mask(w)
	s.PushNumber(float64(res))
	return 1
}

func bit32Replace(s *vm.State) int {
	r := bit32CheckU32(s, 1)
	v := bit32CheckU32(s, 2)
	f, w := bit32FieldArgs(s, 3)
	m := bit32Mask(w)
	v &= m
	r = (r &^ (m << uint(f))) | (v << uint(f))
	s.PushNumber(float64(r))
	return 1
}

// ----------------------------------------------------------------------
// counts and byteswap
// ----------------------------------------------------------------------

func bit32Countlz(s *vm.State) int {
	v := bit32CheckU32(s, 1)
	if v == 0 {
		s.PushNumber(float64(bit32NBits))
	} else {
		s.PushNumber(float64(bits.LeadingZeros32(v)))
	}
	return 1
}

func bit32Countrz(s *vm.State) int {
	v := bit32CheckU32(s, 1)
	if v == 0 {
		s.PushNumber(float64(bit32NBits))
	} else {
		s.PushNumber(float64(bits.TrailingZeros32(v)))
	}
	return 1
}

func bit32Byteswap(s *vm.State) int {
	v := bit32CheckU32(s, 1)
	s.PushNumber(float64(bits.ReverseBytes32(v)))
	return 1
}

// ----------------------------------------------------------------------
// registration
// ----------------------------------------------------------------------

var bit32Funcs = []vm.LFnEntry{
	{Name: "arshift", Fn: bit32Arshift},
	{Name: "band", Fn: bit32Band},
	{Name: "bnot", Fn: bit32Bnot},
	{Name: "bor", Fn: bit32Bor},
	{Name: "bxor", Fn: bit32Bxor},
	{Name: "btest", Fn: bit32Btest},
	{Name: "extract", Fn: bit32Extract},
	{Name: "lrotate", Fn: bit32Lrotate},
	{Name: "lshift", Fn: bit32Lshift},
	{Name: "replace", Fn: bit32Replace},
	{Name: "rrotate", Fn: bit32Rrotate},
	{Name: "rshift", Fn: bit32Rshift},
	{Name: "countlz", Fn: bit32Countlz},
	{Name: "countrz", Fn: bit32Countrz},
	{Name: "byteswap", Fn: bit32Byteswap},
}

// openBit32Impl registers the bit32 library as a global table.
// Mirrors upstream luaopen_bit32 / luaL_register(L, "bit32", bitlib).
func openBit32Impl(s *vm.State) {
	s.NewTable()
	s.LRegisterList(bit32Funcs)
	s.PushValue(-1)
	s.SetGlobal("bit32")
	s.Pop(1)
}
