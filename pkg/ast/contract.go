// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

// Package ast contains the lexer, parser, and abstract syntax tree for
// Luau source code. The shape of the AST mirrors upstream Luau's
// Ast/include/Luau/Ast.h closely enough that the compiler can be ported
// idiomatically; node names use Go conventions (NodeExprCall instead of
// AstExprCall).
//
// This file is the contract surface for the ast package. Tier 1 froze
// these signatures and downstream tiers code against them. Worker agents
// porting Lexer.cpp / Parser.cpp / Ast.cpp may add new exported fields
// or methods, but must not rename or remove any symbol declared here.
package ast

import "io"

// ----------------------------------------------------------------------
// Source positions
// ----------------------------------------------------------------------

// Position identifies a source location as a line/column pair. Lines and
// columns are 0-based to match upstream Luau (Position in Location.h).
type Position struct {
	Line   uint32
	Column uint32
}

// Location is a half-open span [Begin, End) in source code.
type Location struct {
	Begin Position
	End   Position
}

// Contains reports whether other is contained within l.
func (l Location) Contains(other Location) bool {
	return l.containsPos(other.Begin) && (other.End == l.End || l.containsPos(other.End))
}

func (l Location) containsPos(p Position) bool {
	if p.Line < l.Begin.Line || p.Line > l.End.Line {
		return p.Line >= l.Begin.Line && p.Line <= l.End.Line
	}
	if p.Line == l.Begin.Line && p.Column < l.Begin.Column {
		return false
	}
	if p.Line == l.End.Line && p.Column > l.End.Column {
		return false
	}
	return true
}

// ----------------------------------------------------------------------
// Lexer
// ----------------------------------------------------------------------

// TokenKind enumerates the lexical token kinds produced by Lexer. Values
// 0..255 represent the corresponding ASCII character; values from
// CharEnd onwards name multi-character tokens, keywords, and special
// markers. The order matches enum Lexeme::Type in upstream Lexer.h.
type TokenKind uint16

const (
	TokenEof TokenKind = 0

	// 1..255 are reserved for single-character tokens (the byte itself).

	CharEnd TokenKind = 256

	TokenEqual        TokenKind = 257 // ==
	TokenLessEqual    TokenKind = 258 // <=
	TokenGreaterEqual TokenKind = 259 // >=
	TokenNotEqual     TokenKind = 260 // ~=
	TokenDot2         TokenKind = 261 // ..
	TokenDot3         TokenKind = 262 // ...
	TokenSkinnyArrow  TokenKind = 263 // ->
	TokenDoubleColon  TokenKind = 264 // ::
	TokenFloorDiv     TokenKind = 265 // //

	TokenInterpStringBegin  TokenKind = 266 // `prefix{
	TokenInterpStringMid    TokenKind = 267 // }middle{
	TokenInterpStringEnd    TokenKind = 268 // }suffix`
	TokenInterpStringSimple TokenKind = 269 // `whole`

	TokenAddAssign      TokenKind = 270 // +=
	TokenSubAssign      TokenKind = 271 // -=
	TokenMulAssign      TokenKind = 272 // *=
	TokenDivAssign      TokenKind = 273 // /=
	TokenFloorDivAssign TokenKind = 274 // //=
	TokenModAssign      TokenKind = 275 // %=
	TokenPowAssign      TokenKind = 276 // ^=
	TokenConcatAssign   TokenKind = 277 // ..=

	TokenRawString    TokenKind = 278 // [[...]]
	TokenQuotedString TokenKind = 279 // "..." or '...'
	TokenNumber       TokenKind = 280
	TokenName         TokenKind = 281

	TokenComment      TokenKind = 282 // --line comment
	TokenBlockComment TokenKind = 283 // --[[ block comment ]]

	TokenAttribute     TokenKind = 284 // @native, @checked, ...
	TokenAttributeOpen TokenKind = 285 // @[

	TokenBrokenString          TokenKind = 286
	TokenBrokenComment         TokenKind = 287
	TokenBrokenUnicode         TokenKind = 288
	TokenBrokenInterpDoubleBrace TokenKind = 289
	TokenError                 TokenKind = 290

	TokenAnd      TokenKind = 291
	TokenBreak    TokenKind = 292
	TokenDo       TokenKind = 293
	TokenElse     TokenKind = 294
	TokenElseif   TokenKind = 295
	TokenEnd      TokenKind = 296
	TokenFalse    TokenKind = 297
	TokenFor      TokenKind = 298
	TokenFunction TokenKind = 299
	TokenIf       TokenKind = 300
	TokenIn       TokenKind = 301
	TokenLocal    TokenKind = 302
	TokenNil      TokenKind = 303
	TokenNot      TokenKind = 304
	TokenOr       TokenKind = 305
	TokenRepeat   TokenKind = 306
	TokenReturn   TokenKind = 307
	TokenThen     TokenKind = 308
	TokenTrue     TokenKind = 309
	TokenUntil    TokenKind = 310
	TokenWhile    TokenKind = 311
)

// QuoteStyle distinguishes single-quoted from double-quoted strings.
type QuoteStyle uint8

const (
	QuoteSingle QuoteStyle = 0
	QuoteDouble QuoteStyle = 1
)

// Token is a single lexeme emitted by the lexer. The Value byte slice
// references the original source buffer when possible; the lexer
// promises not to mutate it. NumericString carries the literal text of a
// numeric token for later decoding.
type Token struct {
	Kind     TokenKind
	Location Location
	// Value carries the textual content for strings, names, numbers, and
	// comments. It aliases the source buffer; do not modify.
	Value []byte
	// Length, when non-zero, overrides len(Value) for tokens that span
	// portions of the source where Value points to a longer buffer.
	Length uint32
	// Quote indicates the quote style for QuotedString tokens.
	Quote QuoteStyle
	// BlockDepth records the depth (number of `=` signs) for long
	// brackets and block comments.
	BlockDepth uint32
}

// Lexer tokenizes a Luau source buffer.
type Lexer interface {
	// Next advances to the next token and returns it.
	Next() Token
	// Peek returns the current token without consuming it.
	Peek() Token
	// Done reports whether the end of input has been reached.
	Done() bool
}

// NewLexer constructs a Lexer over source. The chunkname is used in
// diagnostic messages and may be empty.
func NewLexer(chunkname string, source []byte) Lexer { return newLexer(chunkname, source) }

// ----------------------------------------------------------------------
// AST node hierarchy
// ----------------------------------------------------------------------

// Node is the common interface implemented by every AST node.
type Node interface {
	// Loc returns the source location spanning the entire node.
	Loc() Location
	// nodeMarker is a sentinel to keep this interface closed to
	// implementations in this package.
	nodeMarker()
}

// Expr is the marker interface for expression nodes.
type Expr interface {
	Node
	exprMarker()
}

// Stat is the marker interface for statement nodes.
type Stat interface {
	Node
	statMarker()
}

// TypeExpr is the marker interface for type-annotation nodes.
type TypeExpr interface {
	Node
	typeMarker()
}

// TypePack is the marker interface for type-pack nodes (variadics,
// generic packs, explicit type tuples).
type TypePack interface {
	Node
	typePackMarker()
}

// Name is an identifier with an attached source location.
type Name struct {
	Name     string
	Location Location
}

// Attribute is a @attribute applied to a function, statement, or type.
type Attribute struct {
	Location Location
	Name     string
}

// GenericType is a generic type parameter declaration.
type GenericType struct {
	Location   Location
	Name       string
	Default    TypeExpr // nil if none
}

// GenericTypePack is a generic type-pack parameter declaration.
type GenericTypePack struct {
	Location Location
	Name     string
	Default  TypePack // nil if none
}

// Block is a sequence of statements with its own scope.
type Block struct {
	Location Location
	Body     []Stat
	HasEnd   bool
}

// Program is the root node of a parsed Luau source file.
type Program struct {
	Root        *Block
	Lines       int      // 1-based count of source lines
	HotComments []HotComment
	CommentLocs []Location // for tooling that needs to walk comments
}

// HotComment records a "--!" directive at the top of a file
// (e.g. --!strict, --!native).
type HotComment struct {
	Header   bool
	Location Location
	Content  string
}

// ----------------------------------------------------------------------
// Parser
// ----------------------------------------------------------------------

// ParseOptions controls parser behavior. Defaults match upstream Luau's
// ParseOptions struct.
type ParseOptions struct {
	// AllowDeclarationSyntax enables `declare function`, `declare class`,
	// and similar type-declaration statements used in .d.luau files.
	AllowDeclarationSyntax bool
	// CaptureComments retains comment tokens in the output so tools can
	// preserve them.
	CaptureComments bool
	// StoreCstData retains concrete-syntax-tree side data (whitespace,
	// trivia) for source-faithful round-tripping. Not used by the
	// compiler.
	StoreCstData bool
	// AllowLintErrorsToTouchSyntax permits the parser to keep going past
	// some syntax errors to produce a partial tree.
	AllowLintErrorsToTouchSyntax bool
}

// ParseError records a syntax error with location.
type ParseError struct {
	Location Location
	Msg      string
}

func (e *ParseError) Error() string { return e.Msg }

// ParseResult is what the parser returns.
type ParseResult struct {
	Program  *Program
	Errors   []ParseError
	Comments []Token // populated when ParseOptions.CaptureComments is true
}

// Parse parses source as a top-level Luau chunk.
func Parse(chunkname string, source []byte, opts ParseOptions) *ParseResult {
	return parse(chunkname, source, opts)
}

// ParseExpr parses source as a single Luau expression. Useful for REPL
// input.
func ParseExpr(chunkname string, source []byte, opts ParseOptions) (Expr, []ParseError) {
	return parseExpr(chunkname, source, opts)
}

// ----------------------------------------------------------------------
// Visitor
// ----------------------------------------------------------------------

// Visitor walks an AST. Methods return true to descend into children,
// false to skip. The default Visitor (returned by NewVisitor) descends
// everywhere; embed it to override only the methods you care about.
type Visitor interface {
	VisitExpr(Expr) bool
	VisitStat(Stat) bool
	VisitType(TypeExpr) bool
	VisitTypePack(TypePack) bool
}

// Walk applies v to every node in n. n may be Expr, Stat, TypeExpr,
// TypePack, *Block, or *Program.
func Walk(v Visitor, n any) { walk(v, n) }

// ----------------------------------------------------------------------
// PrettyPrint
// ----------------------------------------------------------------------

// PrettyPrint writes a human-readable representation of n to w. Intended
// for debugging the parser; it is not source-faithful.
func PrettyPrint(w io.Writer, n Node) error { return prettyPrint(w, n) }
