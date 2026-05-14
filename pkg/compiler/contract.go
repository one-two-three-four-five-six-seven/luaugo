// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

// Package compiler lowers a Luau AST to a bytecode.Module that, when
// serialized via the bytecode package, is byte-identical to the output
// of upstream `luau --compile=binary` for the same source and
// equivalent options. See `tools/UPSTREAM_MAP.md` for the upstream
// sources of truth.
package compiler

import (
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/ast"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"
)

// OptimizationLevel selects the compiler's optimization preset. Values
// mirror upstream's -O0 / -O1 / -O2 flags.
type OptimizationLevel uint8

const (
	// OptNone (-O0) emits straightforward bytecode with no folding.
	OptNone OptimizationLevel = 0
	// OptBaseline (-O1) is the default; enables constant folding,
	// fastcall detection, and unreachable-code elimination.
	OptBaseline OptimizationLevel = 1
	// OptAggressive (-O2) additionally enables inlining and loop
	// unrolling. Match upstream's set of optimizations exactly.
	OptAggressive OptimizationLevel = 2
)

// DebugLevel selects how much debug information is embedded in the
// emitted bytecode. Mirrors upstream's -g0 / -g1 / -g2 flags.
type DebugLevel uint8

const (
	// DebugNone (-g0) emits no debug info.
	DebugNone DebugLevel = 0
	// DebugLines (-g1) emits line info only.
	DebugLines DebugLevel = 1
	// DebugFull (-g2) emits line info plus local variable and upvalue
	// names.
	DebugFull DebugLevel = 2
)

// CoverageLevel controls coverage-instrumentation emission. Mirrors
// upstream's --coverage flag.
type CoverageLevel uint8

const (
	CoverageOff       CoverageLevel = 0
	CoverageStatement CoverageLevel = 1
	CoverageBranch    CoverageLevel = 2
)

// TypeInfoLevel controls type-info emission for the bytecode v4+ type
// table. Mirrors upstream's `--type-info` levels.
type TypeInfoLevel uint8

const (
	TypeInfoNone     TypeInfoLevel = 0
	TypeInfoFunc     TypeInfoLevel = 1 // function signatures
	TypeInfoFull     TypeInfoLevel = 2 // arguments, upvalues, locals
)

// Options controls compiler behavior. The default Options{} matches the
// settings used by `luau --compile=binary` with no extra flags.
type Options struct {
	OptimizationLevel OptimizationLevel
	DebugLevel        DebugLevel
	CoverageLevel     CoverageLevel
	TypeInfoLevel     TypeInfoLevel

	// VectorLib, VectorCtor, and VectorType override the default
	// vector library binding used for vector-constant folding.
	VectorLib  string
	VectorCtor string
	VectorType string

	// MutableGlobals is a list of global names that the compiler must
	// not assume are read-only (defaults to none).
	MutableGlobals []string

	// UserdataTypes lists host-registered userdata type names so the
	// compiler can emit tagged-userdata type info entries.
	UserdataTypes []string

	// LibrariesWithKnownMembers enumerates library tables whose member
	// shape is known at compile time, enabling fast field access.
	LibrariesWithKnownMembers []string

	// DisabledBuiltins lists builtin names that should not be
	// substituted into LOP_FASTCALL* opcodes.
	DisabledBuiltins []string
}

// Defaults returns the canonical default options used by upstream when
// `luau --compile=binary` is invoked with no flags.
func Defaults() Options {
	return Options{
		OptimizationLevel: OptBaseline,
		DebugLevel:        DebugLines,
	}
}

// CompileError describes a compile-time failure. The byte at offset 0
// of an encoded bytecode blob is 0 to indicate an error, followed by the
// error message; the decoder surfaces this via DecodeError.
type CompileError struct {
	Location ast.Location
	Msg      string
}

func (e *CompileError) Error() string { return e.Msg }

// Compile lowers prog to a bytecode.Module. The returned module can be
// serialized with bytecode.Encode to produce a binary blob compatible
// with upstream Luau.
func Compile(prog *ast.Program, opts Options) (*bytecode.Module, error) {
	return compile(prog, opts)
}

// CompileSource is a convenience that lexes, parses, and compiles
// source in one step. It is equivalent to ast.Parse followed by
// Compile, but returns parse errors as *CompileError values.
func CompileSource(chunkname string, source []byte, opts Options) (*bytecode.Module, error) {
	return compileSource(chunkname, source, opts)
}

// CompileBinary is a convenience that compiles source and serializes
// the resulting module in a single call. The output is suitable for
// direct consumption by vm.State.Load.
func CompileBinary(chunkname string, source []byte, opts Options) ([]byte, error) {
	return compileBinary(chunkname, source, opts)
}
