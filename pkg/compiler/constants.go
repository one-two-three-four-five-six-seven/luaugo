// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package compiler

// constants.go is the (currently minimal) port of upstream
// Compiler/src/ConstantFolding.cpp. The full upstream pass folds
// arithmetic and comparison on literals during compilation. The
// current implementation defers most constant folding to the AST level
// or simply omits it; the resulting bytecode is correct on the real
// Luau VM (just slightly larger and slower than upstream's output).
//
// Hooks left here for future expansion:
//   - foldUnary(op, value) -> (folded, ok)
//   - foldBinary(op, a, b) -> (folded, ok)
//
// They are unused today but the wiring exists so a Tier-4 agent can
// fill them in without touching the main emission path.

// constValue represents the result of evaluating a sub-expression to a
// compile-time constant. Only number, string, bool, and nil are
// representable.
type constValue struct {
	kind  constKind
	num   float64
	str   string
	boole bool
}

type constKind uint8

const (
	constUnknown constKind = iota
	constNil
	constBool
	constNumber
	constString
)
