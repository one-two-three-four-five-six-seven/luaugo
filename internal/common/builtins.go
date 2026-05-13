// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package common

// Builtin identifies a built-in function for the LOP_FASTCALL family of
// instructions. Values are part of the bytecode format and must match
// upstream Bytecode.h enum LuauBuiltinFunction exactly.
type Builtin uint16

// Builtin function ids. Order is fixed by upstream Bytecode.h.
const (
	BuiltinNone Builtin = 0

	BuiltinAssert Builtin = 1

	BuiltinMathAbs   Builtin = 2
	BuiltinMathAcos  Builtin = 3
	BuiltinMathAsin  Builtin = 4
	BuiltinMathAtan2 Builtin = 5
	BuiltinMathAtan  Builtin = 6
	BuiltinMathCeil  Builtin = 7
	BuiltinMathCosh  Builtin = 8
	BuiltinMathCos   Builtin = 9
	BuiltinMathDeg   Builtin = 10
	BuiltinMathExp   Builtin = 11
	BuiltinMathFloor Builtin = 12
	BuiltinMathFmod  Builtin = 13
	BuiltinMathFrexp Builtin = 14
	BuiltinMathLdexp Builtin = 15
	BuiltinMathLog10 Builtin = 16
	BuiltinMathLog   Builtin = 17
	BuiltinMathMax   Builtin = 18
	BuiltinMathMin   Builtin = 19
	BuiltinMathModf  Builtin = 20
	BuiltinMathPow   Builtin = 21
	BuiltinMathRad   Builtin = 22
	BuiltinMathSinh  Builtin = 23
	BuiltinMathSin   Builtin = 24
	BuiltinMathSqrt  Builtin = 25
	BuiltinMathTanh  Builtin = 26
	BuiltinMathTan   Builtin = 27

	BuiltinBit32ArShift Builtin = 28
	BuiltinBit32Band    Builtin = 29
	BuiltinBit32Bnot    Builtin = 30
	BuiltinBit32Bor     Builtin = 31
	BuiltinBit32Bxor    Builtin = 32
	BuiltinBit32Btest   Builtin = 33
	BuiltinBit32Extract Builtin = 34
	BuiltinBit32LRotate Builtin = 35
	BuiltinBit32LShift  Builtin = 36
	BuiltinBit32Replace Builtin = 37
	BuiltinBit32RRotate Builtin = 38
	BuiltinBit32RShift  Builtin = 39

	BuiltinType Builtin = 40

	BuiltinStringByte Builtin = 41
	BuiltinStringChar Builtin = 42
	BuiltinStringLen  Builtin = 43

	BuiltinTypeof Builtin = 44

	BuiltinStringSub Builtin = 45

	BuiltinMathClamp Builtin = 46
	BuiltinMathSign  Builtin = 47
	BuiltinMathRound Builtin = 48

	BuiltinRawSet   Builtin = 49
	BuiltinRawGet   Builtin = 50
	BuiltinRawEqual Builtin = 51

	BuiltinTableInsert Builtin = 52
	BuiltinTableUnpack Builtin = 53

	BuiltinVector Builtin = 54

	BuiltinBit32CountLz Builtin = 55
	BuiltinBit32CountRz Builtin = 56

	BuiltinSelectVararg Builtin = 57

	BuiltinRawLen Builtin = 58

	BuiltinBit32ExtractK Builtin = 59

	BuiltinGetMetatable Builtin = 60
	BuiltinSetMetatable Builtin = 61

	BuiltinToNumber Builtin = 62
	BuiltinToString Builtin = 63

	BuiltinBit32ByteSwap Builtin = 64

	BuiltinBufferReadI8   Builtin = 65
	BuiltinBufferReadU8   Builtin = 66
	BuiltinBufferWriteU8  Builtin = 67
	BuiltinBufferReadI16  Builtin = 68
	BuiltinBufferReadU16  Builtin = 69
	BuiltinBufferWriteU16 Builtin = 70
	BuiltinBufferReadI32  Builtin = 71
	BuiltinBufferReadU32  Builtin = 72
	BuiltinBufferWriteU32 Builtin = 73
	BuiltinBufferReadF32  Builtin = 74
	BuiltinBufferWriteF32 Builtin = 75
	BuiltinBufferReadF64  Builtin = 76
	BuiltinBufferWriteF64 Builtin = 77

	BuiltinVectorMagnitude Builtin = 78
	BuiltinVectorNormalize Builtin = 79
	BuiltinVectorCross     Builtin = 80
	BuiltinVectorDot       Builtin = 81
	BuiltinVectorFloor     Builtin = 82
	BuiltinVectorCeil      Builtin = 83
	BuiltinVectorAbs       Builtin = 84
	BuiltinVectorSign      Builtin = 85
	BuiltinVectorClamp     Builtin = 86
	BuiltinVectorMin       Builtin = 87
	BuiltinVectorMax       Builtin = 88

	BuiltinMathLerp   Builtin = 89
	BuiltinVectorLerp Builtin = 90

	BuiltinMathIsNaN    Builtin = 91
	BuiltinMathIsInf    Builtin = 92
	BuiltinMathIsFinite Builtin = 93

	BuiltinIntegerCreate    Builtin = 94
	BuiltinIntegerToNumber  Builtin = 95
	BuiltinIntegerNeg       Builtin = 96
	BuiltinIntegerAdd       Builtin = 97
	BuiltinIntegerSub       Builtin = 98
	BuiltinIntegerMul       Builtin = 99
	BuiltinIntegerDiv       Builtin = 100
	BuiltinIntegerMin       Builtin = 101
	BuiltinIntegerMax       Builtin = 102
	BuiltinIntegerRem       Builtin = 103
	BuiltinIntegerIdiv      Builtin = 104
	BuiltinIntegerUdiv      Builtin = 105
	BuiltinIntegerUrem      Builtin = 106
	BuiltinIntegerMod       Builtin = 107
	BuiltinIntegerClamp     Builtin = 108
	BuiltinIntegerBand      Builtin = 109
	BuiltinIntegerBor       Builtin = 110
	BuiltinIntegerBnot      Builtin = 111
	BuiltinIntegerBxor      Builtin = 112
	BuiltinIntegerLt        Builtin = 113
	BuiltinIntegerLe        Builtin = 114
	BuiltinIntegerULt       Builtin = 115
	BuiltinIntegerULe       Builtin = 116
	BuiltinIntegerGt        Builtin = 117
	BuiltinIntegerGe        Builtin = 118
	BuiltinIntegerUGt       Builtin = 119
	BuiltinIntegerUGe       Builtin = 120
	BuiltinIntegerLShift    Builtin = 121
	BuiltinIntegerRShift    Builtin = 122
	BuiltinIntegerArShift   Builtin = 123
	BuiltinIntegerLRotate   Builtin = 124
	BuiltinIntegerRRotate   Builtin = 125
	BuiltinIntegerExtract   Builtin = 126
	BuiltinIntegerBtest     Builtin = 127
	BuiltinIntegerCountRz   Builtin = 128
	BuiltinIntegerCountLz   Builtin = 129
	BuiltinIntegerBSwap     Builtin = 130
	BuiltinBufferReadInt    Builtin = 131
	BuiltinBufferWriteInt   Builtin = 132
)
