// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import (
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// utf8.go: implementation of Luau's `utf8` standard library, mirroring
// upstream .upstream/VM/src/lutf8lib.cpp.
//
// All decoding goes through utf8Decode, a direct port of upstream's
// static utf8_decode in lutf8lib.cpp: it rejects overlong encodings,
// surrogates (U+D800..U+DFFF) and codepoints above MAXUNICODE.
// Go's stdlib unicode/utf8 is intentionally NOT used for decoding
// because its acceptance rules differ subtly from upstream's.

const utf8MaxUnicode = 0x10FFFF

// utf8PatternString is upstream's UTF8PATT: a Lua pattern matching one
// UTF-8 codepoint. Published as utf8.charpattern.
const utf8PatternString = "[\x00-\x7F\xC2-\xF4][\x80-\xBF]*"

// utf8Decode decodes one UTF-8 sequence starting at s[off]. Returns
// the codepoint and the byte index immediately past the sequence, or
// (-1, -1) when the sequence is invalid by Luau's strict rules.
//
// Direct translation of upstream lutf8lib.cpp's utf8_decode.
func utf8Decode(s string, off int) (codepoint int, next int) {
	if off < 0 || off >= len(s) {
		return -1, -1
	}
	c := uint(s[off])
	if c < 0x80 {
		return int(c), off + 1
	}
	limits := [4]uint{0xFF, 0x7F, 0x7FF, 0xFFFF}
	var res uint
	count := 0
	for c&0x40 != 0 {
		count++
		if off+count >= len(s) {
			return -1, -1
		}
		cc := uint(s[off+count])
		if cc&0xC0 != 0x80 {
			return -1, -1
		}
		res = (res << 6) | (cc & 0x3F)
		c <<= 1
	}
	res |= (c & 0x7F) << uint(count*5)
	if count > 3 || res > utf8MaxUnicode || res <= limits[count] {
		return -1, -1
	}
	if res-0xD800 < 0x800 { // surrogate
		return -1, -1
	}
	return int(res), off + count + 1
}

// utf8Encode writes the UTF-8 encoding of cp into buf and returns the
// number of bytes written. Mirrors upstream luaO_utf8esc; cp must be
// in [0, MAXUNICODE]. buf must have capacity >= 4.
func utf8Encode(buf []byte, cp uint32) int {
	if cp < 0x80 {
		buf[0] = byte(cp)
		return 1
	}
	var n int
	switch {
	case cp < 0x800:
		n = 2
	case cp < 0x10000:
		n = 3
	default:
		n = 4
	}
	for i := n - 1; i > 0; i-- {
		buf[i] = byte(0x80 | (cp & 0x3F))
		cp >>= 6
	}
	lead := byte(0xFF) << uint(8-n) // n=2 -> 0xC0, n=3 -> 0xE0, n=4 -> 0xF0
	buf[0] = lead | byte(cp)
	return n
}

// utf8PosRelat translates a relative string position into a 1-based
// absolute index. Mirrors upstream's u_posrelat from lutf8lib.cpp.
// Suffixed `utf8` so it cannot clash with lib/string.go's posrelat.
func utf8PosRelat(pos int, length int) int {
	if pos >= 0 {
		return pos
	}
	if -pos > length {
		return 0
	}
	return length + pos + 1
}

// iscontByte reports whether b is a UTF-8 continuation byte.
func iscontByte(b byte) bool { return b&0xC0 == 0x80 }

// utf8Char implements utf8.char(...).
func utf8Char(s *vm.State) int {
	n := s.Top()
	if n == 0 {
		s.PushString("")
		return 1
	}
	out := make([]byte, 0, n*4)
	var buf [4]byte
	for i := 1; i <= n; i++ {
		cp, ok := s.ToInteger(i)
		if !ok {
			s.LArgError(i, "number expected")
		}
		if cp < 0 || cp > utf8MaxUnicode {
			s.LArgError(i, "value out of range")
		}
		l := utf8Encode(buf[:], uint32(cp))
		out = append(out, buf[:l]...)
	}
	s.PushString(string(out))
	return 1
}

// utf8Len implements utf8.len(s, i?, j?). Mirrors upstream `utflen`.
func utf8Len(s *vm.State) int {
	str := s.LCheckString(1)
	length := len(str)
	posi := utf8PosRelat(int(s.LOptInteger(2, 1)), length)
	posj := utf8PosRelat(int(s.LOptInteger(3, -1)), length)
	if !(1 <= posi && posi-1 <= length) {
		s.LArgError(2, "initial position out of string")
	}
	if !(posj-1 < length) {
		s.LArgError(3, "final position out of string")
	}
	posi--
	posj--

	n := 0
	for posi <= posj {
		cp, next := utf8Decode(str, posi)
		if cp < 0 {
			s.PushNil()
			s.PushInteger(int64(posi + 1))
			return 2
		}
		posi = next
		n++
	}
	s.PushInteger(int64(n))
	return 1
}

// utf8Codepoint implements utf8.codepoint(s, i?, j?).
func utf8Codepoint(s *vm.State) int {
	str := s.LCheckString(1)
	length := len(str)
	posi := utf8PosRelat(int(s.LOptInteger(2, 1)), length)
	pose := utf8PosRelat(int(s.LOptInteger(3, int64(posi))), length)
	if posi < 1 {
		s.LArgError(2, "out of range")
	}
	if pose > length {
		s.LArgError(3, "out of range")
	}
	if posi > pose {
		return 0
	}
	off := posi - 1
	end := pose
	pushed := 0
	for off < end {
		cp, next := utf8Decode(str, off)
		if cp < 0 {
			s.LError("invalid UTF-8 code")
		}
		s.PushInteger(int64(cp))
		pushed++
		off = next
	}
	return pushed
}

// utf8Offset implements utf8.offset(s, n, i?). Mirrors upstream
// `byteoffset`.
func utf8Offset(s *vm.State) int {
	str := s.LCheckString(1)
	length := len(str)
	n := int(s.LCheckInteger(2))
	defI := 1
	if n < 0 {
		defI = length + 1
	}
	posi := utf8PosRelat(int(s.LOptInteger(3, int64(defI))), length)
	if !(1 <= posi && posi-1 <= length) {
		s.LArgError(3, "position out of range")
	}
	posi--

	if n == 0 {
		for posi > 0 && posi < length && iscontByte(str[posi]) {
			posi--
		}
	} else {
		if posi < length && iscontByte(str[posi]) {
			s.LError("initial position is a continuation byte")
		}
		if n < 0 {
			for n < 0 && posi > 0 {
				posi--
				for posi > 0 && iscontByte(str[posi]) {
					posi--
				}
				n++
			}
		} else {
			n--
			for n > 0 && posi < length {
				posi++
				for posi < length && iscontByte(str[posi]) {
					posi++
				}
				n--
			}
		}
	}
	if n == 0 {
		s.PushInteger(int64(posi + 1))
	} else {
		s.PushNil()
	}
	return 1
}

// utf8IterAux is the stateless iterator returned by utf8.codes.
func utf8IterAux(s *vm.State) int {
	str := s.LCheckString(1)
	length := len(str)
	prev, _ := s.ToInteger(2)
	n := int(prev) - 1
	if n < 0 {
		n = 0
	} else if n < length {
		n++
		for n < length && iscontByte(str[n]) {
			n++
		}
	}
	if n >= length {
		return 0
	}
	cp, next := utf8Decode(str, n)
	if cp < 0 {
		s.LError("invalid UTF-8 code")
	}
	if next < length && iscontByte(str[next]) {
		s.LError("invalid UTF-8 code")
	}
	s.PushInteger(int64(n + 1))
	s.PushInteger(int64(cp))
	return 2
}

// utf8Codes implements utf8.codes(s). Returns (iterator, s, 0).
func utf8Codes(s *vm.State) int {
	s.LCheckString(1)
	s.PushGoFunction(utf8IterAux, 0)
	s.PushValue(1)
	s.PushInteger(0)
	return 3
}

// openUTF8 installs the `utf8` library table as a global. Mirrors
// upstream luaopen_utf8.
func openUTF8(s *vm.State) {
	s.CreateTable(0, 8)
	s.LRegisterList([]vm.LFnEntry{
		{Name: "offset", Fn: utf8Offset},
		{Name: "codepoint", Fn: utf8Codepoint},
		{Name: "char", Fn: utf8Char},
		{Name: "len", Fn: utf8Len},
		{Name: "codes", Fn: utf8Codes},
	})
	s.PushString(utf8PatternString)
	s.SetField(-2, "charpattern")
	// Publish as _G.utf8. Pop the duplicate; the original table is
	// left on the stack to match math.go's convention.
	s.PushValue(-1)
	s.SetGlobal("utf8")
	s.Pop(1)
}
