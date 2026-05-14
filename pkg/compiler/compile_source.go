// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package compiler

import (
	"fmt"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/ast"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"
)

// compileSource lexes, parses, and compiles source into a module.
//
// When the parser collects multiple errors via recovery, only the
// first is surfaced through CompileError.Msg, matching upstream Luau
// which aborts parsing at the first fatal error and reports a single
// "<chunkid>:<line>: <msg>" string. Surfacing all recovered errors
// would produce a "msg1; msg2; ..." string that confuses fixtures
// such as conformance/basic.luau:596 which match against an exact
// 'Incomplete statement: ...' suffix.
func compileSource(chunkname string, source []byte, opts Options) (*bytecode.Module, error) {
	res := ast.Parse(chunkname, source, ast.ParseOptions{})
	if len(res.Errors) > 0 {
		first := res.Errors[0]
		return nil, &CompileError{Location: first.Location, Msg: first.Msg}
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
