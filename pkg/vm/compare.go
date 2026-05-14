// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import "strings"

// compare.go: equality and ordering with metamethod fallback. Mirrors
// upstream lvmutils.cpp luaV_equalval, luaV_lessthan, luaV_lessequal.

// equalVal implements `a == b` including __eq metamethod dispatch.
//
// Mirrors upstream luaV_equalval (lvmutils.cpp): for tables and
// userdata, if a shared `__eq` metamethod exists between the two
// operands (per get_compTM), the metamethod is consulted EVEN when
// a and b are the same reference. The raw-equality shortcut only
// applies for non-table / non-userdata tags, where no __eq applies.
// This matches the events.luau:366 test case
// (`assert(t ~= t)` with `__eq = print` returning a falsy value).
func (s *stateImpl) equalVal(a, b value) bool {
	if a.tag != b.tag {
		return false
	}
	switch a.tag {
	case TTable:
		ta := a.gc.(*table)
		tb := b.gc.(*table)
		mm, ok := s.getCompTM(ta.metatable, tb.metatable, TMEq)
		if !ok {
			return ta == tb
		}
		return s.callEqTM(mm, a, b)
	case TUserdata:
		ua := a.gc.(*userdata)
		ub := b.gc.(*userdata)
		mm, ok := s.getCompTM(ua.metatable, ub.metatable, TMEq)
		if !ok {
			return ua == ub
		}
		return s.callEqTM(mm, a, b)
	}
	return rawEqual(a, b)
}

// getCompTM returns the shared metamethod `event` between mt1 and
// mt2 if both have it and they match, mirroring upstream
// lvmutils.cpp get_compTM. The second result is false if no
// applicable metamethod can be used (caller falls back to raw eq).
func (s *stateImpl) getCompTM(mt1, mt2 *table, event TM) (value, bool) {
	if mt1 == nil {
		return value{}, false
	}
	tm1 := s.gs.getTagMethod(mt1, event)
	if tm1.tag == TNil {
		return value{}, false
	}
	if mt1 == mt2 {
		return tm1, true
	}
	if mt2 == nil {
		return value{}, false
	}
	tm2 := s.gs.getTagMethod(mt2, event)
	if tm2.tag == TNil {
		return value{}, false
	}
	if rawEqual(tm1, tm2) {
		return tm1, true
	}
	return value{}, false
}

func (s *stateImpl) callEqTM(mm value, a, b value) bool {
	base := metamethodBase(s, s.top)
	savedTop := s.top
	savedLen := len(s.stack)
	if base > s.top {
		if needLen := base + 3; needLen > len(s.stack) {
			s.reserve(needLen - s.top)
		}
		s.top = base
	}
	s.push(mm)
	s.push(a)
	s.push(b)
	s.callValue(base, 2, 1)
	r := s.stack[base]
	if savedLen > s.top {
		s.stack = s.stack[:savedLen]
	}
	s.top = savedTop
	return !r.isFalse()
}

// lessThan implements `a < b` with __lt fallback.
func (s *stateImpl) lessThanVal(a, b value) bool {
	if a.tag == TNumber && b.tag == TNumber {
		return a.num < b.num
	}
	if a.tag == TString && b.tag == TString {
		return strings.Compare(a.gc.(*tString).str(), b.gc.(*tString).str()) < 0
	}
	if a.tag != b.tag {
		s.runtimeError("attempt to compare " + a.tag.String() + " < " + b.tag.String())
	}
	if r, ok := s.callOrderTM(a, b, TMLt); ok {
		return r
	}
	s.runtimeError("attempt to compare two " + a.tag.String() + " values")
	return false
}

// lessEqualVal implements `a <= b` with __le and __lt fallback.
func (s *stateImpl) lessEqualVal(a, b value) bool {
	if a.tag == TNumber && b.tag == TNumber {
		return a.num <= b.num
	}
	if a.tag == TString && b.tag == TString {
		return strings.Compare(a.gc.(*tString).str(), b.gc.(*tString).str()) <= 0
	}
	if a.tag != b.tag {
		s.runtimeError("attempt to compare " + a.tag.String() + " <= " + b.tag.String())
	}
	if r, ok := s.callOrderTM(a, b, TMLe); ok {
		return r
	}
	// Fall back to `not (b < a)`.
	if r, ok := s.callOrderTM(b, a, TMLt); ok {
		return !r
	}
	s.runtimeError("attempt to compare two " + a.tag.String() + " values")
	return false
}

// callOrderTM attempts to call an ordering metamethod on (a, b).
// Mirrors upstream lvmutils.cpp call_orderTM: BOTH operands must
// expose the same metamethod (by rawEqual) for the order op to
// apply. Otherwise the caller raises an "attempt to compare" error.
// This matches events.luau:265 (`not pcall(function() return c < d end)`)
// where only `c` has a metatable with `__lt`.
func (s *stateImpl) callOrderTM(a, b value, tm TM) (bool, bool) {
	mm := s.gs.getTagMethodForValue(a, tm)
	if mm.tag == TNil {
		return false, false
	}
	mm2 := s.gs.getTagMethodForValue(b, tm)
	if mm2.tag == TNil || !rawEqual(mm, mm2) {
		return false, false
	}
	base := metamethodBase(s, s.top)
	savedTop := s.top
	savedLen := len(s.stack)
	if base > s.top {
		if needLen := base + 3; needLen > len(s.stack) {
			s.reserve(needLen - s.top)
		}
		s.top = base
	}
	s.push(mm)
	s.push(a)
	s.push(b)
	s.callValue(base, 2, 1)
	r := s.stack[base]
	if savedLen > s.top {
		s.stack = s.stack[:savedLen]
	}
	s.top = savedTop
	return !r.isFalse(), true
}
