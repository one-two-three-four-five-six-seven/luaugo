// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"fmt"
	"math"
	"strings"
)

// arith.go: arithmetic, length, concat, and unary helpers that may
// invoke metamethods. Mirrors upstream lvmutils.cpp luaV_doarithimpl,
// luaV_dolen, luaV_concat.

// doArith performs the arithmetic operation identified by tm on b and
// c and stores the result into dst. Returns true if the operation was
// completed; on failure it raises a Lua error via panic.
func (s *stateImpl) doArith(tm TM, b, c value) value {
	// Fast path: both numbers.
	if b.tag == TNumber && c.tag == TNumber {
		return numberValue(arithNumberOp(tm, b.num, c.num))
	}
	// Vector arithmetic for +, -, *, /, %, unm, idiv (component-wise).
	if (b.tag == TVector || c.tag == TVector) && tm >= TMAdd && tm <= TMUnm {
		if r, ok := vectorArith(tm, b, c); ok {
			return r
		}
	}
	// String coercion for numeric ops: try to coerce both operands.
	if bn, ok := b.asNumber(); ok {
		if cn, ok2 := c.asNumber(); ok2 {
			return numberValue(arithNumberOp(tm, bn, cn))
		}
	}
	// Metamethod fallback.
	if v, ok := s.callBinTM(b, c, tm); ok {
		return v
	}
	s.arithError(tm, b, c)
	return nilValue()
}

func arithNumberOp(tm TM, a, b float64) float64 {
	switch tm {
	case TMAdd:
		return a + b
	case TMSub:
		return a - b
	case TMMul:
		return a * b
	case TMDiv:
		return a / b
	case TMMod:
		return luaMod(a, b)
	case TMPow:
		return math.Pow(a, b)
	case TMIDiv:
		return luaIDiv(a, b)
	case TMUnm:
		return -a
	}
	return 0
}

// luaMod implements Lua's `%` (matches a - floor(a/b)*b).
func luaMod(a, b float64) float64 {
	if b == 0 {
		return math.NaN()
	}
	m := math.Mod(a, b)
	if m != 0 && ((m < 0) != (b < 0)) {
		m += b
	}
	return m
}

// luaIDiv implements Lua's `//` (floor division).
func luaIDiv(a, b float64) float64 {
	if b == 0 {
		// 0//0 = NaN; n//0 = +/-inf per Lua.
		if a == 0 {
			return math.NaN()
		}
		if (a > 0) == (b >= 0) {
			return math.Inf(1)
		}
		return math.Inf(-1)
	}
	return math.Floor(a / b)
}

// vectorArith handles vector op number / number op vector / vector op vector.
func vectorArith(tm TM, b, c value) (value, bool) {
	var av, bv Vector
	if b.tag == TVector {
		av = vectorFromValue(b)
	} else if n, ok := b.asNumber(); ok {
		x := float32(n)
		av = Vector{x, x, x, x}
	} else {
		return nilValue(), false
	}
	if c.tag == TVector {
		bv = vectorFromValue(c)
	} else if tm != TMUnm {
		if n, ok := c.asNumber(); ok {
			x := float32(n)
			bv = Vector{x, x, x, x}
		} else {
			return nilValue(), false
		}
	}
	switch tm {
	case TMAdd:
		return valueFromVector(av.Add(bv)), true
	case TMSub:
		return valueFromVector(av.Sub(bv)), true
	case TMMul:
		return valueFromVector(av.Mul(bv)), true
	case TMDiv:
		return valueFromVector(av.Div(bv)), true
	case TMMod:
		return valueFromVector(Vector{
			X: float32(luaMod(float64(av.X), float64(bv.X))),
			Y: float32(luaMod(float64(av.Y), float64(bv.Y))),
			Z: float32(luaMod(float64(av.Z), float64(bv.Z))),
			W: float32(luaMod(float64(av.W), float64(bv.W))),
		}), true
	case TMIDiv:
		return valueFromVector(Vector{
			X: float32(luaIDiv(float64(av.X), float64(bv.X))),
			Y: float32(luaIDiv(float64(av.Y), float64(bv.Y))),
			Z: float32(luaIDiv(float64(av.Z), float64(bv.Z))),
			W: float32(luaIDiv(float64(av.W), float64(bv.W))),
		}), true
	case TMPow:
		return valueFromVector(Vector{
			X: float32(math.Pow(float64(av.X), float64(bv.X))),
			Y: float32(math.Pow(float64(av.Y), float64(bv.Y))),
			Z: float32(math.Pow(float64(av.Z), float64(bv.Z))),
			W: float32(math.Pow(float64(av.W), float64(bv.W))),
		}), true
	case TMUnm:
		return valueFromVector(av.Neg()), true
	}
	return nilValue(), false
}

// callBinTM tries to call a binary metamethod for (a, b) under tm.
// Returns (result, true) on success, (zero, false) if no metamethod
// could be located on either operand. Any Lua error raised by the
// metamethod propagates as a panic.
func (s *stateImpl) callBinTM(a, b value, tm TM) (value, bool) {
	mm := s.gs.getTagMethodForValue(a, tm)
	if mm.tag == TNil {
		mm = s.gs.getTagMethodForValue(b, tm)
	}
	if mm.tag == TNil {
		return nilValue(), false
	}
	// Call mm(a, b) and capture the first return value. We allocate
	// the metamethod's argument area ABOVE the caller's used
	// registers (base+MaxStackSize) so it can't overlap any live
	// slots, and we restore both L.top and the slice length on exit.
	resBase := metamethodBase(s, s.top)
	savedTop := s.top
	savedLen := len(s.stack)
	if resBase > s.top {
		if needLen := resBase + 3; needLen > len(s.stack) {
			s.reserve(needLen - s.top)
		}
		s.top = resBase
	}
	s.push(mm)
	s.push(a)
	s.push(b)
	s.callValue(resBase, 2, 1)
	r := s.stack[resBase]
	if savedLen > s.top {
		s.stack = s.stack[:savedLen]
	}
	s.top = savedTop
	return r, true
}

// metamethodBase returns the lowest stack index at which a metamethod
// call may push its arguments without aliasing the caller's live
// registers. Mirrors the upstream invariant that __index/__newindex
// (and arith metamethods) allocate above ci->top.
func metamethodBase(s *stateImpl, fallback int) int {
	if ci := s.currentFrame(); ci != nil && ci.cl != nil && ci.cl.proto != nil {
		if hi := ci.base + int(ci.cl.proto.MaxStackSize); hi > fallback {
			return hi
		}
	}
	return fallback
}

// callUnaryTM is the unary version (for TMUnm, TMLen).
//
// Mirrors upstream luaV_dolen / luaV_doarith for TM_UNM: the
// metamethod is called as mm(value, nil). The second argument is the
// upstream "luaO_nilobject" placeholder; user-defined __len /__unm
// functions ignore it. Passing the value twice (as we used to) made
// callers like `__len = error` see error as `error(value, value)`
// where the second value is interpreted as the level argument and
// errored with "invalid argument #2 (integer expected, got table)"
// before the error could surface the intended payload
// (events.luau:403).
func (s *stateImpl) callUnaryTM(a value, tm TM) (value, bool) {
	mm := s.gs.getTagMethodForValue(a, tm)
	if mm.tag == TNil {
		return nilValue(), false
	}
	resBase := metamethodBase(s, s.top)
	savedTop := s.top
	savedLen := len(s.stack)
	if resBase > s.top {
		if needLen := resBase + 3; needLen > len(s.stack) {
			s.reserve(needLen - s.top)
		}
		s.top = resBase
	}
	s.push(mm)
	s.push(a)
	s.push(nilValue())
	s.callValue(resBase, 2, 1)
	r := s.stack[resBase]
	if savedLen > s.top {
		s.stack = s.stack[:savedLen]
	}
	s.top = savedTop
	return r, true
}

// arithError raises a Lua runtime error for a failed arithmetic op.
func (s *stateImpl) arithError(tm TM, b, c value) {
	op := ""
	switch tm {
	case TMAdd:
		op = "add"
	case TMSub:
		op = "sub"
	case TMMul:
		op = "mul"
	case TMDiv:
		op = "div"
	case TMMod:
		op = "mod"
	case TMPow:
		op = "pow"
	case TMIDiv:
		op = "idiv"
	case TMUnm:
		op = "unm"
	}
	// Unary (unm) only has one operand; report its type.
	if tm == TMUnm {
		s.runtimeError("attempt to perform arithmetic (" + op + ") on " + b.tag.String() + " value")
		return
	}
	// Binary ops: upstream's luaG_aritherror lists both operand types
	// when they differ, only one when they match
	// (ldebug.cpp:271-274). conformance/errors.luau:379-382 hard-
	// codes these exact shapes.
	t1 := b.tag.String()
	t2 := c.tag.String()
	if t1 == t2 {
		s.runtimeError("attempt to perform arithmetic (" + op + ") on " + t1)
	} else {
		s.runtimeError("attempt to perform arithmetic (" + op + ") on " + t1 + " and " + t2)
	}
}

// doLen implements `#v`. Calls __len for tables/userdata when present.
// Validates that __len returns a number, matching upstream luaV_dolen.
func (s *stateImpl) doLen(v value) value {
	switch v.tag {
	case TString:
		return numberValue(float64(v.gc.(*tString).len()))
	case TBuffer:
		return numberValue(float64(v.gc.(*buffer).Len()))
	case TTable:
		// Per Lua: __len applies only if metatable says so. Without
		// metatable, raw length is returned. Luau follows the same rule.
		t := v.gc.(*table)
		if t.metatable != nil {
			if r, ok := s.callUnaryTM(v, TMLen); ok {
				if r.tag != TNumber {
					s.runtimeError("'__len' must return a number")
				}
				return r
			}
		}
		return numberValue(float64(t.rawLen()))
	}
	if r, ok := s.callUnaryTM(v, TMLen); ok {
		if r.tag != TNumber {
			s.runtimeError("'__len' must return a number")
		}
		return r
	}
	s.runtimeError("attempt to get length of a " + v.tag.String() + " value")
	return nilValue()
}

// doConcat concatenates the n top-of-stack values into one string,
// leaving the result at base. Mirrors luaV_concat semantics with
// __concat fallback.
//
// Algorithm matches upstream luaV_concat (lvmutils.cpp): operates
// right-to-left. Each pass either collapses the rightmost pair via
// __concat (shrinking total by 1) or collects a maximal contiguous
// run of stringifiable values into one joined string. The
// right-to-left order is essential so a non-string __concat result
// (e.g. a table whose __concat returns another table) is paired
// with the NEXT operand to its left on the next iteration, rather
// than re-paired with the same left operand in an infinite loop
// (events.luau:253 -- the originally-timing-out site).
func (s *stateImpl) doConcat(base, n int) {
	if n < 2 {
		return
	}
	total := n
	last := n - 1
	for total > 1 {
		topMinus1 := s.stack[base+last]
		topMinus2 := s.stack[base+last-1]
		_, lOK := topMinus2.asString()
		_, rOK := topMinus1.asString()
		if !lOK || !rOK {
			r, ok := s.callBinTM(topMinus2, topMinus1, TMConcat)
			if !ok {
				// Luau formats concat type errors as
				//   "attempt to concatenate <lhs> with <rhs>"
				// naming BOTH operands' types in source order, not
				// just the offending one. This is consistent across
				// lhs-bad, rhs-bad, and both-bad cases (verified
				// against upstream via internal/upstreamvm).
				// basic.luau:123 asserts on the substring
				// "attempt to concatenate nil with string" so this
				// formatting must be preserved verbatim.
				s.runtimeError("attempt to concatenate " + topMinus2.tag.String() + " with " + topMinus1.tag.String())
			}
			s.stack[base+last-1] = r
			s.stack[base+last] = nilValue()
			last--
			total--
			continue
		}
		// Collect a maximal run of stringifiable values ending at `last`.
		runLen := 2
		ls, _ := topMinus2.asString()
		rs, _ := topMinus1.asString()
		parts := []string{ls, rs}
		for runLen < total {
			v := s.stack[base+last-runLen]
			str, ok := v.asString()
			if !ok {
				break
			}
			parts = append([]string{str}, parts...)
			runLen++
		}
		joined := strings.Join(parts, "")
		ts := s.gs.intern(joined)
		s.stack[base+last-runLen+1] = stringValue(ts)
		for j := base + last - runLen + 2; j <= base+last; j++ {
			s.stack[j] = nilValue()
		}
		last -= runLen - 1
		total -= runLen - 1
	}
	if last != 0 {
		s.stack[base] = s.stack[base+last]
	}
	for j := base + 1; j < base+n; j++ {
		s.stack[j] = nilValue()
	}
}

// runtimeError raises a Lua runtime error with the given message.
// Carries Go panic semantics; pcall recovers it.
//
// Mirrors upstream ldebug.cpp pusherror: when the innermost call
// frame is a Lua frame the message is prefixed with
// "chunkname:line: "; otherwise the message is left bare. This
// matches upstream behaviour where errors raised from C functions
// (Go in our port) produce unprefixed messages, so fixtures like
// events.luau:22 that assert on the exact error string pass.
func (s *stateImpl) runtimeError(msg string) {
	msg = s.addErrorWhere(msg)
	panic(luaRTError{msg: msg, value: stringValue(s.gs.intern(msg))})
}

// addErrorWhere prepends "chunkname:line: " to msg using ONLY the
// innermost call frame, mirroring upstream pusherror semantics.
// Returns msg unchanged when the top frame is a Go closure or
// when no debug info is available. Avoids duplicating an
// already-present prefix (e.g. produced by LError on the same call
// site).
func (s *stateImpl) addErrorWhere(msg string) string {
	if len(s.frames) == 0 {
		return msg
	}
	ci := s.frames[len(s.frames)-1]
	if ci == nil || ci.cl == nil || ci.cl.isGo || ci.cl.proto == nil {
		return msg
	}
	p := ci.cl.proto
	line := lineForPC(p, ci.savedpc-1)
	if line <= 0 {
		return msg
	}
	name := chunkNameForProto(s.gs, p)
	if name == "" {
		name = "?"
	}
	prefix := fmt.Sprintf("%s:%d: ", name, line)
	if strings.HasPrefix(msg, prefix) {
		return msg
	}
	return prefix + msg
}

// runtimeErrorValue raises a Lua error with the given Lua value.
func (s *stateImpl) runtimeErrorValue(v value) {
	if v.tag == TString {
		panic(luaRTError{msg: v.gc.(*tString).str(), value: v})
	}
	panic(luaRTError{msg: "(error object is not a string)", value: v})
}

// luaRTError is the sentinel error value that Lua-level errors travel
// through Go's panic/recover machinery as. It is recovered at every
// pcall and Resume boundary.
type luaRTError struct {
	msg   string
	value value
}

func (e luaRTError) Error() string { return e.msg }
func (e luaRTError) LuaValue() any {
	switch e.value.tag {
	case TString:
		return e.value.gc.(*tString).str()
	case TNumber:
		return e.value.num
	case TBoolean:
		return e.value.bool_
	case TNil:
		return nil
	}
	return e.msg
}
