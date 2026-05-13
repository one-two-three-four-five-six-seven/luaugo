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
// Each builtin returns the number of results pushed at `resBase`, or
// -1 if the fast path could not handle the inputs (in which case the
// caller falls back to the slow Lua-callable form).
//
// In Tier 3 the FASTCALL opcode is followed by a regular CALL that
// invokes the same builtin via its normal Lua-callable form (which
// Tier 4 supplies). We therefore implement just enough builtins to
// power tests that don't have Tier-4 yet.

// invokeBuiltin attempts to invoke builtin id with `nargs` from
// regBase..regBase+nargs-1 and store nresults at regBase. Returns true
// if handled; false to fall back.
func invokeBuiltin(L *stateImpl, id common.Builtin, regBase, nargs, nresults int) bool {
	switch id {
	case common.BuiltinMathAbs:
		if nargs != 1 || nresults < 1 {
			return false
		}
		v, ok := L.stack[regBase].asNumber()
		if !ok {
			return false
		}
		L.stack[regBase] = numberValue(math.Abs(v))
		return true
	case common.BuiltinMathFloor:
		if nargs != 1 || nresults < 1 {
			return false
		}
		v, ok := L.stack[regBase].asNumber()
		if !ok {
			return false
		}
		L.stack[regBase] = numberValue(math.Floor(v))
		return true
	case common.BuiltinMathCeil:
		if nargs != 1 || nresults < 1 {
			return false
		}
		v, ok := L.stack[regBase].asNumber()
		if !ok {
			return false
		}
		L.stack[regBase] = numberValue(math.Ceil(v))
		return true
	case common.BuiltinMathSqrt:
		if nargs != 1 || nresults < 1 {
			return false
		}
		v, ok := L.stack[regBase].asNumber()
		if !ok {
			return false
		}
		L.stack[regBase] = numberValue(math.Sqrt(v))
		return true
	case common.BuiltinType:
		if nargs != 1 || nresults < 1 {
			return false
		}
		t := L.stack[regBase].tag
		L.stack[regBase] = stringValue(L.gs.intern(t.String()))
		return true
	case common.BuiltinTypeof:
		if nargs != 1 || nresults < 1 {
			return false
		}
		// Same as type for builtin types; userdata may carry custom name.
		t := L.stack[regBase].tag
		L.stack[regBase] = stringValue(L.gs.intern(t.String()))
		return true
	}
	return false
}
