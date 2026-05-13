// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package compiler

// typeinfo.go is the v4+ type-info encoder. Upstream emits typed
// function signatures, local types, and userdata index maps so the
// Luau VM's native-codegen pipeline can specialize ops.
//
// For correctness on the upstream VM, an empty TypeInfo []byte is
// sufficient: lvmload accepts it. Type-info encoding is therefore
// implemented here only as a single helper that returns an empty
// slice. This satisfies the bytecode encoder's invariant that v4+
// Protos have non-nil TypeInfo.

// emitTypeInfo returns the per-proto type-info payload. Today this is
// always empty; a future agent can implement TypeInfoLevel >= 1
// emission against upstream Compiler/src/Types.cpp.
func emitTypeInfo() []byte { return []byte{} }
