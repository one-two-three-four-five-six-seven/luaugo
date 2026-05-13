// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package compiler

import "github.com/luaugo/luaugo/internal/common"

// builtins.go maps function names like "math.abs" to common.Builtin ids
// used by FASTCALL opcodes. The current compiler does NOT emit FASTCALL
// substitutions; this table is provided for a future optimization pass.
//
// The mapping mirrors BUILTIN_TABLE in upstream Compiler/src/Builtins.cpp.
//
// IMPORTANT: omitting FASTCALL never affects correctness. The VM falls
// back to the GETIMPORT+CALL path which produces identical observable
// behavior, just slightly slower for hot loops.

// lookupBuiltin resolves a global-prefixed call to a builtin id, or
// BuiltinNone if the call is not a known fast-callable builtin.
func lookupBuiltin(prefix, name string) common.Builtin {
	switch prefix {
	case "":
		switch name {
		case "assert":
			return common.BuiltinAssert
		case "type":
			return common.BuiltinType
		case "typeof":
			return common.BuiltinTypeof
		case "rawset":
			return common.BuiltinRawSet
		case "rawget":
			return common.BuiltinRawGet
		case "rawequal":
			return common.BuiltinRawEqual
		case "rawlen":
			return common.BuiltinRawLen
		case "getmetatable":
			return common.BuiltinGetMetatable
		case "setmetatable":
			return common.BuiltinSetMetatable
		case "tonumber":
			return common.BuiltinToNumber
		case "tostring":
			return common.BuiltinToString
		case "select":
			return common.BuiltinSelectVararg
		}
	case "math":
		return mathBuiltin(name)
	case "bit32":
		return bit32Builtin(name)
	case "string":
		return stringBuiltin(name)
	case "table":
		return tableBuiltin(name)
	}
	return common.BuiltinNone
}

func mathBuiltin(name string) common.Builtin {
	switch name {
	case "abs":
		return common.BuiltinMathAbs
	case "acos":
		return common.BuiltinMathAcos
	case "asin":
		return common.BuiltinMathAsin
	case "atan2":
		return common.BuiltinMathAtan2
	case "atan":
		return common.BuiltinMathAtan
	case "ceil":
		return common.BuiltinMathCeil
	case "cosh":
		return common.BuiltinMathCosh
	case "cos":
		return common.BuiltinMathCos
	case "deg":
		return common.BuiltinMathDeg
	case "exp":
		return common.BuiltinMathExp
	case "floor":
		return common.BuiltinMathFloor
	case "fmod":
		return common.BuiltinMathFmod
	case "frexp":
		return common.BuiltinMathFrexp
	case "ldexp":
		return common.BuiltinMathLdexp
	case "log10":
		return common.BuiltinMathLog10
	case "log":
		return common.BuiltinMathLog
	case "max":
		return common.BuiltinMathMax
	case "min":
		return common.BuiltinMathMin
	case "modf":
		return common.BuiltinMathModf
	case "pow":
		return common.BuiltinMathPow
	case "rad":
		return common.BuiltinMathRad
	case "sinh":
		return common.BuiltinMathSinh
	case "sin":
		return common.BuiltinMathSin
	case "sqrt":
		return common.BuiltinMathSqrt
	case "tanh":
		return common.BuiltinMathTanh
	case "tan":
		return common.BuiltinMathTan
	case "clamp":
		return common.BuiltinMathClamp
	case "sign":
		return common.BuiltinMathSign
	case "round":
		return common.BuiltinMathRound
	case "lerp":
		return common.BuiltinMathLerp
	}
	return common.BuiltinNone
}

func bit32Builtin(name string) common.Builtin {
	switch name {
	case "arshift":
		return common.BuiltinBit32ArShift
	case "band":
		return common.BuiltinBit32Band
	case "bnot":
		return common.BuiltinBit32Bnot
	case "bor":
		return common.BuiltinBit32Bor
	case "bxor":
		return common.BuiltinBit32Bxor
	case "btest":
		return common.BuiltinBit32Btest
	case "extract":
		return common.BuiltinBit32Extract
	case "lrotate":
		return common.BuiltinBit32LRotate
	case "lshift":
		return common.BuiltinBit32LShift
	case "replace":
		return common.BuiltinBit32Replace
	case "rrotate":
		return common.BuiltinBit32RRotate
	case "rshift":
		return common.BuiltinBit32RShift
	case "countlz":
		return common.BuiltinBit32CountLz
	case "countrz":
		return common.BuiltinBit32CountRz
	case "byteswap":
		return common.BuiltinBit32ByteSwap
	}
	return common.BuiltinNone
}

func stringBuiltin(name string) common.Builtin {
	switch name {
	case "byte":
		return common.BuiltinStringByte
	case "char":
		return common.BuiltinStringChar
	case "len":
		return common.BuiltinStringLen
	case "sub":
		return common.BuiltinStringSub
	}
	return common.BuiltinNone
}

func tableBuiltin(name string) common.Builtin {
	switch name {
	case "insert":
		return common.BuiltinTableInsert
	case "unpack":
		return common.BuiltinTableUnpack
	}
	return common.BuiltinNone
}
