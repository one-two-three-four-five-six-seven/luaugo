// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package compiler

// builtinfolding.go is the (currently minimal) port of upstream
// Compiler/src/BuiltinFolding.cpp. Upstream evaluates calls like
// math.abs(-1), bit32.band(0xFF, 0x0F), and string.len("hello") at
// compile time when their arguments are literal constants.
//
// The current compiler emits the runtime call instead. This is correct
// (the real Luau VM produces the same result) just slightly larger
// bytecode. A future Tier-4 agent can populate the foldBuiltin* helpers
// here without touching emission code in compiler.go.

// foldBuiltinUnary attempts to evaluate fn(arg) at compile time. Returns
// (result, true) on success, otherwise (_, false).
func foldBuiltinUnary(fn string, arg constValue) (constValue, bool) {
	_ = fn
	_ = arg
	return constValue{}, false
}

// foldBuiltinBinary attempts to evaluate fn(a, b) at compile time.
func foldBuiltinBinary(fn string, a, b constValue) (constValue, bool) {
	_ = fn
	_ = a
	_ = b
	return constValue{}, false
}
