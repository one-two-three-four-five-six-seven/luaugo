// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

// Package lib's string library mirrors upstream VM/src/lstrlib.cpp.
// Functions: byte, char, find, format, gmatch, gsub, len, lower, match,
// rep, reverse, sub, upper, split, pack, packsize, unpack. Patterns are
// Lua 5.x patterns (not regex). The string metatable is set on
// gs.mt[TString] so that ("x"):upper() works.

package lib

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// ----------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------

func posRelat(pos int, length int) int {
	if pos < 0 {
		pos += length + 1
	}
	if pos < 0 {
		return 0
	}
	return pos
}

func asciiToLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func asciiToUpper(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - ('a' - 'A')
	}
	return b
}

// ----------------------------------------------------------------------
// Open
// ----------------------------------------------------------------------

func openString(s *vm.State) {
	s.NewTable()

	entries := []vm.LFnEntry{
		{Name: "byte", Fn: strByte},
		{Name: "char", Fn: strChar},
		{Name: "find", Fn: strFind},
		{Name: "format", Fn: strFormat},
		{Name: "gmatch", Fn: strGmatch},
		{Name: "gsub", Fn: strGsub},
		{Name: "len", Fn: strLen},
		{Name: "lower", Fn: strLower},
		{Name: "match", Fn: strMatch},
		{Name: "rep", Fn: strRep},
		{Name: "reverse", Fn: strReverse},
		{Name: "sub", Fn: strSub},
		{Name: "upper", Fn: strUpper},
		{Name: "split", Fn: strSplit},
		{Name: "pack", Fn: strPack},
		{Name: "packsize", Fn: strPacksize},
		{Name: "unpack", Fn: strUnpack},
	}
	s.LRegisterList(entries)

	s.PushValue(-1)
	s.SetGlobal("string")

	// Metatable for strings: {__index = string}.
	s.NewTable()
	s.PushValue(-2)
	s.SetField(-2, "__index")

	// Bind the metatable to TString via SetMetatable on an empty string.
	s.PushString("")
	s.Insert(-2)
	s.SetMetatable(-2)
	s.Pop(1)

	s.Pop(1) // pop strlib
}

// ----------------------------------------------------------------------
// Trivial functions
// ----------------------------------------------------------------------

func strLen(s *vm.State) int {
	v := s.LCheckString(1)
	s.PushInteger(int64(len(v)))
	return 1
}

func strSub(s *vm.State) int {
	str := s.LCheckString(1)
	l := len(str)
	start := posRelat(int(s.LCheckInteger(2)), l)
	end := posRelat(int(s.LOptInteger(3, -1)), l)
	if start < 1 {
		start = 1
	}
	if end > l {
		end = l
	}
	if start <= end {
		s.PushString(str[start-1 : end])
	} else {
		s.PushString("")
	}
	return 1
}

func strReverse(s *vm.State) int {
	str := s.LCheckString(1)
	b := make([]byte, len(str))
	for i := 0; i < len(str); i++ {
		b[len(str)-1-i] = str[i]
	}
	s.PushString(string(b))
	return 1
}

func strLower(s *vm.State) int {
	str := s.LCheckString(1)
	b := make([]byte, len(str))
	for i := 0; i < len(str); i++ {
		b[i] = asciiToLower(str[i])
	}
	s.PushString(string(b))
	return 1
}

func strUpper(s *vm.State) int {
	str := s.LCheckString(1)
	b := make([]byte, len(str))
	for i := 0; i < len(str); i++ {
		b[i] = asciiToUpper(str[i])
	}
	s.PushString(string(b))
	return 1
}

func strRep(s *vm.State) int {
	str := s.LCheckString(1)
	n := int(s.LCheckInteger(2))
	sep := s.LOptString(3, "")
	if n <= 0 {
		s.PushString("")
		return 1
	}
	const maxStrSize = 1 << 30
	lenStr := len(str)
	lenSep := len(sep)
	// Bail out cheaply for any product that would obviously overflow
	// or exceed the per-string size limit, before allocating anything.
	// We compute `lenStr*n + lenSep*(n-1)` defensively to avoid signed
	// 64-bit overflow on 64-bit ints and to refuse multi-GB strings
	// the way upstream lstrlib's luaL_addsize check does.
	if n > 0 && lenStr > maxStrSize/n {
		s.LError("resulting string too large")
		return 0
	}
	if sep == "" {
		s.PushString(strings.Repeat(str, n))
		return 1
	}
	if n > 1 && lenSep > maxStrSize/(n-1) {
		s.LError("resulting string too large")
		return 0
	}
	total := lenStr*n + lenSep*(n-1)
	if total < 0 || total > maxStrSize {
		s.LError("resulting string too large")
		return 0
	}
	var b strings.Builder
	b.Grow(total)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(str)
	}
	s.PushString(b.String())
	return 1
}

func strByte(s *vm.State) int {
	str := s.LCheckString(1)
	l := len(str)
	posi := posRelat(int(s.LOptInteger(2, 1)), l)
	pose := posRelat(int(s.LOptInteger(3, int64(posi))), l)
	if posi <= 0 {
		posi = 1
	}
	if pose > l {
		pose = l
	}
	if posi > pose {
		return 0
	}
	n := pose - posi + 1
	for i := 0; i < n; i++ {
		s.PushInteger(int64(uint8(str[posi-1+i])))
	}
	return n
}

func strChar(s *vm.State) int {
	n := s.Top()
	b := make([]byte, n)
	for i := 1; i <= n; i++ {
		c := s.LCheckInteger(i)
		if c < 0 || c > 255 {
			s.LArgError(i, "value out of range")
		}
		b[i-1] = byte(c)
	}
	s.PushString(string(b))
	return 1
}

// ----------------------------------------------------------------------
// Split (Luau extension)
// ----------------------------------------------------------------------

func strSplit(s *vm.State) int {
	haystack := s.LCheckString(1)
	needle := s.LOptString(2, ",")

	s.CreateTable(0, 0)

	if len(needle) == 0 {
		for i := 0; i < len(haystack); i++ {
			s.PushString(haystack[i : i+1])
			s.RawSetI(-2, i+1)
		}
		return 1
	}

	idx := 1
	start := 0
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			s.PushString(haystack[start:i])
			s.RawSetI(-2, idx)
			idx++
			start = i + len(needle)
			i += len(needle) - 1
		}
	}
	s.PushString(haystack[start:])
	s.RawSetI(-2, idx)
	return 1
}

// ----------------------------------------------------------------------
// Pattern matching (mirrors lstrlib.cpp).
// ----------------------------------------------------------------------

const (
	luaMaxCaptures = 32
	maxRecDepth    = 200
	capUnfinished  = -1
	capPosition    = -2
	lEsc           = '%'
	specials       = "^$*+?.([%-"
)

type capture struct {
	init int
	len  int
}

type matchState struct {
	src        string
	pattern    string
	srcEnd     int
	pEnd       int
	level      int
	matchDepth int
	captures   [luaMaxCaptures]capture
	err        string
}

func (ms *matchState) raise(format string, args ...any) {
	if ms.err == "" {
		ms.err = fmt.Sprintf(format, args...)
	}
}

func (ms *matchState) classend(p int) int {
	if p >= ms.pEnd {
		ms.raise("malformed pattern")
		return p
	}
	c := ms.pattern[p]
	p++
	switch c {
	case lEsc:
		if p >= ms.pEnd {
			ms.raise("malformed pattern (ends with '%%')")
			return p
		}
		return p + 1
	case '[':
		if p < ms.pEnd && ms.pattern[p] == '^' {
			p++
		}
		for {
			if p >= ms.pEnd {
				ms.raise("malformed pattern (missing ']')")
				return p
			}
			ch := ms.pattern[p]
			p++
			if ch == lEsc && p < ms.pEnd {
				p++
			}
			if p >= ms.pEnd {
				ms.raise("malformed pattern (missing ']')")
				return p
			}
			if ms.pattern[p] == ']' {
				return p + 1
			}
		}
	default:
		return p
	}
}

func matchClass(c byte, cl byte) bool {
	var res bool
	clLower := asciiToLower(cl)
	switch clLower {
	case 'a':
		res = isAlpha(c)
	case 'c':
		res = isCntrl(c)
	case 'd':
		res = isDigit(c)
	case 'g':
		res = isGraph(c)
	case 'l':
		res = isLower(c)
	case 'p':
		res = isPunct(c)
	case 's':
		res = isSpace(c)
	case 'u':
		res = isUpper(c)
	case 'w':
		res = isAlnum(c)
	case 'x':
		res = isXDigit(c)
	case 'z':
		res = c == 0
	default:
		return cl == c
	}
	if isLowerASCII(cl) {
		return res
	}
	return !res
}

func matchBracketClass(ms *matchState, c byte, p, ec int) bool {
	sig := true
	if p+1 < ms.pEnd && ms.pattern[p+1] == '^' {
		sig = false
		p++
	}
	for p++; p < ec; p++ {
		if ms.pattern[p] == lEsc {
			p++
			if p < ec && matchClass(c, ms.pattern[p]) {
				return sig
			}
		} else if p+1 < ec && ms.pattern[p+1] == '-' && p+2 < ec {
			lo := ms.pattern[p]
			hi := ms.pattern[p+2]
			p += 2
			if lo <= c && c <= hi {
				return sig
			}
		} else if ms.pattern[p] == c {
			return sig
		}
	}
	return !sig
}

func singleMatch(ms *matchState, s, p, ep int) bool {
	if s >= ms.srcEnd {
		return false
	}
	c := ms.src[s]
	switch ms.pattern[p] {
	case '.':
		return true
	case lEsc:
		if p+1 >= ms.pEnd {
			return false
		}
		return matchClass(c, ms.pattern[p+1])
	case '[':
		return matchBracketClass(ms, c, p, ep-1)
	default:
		return ms.pattern[p] == c
	}
}

func matchBalance(ms *matchState, s, p int) int {
	if p+1 >= ms.pEnd {
		ms.raise("malformed pattern (missing arguments to '%%b')")
		return -1
	}
	if s >= ms.srcEnd || ms.src[s] != ms.pattern[p] {
		return -1
	}
	b := ms.pattern[p]
	e := ms.pattern[p+1]
	cont := 1
	s++
	for s < ms.srcEnd {
		if ms.src[s] == e {
			cont--
			if cont == 0 {
				return s + 1
			}
		} else if ms.src[s] == b {
			cont++
		}
		s++
	}
	return -1
}

func maxExpand(ms *matchState, s, p, ep int) int {
	i := 0
	for singleMatch(ms, s+i, p, ep) {
		i++
	}
	for i >= 0 {
		res := doMatch(ms, s+i, ep+1)
		if res >= 0 {
			return res
		}
		i--
	}
	return -1
}

func minExpand(ms *matchState, s, p, ep int) int {
	for {
		res := doMatch(ms, s, ep+1)
		if res >= 0 {
			return res
		}
		if singleMatch(ms, s, p, ep) {
			s++
		} else {
			return -1
		}
	}
}

func startCapture(ms *matchState, s, p, what int) int {
	level := ms.level
	if level >= luaMaxCaptures {
		ms.raise("too many captures")
		return -1
	}
	ms.captures[level].init = s
	ms.captures[level].len = what
	ms.level = level + 1
	res := doMatch(ms, s, p)
	if res < 0 {
		ms.level--
	}
	return res
}

func endCapture(ms *matchState, s, p int) int {
	l := -1
	for i := ms.level - 1; i >= 0; i-- {
		if ms.captures[i].len == capUnfinished {
			l = i
			break
		}
	}
	if l < 0 {
		ms.raise("invalid pattern capture")
		return -1
	}
	ms.captures[l].len = s - ms.captures[l].init
	res := doMatch(ms, s, p)
	if res < 0 {
		ms.captures[l].len = capUnfinished
	}
	return res
}

func matchCapture(ms *matchState, s, l int) int {
	l -= '1'
	if l < 0 || l >= ms.level || ms.captures[l].len == capUnfinished {
		ms.raise("invalid capture index %%%d", l+1)
		return -1
	}
	length := ms.captures[l].len
	if length == capPosition {
		return s
	}
	if ms.srcEnd-s >= length && ms.src[ms.captures[l].init:ms.captures[l].init+length] == ms.src[s:s+length] {
		return s + length
	}
	return -1
}

// matchDefault handles the "pattern class plus optional suffix" branch
// from upstream's match() dflt: case. Returns the new s on success or
// -1 on failure. The boolean `cont` indicates that the caller should
// continue the iterative match loop at position (s, p).
func matchDefault(ms *matchState, s, p int) (newS, newP int, cont bool, done bool) {
	ep := ms.classend(p)
	if !singleMatch(ms, s, p, ep) {
		if ep < ms.pEnd {
			switch ms.pattern[ep] {
			case '*', '?', '-':
				return s, ep + 1, true, false
			}
		}
		return -1, 0, false, true
	}
	var suf byte
	if ep < ms.pEnd {
		suf = ms.pattern[ep]
	}
	switch suf {
	case '?':
		res := doMatch(ms, s+1, ep+1)
		if res >= 0 {
			return res, 0, false, true
		}
		return s, ep + 1, true, false
	case '+':
		s++
		return maxExpand(ms, s, p, ep), 0, false, true
	case '*':
		return maxExpand(ms, s, p, ep), 0, false, true
	case '-':
		return minExpand(ms, s, p, ep), 0, false, true
	default:
		return s + 1, ep, true, false
	}
}

// doMatch is the recursive matcher. Returns the position one past the
// end of the match, or -1 on failure. Mirrors lstrlib.cpp's match().
func doMatch(ms *matchState, s, p int) int {
	if ms.err != "" {
		return -1
	}
	ms.matchDepth--
	if ms.matchDepth <= 0 {
		ms.raise("pattern too complex")
		ms.matchDepth++
		return -1
	}
	defer func() { ms.matchDepth++ }()

	for {
		if p >= ms.pEnd {
			return s
		}
		switch ms.pattern[p] {
		case '(':
			if p+1 < ms.pEnd && ms.pattern[p+1] == ')' {
				return startCapture(ms, s, p+2, capPosition)
			}
			return startCapture(ms, s, p+1, capUnfinished)

		case ')':
			return endCapture(ms, s, p+1)

		case '$':
			if p+1 == ms.pEnd {
				if s == ms.srcEnd {
					return s
				}
				return -1
			}
			// Not the last char — treat '$' as literal via default.
			ns, np, cont, done := matchDefault(ms, s, p)
			if done {
				return ns
			}
			if cont {
				s, p = ns, np
				continue
			}
			return -1

		case lEsc:
			if p+1 < ms.pEnd {
				pc := ms.pattern[p+1]
				switch pc {
				case 'b':
					ns := matchBalance(ms, s, p+2)
					if ns >= 0 {
						s = ns
						p += 4
						continue
					}
					return -1
				case 'f':
					p += 2
					if p >= ms.pEnd || ms.pattern[p] != '[' {
						ms.raise("missing '[' after '%%f' in pattern")
						return -1
					}
					ep := ms.classend(p)
					var prev byte
					if s > 0 {
						prev = ms.src[s-1]
					}
					var cur byte
					if s < ms.srcEnd {
						cur = ms.src[s]
					}
					if !matchBracketClass(ms, prev, p, ep-1) && matchBracketClass(ms, cur, p, ep-1) {
						p = ep
						continue
					}
					return -1
				case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
					ns := matchCapture(ms, s, int(pc))
					if ns >= 0 {
						s = ns
						p += 2
						continue
					}
					return -1
				default:
					ns, np, cont, done := matchDefault(ms, s, p)
					if done {
						return ns
					}
					if cont {
						s, p = ns, np
						continue
					}
					return -1
				}
			}
			ns, np, cont, done := matchDefault(ms, s, p)
			if done {
				return ns
			}
			if cont {
				s, p = ns, np
				continue
			}
			return -1

		default:
			ns, np, cont, done := matchDefault(ms, s, p)
			if done {
				return ns
			}
			if cont {
				s, p = ns, np
				continue
			}
			return -1
		}
	}
}

func prepState(ms *matchState, src, pattern string) {
	ms.src = src
	ms.pattern = pattern
	ms.srcEnd = len(src)
	ms.pEnd = len(pattern)
	ms.matchDepth = maxRecDepth
}

func reprepState(ms *matchState) {
	ms.level = 0
	ms.matchDepth = maxRecDepth
	ms.err = ""
}

func noSpecials(p string) bool {
	return !strings.ContainsAny(p, specials)
}

func pushOneCapture(s *vm.State, ms *matchState, i, sStart, sEnd int) {
	if i >= ms.level {
		if i == 0 {
			s.PushString(ms.src[sStart:sEnd])
			return
		}
		ms.raise("invalid capture index")
		return
	}
	cl := ms.captures[i].len
	if cl == capUnfinished {
		ms.raise("unfinished capture")
		return
	}
	if cl == capPosition {
		s.PushInteger(int64(ms.captures[i].init + 1))
		return
	}
	init := ms.captures[i].init
	s.PushString(ms.src[init : init+cl])
}

func pushCaptures(s *vm.State, ms *matchState, sStart, sEnd int) int {
	nlevels := ms.level
	if nlevels == 0 && sStart >= 0 {
		nlevels = 1
	}
	for i := 0; i < nlevels; i++ {
		pushOneCapture(s, ms, i, sStart, sEnd)
		if ms.err != "" {
			// Stop pushing partial captures; caller will surface the
			// error after rolling back the stack.
			return i
		}
	}
	return nlevels
}

func findOrMatchAux(s *vm.State, find bool) int {
	src := s.LCheckString(1)
	pattern := s.LCheckString(2)
	ls := len(src)
	init := posRelat(int(s.LOptInteger(3, 1)), ls)
	if init < 1 {
		init = 1
	} else if init > ls+1 {
		s.PushNil()
		return 1
	}

	plain := false
	if find && !s.IsNoneOrNil(4) {
		plain = s.ToBoolean(4)
	}

	if find && (plain || noSpecials(pattern)) {
		idx := strings.Index(src[init-1:], pattern)
		if idx >= 0 {
			start := init + idx
			end := start + len(pattern) - 1
			s.PushInteger(int64(start))
			s.PushInteger(int64(end))
			return 2
		}
		s.PushNil()
		return 1
	}

	var ms matchState
	anchor := len(pattern) > 0 && pattern[0] == '^'
	patStart := 0
	if anchor {
		patStart = 1
	}
	prepState(&ms, src, pattern[patStart:])
	s1 := init - 1
	for {
		reprepState(&ms)
		res := doMatch(&ms, s1, 0)
		if ms.err != "" {
			s.LError("%s", ms.err)
		}
		if res >= 0 {
			if find {
				s.PushInteger(int64(s1 + 1))
				s.PushInteger(int64(res))
				nc := pushCaptures(s, &ms, -1, 0)
				if ms.err != "" {
					s.LError("%s", ms.err)
				}
				return nc + 2
			}
			nc := pushCaptures(s, &ms, s1, res)
			if ms.err != "" {
				s.LError("%s", ms.err)
			}
			return nc
		}
		s1++
		if s1 > ms.srcEnd || anchor {
			break
		}
	}
	s.PushNil()
	return 1
}

func strFind(s *vm.State) int  { return findOrMatchAux(s, true) }
func strMatch(s *vm.State) int { return findOrMatchAux(s, false) }

// strGmatch returns an iterator closure. Iterator state (src, pattern,
// current position) is captured in the Go closure value because the
// public API does not expose lua_upvalueindex.
func strGmatch(s *vm.State) int {
	src := s.LCheckString(1)
	pattern := s.LCheckString(2)
	pos := 0
	iter := func(s *vm.State) int {
		var ms matchState
		prepState(&ms, src, pattern)
		for pos <= ms.srcEnd {
			reprepState(&ms)
			e := doMatch(&ms, pos, 0)
			if ms.err != "" {
				s.LError("%s", ms.err)
				return 0
			}
			if e >= 0 {
				start := pos
				if e == pos {
					pos = e + 1
				} else {
					pos = e
				}
				nc := pushCaptures(s, &ms, start, e)
				if ms.err != "" {
					s.LError("%s", ms.err)
					return 0
				}
				return nc
			}
			pos++
		}
		return 0
	}
	s.PushGoFunction(iter, 0)
	return 1
}

// ----------------------------------------------------------------------
// gsub
// ----------------------------------------------------------------------

func strGsub(s *vm.State) int {
	src := s.LCheckString(1)
	pattern := s.LCheckString(2)
	srcl := len(src)
	maxS := int(s.LOptInteger(4, int64(srcl+1)))
	repType := s.Type(3)
	if repType != vm.TString && repType != vm.TNumber && repType != vm.TFunction && repType != vm.TTable {
		s.LArgError(3, "string/function/table expected")
	}

	anchor := len(pattern) > 0 && pattern[0] == '^'
	patStart := 0
	if anchor {
		patStart = 1
	}

	var ms matchState
	prepState(&ms, src, pattern[patStart:])

	var b strings.Builder
	srcPos := 0
	n := 0
	for n < maxS {
		reprepState(&ms)
		e := doMatch(&ms, srcPos, 0)
		if ms.err != "" {
			s.LError("%s", ms.err)
		}
		if e >= 0 {
			n++
			gsubAddValue(s, &ms, &b, srcPos, e, repType)
			if ms.err != "" {
				s.LError("%s", ms.err)
			}
		}
		// Advance the source position. Mirrors upstream:
		//   if (e && e > src) src = e;
		//   else if (src < ms.src_end) addchar(*src++);
		//   else break;
		// Importantly, when match terminates AT src_end we must
		// break out, not run another iteration with srcPos beyond
		// src_end (which would index out of bounds in subsequent
		// pattern operations such as %f).
		if e >= 0 && e > srcPos {
			srcPos = e
		} else if srcPos < ms.srcEnd {
			b.WriteByte(ms.src[srcPos])
			srcPos++
		} else {
			break
		}
		if anchor {
			break
		}
	}
	if srcPos < ms.srcEnd {
		b.WriteString(ms.src[srcPos:])
	}
	s.PushString(b.String())
	s.PushInteger(int64(n))
	return 2
}

func gsubAddValue(s *vm.State, ms *matchState, b *strings.Builder, sStart, sEnd int, repType vm.Type) {
	switch repType {
	case vm.TFunction:
		s.PushValue(3)
		nargs := pushCaptures(s, ms, sStart, sEnd)
		if ms.err != "" {
			return
		}
		s.Call(nargs, 1)
		gsubAddResult(s, ms, b, sStart, sEnd)
	case vm.TTable:
		pushOneCapture(s, ms, 0, sStart, sEnd)
		if ms.err != "" {
			return
		}
		s.GetTable(3)
		gsubAddResult(s, ms, b, sStart, sEnd)
	default:
		rep, _ := s.ToString(3)
		gsubAddString(s, ms, b, sStart, sEnd, rep)
	}
}

func gsubAddString(s *vm.State, ms *matchState, b *strings.Builder, sStart, sEnd int, news string) {
	for i := 0; i < len(news); i++ {
		c := news[i]
		if c != lEsc {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(news) {
			s.LError("invalid use of '%%' in replacement string")
			return
		}
		c = news[i]
		if !(c >= '0' && c <= '9') {
			if c != lEsc {
				s.LError("invalid use of '%%' in replacement string")
				return
			}
			b.WriteByte(c)
			continue
		}
		if c == '0' {
			b.WriteString(ms.src[sStart:sEnd])
			continue
		}
		idx := int(c - '1')
		if idx >= ms.level {
			if idx == 0 {
				b.WriteString(ms.src[sStart:sEnd])
				continue
			}
			s.LError("invalid capture index %%%d in replacement string", idx+1)
			return
		}
		cl := ms.captures[idx].len
		if cl == capPosition {
			b.WriteString(strconv.Itoa(ms.captures[idx].init + 1))
		} else {
			init := ms.captures[idx].init
			b.WriteString(ms.src[init : init+cl])
		}
	}
}

func gsubAddResult(s *vm.State, ms *matchState, b *strings.Builder, sStart, sEnd int) {
	if !s.ToBoolean(-1) {
		s.Pop(1)
		b.WriteString(ms.src[sStart:sEnd])
		return
	}
	v, ok := s.ToString(-1)
	if !ok {
		s.LError("invalid replacement value (a %s)", s.Type(-1).String())
		return
	}
	b.WriteString(v)
	s.Pop(1)
}

// ----------------------------------------------------------------------
// Format
// ----------------------------------------------------------------------

const formatFlags = "-+ #0"

func strFormat(s *vm.State) int {
	top := s.Top()
	frm := s.LCheckString(1)
	arg := 1
	var b strings.Builder

	for i := 0; i < len(frm); {
		c := frm[i]
		if c != lEsc {
			b.WriteByte(c)
			i++
			continue
		}
		i++
		if i >= len(frm) {
			s.LError("invalid format string (ends with '%%')")
			return 0
		}
		if frm[i] == lEsc {
			b.WriteByte(lEsc)
			i++
			continue
		}
		if frm[i] == '*' {
			i++
			arg++
			if arg > top {
				s.LError("missing argument #%d", arg)
			}
			b.WriteString(s.LToLString(arg))
			continue
		}
		spec, conv, ni, err := scanFormat(frm, i)
		if err != "" {
			s.LError("%s", err)
			return 0
		}
		i = ni
		arg++
		if arg > top {
			s.LError("missing argument #%d", arg)
		}
		switch conv {
		case 'c':
			// Lua's %c emits a single raw byte (NOT a UTF-8 rune like
			// Go's fmt.Sprintf("%c",...) does). Width/flag handling
			// (e.g. "%5c") still applies, but the produced character
			// is the raw byte value modulo 256.
			n := s.LCheckInteger(arg)
			if n < 0 || n > 255 {
				s.LError("bad argument #%d to 'format' (value out of range)", arg)
				return 0
			}
			ch := byte(n)
			if spec == "" {
				b.WriteByte(ch)
			} else {
				// Render via %s with the same flag/width spec to honor
				// alignment without re-interpreting the byte as a rune.
				b.WriteString(fmt.Sprintf("%"+spec+"s", string([]byte{ch})))
			}
		case 'd', 'i':
			n := s.LCheckInteger(arg)
			b.WriteString(fmt.Sprintf("%"+spec+"d", n))
		case 'u':
			n := s.LCheckInteger(arg)
			b.WriteString(fmt.Sprintf("%"+spec+"d", uint64(n)))
		case 'o':
			n := s.LCheckInteger(arg)
			b.WriteString(fmt.Sprintf("%"+spec+"o", uint64(n)))
		case 'x':
			n := s.LCheckInteger(arg)
			b.WriteString(fmt.Sprintf("%"+spec+"x", uint64(n)))
		case 'X':
			n := s.LCheckInteger(arg)
			b.WriteString(fmt.Sprintf("%"+spec+"X", uint64(n)))
		case 'e', 'E', 'f', 'g', 'G':
			n := s.LCheckNumber(arg)
			b.WriteString(fmt.Sprintf("%"+spec+string(conv), n))
		case 's':
			v := s.LCheckString(arg)
			b.WriteString(fmt.Sprintf("%"+spec+"s", v))
		case 'q':
			v := s.LCheckString(arg)
			b.WriteString(quoteString(v))
		default:
			s.LError("invalid option '%%%c' to 'format'", conv)
		}
	}
	s.PushString(b.String())
	return 1
}

func scanFormat(frm string, i int) (spec string, conv byte, ni int, errMsg string) {
	start := i
	flagSeen := map[byte]bool{}
	for i < len(frm) && strings.IndexByte(formatFlags, frm[i]) >= 0 {
		if flagSeen[frm[i]] {
			return "", 0, i, "invalid format (repeated flags)"
		}
		flagSeen[frm[i]] = true
		i++
	}
	for j := 0; j < 2 && i < len(frm) && frm[i] >= '0' && frm[i] <= '9'; j++ {
		i++
	}
	if i < len(frm) && frm[i] == '.' {
		i++
		for j := 0; j < 2 && i < len(frm) && frm[i] >= '0' && frm[i] <= '9'; j++ {
			i++
		}
	}
	if i < len(frm) && frm[i] >= '0' && frm[i] <= '9' {
		return "", 0, i, "invalid format (width or precision too long)"
	}
	if i >= len(frm) {
		return "", 0, i, "invalid format string"
	}
	conv = frm[i]
	spec = frm[start:i]
	ni = i + 1
	return
}

func quoteString(v string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch c {
		case '"', '\\', '\n':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\r':
			b.WriteString("\\r")
		case 0:
			b.WriteString("\\000")
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ----------------------------------------------------------------------
// Pack / Unpack / Packsize
// ----------------------------------------------------------------------

type packHeader struct {
	little   bool
	maxAlign int
}

func initPackHeader() packHeader {
	return packHeader{little: true, maxAlign: 1}
}

type packKind int

const (
	kInt packKind = iota
	kUint
	kFloat
	kChar
	kString
	kZstr
	kPadding
	kPadAlign
	kNop
)

// packReadDigits parses an unsigned decimal at fmtStr[i]. When no
// digit is present the default `def` is returned. The `over` flag is
// reported when the parsed number exceeds packsizeMax (1GB) or would
// have overflowed Go int during accumulation. Mirrors upstream
// lstrlib's `getnum` for purposes of distinguishing "size too large"
// from "out of limits".
func packReadDigits(fmtStr string, i int, def int) (v int, ni int, over bool) {
	if i >= len(fmtStr) || fmtStr[i] < '0' || fmtStr[i] > '9' {
		return def, i, false
	}
	v = 0
	for i < len(fmtStr) && fmtStr[i] >= '0' && fmtStr[i] <= '9' {
		d := int(fmtStr[i] - '0')
		// Detect signed-int overflow on the multiply or add.
		if v > (int(^uint(0)>>1)-d)/10 {
			over = true
			// Consume the rest of the digit run so callers advance
			// past the number cleanly.
			for i < len(fmtStr) && fmtStr[i] >= '0' && fmtStr[i] <= '9' {
				i++
			}
			return v, i, true
		}
		v = v*10 + d
		i++
	}
	if v > packsizeMax {
		over = true
	}
	return v, i, over
}

func packOption(s *vm.State, h *packHeader, fmtStr string, i int) (packKind, int, int) {
	if i >= len(fmtStr) {
		return kNop, 0, i
	}
	opt := fmtStr[i]
	i++
	switch opt {
	case 'b':
		return kInt, 1, i
	case 'B':
		return kUint, 1, i
	case 'h':
		return kInt, 2, i
	case 'H':
		return kUint, 2, i
	case 'l':
		return kInt, 8, i
	case 'L':
		return kUint, 8, i
	case 'j':
		return kInt, 4, i
	case 'J':
		return kUint, 4, i
	case 'T':
		return kUint, 4, i
	case 'f':
		return kFloat, 4, i
	case 'd':
		return kFloat, 8, i
	case 'n':
		return kFloat, 8, i
	case 'i':
		sz, ni, over := packReadDigits(fmtStr, i, 4)
		if over {
			s.LError("size specifier is too large")
		}
		if sz <= 0 || sz > 16 {
			s.LError("integral size (%d) out of limits [1,16]", sz)
		}
		return kInt, sz, ni
	case 'I':
		sz, ni, over := packReadDigits(fmtStr, i, 4)
		if over {
			s.LError("size specifier is too large")
		}
		if sz <= 0 || sz > 16 {
			s.LError("integral size (%d) out of limits [1,16]", sz)
		}
		return kUint, sz, ni
	case 's':
		sz, ni, over := packReadDigits(fmtStr, i, 4)
		if over {
			s.LError("size specifier is too large")
		}
		if sz <= 0 || sz > 16 {
			s.LError("integral size (%d) out of limits [1,16]", sz)
		}
		return kString, sz, ni
	case 'c':
		sz, ni, over := packReadDigits(fmtStr, i, -1)
		if over {
			s.LError("size specifier is too large")
		}
		if sz == -1 {
			s.LError("missing size for format option 'c'")
		}
		return kChar, sz, ni
	case 'z':
		return kZstr, 0, i
	case 'x':
		return kPadding, 1, i
	case 'X':
		return kPadAlign, 0, i
	case ' ':
		return kNop, 0, i
	case '<':
		h.little = true
		return kNop, 0, i
	case '>':
		h.little = false
		return kNop, 0, i
	case '=':
		h.little = true
		return kNop, 0, i
	case '!':
		ma, ni, over := packReadDigits(fmtStr, i, 8)
		if over {
			s.LError("size specifier is too large")
		}
		if ma <= 0 || ma > 16 {
			s.LError("integral size (%d) out of limits [1,16]", ma)
		}
		h.maxAlign = ma
		return kNop, 0, ni
	}
	s.LError("invalid format option '%c'", opt)
	return kNop, 0, i
}

func packDetails(s *vm.State, h *packHeader, totalSize int, fmtStr string, i int) (kind packKind, size, nAlign, ni int) {
	kind, size, ni = packOption(s, h, fmtStr, i)
	align := size
	if kind == kPadAlign {
		if ni >= len(fmtStr) {
			s.LArgError(1, "invalid next option for option 'X'")
		}
		var sub packKind
		sub, align, ni = packOption(s, h, fmtStr, ni)
		if sub == kChar || align == 0 {
			s.LArgError(1, "invalid next option for option 'X'")
		}
	}
	if align <= 1 || kind == kChar {
		nAlign = 0
	} else {
		if align > h.maxAlign {
			align = h.maxAlign
		}
		if align&(align-1) != 0 {
			s.LArgError(1, "format asks for alignment not power of 2")
		}
		nAlign = (align - (totalSize & (align - 1))) & (align - 1)
	}
	return
}

func packIntBytes(out []byte, n uint64, little bool, size int, neg bool) {
	for i := 0; i < size; i++ {
		var idx int
		if little {
			idx = i
		} else {
			idx = size - 1 - i
		}
		out[idx] = byte(n & 0xff)
		n >>= 8
	}
	if neg && size > 8 {
		for i := 8; i < size; i++ {
			var idx int
			if little {
				idx = i
			} else {
				idx = size - 1 - i
			}
			out[idx] = 0xff
		}
	}
}

// unpackIntBytes reads `size` bytes starting at data[off] as either a
// signed or unsigned little/big-endian integer and returns it as int64.
// For sizes larger than 8 (the size of lua_Integer), it validates the
// high bytes: they must be all 0 for non-negative values or all 0xff
// for negative (sign-extended) signed values, otherwise the integer
// "does not fit" into lua_Integer. Mirrors lstrlib.cpp `unpackint`.
func unpackIntBytes(data string, off int, little bool, size int, signed bool) (int64, bool) {
	var u uint64
	limit := size
	if limit > 8 {
		limit = 8
	}
	for i := limit - 1; i >= 0; i-- {
		u <<= 8
		var idx int
		if little {
			idx = i
		} else {
			idx = size - 1 - i
		}
		u |= uint64(data[off+idx])
	}
	if size < 8 {
		if signed {
			mask := uint64(1) << uint(size*8-1)
			u = (u ^ mask) - mask
		}
	} else if size > 8 {
		// Validate high bytes match the sign-extension pattern.
		var mask byte
		if signed && int64(u) < 0 {
			mask = 0xff
		}
		for i := limit; i < size; i++ {
			var idx int
			if little {
				idx = i
			} else {
				idx = size - 1 - i
			}
			if data[off+idx] != mask {
				return 0, false
			}
		}
	}
	return int64(u), true
}

func strPack(s *vm.State) int {
	fmtStr := s.LCheckString(1)
	h := initPackHeader()
	arg := 1
	totalSize := 0
	var b strings.Builder
	i := 0
	for i < len(fmtStr) {
		kind, size, nAlign, ni := packDetails(s, &h, totalSize, fmtStr, i)
		i = ni
		totalSize += nAlign + size
		for j := 0; j < nAlign; j++ {
			b.WriteByte(0)
		}
		arg++
		switch kind {
		case kInt:
			if arg > s.Top() {
				s.LError("missing argument #%d", arg)
			}
			n := s.LCheckInteger(arg)
			if size < 8 {
				lim := int64(1) << uint(size*8-1)
				if n < -lim || n >= lim {
					s.LArgError(arg, "integer overflow")
				}
			}
			tmp := make([]byte, size)
			packIntBytes(tmp, uint64(n), h.little, size, n < 0)
			b.Write(tmp)
		case kUint:
			if arg > s.Top() {
				s.LError("missing argument #%d", arg)
			}
			n := s.LCheckInteger(arg)
			if size < 8 {
				if uint64(n) >= uint64(1)<<uint(size*8) {
					s.LArgError(arg, "unsigned overflow")
				}
			}
			tmp := make([]byte, size)
			packIntBytes(tmp, uint64(n), h.little, size, false)
			b.Write(tmp)
		case kFloat:
			if arg > s.Top() {
				s.LError("missing argument #%d", arg)
			}
			n := s.LCheckNumber(arg)
			tmp := make([]byte, size)
			if size == 4 {
				bits := math.Float32bits(float32(n))
				if h.little {
					binary.LittleEndian.PutUint32(tmp, bits)
				} else {
					binary.BigEndian.PutUint32(tmp, bits)
				}
			} else {
				bits := math.Float64bits(n)
				if h.little {
					binary.LittleEndian.PutUint64(tmp, bits)
				} else {
					binary.BigEndian.PutUint64(tmp, bits)
				}
			}
			b.Write(tmp)
		case kChar:
			if arg > s.Top() {
				s.LError("missing argument #%d", arg)
			}
			sv := s.LCheckString(arg)
			if len(sv) > size {
				s.LArgError(arg, "string longer than given size")
			}
			b.WriteString(sv)
			for j := len(sv); j < size; j++ {
				b.WriteByte(0)
			}
		case kString:
			if arg > s.Top() {
				s.LError("missing argument #%d", arg)
			}
			sv := s.LCheckString(arg)
			if size < 8 {
				if uint64(len(sv)) >= uint64(1)<<uint(size*8) {
					s.LArgError(arg, "string length does not fit in given size")
				}
			}
			tmp := make([]byte, size)
			packIntBytes(tmp, uint64(len(sv)), h.little, size, false)
			b.Write(tmp)
			b.WriteString(sv)
			totalSize += len(sv)
		case kZstr:
			if arg > s.Top() {
				s.LError("missing argument #%d", arg)
			}
			sv := s.LCheckString(arg)
			if strings.IndexByte(sv, 0) >= 0 {
				s.LArgError(arg, "string contains zeros")
			}
			b.WriteString(sv)
			b.WriteByte(0)
			totalSize += len(sv) + 1
		case kPadding:
			b.WriteByte(0)
			arg--
		case kPadAlign, kNop:
			arg--
		}
	}
	s.PushString(b.String())
	return 1
}

// packsizeMax mirrors upstream's MAXSIZE for pack results: results
// larger than 1GB must error with "format result too large". The
// conformance test packsize("c1073741824") == 2^30 must be reachable
// (i.e. the limit is INCLUSIVE of 2^30), but anything more must trip.
const packsizeMax = 1 << 30

func strPacksize(s *vm.State) int {
	fmtStr := s.LCheckString(1)
	h := initPackHeader()
	totalSize := 0
	i := 0
	for i < len(fmtStr) {
		kind, size, nAlign, ni := packDetails(s, &h, totalSize, fmtStr, i)
		i = ni
		if kind == kString || kind == kZstr {
			s.LArgError(1, "variable-length format")
		}
		// Detect overflow before accumulating, so we can report "too
		// large" exactly like upstream lstrlib's `luaL_argcheck(L,
		// size <= MAXSIZE - size_, ...)`. We compute the addition in
		// a wider domain (int64) to avoid signed-int wrap on 32-bit
		// arches; on 64-bit Go this is also fine because size and
		// nAlign are bounded by the format syntax (each <=2^31).
		add := int64(nAlign) + int64(size)
		if int64(totalSize)+add > int64(packsizeMax) {
			s.LArgError(1, "format result too large")
		}
		totalSize += int(add)
	}
	s.PushInteger(int64(totalSize))
	return 1
}

func strUnpack(s *vm.State) int {
	fmtStr := s.LCheckString(1)
	data := s.LCheckString(2)
	h := initPackHeader()
	pos := posRelat(int(s.LOptInteger(3, 1)), len(data)) - 1
	if pos < 0 {
		pos = 0
	}
	if pos > len(data) {
		s.LArgError(3, "initial position out of string")
	}
	i := 0
	n := 0
	for i < len(fmtStr) {
		kind, size, nAlign, ni := packDetails(s, &h, pos, fmtStr, i)
		i = ni
		if kind != kNop && kind != kPadAlign && kind != kZstr {
			if nAlign+size > len(data)-pos {
				s.LArgError(2, "data string too short")
			}
		}
		pos += nAlign
		switch kind {
		case kInt:
			v, ok := unpackIntBytes(data, pos, h.little, size, true)
			if !ok {
				s.LError("%d-byte integer does not fit into Lua Integer", size)
			}
			s.PushNumber(float64(v))
			n++
		case kUint:
			v, ok := unpackIntBytes(data, pos, h.little, size, false)
			if !ok {
				s.LError("%d-byte integer does not fit into Lua Integer", size)
			}
			s.PushNumber(float64(uint64(v)))
			n++
		case kFloat:
			tmp := []byte(data[pos : pos+size])
			if size == 4 {
				var bits uint32
				if h.little {
					bits = binary.LittleEndian.Uint32(tmp)
				} else {
					bits = binary.BigEndian.Uint32(tmp)
				}
				s.PushNumber(float64(math.Float32frombits(bits)))
			} else {
				var bits uint64
				if h.little {
					bits = binary.LittleEndian.Uint64(tmp)
				} else {
					bits = binary.BigEndian.Uint64(tmp)
				}
				s.PushNumber(math.Float64frombits(bits))
			}
			n++
		case kChar:
			s.PushString(data[pos : pos+size])
			n++
		case kString:
			ln, ok := unpackIntBytes(data, pos, h.little, size, false)
			if !ok {
				s.LError("%d-byte integer does not fit into Lua Integer", size)
			}
			length := int(uint64(ln))
			if length < 0 || pos+size+length > len(data) {
				s.LArgError(2, "data string too short")
			}
			s.PushString(data[pos+size : pos+size+length])
			pos += length
			n++
		case kZstr:
			if pos >= len(data) {
				s.LArgError(2, "unfinished string for format 'z'")
			}
			end := strings.IndexByte(data[pos:], 0)
			if end < 0 {
				s.LArgError(2, "unfinished string for format 'z'")
			}
			s.PushString(data[pos : pos+end])
			pos += end + 1
			n++
		case kPadAlign, kPadding, kNop:
			// no value pushed
		}
		pos += size
	}
	s.PushInteger(int64(pos + 1))
	return n + 1
}

// ----------------------------------------------------------------------
// ctype helpers (byte-oriented, ASCII-only)
// ----------------------------------------------------------------------

func isAlpha(c byte) bool      { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isAlnum(c byte) bool      { return isAlpha(c) || isDigit(c) }
func isLower(c byte) bool      { return c >= 'a' && c <= 'z' }
func isUpper(c byte) bool      { return c >= 'A' && c <= 'Z' }
func isLowerASCII(c byte) bool { return c >= 'a' && c <= 'z' }
func isSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}
func isCntrl(c byte) bool { return c < 0x20 || c == 0x7f }
func isPunct(c byte) bool {
	if c < 0x80 {
		r := rune(c)
		return unicode.IsPunct(r) || unicode.IsSymbol(r) ||
			(c >= '!' && c <= '/') || (c >= ':' && c <= '@') ||
			(c >= '[' && c <= '`') || (c >= '{' && c <= '~')
	}
	return false
}
func isGraph(c byte) bool { return c > 0x20 && c < 0x7f }
func isXDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
