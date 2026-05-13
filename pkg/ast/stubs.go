// Copyright (c) luaugo contributors. Licensed under the MIT License.

package ast

import "io"

// This file holds Tier-1 placeholder implementations. Tier 2's AST agent
// will replace these with real implementations.

func newLexer(chunkname string, source []byte) Lexer {
	panic("ast: lexer not yet implemented (Tier 2 AST agent)")
}

func parse(chunkname string, source []byte, opts ParseOptions) *ParseResult {
	panic("ast: parser not yet implemented (Tier 2 AST agent)")
}

func parseExpr(chunkname string, source []byte, opts ParseOptions) (Expr, []ParseError) {
	panic("ast: parseExpr not yet implemented (Tier 2 AST agent)")
}

func walk(v Visitor, n any) {
	panic("ast: walk not yet implemented (Tier 2 AST agent)")
}

func prettyPrint(w io.Writer, n Node) error {
	panic("ast: prettyPrint not yet implemented (Tier 2 AST agent)")
}

// Suppress unused-import lints on early builds.
var _ = io.Discard
