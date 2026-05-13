// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

// Package bytecode is the single source of truth for Luau bytecode
// serialization. The encoder produces blobs byte-identical to those
// produced by upstream `luau --compile=binary`; the decoder consumes
// blobs produced by any upstream Luau bytecode v3 through v9.
//
// The compiler and the VM loader both depend on this package: neither
// reimplements the binary layout. See `tools/UPSTREAM_MAP.md` for the
// upstream sources of truth (Bytecode.h, BytecodeBuilder.cpp, lvmload.cpp).
package bytecode

import "github.com/luaugo/luaugo/internal/common"

// ----------------------------------------------------------------------
// In-memory Proto representation
// ----------------------------------------------------------------------

// Module is the decoded form of a Luau bytecode blob. It corresponds to
// the on-disk format produced by BytecodeBuilder::finalize().
type Module struct {
	// Version is the bytecode format version. Must be in
	// [common.BytecodeVersionMin, common.BytecodeVersionMax].
	Version uint8
	// TypeVersion is the type-info encoding version. Zero indicates no
	// type info was emitted (bytecode v3 only).
	TypeVersion uint8
	// Strings is the per-module string table. Indices into Strings used
	// elsewhere in the module are 1-based; index 0 means "no string".
	Strings []string
	// UserdataTypeNames maps userdata type ids to string-table indices
	// (1-based). Present only for type-info v3+.
	UserdataTypeNames []uint32
	// Protos is the list of function prototypes. The main function is
	// indexed by MainProto.
	Protos []*Proto
	// MainProto is the index into Protos of the chunk's top-level proto.
	MainProto uint32
}

// Proto is a single compiled function. Field order and layout mirror
// upstream struct Proto in VM/src/lobject.h.
type Proto struct {
	// MaxStackSize is the number of stack slots the function reserves
	// (including locals and temporaries). Always at least Numparams+1.
	MaxStackSize uint8
	// NumParams is the count of named (non-vararg) parameters.
	NumParams uint8
	// NumUpvalues is the count of upvalues captured by this proto.
	NumUpvalues uint8
	// IsVararg is 1 iff this function takes ... varargs.
	IsVararg uint8

	// Flags is the proto flag bitmask (LPF_*). Present for bytecode v4+.
	Flags common.ProtoFlag

	// TypeInfo is the raw bytecode-encoded type information section.
	// The compiler emits this in a format described by TypeVersion;
	// luaugo does not interpret it for execution.
	TypeInfo []byte

	// Code is the instruction stream (one or more 32-bit words per
	// logical instruction).
	Code []uint32

	// Constants holds the function's constant table entries.
	Constants []Constant

	// Protos holds indices into Module.Protos for nested closures
	// referenced by this proto.
	Protos []uint32

	// LineDefined is the source line on which this function was
	// declared, 1-based. Present for bytecode v2+.
	LineDefined uint32

	// DebugName is the index into Module.Strings (1-based) of this
	// proto's debug name, or 0 for no name.
	DebugName uint32

	// LineInfo, when present, is the compressed line-info section
	// produced by BytecodeBuilder::dumpLineInfo. The encoding is
	// version-specific and luaugo round-trips it verbatim.
	LineInfo *LineInfo

	// DebugInfo, when present, carries local-variable and upvalue
	// names. Round-tripped verbatim by luaugo.
	DebugInfo *DebugInfo
}

// LineInfo carries the line-info section of a proto. Both LineInfo1 and
// LineInfo2 are present; the on-disk format stores them packed.
type LineInfo struct {
	// LineGapLog2 is the encoding parameter used to pack lines.
	LineGapLog2 uint8
	// AbsLineInfo is the absolute-line lookup table.
	AbsLineInfo []int32
	// LineInfo is the delta-encoded per-instruction line list.
	LineInfo []int8
}

// DebugInfo carries the optional debug-info section of a proto.
type DebugInfo struct {
	Locals   []DebugLocal
	Upvalues []uint32 // indices into Module.Strings
}

// DebugLocal records the debug name of a single local variable.
type DebugLocal struct {
	Name    uint32 // index into Module.Strings (1-based)
	StartPC uint32
	EndPC   uint32
	Reg     uint8
}

// ----------------------------------------------------------------------
// Constant entries
// ----------------------------------------------------------------------

// Constant is an entry in a proto's constant table.
type Constant interface {
	// Tag returns the on-disk tag for this constant.
	Tag() common.ConstantTag
	constantMarker()
}

// ConstantNilEntry encodes a nil constant (LBC_CONSTANT_NIL).
type ConstantNilEntry struct{}

// ConstantBooleanEntry encodes a boolean constant (LBC_CONSTANT_BOOLEAN).
type ConstantBooleanEntry struct {
	Value bool
}

// ConstantNumberEntry encodes a 64-bit IEEE 754 number constant.
type ConstantNumberEntry struct {
	Value float64
}

// ConstantIntegerEntry encodes a 64-bit signed integer constant
// (bytecode v8+).
type ConstantIntegerEntry struct {
	Value int64
}

// ConstantStringEntry encodes a string constant. Index is 1-based into
// Module.Strings; 0 means the empty string.
type ConstantStringEntry struct {
	Index uint32
}

// ConstantImportEntry encodes an import path constant. It is the
// 32-bit packed form upstream uses: two bits of length followed by
// 1, 2, or 3 ten-bit indices into the constant table.
type ConstantImportEntry struct {
	Packed uint32
}

// ConstantTableEntry encodes a "table shape" template: a list of key
// constants. Tag is ConstantTable for older shapes and
// ConstantTableWithConstants for v7+ shapes that include values.
type ConstantTableEntry struct {
	Keys []uint32 // indices into the surrounding constant table
}

// ConstantTableWithConstantsEntry is the v7+ form that includes both
// keys and pre-computed values.
type ConstantTableWithConstantsEntry struct {
	Pairs []ConstantTablePair
}

// ConstantTablePair is one key/value pair in a v7+ table template.
type ConstantTablePair struct {
	Key   uint32
	Value uint32
}

// ConstantClosureEntry encodes a closure constant referencing a
// pre-created proto by its index in Module.Protos.
type ConstantClosureEntry struct {
	ProtoIndex uint32
}

// ConstantVectorEntry encodes a vector literal (bytecode v5+).
type ConstantVectorEntry struct {
	X, Y, Z, W float32
}

func (ConstantNilEntry) Tag() common.ConstantTag    { return common.ConstantNil }
func (ConstantBooleanEntry) Tag() common.ConstantTag { return common.ConstantBoolean }
func (ConstantNumberEntry) Tag() common.ConstantTag  { return common.ConstantNumber }
func (ConstantIntegerEntry) Tag() common.ConstantTag { return common.ConstantInteger }
func (ConstantStringEntry) Tag() common.ConstantTag  { return common.ConstantString }
func (ConstantImportEntry) Tag() common.ConstantTag  { return common.ConstantImport }
func (ConstantTableEntry) Tag() common.ConstantTag   { return common.ConstantTable }
func (ConstantTableWithConstantsEntry) Tag() common.ConstantTag {
	return common.ConstantTableWithConstants
}
func (ConstantClosureEntry) Tag() common.ConstantTag { return common.ConstantClosure }
func (ConstantVectorEntry) Tag() common.ConstantTag  { return common.ConstantVector }

func (ConstantNilEntry) constantMarker()                {}
func (ConstantBooleanEntry) constantMarker()            {}
func (ConstantNumberEntry) constantMarker()             {}
func (ConstantIntegerEntry) constantMarker()            {}
func (ConstantStringEntry) constantMarker()             {}
func (ConstantImportEntry) constantMarker()             {}
func (ConstantTableEntry) constantMarker()              {}
func (ConstantTableWithConstantsEntry) constantMarker() {}
func (ConstantClosureEntry) constantMarker()            {}
func (ConstantVectorEntry) constantMarker()             {}

// ----------------------------------------------------------------------
// Encode / Decode
// ----------------------------------------------------------------------

// EncodeOptions controls bytecode emission. Future versions of luaugo
// may add fields here; do not rely on the zero value being unchanged.
type EncodeOptions struct {
	// VectorComponents selects 3-wide (default) or 4-wide vector
	// constants. Must be 3 or 4.
	VectorComponents uint8
}

// DecodeError describes a failure to load a bytecode blob.
type DecodeError struct {
	Offset uint64
	Msg    string
}

func (e *DecodeError) Error() string { return e.Msg }

// Decode parses a Luau bytecode blob and returns the resulting Module.
// If the leading byte is zero, the blob represents a compile-time
// error: the remainder of the blob is the error message and Decode
// returns it wrapped in an *DecodeError with Offset 0.
func Decode(blob []byte) (*Module, error) { return decode(blob) }

// Encode serializes a Module into the Luau bytecode binary format.
// The result is byte-identical to what upstream `luau --compile=binary`
// would emit for the same Module given equivalent EncodeOptions.
func Encode(m *Module, opts EncodeOptions) ([]byte, error) { return encode(m, opts) }

// ----------------------------------------------------------------------
// Disassembly
// ----------------------------------------------------------------------

// Disassemble returns a textual representation of m similar to the
// output of `luau --compile=text`. Intended for debugging only.
func Disassemble(m *Module) string { return disassemble(m) }

// DisassembleProto returns a textual representation of a single proto.
func DisassembleProto(m *Module, p *Proto) string { return disassembleProto(m, p) }

// ----------------------------------------------------------------------
// Varint codec helpers (exposed for tooling and tests)
// ----------------------------------------------------------------------

// VarintAppend appends the Luau variable-length encoding of v to dst
// and returns the extended slice. Encoding uses 7 data bits per byte
// with the MSB set on every non-final byte.
func VarintAppend(dst []byte, v uint32) []byte { return varintAppend(dst, v) }

// VarintRead reads a Luau variable-length integer from src starting at
// pos and returns the value and the number of bytes consumed.
func VarintRead(src []byte, pos int) (value uint32, n int, err error) {
	return varintRead(src, pos)
}
