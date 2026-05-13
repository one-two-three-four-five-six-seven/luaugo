// Copyright (c) luaugo contributors. Licensed under the MIT License.

package compiler

import (
	"github.com/luaugo/luaugo/pkg/ast"
	"github.com/luaugo/luaugo/pkg/bytecode"
)

// Tier-1 placeholders for Tier 3's compiler agent.

func compile(prog *ast.Program, opts Options) (*bytecode.Module, error) {
	panic("compiler: Compile not yet implemented (Tier 3 compiler agent)")
}

func compileSource(chunkname string, source []byte, opts Options) (*bytecode.Module, error) {
	panic("compiler: CompileSource not yet implemented (Tier 3 compiler agent)")
}

func compileBinary(chunkname string, source []byte, opts Options) ([]byte, error) {
	panic("compiler: CompileBinary not yet implemented (Tier 3 compiler agent)")
}
