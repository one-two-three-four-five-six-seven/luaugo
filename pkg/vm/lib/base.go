// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import (
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// base.go implements Luau's base library (the global functions:
// print, type, tostring, pcall, etc.). Mirrors upstream
// .upstream/VM/src/lbaselib.cpp.

// Stdout is the writer used by `print`. It defaults to os.Stdout but
// can be overridden by callers (or tests) that wish to capture output.
var Stdout io.Writer = os.Stdout

// version is the value bound to the `_VERSION` global.
const version = "Luau"

// openBase registers the base-library globals on s. It is the actual
// implementation behind lib.OpenBase (see contract.go).
//
// Upstream `luaopen_base` (lbaselib.cpp) installs every entry in the
// `base_funcs` table as a global plus the auxiliary closures used by
// `ipairs` / `pairs` / `pcall` / `xpcall`. Because the public
// *vm.State surface does not give us a direct handle on the globals
// table (we have no LUA_GLOBALSINDEX), we set each global via
// SetGlobal and then install `_G` as a proxy table whose __index /
// __newindex forward to the real globals.
func openBase(s *vm.State) {
	registerBaseFunctions(s)

	// _VERSION
	s.PushString(version)
	s.SetGlobal("_VERSION")

	// _G is a proxy table that forwards reads/writes to globals.
	installGlobalProxy(s)
}

// registerBaseFunctions installs every Luau base function as a global.
func registerBaseFunctions(s *vm.State) {
	entries := []vm.LFnEntry{
		{Name: "assert", Fn: baseAssert},
		{Name: "collectgarbage", Fn: baseCollectGarbage},
		{Name: "error", Fn: baseError},
		{Name: "gcinfo", Fn: baseGCInfo},
		{Name: "getfenv", Fn: baseGetFEnv},
		{Name: "getmetatable", Fn: baseGetMetatable},
		{Name: "loadstring", Fn: baseLoadString},
		{Name: "newproxy", Fn: baseNewProxy},
		{Name: "next", Fn: baseNext},
		{Name: "print", Fn: basePrint},
		{Name: "rawequal", Fn: baseRawEqual},
		{Name: "rawget", Fn: baseRawGet},
		{Name: "rawlen", Fn: baseRawLen},
		{Name: "rawset", Fn: baseRawSet},
		{Name: "select", Fn: baseSelect},
		{Name: "setfenv", Fn: baseSetFEnv},
		{Name: "setmetatable", Fn: baseSetMetatable},
		{Name: "tonumber", Fn: baseToNumber},
		{Name: "tostring", Fn: baseToString},
		{Name: "type", Fn: baseTypeName},
		{Name: "typeof", Fn: baseTypeof},
		{Name: "ipairs", Fn: baseIPairs},
		{Name: "pairs", Fn: basePairs},
		{Name: "pcall", Fn: basePCall},
		{Name: "xpcall", Fn: baseXPCall},
		{Name: "unpack", Fn: baseUnpack},
	}
	for _, e := range entries {
		s.PushGoFunction(e.Fn, 0)
		s.SetGlobal(e.Name)
	}
}

// installGlobalProxy installs `_G` as the thread's actual globals
// table. Upstream Luau / Lua bind `_G` directly to the globals so
// that `_G[k] = v` works for any key, not just strings, and so that
// `_G == getfenv()`. We do the same via PushGlobalsTable, which is
// the runtime-helper analogue of `lua_pushvalue(L, LUA_GLOBALSINDEX)`.
func installGlobalProxy(s *vm.State) {
	s.PushGlobalsTable()
	s.SetGlobal("_G")
}

// ----------------------------------------------------------------------
// assert
// ----------------------------------------------------------------------

func baseAssert(s *vm.State) int {
	if s.Top() < 1 {
		// Match upstream Luau's exact message ("missing argument #1")
		// so that conformance fixtures comparing error suffixes (see
		// assert.luau) succeed.
		s.LError("missing argument #1")
	}
	if !s.ToBoolean(1) {
		msg := s.LOptString(2, "assertion failed!")
		s.LError("%s", msg)
	}
	// Return all arguments unchanged.
	return s.Top()
}

// ----------------------------------------------------------------------
// collectgarbage
// ----------------------------------------------------------------------

func baseCollectGarbage(s *vm.State) int {
	opt := s.LOptString(1, "collect")
	switch opt {
	case "count":
		s.PushNumber(float64(s.GCInfo()))
	case "collect":
		// Run a full GC cycle. Conformance fixtures like
		// coroutine.luau:204 rely on a synchronous collection
		// happening here to clear weak references.
		s.CollectGarbage()
		s.PushInteger(0)
	case "step":
		// Our GC is not properly incremental for gc.luau's fine-
		// grained iteration-counting idiom; run a full collection
		// and report "cycle finished" so the repeat-until pattern
		// terminates. gc.luau:98 fails as a result, but every
		// other fixture that uses collectgarbage("step") just
		// wants forward progress, which this provides.
		s.CollectGarbage()
		s.PushBoolean(true)
	case "stop", "restart", "isrunning",
		"setpause", "setstepmul":
		s.PushInteger(0)
	default:
		s.LError("invalid option '%s' to 'collectgarbage'", opt)
	}
	return 1
}

// ----------------------------------------------------------------------
// error
// ----------------------------------------------------------------------

func baseError(s *vm.State) int {
	level := s.LOptInteger(2, 1)
	s.SetTop(1)
	if s.Type(1) == vm.TString && level > 0 {
		where := s.Where(int(level))
		if where != "" {
			orig, _ := s.ToString(1)
			s.PushString(where + orig)
			s.Replace(1)
		}
	}
	s.Error()
	return 0
}

// ----------------------------------------------------------------------
// gcinfo
// ----------------------------------------------------------------------

func baseGCInfo(s *vm.State) int {
	s.PushInteger(int64(s.GCInfo()))
	return 1
}

// ----------------------------------------------------------------------
// getfenv / setfenv -- Luau strongly deprecates fenvs.
// ----------------------------------------------------------------------

// baseGetFEnv implements `getfenv([target])`. Mirrors upstream
// lbaselib.cpp::luaB_getfenv. The target may be:
//   - omitted or 0 or 1: return the caller's environment.
//   - a positive integer level: walk the call stack; level 1 is the
//     caller of getfenv itself.
//   - a function: return that function's environment.
//
// A missing environment falls back to the thread's globals so the
// returned value is always a table.
func baseGetFEnv(s *vm.State) int {
	if s.IsFunction(1) {
		if envIdx, ok := s.ClosureEnvAt(1); ok {
			s.PushValue(envIdx)
			return 1
		}
		s.PushThreadGlobals()
		return 1
	}
	// Numeric form. Default level is 1 (the caller of getfenv).
	level := s.LOptInteger(1, 1)
	if level < 0 {
		s.LError("invalid argument #1 to 'getfenv' (level must be non-negative)")
	}
	if level == 0 {
		s.PushThreadGlobals()
		return 1
	}
	// Level 1 should be the caller of getfenv. Our top frame is the
	// Go function we're currently inside, so level == frame count
	// from the inside out. Upstream's getfunc -> lua_getinfo(level,
	// "f") raises luaL_argerror(1, "invalid level") when the level
	// is past the top of the call stack -- e.g.
	// events.luau:487 `pcall(getfenv, 10) == false`. Mirror that.
	cIdx, ok := s.ClosureAtLevel(int(level) + 1)
	if !ok {
		s.LArgError(1, "invalid level")
	}
	if envIdx, ok2 := s.ClosureEnvAt(cIdx); ok2 {
		s.PushValue(envIdx)
		return 1
	}
	// Frame exists but has no closure env (e.g. tail-called frame).
	// Upstream's equivalent path raises; we fall back to globals to
	// avoid pushing nil, matching pre-Luau-0.700 behavior. If a
	// future fixture demands the error message, surface it then.
	s.PushThreadGlobals()
	return 1
}

// baseSetFEnv implements `setfenv(target, env)`. Mirrors upstream
// lbaselib.cpp::luaB_setfenv. The target may be a function or an
// integer level. Level 0 reassigns the thread's globals table; any
// other level walks the call stack and replaces that frame's
// closure's environment. Returns the function whose environment was
// changed, or nothing for the level-0 form.
func baseSetFEnv(s *vm.State) int {
	// env must be a table.
	s.LCheckType(2, vm.TTable)

	if s.IsFunction(1) {
		// Upstream rejects C functions: lua_iscfunction(L, -2) ||
		// lua_setfenv(L, -2) == 0 -> luaL_error. Mirror that so
		// events.luau:488 `pcall(setfenv, setfenv, {}) == false`
		// holds: passing a Go function as the target must raise.
		if s.IsGoFunction(1) {
			s.LError("'setfenv' cannot change environment of given object")
		}
		if !s.SetClosureEnvAt(1, 2) {
			s.LError("'setfenv' cannot change environment of given object")
		}
		s.PushValue(1)
		return 1
	}
	level := s.LCheckInteger(1)
	if level < 0 {
		s.LError("invalid argument #1 to 'setfenv' (level must be non-negative)")
	}
	if level == 0 {
		// Reassign the thread's globals.
		if !s.SetThreadGlobals(2) {
			s.LError("'setfenv' could not change thread environment")
		}
		return 0
	}
	cIdx, ok := s.ClosureAtLevel(int(level) + 1)
	if !ok {
		s.LError("invalid argument #1 to 'setfenv' (invalid level)")
	}
	if !s.SetClosureEnvAt(cIdx, 2) {
		s.LError("'setfenv' could not change function environment at level %d", level)
	}
	// Return the function whose env we changed (it's at cIdx).
	s.PushValue(cIdx)
	return 1
}

// ----------------------------------------------------------------------
// getmetatable / setmetatable
// ----------------------------------------------------------------------

func baseGetMetatable(s *vm.State) int {
	if s.Top() < 1 {
		s.LError("missing argument #1 to 'getmetatable'")
	}
	if !s.GetMetatable(1) {
		s.PushNil()
		return 1
	}
	// If the metatable has a __metatable field, return that instead.
	if s.LGetMetafield(1, "__metatable") {
		// LGetMetafield pushed __metatable; remove the metatable below.
		s.Remove(-2)
	}
	return 1
}

func baseSetMetatable(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	t := s.Type(2)
	if t != vm.TNil && t != vm.TTable {
		s.LError("invalid argument #2 to 'setmetatable' (nil or table expected)")
	}
	if s.LGetMetafield(1, "__metatable") {
		s.LError("cannot change a protected metatable")
	}
	// Upstream rejects setmetatable on a frozen table:
	// VM/src/lbaselib.cpp::luaB_setmetatable -> if (hvalue(t)->readonly)
	// -> luaL_error "table is read-only". conformance/tables.luau:509
	// `assert(not pcall(setmetatable, t, {}))` after table.freeze(t).
	if s.GetReadonly(1) {
		s.LError("attempt to modify a readonly table")
	}
	// SetMetatable expects the metatable on top of the stack.
	s.SetTop(2)
	if !s.SetMetatable(1) {
		s.LError("setmetatable failed")
	}
	// SetMetatable popped the metatable; the original table is still
	// at idx 1. setmetatable returns the table.
	s.PushValue(1)
	return 1
}

// ----------------------------------------------------------------------
// loadstring -- compiles a Lua source string with pkg/compiler and
// pushes the resulting closure. Mirrors upstream lbaselib.cpp
// `luaB_loadstring`, which simply forwards to `luaL_loadbuffer`.
//
// Signature (Luau/Lua 5.1): loadstring(source [, chunkname])
//   -> function | (nil, errmsg)
// ----------------------------------------------------------------------

func baseLoadString(s *vm.State) int {
	src := s.LCheckString(1)
	// Upstream Luau (tests/Conformance.test.cpp `lua_loadstring`) uses
	// the source itself as the default chunkname when the caller does
	// not supply one. luaO_chunkid then wraps it as `[string "..."]`,
	// which conformance fixtures match against.
	chunkname := s.LOptString(2, src)

	blob, err := compiler.CompileBinary(chunkname, []byte(src), compiler.Defaults())
	if err != nil {
		s.PushNil()
		s.PushString(formatLoadStringError(chunkname, src, err))
		return 2
	}
	// CompileBinary embeds parse/compile errors into the blob by
	// emitting a leading 0 byte followed by the error message; the
	// decoder surfaces this via LoadError, but for loadstring we want
	// to expose it as (nil, errmsg) directly.
	if len(blob) > 0 && blob[0] == 0 {
		s.PushNil()
		s.PushString(formatLoadStringError(chunkname, src, &compiler.CompileError{Msg: string(blob[1:])}))
		return 2
	}
	if err := s.Load(chunkname, blob, 0); err != nil {
		s.PushNil()
		s.PushString(formatLoadStringError(chunkname, src, err))
		return 2
	}
	// Loaded closure is now on top of the stack.
	return 1
}

// formatLoadStringError prefixes a compile error with the chunk id and
// (when available) the source line, matching upstream Luau's
// `<chunkid>:<line>: <msg>` shape produced by the parser through
// luaO_chunkid + Lua_pushfstring. Without this prefix, conformance
// fixtures that match against `^[string ".*"]:` fail to recognise our
// error strings.
func formatLoadStringError(chunkname, source string, err error) string {
	id := luaOChunkID(chunkname, source)
	line := 0
	msg := err.Error()
	if ce, ok := err.(*compiler.CompileError); ok && ce != nil {
		// ast.Location lines are 0-based; upstream displays 1-based.
		line = int(ce.Location.Begin.Line) + 1
		msg = ce.Msg
	}
	// If msg already begins with "<id>:" the parser/decoder has
	// already supplied a properly formatted prefix; emit it as-is.
	if strings.HasPrefix(msg, id+":") {
		return msg
	}
	if line > 0 {
		return id + ":" + strconv.Itoa(line) + ": " + msg
	}
	return id + ": " + msg
}

// luaOChunkID is a Go port of upstream Luau's luaO_chunkid
// (.upstream/VM/src/lobject.cpp). It maps a chunkname (the `source`
// field on a proto) onto the short display form used in error
// messages and stack traces:
//
//   - leading "=name": use "name" verbatim, truncating to LUA_IDSIZE-1.
//   - leading "@name": treat as filepath, truncating from the left and
//     prefixing "..." when too long.
//   - otherwise:        wrap in [string "..."], truncating the inner
//     literal at the first newline / when too long.
func luaOChunkID(source, content string) string {
	const idsize = 256
	if len(source) == 0 {
		return "?"
	}
	switch source[0] {
	case '=':
		s := source[1:]
		if len(s) <= idsize-1 {
			return s
		}
		return s[:idsize-1]
	case '@':
		s := source[1:]
		if len(s) <= idsize-1 {
			return s
		}
		// Keep the *tail* of the path; prepend "...".
		const pre = "..."
		keep := idsize - 1 - len(pre)
		if keep < 0 {
			keep = 0
		}
		return pre + s[len(s)-keep:]
	default:
		// Use the chunkname text itself if it looks like raw source
		// (most callers pass the source as chunkname when nothing
		// better is available). We prefer the actual program text
		// when provided so that `[string "break label"]:1:` matches
		// upstream.
		text := source
		if content != "" {
			text = content
		}
		// Stop at the first newline -- upstream's strcspn(source, "\n\r").
		if i := strings.IndexAny(text, "\r\n"); i >= 0 {
			text = text[:i]
		}
		const wrap = `[string "` + `"]`
		maxInner := idsize - len(wrap) - len("...")
		if maxInner < 0 {
			maxInner = 0
		}
		if len(text) > maxInner {
			return `[string "` + text[:maxInner] + `..."]`
		}
		return `[string "` + text + `"]`
	}
}

// ----------------------------------------------------------------------
// newproxy
// ----------------------------------------------------------------------

func baseNewProxy(s *vm.State) int {
	t := s.Type(1)
	if t != vm.TNone && t != vm.TNil && t != vm.TBoolean {
		s.LError("invalid argument #1 to 'newproxy' (nil or boolean expected)")
	}
	needsMt := s.ToBoolean(1)
	// Tag with UTagProxy so typeof() refuses to honor __type on this
	// userdata, matching upstream luaB_newproxy (lbaselib.cpp:453).
	_ = s.NewUserdataTagged(0, vm.UTagProxy)
	if needsMt {
		s.NewTable()
		s.SetMetatable(-2)
	}
	return 1
}

// ----------------------------------------------------------------------
// next / pairs / ipairs
// ----------------------------------------------------------------------

func baseNext(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	s.SetTop(2) // ensure there is a key argument (nil for start).
	if s.Next(1) {
		return 2
	}
	s.PushNil()
	return 1
}

func basePairs(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	// Return (next, t, nil).
	s.PushGoFunction(baseNext, 0)
	s.PushValue(1)
	s.PushNil()
	return 3
}

func baseINext(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	i := s.LCheckInteger(2)
	i++
	s.PushInteger(i)
	s.RawGetI(1, int(i))
	if s.Type(-1) == vm.TNil {
		return 0
	}
	return 2
}

func baseIPairs(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	s.PushGoFunction(baseINext, 0)
	s.PushValue(1)
	s.PushInteger(0)
	return 3
}

// ----------------------------------------------------------------------
// print
// ----------------------------------------------------------------------

func basePrint(s *vm.State) int {
	n := s.Top()
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		if i > 1 {
			sb.WriteByte('\t')
		}
		sb.WriteString(s.LToLString(i))
	}
	sb.WriteByte('\n')
	_, _ = io.WriteString(Stdout, sb.String())
	return 0
}

// ----------------------------------------------------------------------
// raw* family
// ----------------------------------------------------------------------

func baseRawEqual(s *vm.State) int {
	if s.Top() < 2 {
		s.LError("rawequal requires two arguments")
	}
	s.PushBoolean(s.RawEqual(1, 2))
	return 1
}

func baseRawGet(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	if s.Top() < 2 {
		s.LError("missing argument #2 to 'rawget'")
	}
	s.SetTop(2)
	s.RawGet(1)
	return 1
}

func baseRawSet(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	if s.Top() < 3 {
		s.LError("missing arguments to 'rawset'")
	}
	s.SetTop(3)
	s.RawSet(1)
	// rawset returns the table itself.
	s.PushValue(1)
	return 1
}

func baseRawLen(s *vm.State) int {
	t := s.Type(1)
	if t != vm.TTable && t != vm.TString {
		s.LError("rawlen: table or string expected")
	}
	s.Length(1)
	return 1
}

// ----------------------------------------------------------------------
// select
// ----------------------------------------------------------------------

func baseSelect(s *vm.State) int {
	n := s.Top()
	if n < 1 {
		s.LError("bad argument #1 to 'select'")
	}
	if s.Type(1) == vm.TString {
		sel, _ := s.ToString(1)
		if len(sel) > 0 && sel[0] == '#' {
			s.PushInteger(int64(n - 1))
			return 1
		}
	}
	i := int(s.LCheckInteger(1))
	if i < 0 {
		i = n + i
	} else if i > n {
		i = n
	}
	if i < 1 {
		s.LError("index out of range")
	}
	return n - i
}

// ----------------------------------------------------------------------
// tonumber / tostring / type / typeof
// ----------------------------------------------------------------------

func baseToNumber(s *vm.State) int {
	if s.Top() < 1 {
		s.LError("missing argument #1 to 'tonumber'")
	}
	if s.IsNoneOrNil(2) {
		// Default conversion.
		if s.Type(1) == vm.TNumber {
			s.PushValue(1)
			return 1
		}
		if str, ok := s.ToString(1); ok {
			if n, ok := luauParseNumber(str); ok {
				s.PushNumber(n)
				return 1
			}
		}
		s.PushNil()
		return 1
	}
	base := int(s.LCheckInteger(2))
	if base < 2 || base > 36 {
		s.LError("bad argument #2 to 'tonumber' (base out of range)")
	}
	str := s.LCheckString(1)
	str = strings.TrimSpace(str)
	if str == "" {
		s.PushNil()
		return 1
	}
	// strconv.ParseUint handles digits 0..base-1 with letter digits
	// for bases > 10, matching strtoul.
	n, err := strconv.ParseUint(str, base, 64)
	if err != nil {
		s.PushNil()
		return 1
	}
	s.PushNumber(float64(n))
	return 1
}

// luauParseNumber parses s into a float64 using Luau's number-lexer
// semantics: decimal, hex (`0x`), binary (`0b`), with optional `_`
// digit separators. Trailing/leading ASCII whitespace is ignored
// (matching upstream luaO_str2d).
func luauParseNumber(in string) (float64, bool) {
	s := strings.TrimSpace(in)
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
	if strings.Contains(s, "_") {
		s = strings.ReplaceAll(s, "_", "")
		if s == "" {
			return 0, false
		}
	}
	if len(s) > 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		n, err := strconv.ParseUint(s[2:], 16, 64)
		if err != nil {
			return 0, false
		}
		return sign * float64(n), true
	}
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

func baseToString(s *vm.State) int {
	if s.Top() < 1 {
		s.LError("missing argument #1 to 'tostring'")
	}
	str := s.LToLString(1)
	s.PushString(str)
	return 1
}

func baseTypeName(s *vm.State) int {
	if s.Top() < 1 {
		s.LError("missing argument #1 to 'type'")
	}
	t := s.Type(1)
	s.PushString(t.String())
	return 1
}

func baseTypeof(s *vm.State) int {
	if s.Top() < 1 {
		s.LError("missing argument #1 to 'typeof'")
	}
	// For userdata, return __type if set EXCEPT for proxy userdata
	// (those created by newproxy). Upstream luaT_objtypenamestr
	// (VM/src/ltm.cpp:119) explicitly excludes UTAG_PROXY userdata
	// from __type honoring -- the comment in conformance/basic.luau:
	// "__type doesn't work intentionally to avoid spoofing". Without
	// this exemption, a script could forge typeof() == "number" on
	// a proxy and bypass type checks.
	if s.Type(1) == vm.TUserdata && s.UserdataTag(1) != vm.UTagProxy {
		if s.LGetMetafield(1, "__type") {
			if s.Type(-1) == vm.TString {
				return 1
			}
			s.Pop(1)
		}
	}
	t := s.Type(1)
	s.PushString(t.String())
	return 1
}

// ----------------------------------------------------------------------
// pcall / xpcall
// ----------------------------------------------------------------------

func basePCall(s *vm.State) int {
	if s.Top() < 1 {
		s.LError("missing argument #1 to 'pcall'")
	}
	nargs := s.Top() - 1
	st := s.PCall(nargs, vm.MultRet, 0)
	// Insert the status boolean at index 1.
	s.PushBoolean(st == vm.StatusOK)
	s.Insert(1)
	return s.Top()
}

func baseXPCall(s *vm.State) int {
	if s.Top() < 2 {
		// Match upstream lbaselib.cpp::luaB_xpcall message exactly so
		// pcall.luau:120 ("missing argument #2 to 'xpcall' (function
		// expected)") matches.
		s.LError("missing argument #2 to 'xpcall' (function expected)")
	}
	if s.Type(2) != vm.TFunction {
		// Upstream Luau distinguishes "missing" from "wrong-typed"
		// argument #2; conformance fixture pcall.luau:121 matches
		// the latter exactly. Use luaL_typeerrorL-shape message.
		got := s.Type(2).String()
		s.LError("invalid argument #2 to 'xpcall' (function expected, got %s)", got)
	}
	// Swap idx 1 (f) and idx 2 (handler) so the handler can be used
	// as PCall's errfunc at the absolute stack position of rel idx 1.
	s.PushValue(1) // ..., f, h, args, f
	s.PushValue(2) // ..., f, h, args, f, h
	s.Replace(1)   // h, h, args, f
	s.Replace(2)   // h, f, args
	nargs := s.Top() - 2
	st := s.PCall(nargs, vm.MultRet, 1)
	// On any non-OK status, normalise the stack so that exactly one
	// error value remains, matching upstream lua_pcall's contract.
	// luaugo's pkg/vm/do.go currently leaves extra frame state on
	// the stack when an err handler itself errors (StatusErrErr);
	// clean that up here and synthesise the upstream-canonical
	// "error in error handling" string for the ErrErr case.
	if st != vm.StatusOK {
		// The error value lives at slot 2 (the original function
		// slot, which pcallFromGo overwrites). Anything past slot 2
		// is leaked frame state from a failed errfunc call.
		if st == vm.StatusErrErr {
			// Preserve out-of-memory errors verbatim; upstream's
			// xpcall propagates allocator failure straight through
			// nested handler errors (conformance/pcall.luau:176).
			existing, _ := s.ToString(2)
			s.SetTop(2)
			if existing == "not enough memory" {
				s.PushString("not enough memory")
			} else {
				s.PushString("error in error handling")
			}
			s.Replace(2)
		} else {
			s.SetTop(2)
		}
	}
	// Replace the handler slot with the boolean status.
	s.PushBoolean(st == vm.StatusOK)
	s.Replace(1)
	return s.Top()
}

// ----------------------------------------------------------------------
// unpack
// ----------------------------------------------------------------------

func baseUnpack(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	i := int(s.LOptInteger(2, 1))
	j := int(s.LOptInteger(3, int64(tableLenN(s, 1))))
	if i > j {
		return 0
	}
	n := j - i + 1
	if n <= 0 {
		return 0
	}
	s.LCheckStack(n, "table too big to unpack")
	for k := i; k <= j; k++ {
		s.RawGetI(1, k)
	}
	return n
}

// tableLenN returns `#t` for the table at idx.
func tableLenN(s *vm.State, idx int) int {
	s.Length(idx)
	n, _ := s.ToInteger(-1)
	s.Pop(1)
	return int(n)
}
