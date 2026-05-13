// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package common

// Bytecode version constants. These define the range of bytecode versions
// the luaugo VM understands and the version the luaugo compiler emits by
// default. Source of truth: upstream Bytecode.h enum LuauBytecodeTag.
const (
	// BytecodeVersionMin is the oldest bytecode version luaugo will load.
	BytecodeVersionMin uint8 = 3
	// BytecodeVersionMax is the newest bytecode version luaugo will load.
	BytecodeVersionMax uint8 = 9
	// BytecodeVersionTarget is the version emitted by the compiler by
	// default. Empirically confirmed by running `luau-compile --binary`
	// from upstream tag 0.720 against the entire conformance corpus:
	// every golden blob is version 9. Upstream's source defines
	// LBC_VERSION_TARGET=6 but the production binary emits 9.
	BytecodeVersionTarget uint8 = 9

	// TypeVersionMin is the oldest type-info encoding luaugo will load.
	TypeVersionMin uint8 = 1
	// TypeVersionMax is the newest type-info encoding luaugo will load.
	TypeVersionMax uint8 = 3
	// TypeVersionTarget is the type-info encoding emitted by the compiler
	// by default.
	TypeVersionTarget uint8 = 3
)

// ConstantTag identifies an entry in a Proto's constant table on disk.
// Values are part of the bytecode format and must not be reordered.
type ConstantTag uint8

const (
	ConstantNil                ConstantTag = 0
	ConstantBoolean            ConstantTag = 1
	ConstantNumber             ConstantTag = 2
	ConstantString             ConstantTag = 3
	ConstantImport             ConstantTag = 4
	ConstantTable              ConstantTag = 5
	ConstantClosure            ConstantTag = 6
	ConstantVector             ConstantTag = 7
	ConstantTableWithConstants ConstantTag = 8 // since bytecode v7
	ConstantInteger            ConstantTag = 9 // since bytecode v8
)

// String returns the upstream LBC_CONSTANT_* name for the tag.
func (t ConstantTag) String() string {
	switch t {
	case ConstantNil:
		return "NIL"
	case ConstantBoolean:
		return "BOOLEAN"
	case ConstantNumber:
		return "NUMBER"
	case ConstantString:
		return "STRING"
	case ConstantImport:
		return "IMPORT"
	case ConstantTable:
		return "TABLE"
	case ConstantClosure:
		return "CLOSURE"
	case ConstantVector:
		return "VECTOR"
	case ConstantTableWithConstants:
		return "TABLE_WITH_CONSTANTS"
	case ConstantInteger:
		return "INTEGER"
	}
	return "INVALID"
}

// TypeTag identifies an entry in a Proto's type information section.
// Values are part of the bytecode format and must not be reordered.
type TypeTag uint16

const (
	TypeNil      TypeTag = 0
	TypeBoolean  TypeTag = 1
	TypeNumber   TypeTag = 2
	TypeString   TypeTag = 3
	TypeTable    TypeTag = 4
	TypeFunction TypeTag = 5
	TypeThread   TypeTag = 6
	TypeUserdata TypeTag = 7
	TypeVector   TypeTag = 8
	TypeBuffer   TypeTag = 9
	TypeInteger  TypeTag = 10

	TypeAny TypeTag = 15

	// TypeTaggedUserdataBase is the start of the tagged-userdata range.
	// Type tags in [TypeTaggedUserdataBase, TypeTaggedUserdataEnd) refer
	// to host-registered userdata types.
	TypeTaggedUserdataBase TypeTag = 64
	TypeTaggedUserdataEnd  TypeTag = 64 + 32

	// TypeOptionalBit is OR-ed into a type tag to mark it optional (i.e.
	// the value may be the type or nil).
	TypeOptionalBit TypeTag = 1 << 7

	TypeInvalid TypeTag = 256
)

// CaptureKind is the kind of upvalue capture emitted by the OpCapture
// instruction following an OpNewClosure. Values must match
// enum LuauCaptureType in upstream Bytecode.h.
type CaptureKind uint8

const (
	// CaptureVal copies the value of the local into a fresh upvalue.
	CaptureVal CaptureKind = 0
	// CaptureRef captures the local by reference into a fresh upvalue.
	CaptureRef CaptureKind = 1
	// CaptureUpval captures a parent function's upvalue.
	CaptureUpval CaptureKind = 2
)

// ProtoFlag is a bitmask stored in Proto.flags. Values must match
// enum LuauProtoFlag in upstream Bytecode.h.
type ProtoFlag uint32

const (
	// ProtoFlagNativeModule tags the main proto for modules with --!native.
	ProtoFlagNativeModule ProtoFlag = 1 << 0
	// ProtoFlagNativeCold marks individual protos as not profitable to
	// compile natively.
	ProtoFlagNativeCold ProtoFlag = 1 << 1
	// ProtoFlagNativeFunction tags the main proto for modules containing
	// at least one function with the @native attribute.
	ProtoFlagNativeFunction ProtoFlag = 1 << 2
)
