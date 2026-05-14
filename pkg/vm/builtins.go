// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"math"

	"github.com/luaugo/luaugo/internal/common"
)

// builtins.go: fast-path implementations of FASTCALL builtins.
//
// This mirrors upstream VM/src/lbuiltins.cpp: each entry takes the
// argument values gathered by the FASTCALL dispatcher and writes
// results directly to the destination register(s), bypassing the
// regular CALL path. A negative return value (-1) signals "could not
// handle this combination of types/arities" and the dispatcher falls
// through to the slow CALL path.
//
// Invariants enforced by upstream (and matched here):
//   - Builtins must not call user code, yield, fail, or reallocate the
//     stack. They are pure transformations of registers.
//   - On success the builtin returns the number of result values it
//     wrote at `res`. The dispatcher truncates / pads to `nresults`.
//   - When `nresults == LUA_MULTRET (-1)`, the builtin still returns
//     an exact count so the dispatcher can update L.top.
//
// FASTCALL semantics (lvmexecute.cpp VM_CASE(LOP_FASTCALL*)):
//   - FASTCALL/FASTCALL3 read args from R(A_call)+1 .. R(A_call)+B_call-1.
//   - FASTCALL1 has 1 arg explicitly in B(insn).
//   - FASTCALL2 has 2 args in B(insn) and AUX (register).
//   - FASTCALL2K has 1 register arg in B and 1 constant arg from AUX.
//   - FASTCALL3 has 3 register args in B, AUX_A, AUX_B.
//
// On success the dispatcher advances PC past the following CALL via
// the C field of the FASTCALL instruction.

// builtinFn is the Go-side mirror of upstream luau_FastFunction.
//
// Arguments (matching upstream signature semantically):
//   - L:        thread state
//   - res:      stack index where result(s) must be written
//   - arg0:     value of the first argument (already loaded)
//   - args:     slice of additional argument values (len == nparams-1
//               when nparams >= 1, else 0)
//   - nresults: number of expected results, or MultRet (-1) for vararg
//   - nparams:  actual number of arguments passed
//
// Returns -1 if the builtin couldn't handle the call (fall back), or
// the number of results written at `res` otherwise.
type builtinFn func(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int

// builtinTable is the dispatch array, indexed by Builtin id.
// Entries that aren't implemented as a fast path are nil (or return
// -1) so callers fall back to the slow CALL path.
var builtinTable [common.BuiltinBufferWriteInt + 1]builtinFn

func init() {
	// Priority order from the C1 brief: these are TRUE fast paths.
	builtinTable[common.BuiltinAssert] = builtinAssert

	builtinTable[common.BuiltinType] = builtinType
	builtinTable[common.BuiltinTypeof] = builtinTypeof

	builtinTable[common.BuiltinMathAbs] = builtinMathAbs
	builtinTable[common.BuiltinMathFloor] = builtinMathFloor
	builtinTable[common.BuiltinMathCeil] = builtinMathCeil
	builtinTable[common.BuiltinMathSqrt] = builtinMathSqrt
	builtinTable[common.BuiltinMathMin] = builtinMathMin
	builtinTable[common.BuiltinMathMax] = builtinMathMax
	builtinTable[common.BuiltinMathClamp] = builtinMathClamp
	builtinTable[common.BuiltinMathSign] = builtinMathSign
	builtinTable[common.BuiltinMathRound] = builtinMathRound
	builtinTable[common.BuiltinMathPow] = builtinMathPow

	builtinTable[common.BuiltinBit32Band] = builtinBit32Band
	builtinTable[common.BuiltinBit32Bor] = builtinBit32Bor
	builtinTable[common.BuiltinBit32Bxor] = builtinBit32Bxor
	builtinTable[common.BuiltinBit32Bnot] = builtinBit32Bnot
	builtinTable[common.BuiltinBit32LShift] = builtinBit32LShift
	builtinTable[common.BuiltinBit32RShift] = builtinBit32RShift
	builtinTable[common.BuiltinBit32ArShift] = builtinBit32ArShift

	builtinTable[common.BuiltinStringLen] = builtinStringLen
	builtinTable[common.BuiltinStringSub] = builtinStringSub
	builtinTable[common.BuiltinStringByte] = builtinStringByte
	builtinTable[common.BuiltinStringChar] = builtinStringChar

	builtinTable[common.BuiltinRawGet] = builtinRawGet
	builtinTable[common.BuiltinRawSet] = builtinRawSet
	builtinTable[common.BuiltinRawEqual] = builtinRawEqual
	builtinTable[common.BuiltinRawLen] = builtinRawLen

	builtinTable[common.BuiltinTableInsert] = builtinTableInsert
	builtinTable[common.BuiltinTableUnpack] = builtinTableUnpack

	builtinTable[common.BuiltinSelectVararg] = builtinSelectVararg

	builtinTable[common.BuiltinGetMetatable] = builtinGetMetatable
	builtinTable[common.BuiltinSetMetatable] = builtinSetMetatable

	builtinTable[common.BuiltinToNumber] = builtinToNumber
	builtinTable[common.BuiltinToString] = builtinToString
}

// ----------------------------------------------------------------------
// FASTCALL dispatch helpers (used by execute.go)
// ----------------------------------------------------------------------

// dispatchFastcall looks up the builtin and invokes it. Returns
// (nresultsWritten, true) on success, or (_, false) when the call
// must fall back to the slow path.
//
// nresults follows upstream convention: MultRet (-1) means "as many
// as the builtin returns".
func dispatchFastcall(L *stateImpl, id common.Builtin, res int, arg0 value, args []value, nresults, nparams int) (int, bool) {
	if int(id) >= len(builtinTable) {
		return 0, false
	}
	fn := builtinTable[id]
	if fn == nil {
		return 0, false
	}
	// Ensure result slot exists.
	if res >= len(L.stack) {
		L.reserve(res + 1 - L.top)
	}
	n := fn(L, res, arg0, args, nresults, nparams)
	if n < 0 {
		return 0, false
	}
	return n, true
}

// ----------------------------------------------------------------------
// math.*
// ----------------------------------------------------------------------

func builtinMathAbs(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 1 && nresults <= 1 && arg0.tag == TNumber {
		L.stack[res] = numberValue(math.Abs(arg0.num))
		return 1
	}
	return -1
}

func builtinMathFloor(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 1 && nresults <= 1 && arg0.tag == TNumber {
		L.stack[res] = numberValue(math.Floor(arg0.num))
		return 1
	}
	return -1
}

func builtinMathCeil(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 1 && nresults <= 1 && arg0.tag == TNumber {
		L.stack[res] = numberValue(math.Ceil(arg0.num))
		return 1
	}
	return -1
}

func builtinMathSqrt(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 1 && nresults <= 1 && arg0.tag == TNumber {
		L.stack[res] = numberValue(math.Sqrt(arg0.num))
		return 1
	}
	return -1
}

func builtinMathPow(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 2 && nresults <= 1 && arg0.tag == TNumber && len(args) >= 1 && args[0].tag == TNumber {
		L.stack[res] = numberValue(math.Pow(arg0.num, args[0].num))
		return 1
	}
	return -1
}

func builtinMathMin(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 2 && nresults <= 1 && arg0.tag == TNumber && len(args) >= 1 && args[0].tag == TNumber {
		r := arg0.num
		if args[0].num < r {
			r = args[0].num
		}
		for i := 2; i < nparams; i++ {
			if i-1 >= len(args) || args[i-1].tag != TNumber {
				return -1
			}
			if args[i-1].num < r {
				r = args[i-1].num
			}
		}
		L.stack[res] = numberValue(r)
		return 1
	}
	return -1
}

func builtinMathMax(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 2 && nresults <= 1 && arg0.tag == TNumber && len(args) >= 1 && args[0].tag == TNumber {
		r := arg0.num
		if args[0].num > r {
			r = args[0].num
		}
		for i := 2; i < nparams; i++ {
			if i-1 >= len(args) || args[i-1].tag != TNumber {
				return -1
			}
			if args[i-1].num > r {
				r = args[i-1].num
			}
		}
		L.stack[res] = numberValue(r)
		return 1
	}
	return -1
}

func builtinMathClamp(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 3 && nresults <= 1 && arg0.tag == TNumber &&
		len(args) >= 2 && args[0].tag == TNumber && args[1].tag == TNumber {
		v, lo, hi := arg0.num, args[0].num, args[1].num
		// Upstream falls back when min > max so error semantics match.
		if lo <= hi {
			r := v
			if r < lo {
				r = lo
			}
			if r > hi {
				r = hi
			}
			L.stack[res] = numberValue(r)
			return 1
		}
	}
	return -1
}

func builtinMathSign(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 1 && nresults <= 1 && arg0.tag == TNumber {
		v := arg0.num
		var s float64
		switch {
		case v > 0:
			s = 1.0
		case v < 0:
			s = -1.0
		default:
			s = 0.0
		}
		L.stack[res] = numberValue(s)
		return 1
	}
	return -1
}

func builtinMathRound(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 1 && nresults <= 1 && arg0.tag == TNumber {
		// math.Round in Go matches C `round()`: half away from zero.
		L.stack[res] = numberValue(math.Round(arg0.num))
		return 1
	}
	return -1
}

// ----------------------------------------------------------------------
// bit32.*
// ----------------------------------------------------------------------

// num2unsigned converts a Lua number to a 32-bit unsigned integer
// using the same truncating-mod-2^32 semantics as upstream
// luai_num2unsigned (lnumutils.h).
func num2unsigned(n float64) uint32 {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0
	}
	// Truncate towards zero, then mod 2^32. This matches the C
	// double-to-uint32 cast behavior for the entire double range.
	t := math.Trunc(n)
	// Reduce modulo 2^64 first to stay in int64 range, then cast.
	const twoPow32 = 4294967296.0
	t = math.Mod(t, twoPow32)
	if t < 0 {
		t += twoPow32
	}
	return uint32(t)
}

func builtinBit32Band(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 2 && nresults <= 1 && arg0.tag == TNumber && len(args) >= 1 && args[0].tag == TNumber {
		r := num2unsigned(arg0.num) & num2unsigned(args[0].num)
		for i := 2; i < nparams; i++ {
			if i-1 >= len(args) || args[i-1].tag != TNumber {
				return -1
			}
			r &= num2unsigned(args[i-1].num)
		}
		L.stack[res] = numberValue(float64(r))
		return 1
	}
	return -1
}

func builtinBit32Bor(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 2 && nresults <= 1 && arg0.tag == TNumber && len(args) >= 1 && args[0].tag == TNumber {
		r := num2unsigned(arg0.num) | num2unsigned(args[0].num)
		for i := 2; i < nparams; i++ {
			if i-1 >= len(args) || args[i-1].tag != TNumber {
				return -1
			}
			r |= num2unsigned(args[i-1].num)
		}
		L.stack[res] = numberValue(float64(r))
		return 1
	}
	return -1
}

func builtinBit32Bxor(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 2 && nresults <= 1 && arg0.tag == TNumber && len(args) >= 1 && args[0].tag == TNumber {
		r := num2unsigned(arg0.num) ^ num2unsigned(args[0].num)
		for i := 2; i < nparams; i++ {
			if i-1 >= len(args) || args[i-1].tag != TNumber {
				return -1
			}
			r ^= num2unsigned(args[i-1].num)
		}
		L.stack[res] = numberValue(float64(r))
		return 1
	}
	return -1
}

func builtinBit32Bnot(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 1 && nresults <= 1 && arg0.tag == TNumber {
		L.stack[res] = numberValue(float64(^num2unsigned(arg0.num)))
		return 1
	}
	return -1
}

func builtinBit32LShift(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 2 && nresults <= 1 && arg0.tag == TNumber && len(args) >= 1 && args[0].tag == TNumber {
		u := num2unsigned(arg0.num)
		s := int(args[0].num)
		// Only specialize the safe range; the slow path handles
		// negative shifts and shifts >= bit width.
		if uint(s) < 32 {
			L.stack[res] = numberValue(float64(u << uint(s)))
			return 1
		}
	}
	return -1
}

func builtinBit32RShift(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 2 && nresults <= 1 && arg0.tag == TNumber && len(args) >= 1 && args[0].tag == TNumber {
		u := num2unsigned(arg0.num)
		s := int(args[0].num)
		if uint(s) < 32 {
			L.stack[res] = numberValue(float64(u >> uint(s)))
			return 1
		}
	}
	return -1
}

func builtinBit32ArShift(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 2 && nresults <= 1 && arg0.tag == TNumber && len(args) >= 1 && args[0].tag == TNumber {
		u := num2unsigned(arg0.num)
		s := int(args[0].num)
		if uint(s) < 32 {
			// Arithmetic shift: cast to int32 first to preserve sign.
			r := uint32(int32(u) >> uint(s))
			L.stack[res] = numberValue(float64(r))
			return 1
		}
	}
	return -1
}

// ----------------------------------------------------------------------
// type / typeof / assert
// ----------------------------------------------------------------------

// typeName returns the canonical interned type name for a value. For
// the common case (no per-type metatable, no __type override) this is
// just ttname[tag]. We always resolve via Type.String() since the
// stateImpl doesn't pre-intern ttname[] at startup.
func typeName(L *stateImpl, v value) *tString {
	return L.gs.intern(v.tag.String())
}

func builtinType(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 1 && nresults <= 1 {
		L.stack[res] = stringValue(typeName(L, arg0))
		return 1
	}
	return -1
}

func builtinTypeof(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 1 && nresults <= 1 {
		// For tables/userdata, upstream consults __type in the
		// metatable. We fall back if any of those are involved to
		// keep the fast path simple; metatable lookups can be subtle.
		switch arg0.tag {
		case TTable:
			t := arg0.gc.(*table)
			if t.metatable != nil {
				return -1
			}
		case TUserdata:
			u := arg0.gc.(*userdata)
			if u.metatable != nil {
				return -1
			}
		case TLightUserdata:
			// Tagged lightuserdata may have a per-tag name; defer.
			if arg0.ltag != 0 {
				return -1
			}
		}
		// Also check the per-type "default" metatable for __type.
		if int(arg0.tag) >= 0 && int(arg0.tag) <= int(TBuffer) {
			if mt := L.gs.mt[arg0.tag]; mt != nil {
				return -1
			}
		}
		L.stack[res] = stringValue(typeName(L, arg0))
		return 1
	}
	return -1
}

func builtinAssert(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	// Upstream: if first arg is truthy AND nresults == 0, return 0
	// (i.e. assert(t) called in a statement context). Otherwise fall
	// back (which raises the error message or returns the values).
	if nparams >= 1 && nresults == 0 && !arg0.isFalse() {
		return 0
	}
	return -1
}

// ----------------------------------------------------------------------
// string.*
// ----------------------------------------------------------------------

func builtinStringLen(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 1 && nresults <= 1 && arg0.tag == TString {
		ts := arg0.gc.(*tString)
		L.stack[res] = numberValue(float64(ts.len()))
		return 1
	}
	return -1
}

func builtinStringSub(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	// Upstream fast path requires exactly 3 params (s, i, j) and
	// 1 <= i <= j < len. Negative indices fall back.
	if nparams >= 3 && nresults <= 1 &&
		arg0.tag == TString && len(args) >= 2 &&
		args[0].tag == TNumber && args[1].tag == TNumber {
		ts := arg0.gc.(*tString)
		i := int(args[0].num)
		j := int(args[1].num)
		if i >= 1 && j >= i && uint(j-1) < uint(ts.len()) {
			s := ts.str()
			sub := s[i-1 : j]
			L.stack[res] = stringValue(L.gs.intern(sub))
			return 1
		}
	}
	return -1
}

func builtinStringByte(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	// Per upstream luauF_byte: requires nparams >= 2 and a number
	// index. Returns one byte per character in [i, j]. We only handle
	// the common "single byte" case to match upstream which returns
	// at most c==nresults bytes.
	if nparams >= 2 && arg0.tag == TString && len(args) >= 1 && args[0].tag == TNumber {
		ts := arg0.gc.(*tString)
		i := int(args[0].num)
		j := i
		if nparams >= 3 && len(args) >= 2 && args[1].tag == TNumber {
			j = int(args[1].num)
		} else if nparams >= 3 {
			j = 0
		}
		if i >= 1 && j >= i && j <= ts.len() {
			c := j - i + 1
			want := c
			if nresults >= 0 {
				want = nresults
			} else {
				want = 1
			}
			// Upstream only writes if c matches the request to avoid
			// stack management complexity.
			if c == want {
				s := ts.str()
				for k := 0; k < c; k++ {
					// Caller has reserved res..res+c-1 (CALL allocates
					// up to MaxStackSize so this is safe).
					if res+k >= len(L.stack) {
						L.reserve(res + k + 1 - L.top)
					}
					L.stack[res+k] = numberValue(float64(byte(s[i+k-1])))
				}
				return c
			}
		}
	}
	return -1
}

func builtinStringChar(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams < 8 && nresults <= 1 {
		var buf [8]byte
		if nparams >= 1 {
			if arg0.tag != TNumber {
				return -1
			}
			ch := int(arg0.num)
			if byte(ch) != byte(ch&0xff) || ch < 0 || ch > 255 {
				return -1
			}
			buf[0] = byte(ch)
		}
		for i := 2; i <= nparams; i++ {
			if i-2 >= len(args) || args[i-2].tag != TNumber {
				return -1
			}
			ch := int(args[i-2].num)
			if ch < 0 || ch > 255 {
				return -1
			}
			buf[i-1] = byte(ch)
		}
		L.stack[res] = stringValue(L.gs.intern(string(buf[:nparams])))
		return 1
	}
	return -1
}

// ----------------------------------------------------------------------
// rawget / rawset / rawequal / rawlen
// ----------------------------------------------------------------------

func builtinRawEqual(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 2 && nresults <= 1 && len(args) >= 1 {
		L.stack[res] = booleanValue(rawEqual(arg0, args[0]))
		return 1
	}
	return -1
}

func builtinRawGet(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 2 && nresults <= 1 && arg0.tag == TTable && len(args) >= 1 {
		t := arg0.gc.(*table)
		L.stack[res] = t.get(args[0])
		return 1
	}
	return -1
}

func builtinRawSet(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 3 && nresults <= 1 && arg0.tag == TTable && len(args) >= 2 {
		key := args[0]
		if key.tag == TNil {
			return -1
		}
		if key.tag == TNumber && math.IsNaN(key.num) {
			return -1
		}
		t := arg0.gc.(*table)
		if t.readonly {
			return -1
		}
		t.set(L.gs, key, args[1])
		// rawset returns the table itself.
		L.stack[res] = arg0
		return 1
	}
	return -1
}

func builtinRawLen(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 1 && nresults <= 1 {
		switch arg0.tag {
		case TTable:
			t := arg0.gc.(*table)
			L.stack[res] = numberValue(float64(t.rawLen()))
			return 1
		case TString:
			ts := arg0.gc.(*tString)
			L.stack[res] = numberValue(float64(ts.len()))
			return 1
		}
	}
	return -1
}

// ----------------------------------------------------------------------
// table.insert / table.unpack
// ----------------------------------------------------------------------

func builtinTableInsert(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	// Upstream only fast-paths the 2-arg form: table.insert(t, v).
	// The 3-arg form (insert at position) shifts elements and is
	// handled by the slow path.
	if nparams == 2 && nresults <= 0 && arg0.tag == TTable && len(args) >= 1 {
		t := arg0.gc.(*table)
		if t.readonly {
			return -1
		}
		pos := t.rawLen() + 1
		t.setNum(L.gs, pos, args[0])
		return 0
	}
	return -1
}

func builtinTableUnpack(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	// Fast path: table.unpack(t) returning all elements 1..n where n
	// fits in the array part. Slow path covers the (t, i, j) form
	// for now (matching upstream's narrow case is fine; the 3-arg
	// fast path can be added later).
	if nparams >= 1 && nresults < 0 && arg0.tag == TTable {
		t := arg0.gc.(*table)
		n := -1
		if nparams == 1 {
			n = t.rawLen()
		} else if nparams == 3 && len(args) >= 2 &&
			args[0].tag == TNumber && args[1].tag == TNumber &&
			args[0].num == 1.0 {
			n = int(args[1].num)
		}
		if n >= 0 && n <= len(t.array) {
			// Ensure stack has room.
			if res+n > len(L.stack) {
				L.reserve(res + n - L.top)
			}
			for i := 0; i < n; i++ {
				L.stack[res+i] = t.array[i]
			}
			return n
		}
	}
	return -1
}

// ----------------------------------------------------------------------
// select(n, ...)
// ----------------------------------------------------------------------

func builtinSelectVararg(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	// Upstream fast path: nparams == 1 and nresults == 1.
	if nparams != 1 || nresults != 1 {
		return -1
	}
	// We need the current frame to find ... slots. Bail out if there
	// is no active Lua frame (only a Go frame).
	ci := L.currentFrame()
	if ci == nil || ci.cl == nil || ci.cl.proto == nil {
		return -1
	}
	// Vararg count is recorded on the callinfo.
	n := ci.numVararg
	if arg0.tag == TNumber {
		i := int(arg0.num)
		if uint(i-1) < uint(n) {
			L.stack[res] = L.stack[ci.varargBase+i-1]
			return 1
		}
		return -1
	}
	if arg0.tag == TString {
		s := arg0.gc.(*tString).str()
		if len(s) > 0 && s[0] == '#' {
			L.stack[res] = numberValue(float64(n))
			return 1
		}
	}
	return -1
}

// ----------------------------------------------------------------------
// getmetatable / setmetatable
// ----------------------------------------------------------------------

func builtinGetMetatable(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	if nparams >= 1 && nresults <= 1 {
		var mt *table
		switch arg0.tag {
		case TTable:
			mt = arg0.gc.(*table).metatable
		case TUserdata:
			mt = arg0.gc.(*userdata).metatable
		default:
			if int(arg0.tag) >= 0 && int(arg0.tag) <= int(TBuffer) {
				mt = L.gs.mt[arg0.tag]
			}
		}
		// Check for __metatable override.
		if mt != nil {
			mmName := L.gs.tmname[TMMetatable]
			if mmName != nil {
				if mtv, _ := mt.getStr(mmName); mtv.tag != TNil {
					L.stack[res] = mtv
					return 1
				}
			}
			L.stack[res] = tableValue(mt)
			return 1
		}
		L.stack[res] = nilValue()
		return 1
	}
	return -1
}

func builtinSetMetatable(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	// Upstream fast-paths only the setmetatable(t, mt) case where t
	// is a writable table with no existing metatable.
	if nparams >= 2 && nresults <= 1 && arg0.tag == TTable &&
		len(args) >= 1 && args[0].tag == TTable {
		t := arg0.gc.(*table)
		if t.readonly || t.metatable != nil {
			return -1
		}
		mt := args[0].gc.(*table)
		t.metatable = mt
		L.gs.barrierTable(t, mt)
		L.stack[res] = arg0
		return 1
	}
	return -1
}

// ----------------------------------------------------------------------
// tonumber / tostring
// ----------------------------------------------------------------------

func builtinToNumber(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	// Upstream only fast-paths the single-arg form (no base).
	if nparams == 1 && nresults <= 1 {
		switch arg0.tag {
		case TNumber:
			L.stack[res] = arg0
			return 1
		case TString:
			if n, ok := arg0.asNumber(); ok {
				L.stack[res] = numberValue(n)
				return 1
			}
			L.stack[res] = nilValue()
			return 1
		default:
			L.stack[res] = nilValue()
			return 1
		}
	}
	return -1
}

func builtinToString(L *stateImpl, res int, arg0 value, args []value, nresults, nparams int) int {
	// Upstream fast-paths nil/boolean/number/string. Other types
	// require __tostring lookup which is slow-path territory.
	if nparams >= 1 && nresults <= 1 {
		switch arg0.tag {
		case TNil:
			L.stack[res] = stringValue(L.gs.intern("nil"))
			return 1
		case TBoolean:
			if arg0.bool_ {
				L.stack[res] = stringValue(L.gs.intern("true"))
			} else {
				L.stack[res] = stringValue(L.gs.intern("false"))
			}
			return 1
		case TNumber:
			L.stack[res] = stringValue(L.gs.intern(formatNumber(arg0.num)))
			return 1
		case TString:
			L.stack[res] = arg0
			return 1
		}
	}
	return -1
}
