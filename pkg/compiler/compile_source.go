// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package compiler

import (
	"fmt"
	"strings"

	"github.com/luaugo/luaugo/pkg/ast"
	"github.com/luaugo/luaugo/pkg/bytecode"
)

// compileSource lexes, parses, and compiles source into a module.
func compileSource(chunkname string, source []byte, opts Options) (*bytecode.Module, error) {
	res := ast.Parse(chunkname, source, ast.ParseOptions{})
	if len(res.Errors) > 0 {
		var b strings.Builder
		for i, e := range res.Errors {
			if i > 0 {
				b.WriteString("; ")
			}
			b.WriteString(e.Msg)
		}
		return nil, &CompileError{Location: res.Errors[0].Location, Msg: b.String()}
	}
	if res.Program == nil {
		return nil, &CompileError{Msg: "compiler: parser returned nil program"}
	}
	return compile(res.Program, opts)
}

// compileBinary lexes, parses, compiles, and serializes source.
func compileBinary(chunkname string, source []byte, opts Options) ([]byte, error) {
	m, err := compileSource(chunkname, source, opts)
	if err != nil {
		// Encode the error as a 0-prefixed compile-error blob, matching
		// upstream's `luau-compile --binary` error format.
		msg := err.Error()
		blob := make([]byte, 0, 1+len(msg))
		blob = append(blob, 0)
		blob = append(blob, msg...)
		return blob, err
	}
	blob, encErr := bytecode.Encode(m, bytecode.EncodeOptions{VectorComponents: 3})
	if encErr != nil {
		return nil, fmt.Errorf("compiler: encode failed: %w", encErr)
	}
	return blob, nil
}
