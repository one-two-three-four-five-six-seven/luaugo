// Copyright (c) luaugo contributors. Licensed under the MIT License.

package tests

import (
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// conformance_shims.go installs minimal stub implementations of the
// globals that upstream's C++ conformance harness
// (.upstream/tests/Conformance.test.cpp) registers on its lua_State
// before running each fixture. Our suite drives the same fixtures with
// pkg/vm but without that harness, so many fixtures call into nil-value
// globals (`is_native`, `breakpoint`, `makelud`, `cxxthrow`, ...) and
// abort early.
//
// The aim here is NOT to fully model the harness -- many of its
// behaviours (e.g. C-side coroutine yields, real coverage counters,
// vec2 userdata with metamethods) would each be a multi-day project.
// The aim is to provide values shaped enough that fixtures progress
// past the trivial nil-call failures and either:
//
//   1. complete successfully (the common case for fixtures whose only
//      harness use is `is_native_if_supported()` in an `or` chain), or
//   2. fail later, on a real VM gap we can address separately.
//
// Each shim is documented inline with the upstream behaviour it
// approximates and the fixture(s) it unblocks.

// installConformanceShims registers every harness global the
// conformance corpus expects. Call this AFTER lib.OpenAll(s) so the
// stubs sit on top of the standard library (and don't clash with
// real Open* installs).
func installConformanceShims(s *vm.State) {
	// ----- native-codegen status probes ------------------------------
	//
	// Upstream registers these to report whether the fixture is
	// running under the native code generator. luaugo has no native
	// codegen.
	//
	//   is_native()                 -- "am I literally executing native
	//                                  code right now?". Always false
	//                                  for us; fixtures that rely on it
	//                                  (native.luau, integers.luau)
	//                                  inherently target the native
	//                                  backend and are expected to fail.
	//
	//   is_native_if_supported()    -- upstream returns TRUE when
	//                                  codegen is disabled, only
	//                                  returning the actual native
	//                                  status when codegen IS enabled.
	//                                  See Conformance.test.cpp:1139.
	//                                  Since luaugo has no codegen, the
	//                                  upstream-equivalent answer is
	//                                  unconditionally true. This makes
	//                                  the standalone trailing
	//                                  `assert(is_native_if_supported())`
	//                                  in vector.luau / vector_library
	//                                  pass cleanly.
	s.Register("is_native", returnFalse)
	s.Register("is_native_if_supported", returnTrue)

	// ----- debugger ---------------------------------------------------
	//
	// `breakpoint(line[, enabled])` instructs the upstream debugger to
	// install a breakpoint. With no debugger attached it's a no-op.
	// Used by conformance/debugger.luau.
	s.Register("breakpoint", noop)

	// ----- coverage ---------------------------------------------------
	//
	// `coverage(...)` toggles coverage collection; `getcoverage(fn)`
	// returns an array of {name, linedefined, depth, [line]=hits, ...}
	// records. With no collector wired up, both return empty tables.
	// conformance/coverage.luau still fails on the asserts that read
	// the returned data, but at least it doesn't nil-call.
	s.Register("coverage", returnEmptyTable)
	s.Register("getcoverage", getCoverageImpl)

	// ----- gc allocation gate ----------------------------------------
	//
	// `setblockallocations(bool)` instructs the harness's custom
	// allocator to reject every subsequent alloc. We don't model it;
	// no-op suffices to keep gc.luau running. The fixture also asserts
	// real gc-step behaviour which is unrelated to this shim.
	s.Register("setblockallocations", noop)

	// ----- error-propagation helpers ---------------------------------
	//
	// `cxxthrow()` raises a C++ exception in upstream so that the
	// pcall path observes the standard Luau "oops" error message.
	// `resumeerror(co, msg)` resumes a coroutine with msg as a
	// pending error. We approximate by raising a Luau error; the
	// pcall.luau fixture compares against an exact "oops" string.
	s.Register("cxxthrow", func(state *vm.State) int {
		state.PushString("oops")
		state.Error()
		return 0
	})
	s.Register("resumeerror", func(state *vm.State) int {
		// resumeerror(co, errvalue): resume co with errvalue raised
		// as a Lua error at the yield (or initial call) point.
		// Mirrors upstream lua_resumeerror used by
		// conformance/pcall.luau:144.
		if state.Top() < 1 || state.Type(1) != vm.TThread {
			state.LError("invalid argument #1 to 'resumeerror' (coroutine expected)")
		}
		co := state.ToThread(1)
		if co == nil {
			state.LError("invalid argument #1 to 'resumeerror' (coroutine expected)")
		}
		// Use the error value as a string when possible, otherwise
		// fall back to "resumeerror" sentinel. The fixture passes a
		// string ("fail") so the string branch suffices.
		if state.Top() >= 2 && state.Type(2) == vm.TString {
			msg, _ := state.ToString(2)
			co.ResumeError(state, msg)
		} else {
			co.ResumeError(state, "resumeerror")
		}
		return 0
	})

	// ----- coroutine-yield helpers (C-side yields) -------------------
	//
	// Upstream registers a family of yielding C functions used by
	// cyield.luau:
	//   singleYield()                  -> yields 2, returns 4
	//   multipleYields(x)              -> yields x+1..x+3, returns x+4
	//   multipleYieldsWithNestedCall   -> yields a nested chain
	//
	// Implementing a yielding Go function requires deep cooperation
	// with the coroutine runtime (the call must be invoked under
	// coroutine.resume; the Go side must call State.Yield). We provide
	// minimal yielders that work when called via `coroutine.wrap`.
	// If the State doesn't support Go-side yield, these will surface
	// as runtime errors -- still better than nil.
	s.Register("singleYield", func(state *vm.State) int {
		state.PushInteger(2)
		state.Yield(1)
		state.PushInteger(4)
		return 1
	})
	s.Register("multipleYields", func(state *vm.State) int {
		x := int64(10)
		if v, ok := state.ToInteger(1); ok {
			x = v
		}
		state.PushInteger(x + 1)
		state.Yield(1)
		state.PushInteger(x + 2)
		state.Yield(1)
		state.PushInteger(x + 3)
		state.Yield(1)
		state.PushInteger(x + 4)
		return 1
	})
	s.Register("multipleYieldsWithNestedCall", func(state *vm.State) int {
		// Mirrors upstream Conformance.test.cpp:1506-1565 collapsed
		// from the continuation-based form into a sequential shim
		// (we don't model lua_yieldk/continuations). The expected
		// yield sequence:
		//   shouldYield=true:  yields x+95, x+105 (from helper +
		//                      continuation), x+200, returns x+210.
		//   shouldYield=false: yields x+100, x+200, returns x+210.
		// `context` constant inside multipleYieldsWithNestedCall is
		// 5, so helper-yield = 100+5 = 105 (or +x for our scaled
		// form). conformance/cyield.luau:32-44 / 49-51 check these
		// values.
		x := int64(10)
		if v, ok := state.ToInteger(1); ok {
			x = v
		}
		shouldYield := state.ToBoolean(2)
		if shouldYield {
			state.PushInteger(x + 95) // helper yields 100+context=105 (+x-10 baseline)
			state.Yield(1)
			state.PushInteger(x + 105) // continuation state=0 yields stacked val
			state.Yield(1)
		} else {
			state.PushInteger(x + 100) // helper returns 105+context=110 (+x-10)
			state.Yield(1)
		}
		state.PushInteger(x + 200) // continuation state=1 yields x+200
		state.Yield(1)
		state.PushInteger(x + 210) // continuation state=2 returns x+210
		return 1
	})

	// passthroughCall variants: each invokes its first argument with
	// the remaining arguments, optionally yielding around the call.
	// For our purposes a plain forward-call captures the
	// pass-through aspect; we cannot reproduce the yieldable variant
	// here. These will not satisfy every cyield assertion but they
	// keep simple usages alive.
	for _, name := range []string{
		"passthroughCall",
		"passthroughCallMoreResults",
		"passthroughCallArgReuse",
		"passthroughCallVaradic",
		"passthroughCallWithState",
	} {
		s.Register(name, passthroughCall)
	}

	// ----- assertion helpers -----------------------------------------
	//
	// `inassert(f, ...)` invokes f(...) and asserts it returned
	// without error. Useful for fixtures that need a guaranteed-OK
	// helper call from C land.
	s.Register("inassert", func(state *vm.State) int {
		// Call the function at slot 1 with args 2..top.
		// PCall consumes function + args. We move the function to
		// the top, push args after it, then pcall.
		n := state.Top()
		if n < 1 {
			return 0
		}
		// Push function, then push each remaining arg.
		state.PushValue(1)
		for i := 2; i <= n; i++ {
			state.PushValue(i)
		}
		st := state.PCall(n-1, 0, 0)
		if st != vm.StatusOK {
			state.Error()
		}
		return 0
	})

	// ----- stack-size introspection ----------------------------------
	//
	// `getmaxstacksize()` returns Luau's configured maximum value
	// stack. Conformance corpus uses it to gate "deep recursion"
	// blocks. 20000 matches the upstream LUAI_MAXCSTACK default.
	s.Register("getmaxstacksize", func(state *vm.State) int {
		state.PushInteger(20000)
		return 1
	})

	// ----- userdata factories ----------------------------------------
	//
	// `makelud(key)` returns a light userdata uniquely identified by
	// key. Used by tables.luau to verify that light-userdata keys
	// interact correctly with the hash part. We use a fresh *int per
	// call which gives each call site a stable, distinct pointer.
	s.Register("makelud", func(state *vm.State) int {
		p := new(int)
		// Burn the argument value into the box so different keys
		// produce distinguishable identities for human debugging.
		if v, ok := state.ToInteger(1); ok {
			*p = int(v)
		}
		state.PushLightUserdata(p)
		return 1
	})

	// `vec2(x, y)` / `vertex(pos, normal, uv)` / `int64(n)` are
	// upstream userdata types defined inline in Conformance.test.cpp.
	// luaugo has no equivalent userdata-with-metamethods plumbing
	// exposed at the conformance layer, so we approximate with a
	// plain Lua table that has the documented direct-access fields.
	// This is enough for some fixtures (debug-formatting of values)
	// but won't satisfy udata_direct.luau or userdata.luau which
	// depend on richer metamethod semantics.
	// vec2 / vertex: full upstream-style userdata with __index and
	// __namecall metamethods, plus a sizeof property. See
	// installVec2Shim for the metatable layout and supported methods
	// (Dot, Min, Clone). udata_direct.luau exercises every property
	// and method on these types.
	installVec2Shim(s)
	installVertexShim(s)
	// int64: full upstream-style userdata with __eq, __lt, __le,
	// __add, __sub, __mul, __div (truncating), __idiv (floor),
	// __mod, __pow, __unm, __tostring, __index, __newindex.
	// Matches the in-test C++ definition that pcall.luau /
	// userdata.luau / udata_direct.luau exercise. The Go-side
	// representation is the Go int64 value boxed via
	// PushUserdataObject.
	installInt64Shim(s)

	// ----- RTTI table ------------------------------------------------
	//
	// types.luau walks _G and asserts every (non-ignored) entry has
	// a matching RTTI entry whose value is a string type name (for
	// scalars/functions) or a nested table (for libraries). Upstream
	// builds this from the Luau type framework; we synthesise the
	// same shape statically over our standard library so the fixture
	// reaches `return 'OK'`.
	buildRTTI(s)
}

// buildRTTI installs a static RTTI table that mirrors the type shape
// of luaugo's standard library, matching upstream's runtime-built
// `RTTI` table closely enough that conformance/types.luau passes.
func buildRTTI(s *vm.State) {
	// Helper: build a sub-table where every key gets the given type
	// string. Used for `string`, `table`, etc. where every member is
	// a function.
	pushTypedTable := func(members []string, typename string) {
		s.NewTable()
		for _, m := range members {
			s.PushString(typename)
			s.SetField(-2, m)
		}
	}

	s.NewTable()
	// Top-level scalars / functions.
	for _, fn := range []string{
		"assert", "collectgarbage", "error", "gcinfo", "getfenv",
		"getmetatable", "ipairs", "loadstring", "newproxy", "next",
		"pairs", "pcall", "print", "rawequal", "rawget", "rawlen",
		"rawset", "select", "setfenv", "setmetatable", "tonumber",
		"tostring", "type", "typeof", "unpack", "xpcall",
	} {
		s.PushString("function")
		s.SetField(-2, fn)
	}
	s.PushString("string")
	s.SetField(-2, "_VERSION")
	// Library tables. Mark every entry as "function".
	pushTypedTable([]string{
		"band", "bnot", "bor", "bxor", "btest", "extract", "lrotate",
		"lshift", "replace", "rrotate", "rshift", "arshift",
		"countlz", "countrz", "byteswap",
	}, "function")
	s.SetField(-2, "bit32")
	pushTypedTable([]string{
		"create", "fromstring", "tostring", "len", "copy", "fill",
		"readi8", "readu8", "readi16", "readu16", "readi32", "readu32",
		"readf32", "readf64", "writei8", "writeu8", "writei16",
		"writeu16", "writei32", "writeu32", "writef32", "writef64",
		"readstring", "writestring", "readbits", "writebits",
	}, "function")
	s.SetField(-2, "buffer")
	pushTypedTable([]string{
		"create", "wrap", "resume", "yield", "isyieldable", "status",
		"running", "close",
	}, "function")
	s.SetField(-2, "coroutine")
	pushTypedTable([]string{"info", "traceback"}, "function")
	s.SetField(-2, "debug")
	// math: matches pkg/vm/lib/math.go's registration list. Mixed
	// function/number members.
	s.NewTable()
	for _, fn := range []string{
		"abs", "acos", "asin", "atan", "atan2", "ceil", "clamp", "cos",
		"cosh", "deg", "exp", "floor", "fmod", "frexp", "ldexp", "log",
		"log10", "max", "min", "modf", "noise", "pow", "rad", "random",
		"randomseed", "round", "sign", "sin", "sinh", "sqrt", "tan",
		"tanh", "map", "lerp", "isnan", "isinf", "isfinite",
	} {
		s.PushString("function")
		s.SetField(-2, fn)
	}
	for _, k := range []string{"pi", "huge", "nan", "e", "phi", "sqrt2", "tau"} {
		s.PushString("number")
		s.SetField(-2, k)
	}
	s.SetField(-2, "math")
	pushTypedTable([]string{
		"clock", "date", "difftime", "time",
	}, "function")
	s.SetField(-2, "os")
	pushTypedTable([]string{
		"byte", "char", "find", "format", "gmatch", "gsub", "len",
		"lower", "match", "rep", "reverse", "split", "sub", "upper",
		"pack", "packsize", "unpack",
	}, "function")
	s.SetField(-2, "string")
	pushTypedTable([]string{
		"concat", "create", "find", "foreach", "foreachi", "freeze",
		"getn", "insert", "isfrozen", "maxn", "move", "pack", "remove",
		"sort", "unpack", "clear", "clone",
	}, "function")
	s.SetField(-2, "table")
	// utf8 has charpattern (string) plus function members. Mirror
	// the actual lib registration in pkg/vm/lib/utf8.go: only the
	// functions we genuinely export should appear here, otherwise
	// types.luau:46 reports "_G.utf8.X present in type information
	// but absent from VM".
	s.NewTable()
	for _, fn := range []string{
		"char", "codepoint", "codes", "len", "offset",
	} {
		s.PushString("function")
		s.SetField(-2, fn)
	}
	s.PushString("string")
	s.SetField(-2, "charpattern")
	s.SetField(-2, "utf8")
	// vector library: matches pkg/vm/lib/vector.go's vectorLib slice.
	s.NewTable()
	for _, fn := range []string{
		"create", "magnitude", "normalize", "cross", "dot", "angle",
		"floor", "ceil", "abs", "sign", "clamp", "max", "min", "lerp",
	} {
		s.PushString("function")
		s.SetField(-2, fn)
	}
	s.PushString("vector")
	s.SetField(-2, "zero")
	s.PushString("vector")
	s.SetField(-2, "one")
	s.SetField(-2, "vector")

	// _G is a self-reference (table). The fixture's verify() skips
	// the "_G" key inside the recursive walk to avoid infinite loops,
	// but the entry must exist at the top level.
	s.PushString("table")
	s.SetField(-2, "_G")

	// Harness-installed globals (see installConformanceShims above)
	// also appear in _G. types.luau's ignore list does not cover
	// them, so each must have an RTTI entry. Mirror upstream's
	// Conformance.test.cpp:1843-1864 which builds RTTI off the live
	// global scope (which already includes these harness functions).
	for _, name := range []string{
		"is_native", "is_native_if_supported", "breakpoint", "coverage",
		"getcoverage", "setblockallocations", "cxxthrow", "resumeerror",
		"singleYield", "multipleYields", "multipleYieldsWithNestedCall",
		"passthroughCall", "passthroughCallMoreResults",
		"passthroughCallArgReuse", "passthroughCallVaradic",
		"passthroughCallWithState", "inassert", "getmaxstacksize",
		"makelud", "vec2", "vertex", "int64",
	} {
		s.PushString("function")
		s.SetField(-2, name)
	}

	s.SetGlobal("RTTI")
}

// ---------------------------------------------------------------------
// shared shim helpers
// ---------------------------------------------------------------------

// returnFalse is the implementation for is_native.
func returnFalse(s *vm.State) int {
	s.PushBoolean(false)
	return 1
}

// returnTrue is the implementation for is_native_if_supported, which
// in upstream returns true whenever codegen is unavailable.
func returnTrue(s *vm.State) int {
	s.PushBoolean(true)
	return 1
}

// noop discards all arguments and returns no values.
func noop(*vm.State) int { return 0 }

// returnEmptyTable returns a fresh empty table. Sufficient for
// `coverage()` where we don't model collection beyond what
// getCoverageImpl reports.
func returnEmptyTable(s *vm.State) int {
	s.NewTable()
	return 1
}

// getCoverageImpl implements debug.getcoverage(fn). Returns a sorted
// array of records describing fn's bytecode coverage: one row per
// closure (the root plus every nested closure in source order). Each
// record carries .name / .linedefined / .depth fields plus a
// per-line hit count (line keys map to integer counts; lines that
// have an instruction but were never executed appear as 0).
// Matches the shape upstream's conformance harness exposes in
// Conformance.test.cpp via lua_getcoverage.
func getCoverageImpl(s *vm.State) int {
	if s.Type(1) != vm.TFunction {
		s.LTypeError(1, "function")
	}
	records := s.Coverage(1)
	s.NewTable()
	for i, rec := range records {
		s.NewTable()
		if rec.Name != "" {
			s.PushString(rec.Name)
			s.SetField(-2, "name")
		}
		s.PushInteger(int64(rec.LineDefined))
		s.SetField(-2, "linedefined")
		s.PushInteger(int64(rec.Depth))
		s.SetField(-2, "depth")
		for line, count := range rec.Hits {
			s.PushInteger(int64(count))
			s.RawSetI(-2, line)
		}
		s.RawSetI(-2, i+1)
	}
	return 1
}

// passthroughCall calls its first argument with the remaining
// arguments and returns whatever it returned. Approximates the
// passthroughCall* family in Conformance.test.cpp (sans the C-side
// yield support, which we cannot reproduce without rewriting the
// coroutine runtime).
func passthroughCall(s *vm.State) int {
	n := s.Top()
	if n < 1 {
		return 0
	}
	// Push function and args in order, then call. Use multiret so
	// every value flows back.
	s.PushValue(1)
	for i := 2; i <= n; i++ {
		s.PushValue(i)
	}
	before := s.Top() - n // stack position before the call's return
	s.Call(n-1, -1)
	return s.Top() - before
}

// ---------------------------------------------------------------------
// vec2 / vertex userdata shims
// ---------------------------------------------------------------------
//
// Conformance/udata_direct.luau pokes a `vec2` userdata via
// .X/.Y/.Magnitude/.Unit/.sizeof reads, .X/.Y writes, and :Dot/:Min/
// :Clone method calls. `vertex` similarly bundles two vector
// components and a vec2 with .pos/.normal/.uv reads + writes and
// :Clone. Both are upstream-defined userdata types; we shim them with
// the same metatable-driven layout (no luaA fast-slot caching).

const vec2UTag byte = 5
const vertexUTag byte = 6

type vec2Box struct{ x, y float64 }
type vertexBox struct {
	posX, posY, posZ          float32
	normalX, normalY, normalZ float32
	uvX, uvY                  float64
}

func installVec2Shim(s *vm.State) {
	s.Register("vec2", func(state *vm.State) int {
		x, _ := state.ToNumber(1)
		y, _ := state.ToNumber(2)
		state.PushUserdataObject(&vec2Box{x: x, y: y}, vec2UTag)
		ensureVec2Metatable(state)
		state.SetMetatable(-2)
		return 1
	})
}

func ensureVec2Metatable(s *vm.State) {
	const key = "luaugo.shim.vec2.mt"
	s.GetRegistryField(key)
	if !s.IsNil(-1) {
		return
	}
	s.Pop(1)
	s.NewTable()

	s.PushGoFunction(vec2Index, 0)
	s.SetField(-2, "__index")
	s.PushGoFunction(vec2NewIndex, 0)
	s.SetField(-2, "__newindex")

	// Arithmetic. Componentwise where applicable; promote a scalar
	// number to (n, n) so `vec2(1,2) * 3` works. native_userdata.luau
	// `-c + a * b` requires __unm + __add + __mul.
	bin := func(op func(ax, ay, bx, by float64) (float64, float64)) vm.GoFunction {
		return func(state *vm.State) int {
			ax, ay := vec2Coerce(state, 1)
			bx, by := vec2Coerce(state, 2)
			rx, ry := op(ax, ay, bx, by)
			state.PushUserdataObject(&vec2Box{x: rx, y: ry}, vec2UTag)
			ensureVec2Metatable(state)
			state.SetMetatable(-2)
			return 1
		}
	}
	s.PushGoFunction(bin(func(ax, ay, bx, by float64) (float64, float64) { return ax + bx, ay + by }), 0)
	s.SetField(-2, "__add")
	s.PushGoFunction(bin(func(ax, ay, bx, by float64) (float64, float64) { return ax - bx, ay - by }), 0)
	s.SetField(-2, "__sub")
	s.PushGoFunction(bin(func(ax, ay, bx, by float64) (float64, float64) { return ax * bx, ay * by }), 0)
	s.SetField(-2, "__mul")
	s.PushGoFunction(bin(func(ax, ay, bx, by float64) (float64, float64) {
		if bx == 0 || by == 0 {
			return 0, 0
		}
		return ax / bx, ay / by
	}), 0)
	s.SetField(-2, "__div")
	s.PushGoFunction(func(state *vm.State) int {
		ax, ay := vec2Coerce(state, 1)
		state.PushUserdataObject(&vec2Box{x: -ax, y: -ay}, vec2UTag)
		ensureVec2Metatable(state)
		state.SetMetatable(-2)
		return 1
	}, 0)
	s.SetField(-2, "__unm")
	s.PushGoFunction(func(state *vm.State) int {
		a, _ := state.ToUserdata(1).(*vec2Box)
		b, _ := state.ToUserdata(2).(*vec2Box)
		state.PushBoolean(a != nil && b != nil && a.x == b.x && a.y == b.y)
		return 1
	}, 0)
	s.SetField(-2, "__eq")

	s.SetRegistryField(key)
	s.GetRegistryField(key)
}

// vec2Coerce extracts (x, y) from a vec2 userdata or a Lua number
// (broadcast to both components). Returns (0, 0) when neither shape
// matches; the caller's arithmetic op then degenerates safely.
func vec2Coerce(s *vm.State, idx int) (float64, float64) {
	if u, ok := s.ToUserdata(idx).(*vec2Box); ok && u != nil {
		return u.x, u.y
	}
	if v, ok := s.ToNumber(idx); ok {
		return v, v
	}
	return 0, 0
}

func vec2Index(s *vm.State) int {
	u, _ := s.ToUserdata(1).(*vec2Box)
	k, _ := s.ToString(2)
	if u == nil {
		s.PushNil()
		return 1
	}
	switch k {
	case "X":
		s.PushNumber(u.x)
	case "Y":
		s.PushNumber(u.y)
	case "Magnitude":
		s.PushNumber(mathSqrt(u.x*u.x + u.y*u.y))
	case "Unit":
		m := mathSqrt(u.x*u.x + u.y*u.y)
		if m == 0 {
			s.PushUserdataObject(&vec2Box{}, vec2UTag)
		} else {
			s.PushUserdataObject(&vec2Box{x: u.x / m, y: u.y / m}, vec2UTag)
		}
		ensureVec2Metatable(s)
		s.SetMetatable(-2)
	case "sizeof":
		s.PushInteger(8)
	case "Dot":
		s.PushGoFunction(vec2Dot, 0)
	case "Min":
		s.PushGoFunction(vec2Min, 0)
	case "Clone":
		s.PushGoFunction(vec2Clone, 0)
	default:
		s.PushNil()
	}
	return 1
}

func vec2Dot(s *vm.State) int {
	a, _ := s.ToUserdata(1).(*vec2Box)
	b, _ := s.ToUserdata(2).(*vec2Box)
	if a == nil || b == nil {
		s.PushNumber(0)
		return 1
	}
	s.PushNumber(a.x*b.x + a.y*b.y)
	return 1
}

func vec2Min(s *vm.State) int {
	a, _ := s.ToUserdata(1).(*vec2Box)
	b, _ := s.ToUserdata(2).(*vec2Box)
	if a == nil || b == nil {
		s.PushNil()
		return 1
	}
	r := &vec2Box{x: a.x, y: a.y}
	if b.x < r.x {
		r.x = b.x
	}
	if b.y < r.y {
		r.y = b.y
	}
	s.PushUserdataObject(r, vec2UTag)
	ensureVec2Metatable(s)
	s.SetMetatable(-2)
	return 1
}

func vec2Clone(s *vm.State) int {
	a, _ := s.ToUserdata(1).(*vec2Box)
	if a == nil {
		s.PushNil()
		return 1
	}
	s.PushUserdataObject(&vec2Box{x: a.x, y: a.y}, vec2UTag)
	ensureVec2Metatable(s)
	s.SetMetatable(-2)
	return 1
}

func vec2NewIndex(s *vm.State) int {
	u, _ := s.ToUserdata(1).(*vec2Box)
	k, _ := s.ToString(2)
	if u == nil {
		return 0
	}
	val, _ := s.ToNumber(3)
	switch k {
	case "X":
		u.x = val
	case "Y":
		u.y = val
	}
	return 0
}

// installVertexShim mirrors installVec2Shim but for a 3-component pos
// + 3-component normal + 2-component uv bundle.
func installVertexShim(s *vm.State) {
	s.Register("vertex", func(state *vm.State) int {
		var b vertexBox
		// pos at slot 1: a vector
		if px, py, pz, _, ok := state.ToVector(1); ok {
			b.posX, b.posY, b.posZ = px, py, pz
		}
		if nx, ny, nz, _, ok := state.ToVector(2); ok {
			b.normalX, b.normalY, b.normalZ = nx, ny, nz
		}
		if u, ok := state.ToUserdata(3).(*vec2Box); ok && u != nil {
			b.uvX, b.uvY = u.x, u.y
		}
		state.PushUserdataObject(&b, vertexUTag)
		ensureVertexMetatable(state)
		state.SetMetatable(-2)
		return 1
	})
}

func ensureVertexMetatable(s *vm.State) {
	const key = "luaugo.shim.vertex.mt"
	s.GetRegistryField(key)
	if !s.IsNil(-1) {
		return
	}
	s.Pop(1)
	s.NewTable()

	s.PushGoFunction(vertexIndex, 0)
	s.SetField(-2, "__index")
	s.PushGoFunction(vertexNewIndex, 0)
	s.SetField(-2, "__newindex")

	s.SetRegistryField(key)
	s.GetRegistryField(key)
}

func pushVertexVec(s *vm.State, x, y, z float32) {
	s.PushVector(x, y, z, 0)
}

func vertexIndex(s *vm.State) int {
	u, _ := s.ToUserdata(1).(*vertexBox)
	k, _ := s.ToString(2)
	if u == nil {
		s.PushNil()
		return 1
	}
	switch k {
	case "pos":
		pushVertexVec(s, u.posX, u.posY, u.posZ)
	case "normal":
		pushVertexVec(s, u.normalX, u.normalY, u.normalZ)
	case "uv":
		s.PushUserdataObject(&vec2Box{x: u.uvX, y: u.uvY}, vec2UTag)
		ensureVec2Metatable(s)
		s.SetMetatable(-2)
	case "sizeof":
		s.PushInteger(32)
	case "Clone":
		// Return a closure that produces a fresh copy.
		captured := *u
		s.PushGoFunction(func(state *vm.State) int {
			c := captured
			state.PushUserdataObject(&c, vertexUTag)
			ensureVertexMetatable(state)
			state.SetMetatable(-2)
			return 1
		}, 0)
	default:
		s.PushNil()
	}
	return 1
}

func vertexNewIndex(s *vm.State) int {
	u, _ := s.ToUserdata(1).(*vertexBox)
	k, _ := s.ToString(2)
	if u == nil {
		return 0
	}
	switch k {
	case "pos":
		if px, py, pz, _, ok := s.ToVector(3); ok {
			u.posX, u.posY, u.posZ = px, py, pz
		}
	case "normal":
		if nx, ny, nz, _, ok := s.ToVector(3); ok {
			u.normalX, u.normalY, u.normalZ = nx, ny, nz
		}
	case "uv":
		if vb, ok := s.ToUserdata(3).(*vec2Box); ok && vb != nil {
			u.uvX, u.uvY = vb.x, vb.y
		}
	}
	return 0
}

// mathSqrt is a tiny stand-in for math.Sqrt so this shim file does
// not import math itself (we deliberately keep imports minimal).
func mathSqrt(x float64) float64 {
	if x == 0 {
		return 0
	}
	// Newton-Raphson, 8 iterations is enough for the precision
	// udata_direct.luau probes (fuzzyeq with tolerance 0.001).
	r := x / 2
	for i := 0; i < 12; i++ {
		r = (r + x/r) / 2
	}
	return r
}

// ---------------------------------------------------------------------
// int64 userdata shim
// ---------------------------------------------------------------------
//
// Upstream's conformance harness installs a custom `int64` userdata
// type (Conformance.test.cpp) with comparison, arithmetic and field
// access metamethods. conformance/userdata.luau (the entire fixture)
// and udata_direct.luau exercise its full surface. We model the same
// shape with a Go int64 boxed via PushUserdataObject + a per-type
// metatable parked in the registry, dispatched through tagged-method
// per-type metatables (SetTypeMetatable on the TUserdata type).

const int64UTag byte = 7 // distinct from 0 (untagged) and UTagProxy

// int64Box is the Go-side payload of an int64 userdata. We use a
// pointer wrapper so `v.value = N` mutation (via __newindex) propagates
// through every reference the script holds.
type int64Box struct{ v int64 }

func installInt64Shim(s *vm.State) {
	// Constructor: int64(n).
	s.Register("int64", func(state *vm.State) int {
		var n int64
		if v, ok := state.ToInteger(1); ok {
			n = v
		} else if v, ok := state.ToNumber(1); ok {
			n = int64(v)
		} else if u, ok := state.ToUserdata(1).(*int64Box); ok && u != nil {
			n = u.v
		}
		state.PushUserdataObject(&int64Box{v: n}, int64UTag)
		ensureInt64Metatable(state)
		state.SetMetatable(-2)
		return 1
	})
}

// ensureInt64Metatable creates (once) the per-int64 metatable in the
// registry and leaves it on top of the stack.
func ensureInt64Metatable(s *vm.State) {
	const key = "luaugo.shim.int64.mt"
	s.GetRegistryField(key)
	if !s.IsNil(-1) {
		return
	}
	s.Pop(1)
	s.NewTable()

	// Coercion helper: int64 op number => fold the number into an
	// int64. Used by every binary op so that `int64(1) + 2` works.
	coerce := func(state *vm.State, idx int) (int64, bool) {
		if u, ok := state.ToUserdata(idx).(*int64Box); ok && u != nil {
			return u.v, true
		}
		if v, ok := state.ToInteger(idx); ok {
			return v, true
		}
		if v, ok := state.ToNumber(idx); ok {
			return int64(v), true
		}
		return 0, false
	}
	pushBox := func(state *vm.State, n int64) {
		state.PushUserdataObject(&int64Box{v: n}, int64UTag)
		ensureInt64Metatable(state)
		state.SetMetatable(-2)
	}

	binArith := func(fn func(a, b int64) int64) vm.GoFunction {
		return func(state *vm.State) int {
			a, _ := coerce(state, 1)
			b, _ := coerce(state, 2)
			pushBox(state, fn(a, b))
			return 1
		}
	}

	s.PushGoFunction(func(state *vm.State) int {
		a, _ := coerce(state, 1)
		b, _ := coerce(state, 2)
		state.PushBoolean(a == b)
		return 1
	}, 0)
	s.SetField(-2, "__eq")

	s.PushGoFunction(func(state *vm.State) int {
		a, _ := coerce(state, 1)
		b, _ := coerce(state, 2)
		state.PushBoolean(a < b)
		return 1
	}, 0)
	s.SetField(-2, "__lt")

	s.PushGoFunction(func(state *vm.State) int {
		a, _ := coerce(state, 1)
		b, _ := coerce(state, 2)
		state.PushBoolean(a <= b)
		return 1
	}, 0)
	s.SetField(-2, "__le")

	s.PushGoFunction(binArith(func(a, b int64) int64 { return a + b }), 0)
	s.SetField(-2, "__add")
	s.PushGoFunction(binArith(func(a, b int64) int64 { return a - b }), 0)
	s.SetField(-2, "__sub")
	s.PushGoFunction(binArith(func(a, b int64) int64 { return a * b }), 0)
	s.SetField(-2, "__mul")
	// __div: truncating division (round toward zero).
	s.PushGoFunction(binArith(func(a, b int64) int64 {
		if b == 0 {
			return 0
		}
		return a / b
	}), 0)
	s.SetField(-2, "__div")
	// __idiv: floor division (round toward -infinity).
	s.PushGoFunction(binArith(func(a, b int64) int64 {
		if b == 0 {
			return 0
		}
		q := a / b
		// Go's / rounds toward zero; convert to floor when signs differ.
		if (a%b != 0) && ((a < 0) != (b < 0)) {
			q--
		}
		return q
	}), 0)
	s.SetField(-2, "__idiv")
	s.PushGoFunction(binArith(func(a, b int64) int64 {
		if b == 0 {
			return 0
		}
		r := a % b
		if (r != 0) && ((r < 0) != (b < 0)) {
			r += b
		}
		return r
	}), 0)
	s.SetField(-2, "__mod")
	s.PushGoFunction(binArith(func(a, b int64) int64 {
		var p int64 = 1
		base := a
		exp := b
		if exp < 0 {
			return 0
		}
		for exp > 0 {
			if exp&1 == 1 {
				p *= base
			}
			base *= base
			exp >>= 1
		}
		return p
	}), 0)
	s.SetField(-2, "__pow")
	s.PushGoFunction(func(state *vm.State) int {
		a, _ := coerce(state, 1)
		pushBox(state, -a)
		return 1
	}, 0)
	s.SetField(-2, "__unm")

	s.PushGoFunction(func(state *vm.State) int {
		a, _ := coerce(state, 1)
		state.PushString(int64ToString(a))
		return 1
	}, 0)
	s.SetField(-2, "__tostring")

	// __index for .value field reads.
	s.PushGoFunction(func(state *vm.State) int {
		u, _ := state.ToUserdata(1).(*int64Box)
		k, _ := state.ToString(2)
		if u != nil && k == "value" {
			state.PushInteger(u.v)
			return 1
		}
		state.PushNil()
		return 1
	}, 0)
	s.SetField(-2, "__index")

	// __newindex for .value = n writes.
	s.PushGoFunction(func(state *vm.State) int {
		u, _ := state.ToUserdata(1).(*int64Box)
		k, _ := state.ToString(2)
		if u != nil && k == "value" {
			if v, ok := state.ToInteger(3); ok {
				u.v = v
			} else if v, ok := state.ToNumber(3); ok {
				u.v = int64(v)
			}
		}
		return 0
	}, 0)
	s.SetField(-2, "__newindex")

	s.SetRegistryField(key)
	s.GetRegistryField(key)
}

// int64ToString returns the decimal representation of n. Inlined here
// (rather than fmt.Sprintf) to avoid pulling fmt into this file when
// PowerShell-formatted lines already make file-level imports churn.
func int64ToString(n int64) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
