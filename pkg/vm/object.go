// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"math"
	"strconv"
	"strings"
)

// value is the in-memory representation of a Lua value. It mirrors
// upstream's TValue (lobject.h): a discriminated union with a tag byte.
//
// We intentionally use a single concrete struct rather than a Go
// interface so values can live in slices (the stack and table array
// part) without an allocation per slot, matching upstream's "TValue
// stack[]" layout.
//
// Numbers are always stored as float64 (upstream's `n` field). The
// TInteger tag is reserved per upstream lua.h for the v8+ explicit
// integer feature; in v0.720 no value of that type is ever stored on
// the stack, so PushInteger stores its argument as float64 with tag
// TNumber. See contract notes.
type value struct {
	tag   Type
	bool_ bool       // valid when tag == TBoolean
	num   float64    // valid when tag == TNumber (or TInteger; unused)
	vec   [4]float32 // valid when tag == TVector
	gc    gcObject   // valid when tag.IsGC()
	ptr   any        // valid when tag == TLightUserdata (carries the Go pointer)
	ltag  int        // valid when tag == TLightUserdata (the user tag)
}

// nilValue returns a fresh nil-tagged value.
func nilValue() value { return value{tag: TNil} }

// booleanValue boxes a bool.
func booleanValue(b bool) value { return value{tag: TBoolean, bool_: b} }

// numberValue boxes a float64 number.
func numberValue(n float64) value { return value{tag: TNumber, num: n} }

// stringValue boxes an interned tString*.
func stringValue(ts *tString) value { return value{tag: TString, gc: ts} }

// tableValue boxes a *table.
func tableValue(t *table) value { return value{tag: TTable, gc: t} }

// vectorValue boxes the components of a vector. w is zero unless the
// VM is configured for 4-wide vectors.
func vectorValue(x, y, z, w float32) value {
	v := value{tag: TVector}
	v.vec[0] = x
	v.vec[1] = y
	v.vec[2] = z
	v.vec[3] = w
	return v
}

// closureValue boxes a closure.
func closureValue(c *closure) value { return value{tag: TFunction, gc: c} }

// userdataValue boxes a userdata.
func userdataValue(u *userdata) value { return value{tag: TUserdata, gc: u} }

// bufferValue boxes a buffer.
func bufferValue(b *buffer) value { return value{tag: TBuffer, gc: b} }

// threadValue boxes a thread (lua_State equivalent).
func threadValue(s *stateImpl) value { return value{tag: TThread, gc: s} }

// isCollectable reports whether v carries a GC pointer. We must
// include TString here (upstream's `iscollectable` macro is `tt >=
// LUA_TSTRING`), even though Type.IsGC() in the locked contract
// reports false for TString — that public predicate exists to drive
// API-level type queries, not internal GC bookkeeping.
func (v value) isCollectable() bool { return v.tag >= TString }

// isFalse mirrors upstream l_isfalse: nil and false are falsy; all
// other values, including 0 and "", are truthy.
func (v value) isFalse() bool {
	switch v.tag {
	case TNil:
		return true
	case TBoolean:
		return !v.bool_
	}
	return false
}

// asString returns the textual representation of v together with a flag
// indicating whether v is, or is coercible to, a string. This mirrors
// upstream `lua_tostring` for the basic cases that don't go through
// __tostring (which is handled by the interpreter).
func (v value) asString() (string, bool) {
	switch v.tag {
	case TString:
		return v.gc.(*tString).str(), true
	case TNumber:
		return formatNumber(v.num), true
	}
	return "", false
}

// asNumber returns v as a float64, coercing numeric strings.
//
// String coercion follows Luau's number lexer (luaO_str2d): decimal
// literals, plus `0x`/`0X` hexadecimal and `0b`/`0B` binary integer
// literals, with optional leading sign and surrounding ASCII space.
// This matches lua_tonumber on the upstream VM, which is what
// luaL_checknumber (and luaL_checkunsigned, used by bit32) consult.
func (v value) asNumber() (float64, bool) {
	switch v.tag {
	case TNumber:
		return v.num, true
	case TString:
		return strToNumber(v.gc.(*tString).str())
	}
	return 0, false
}

// strToNumber parses s into a float64 using Luau's number-lexer
// semantics. Returns false if the entire trimmed string is not a
// well-formed Luau numeric literal. Mirrors upstream luaO_str2d /
// the lexer paths for NUMBER_HEX and NUMBER_BINARY.
func strToNumber(in string) (float64, bool) {
	s := trimASCIISpace(in)
	if s == "" {
		return 0, false
	}
	sign := 1.0
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		sign = -1
		s = s[1:]
	}
	if s == "" {
		return 0, false
	}
	// Hex literal: `0x...` / `0X...`. strconv.ParseFloat does not
	// accept hex without an exponent, so do the integer parse ourselves
	// (Luau's number lexer treats `0x` as an unsigned integer literal).
	if len(s) > 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		n, err := strconv.ParseUint(s[2:], 16, 64)
		if err != nil {
			return 0, false
		}
		return sign * float64(n), true
	}
	// Binary literal: `0b...` / `0B...`. Luau-specific extension.
	if len(s) > 2 && s[0] == '0' && (s[1] == 'b' || s[1] == 'B') {
		n, err := strconv.ParseUint(s[2:], 2, 64)
		if err != nil {
			return 0, false
		}
		return sign * float64(n), true
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return sign * n, true
}

// asInteger returns v as an int64. Floats are accepted iff they
// represent an exact integer value (matching upstream luai_num2int).
func (v value) asInteger() (int64, bool) {
	n, ok := v.asNumber()
	if !ok {
		return 0, false
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	i := int64(n)
	if float64(i) != n {
		return 0, false
	}
	return i, true
}

// rawEqual implements `o1 == o2` without metamethods (upstream
// luaO_rawequalObj).
func rawEqual(a, b value) bool {
	if a.tag != b.tag {
		// Lua's normal equality says number==number even across the
		// TNumber/TInteger tag boundary, but in v0.720 only TNumber is
		// ever stored, so a simple tag comparison suffices.
		return false
	}
	switch a.tag {
	case TNil:
		return true
	case TBoolean:
		return a.bool_ == b.bool_
	case TNumber:
		// NaN != NaN per IEEE 754; Lua follows the same rule.
		return a.num == b.num
	case TVector:
		// Compare only the active components (W is junk in 3-wide
		// builds). Mirrors upstream luaO_rawequalObj's vector path,
		// which dispatches on LUA_VECTOR_SIZE. Bitwise equality (with
		// IEEE NaN != NaN preserved) is intentional: a NaN W slot
		// from a division-by-zero leak must not poison the result.
		if VectorComponents == 4 {
			return a.vec == b.vec
		}
		return a.vec[0] == b.vec[0] && a.vec[1] == b.vec[1] && a.vec[2] == b.vec[2]
	case TLightUserdata:
		return a.ptr == b.ptr && a.ltag == b.ltag
	}
	// All other types: identity comparison on the GC pointer. String
	// equality reduces to pointer equality because the intern table
	// guarantees one *tString per byte sequence.
	if a.isCollectable() {
		return a.gc == b.gc
	}
	return false
}

// formatNumber mirrors upstream luai_num2str (VM/src/lnumprint.cpp).
// Luau uses Schubfach to produce the shortest round-trippable decimal,
// then chooses between fixed-point and scientific notation based on
// the position of the decimal point:
//   - fixed-point when the decimal exponent `dot` is in [-5, 21];
//   - scientific notation otherwise (e+NN with 2+ digit exponent).
//
// We obtain the shortest-significant digits from strconv.AppendFloat
// with 'e' format and precision -1 (Go's shortest round-trip), then
// reformat to match Luau's exact spelling.
func formatNumber(n float64) string {
	if math.IsNaN(n) {
		return "nan"
	}
	if math.IsInf(n, 1) {
		return "inf"
	}
	if math.IsInf(n, -1) {
		return "-inf"
	}
	// AppendFloat with 'e' / -1 yields "X.YYYe+NN" or "X.YYYe-NN"
	// (or "Xe+NN" for single-digit mantissas, including "0e+00" and
	// "-0e+00" for signed zero).
	buf := strconv.AppendFloat(nil, n, 'e', -1, 64)
	s := string(buf)

	sign := ""
	if len(s) > 0 && s[0] == '-' {
		sign = "-"
		s = s[1:]
	}

	eIdx := strings.IndexByte(s, 'e')
	if eIdx < 0 {
		// Shouldn't happen for the 'e' format; defensive fallback.
		return sign + s
	}
	mantissa := s[:eIdx]
	exp, err := strconv.Atoi(s[eIdx+1:])
	if err != nil {
		return sign + s
	}

	var digits string
	var dot int // 1-based position of the decimal point relative to digits
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		digits = mantissa[:i] + mantissa[i+1:]
		dot = i + exp
	} else {
		digits = mantissa
		dot = len(mantissa) + exp
	}

	// Zero (including -0): print "0" or "-0", matching upstream's
	// printspecial / zero path before Schubfach (sign byte preserved).
	if digits == "0" {
		return sign + "0"
	}

	declen := len(digits)

	if dot >= -5 && dot <= 21 {
		// fixed-point
		switch {
		case dot <= 0:
			// "0." + zeros + digits, then trim trailing zeros
			var b strings.Builder
			b.WriteString(sign)
			b.WriteString("0.")
			for i := 0; i < -dot; i++ {
				b.WriteByte('0')
			}
			b.WriteString(digits)
			return trimTrailingZerosAfterDot(b.String())
		case dot >= declen:
			// digits then zero-padding (integer form, no dot)
			var b strings.Builder
			b.WriteString(sign)
			b.WriteString(digits)
			for i := 0; i < dot-declen; i++ {
				b.WriteByte('0')
			}
			return b.String()
		default:
			// dot in the middle of digits
			out := sign + digits[:dot] + "." + digits[dot:]
			return trimTrailingZerosAfterDot(out)
		}
	}

	// scientific: "X.YYYYe+NN" with at least 2-digit exponent
	var b strings.Builder
	b.WriteString(sign)
	b.WriteByte(digits[0])
	if declen > 1 {
		b.WriteByte('.')
		b.WriteString(digits[1:])
	}
	mantStr := trimTrailingZerosAfterDot(b.String())
	expSign := "+"
	e := dot - 1
	if e < 0 {
		expSign = "-"
		e = -e
	}
	// At least two exponent digits, like upstream printexp.
	return mantStr + "e" + expSign + leftPad2(e)
}

// trimTrailingZerosAfterDot strips trailing zeros (and a dangling dot)
// from a decimal string. Has no effect on strings without a '.'.
func trimTrailingZerosAfterDot(s string) string {
	if !strings.ContainsRune(s, '.') {
		return s
	}
	for strings.HasSuffix(s, "0") {
		s = s[:len(s)-1]
	}
	if strings.HasSuffix(s, ".") {
		s = s[:len(s)-1]
	}
	return s
}

// leftPad2 formats a non-negative exponent with at least two digits.
func leftPad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func trimASCIISpace(s string) string {
	lo, hi := 0, len(s)
	for lo < hi && isASCIISpace(s[lo]) {
		lo++
	}
	for hi > lo && isASCIISpace(s[hi-1]) {
		hi--
	}
	return s[lo:hi]
}

func isASCIISpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}
