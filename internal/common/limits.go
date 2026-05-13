// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package common

// Bytecode encoding limits. These are derived from the comments at the
// top of upstream Common/include/Luau/Bytecode.h and the static asserts
// in BytecodeBuilder.cpp.
const (
	// MaxRegister is the highest register index encodable in a single
	// instruction byte. Registers 0..MaxRegister name slots in the
	// function's stack frame, including arguments.
	MaxRegister = 254

	// MaxUpvalue is the highest upvalue index encodable in a single
	// instruction byte. Upvalues 0..MaxUpvalue name slots in the closure
	// object.
	MaxUpvalue = 199

	// MaxConstant is the maximum encodable constant index. Constants are
	// stored in a per-proto table; the bytecode encodes indices in 23
	// bits so the encodable space is the full 24-bit range minus the top
	// bit reserved for future tweaks.
	MaxConstant = 1<<23 - 1

	// MaxClosure is the maximum encodable child-proto index. Closures
	// are created from a child proto referenced by index; the index is
	// stored in a 16-bit field with the high bit reserved.
	MaxClosure = 1<<15 - 1

	// MaxJumpForward and MaxJumpBackward bound the signed 24-bit jump
	// offset used by LOP_JUMPX and the signed 16-bit jump offset used by
	// every other jump instruction. They are expressed here in the
	// largest (24-bit) form; AD-encoded jumps must additionally satisfy
	// the int16 range.
	MaxJumpForward  = 1 << 23
	MaxJumpBackward = -1 << 23

	// MaxLocalVariables is the maximum number of locals visible in a
	// single function. The runtime cap is MaxRegister, but the compiler
	// enforces a tighter limit to leave room for temporaries.
	MaxLocalVariables = 200

	// MaxUpvalueCount is the maximum number of upvalues for a single
	// closure (matches the bytecode limit and upstream
	// LUAI_MAXUPVALUES).
	MaxUpvalueCount = 200

	// MaxArguments is the maximum number of fixed arguments a function
	// can declare.
	MaxArguments = 200

	// MaxMultiRet is the maximum number of return values that can be
	// adjusted via MULTRET in a single instruction.
	MaxMultiRet = 254
)

// IsValidRegister reports whether reg names a valid stack-frame slot.
func IsValidRegister(reg int) bool { return reg >= 0 && reg <= MaxRegister }

// IsValidUpvalue reports whether idx names a valid upvalue slot.
func IsValidUpvalue(idx int) bool { return idx >= 0 && idx <= MaxUpvalue }

// IsValidConstant reports whether idx names a valid constant table entry.
func IsValidConstant(idx int) bool { return idx >= 0 && idx <= MaxConstant }
