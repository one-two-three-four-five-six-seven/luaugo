// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import "strings"

// compare.go: equality and ordering with metamethod fallback. Mirrors
// upstream lvmutils.cpp luaV_equalval, luaV_lessthan, luaV_lessequal.

// equalVal implements `a == b` including __eq metamethod dispatch.
func (s *stateImpl) equalVal(a, b value) bool {
	// Cross-type: number vs integer would unify, but we only store TNumber.
	if a.tag != b.tag {
		return false
	}
	if rawEqual(a, b) {
		return true
	}
	// __eq applies only for two tables or two userdata with the same
	// metatable (upstream semantics).
	switch a.tag {
	case TTable:
		ta := a.gc.(*table)
		tb := b.gc.(*table)
		var mm value
		if ta.metatable != nil {
			mm = s.gs.getTagMethod(ta.metatable, TMEq)
		}
		if mm.tag == TNil && tb.metatable != nil {
			mm = s.gs.getTagMethod(tb.metatable, TMEq)
		}
		if mm.tag == TNil {
			return false
		}
		return s.callEqTM(mm, a, b)
	case TUserdata:
		ua := a.gc.(*userdata)
		ub := b.gc.(*userdata)
		var mm value
		if ua.metatable != nil {
			mm = s.gs.getTagMethod(ua.metatable, TMEq)
		}
		if mm.tag == TNil && ub.metatable != nil {
			mm = s.gs.getTagMethod(ub.metatable, TMEq)
		}
		if mm.tag == TNil {
			return false
		}
		return s.callEqTM(mm, a, b)
	}
	return false
}

func (s *stateImpl) callEqTM(mm value, a, b value) bool {
	base := s.top
	s.push(mm)
	s.push(a)
	s.push(b)
	s.callValue(base, 2, 1)
	r := s.stack[base]
	s.stack = s.stack[:base]
	s.top = base
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
	// Coerce strings to numbers when one side is a number.
	if a.tag != b.tag {
		s.runtimeError("attempt to compare " + a.tag.String() + " with " + b.tag.String())
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
		s.runtimeError("attempt to compare " + a.tag.String() + " with " + b.tag.String())
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
func (s *stateImpl) callOrderTM(a, b value, tm TM) (bool, bool) {
	mm := s.gs.getTagMethodForValue(a, tm)
	if mm.tag == TNil {
		mm = s.gs.getTagMethodForValue(b, tm)
	}
	if mm.tag == TNil {
		return false, false
	}
	base := s.top
	s.push(mm)
	s.push(a)
	s.push(b)
	s.callValue(base, 2, 1)
	r := s.stack[base]
	s.stack = s.stack[:base]
	s.top = base
	return !r.isFalse(), true
}
