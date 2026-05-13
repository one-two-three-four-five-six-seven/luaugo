// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package ast

// This file declares the concrete AST node types. The structure mirrors
// upstream Ast.h: every expression and statement has a corresponding
// AstExpr* / AstStat* class. Field names use Go conventions.

// ----------------------------------------------------------------------
// Expression nodes
// ----------------------------------------------------------------------

// ExprGroup is a parenthesized expression: ( expr ).
type ExprGroup struct {
	Location Location
	Expr     Expr
}

// ExprConstantNil is the `nil` literal.
type ExprConstantNil struct {
	Location Location
}

// ExprConstantBool is a `true` or `false` literal.
type ExprConstantBool struct {
	Location Location
	Value    bool
}

// NumberParseResult records how the lexer interpreted a numeric literal.
type NumberParseResult uint8

const (
	NumberOk NumberParseResult = iota
	NumberMalformed
	NumberBinOverflow
	NumberHexOverflow
	NumberDoublePrefix
)

// ExprConstantNumber is a float64 numeric literal.
type ExprConstantNumber struct {
	Location Location
	Value    float64
	Parse    NumberParseResult
}

// ExprConstantInteger is an explicitly-typed integer literal
// (Luau v8+; used for the integer type system).
type ExprConstantInteger struct {
	Location Location
	Value    int64
	Parse    NumberParseResult
}

// ExprConstantString is a string literal.
type ExprConstantString struct {
	Location Location
	Value    []byte
	Quote    QuoteStyle
}

// ExprLocal references a local variable.
type ExprLocal struct {
	Location Location
	Local    *Local
	Upvalue  bool // true iff this local is captured from an enclosing function
}

// ExprGlobal references a global variable.
type ExprGlobal struct {
	Location Location
	Name     string
}

// ExprVarargs is the `...` expression.
type ExprVarargs struct {
	Location Location
}

// ExprCall is a function call: f(args).
type ExprCall struct {
	Location     Location
	Func         Expr
	Args         []Expr
	Self         bool     // true iff this was a `obj:m(args)` method call
	ArgLocation  Location // location of the (...) argument list
}

// ExprIndexName is a `.name` member access.
type ExprIndexName struct {
	Location    Location
	Expr        Expr
	IndexName   string
	IndexLoc    Location
	OperatorLoc Position
	Op          byte // '.' or ':'
}

// ExprIndexExpr is a `[expr]` index access.
type ExprIndexExpr struct {
	Location Location
	Expr     Expr
	Index    Expr
}

// ExprFunction is a function-expression literal: function(args) body end.
type ExprFunction struct {
	Location      Location
	Attributes    []Attribute
	Generics      []GenericType
	GenericPacks  []GenericTypePack
	Self          *Local // implicit "self" parameter for method definitions
	Args          []*Local
	ArgLocation   Location
	Vararg        bool
	VarargLoc     Location
	VarargAnnot   TypePack // nil if absent
	ReturnAnnot   TypePack // nil if absent
	Body          *Block
	FunctionDepth uint32
	DebugName     string
	HasEnd        bool
}

// ExprTable is a table constructor: {...}.
type ExprTable struct {
	Location Location
	Items    []TableItem
}

// TableItemKind discriminates table constructor entries.
type TableItemKind uint8

const (
	// TableItemList is `value` (an array-style positional entry).
	TableItemList TableItemKind = iota
	// TableItemRecord is `name = value`.
	TableItemRecord
	// TableItemGeneral is `[key] = value`.
	TableItemGeneral
)

// TableItem is one entry in a table constructor.
type TableItem struct {
	Kind  TableItemKind
	Key   Expr // nil for TableItemList
	Value Expr
}

// UnaryOp enumerates Luau unary operators.
type UnaryOp uint8

const (
	UnaryNot UnaryOp = iota
	UnaryMinus
	UnaryLen
)

// ExprUnary is `op operand`.
type ExprUnary struct {
	Location Location
	Op       UnaryOp
	Operand  Expr
}

// BinaryOp enumerates Luau binary operators.
type BinaryOp uint8

const (
	BinaryAdd BinaryOp = iota
	BinarySub
	BinaryMul
	BinaryDiv
	BinaryFloorDiv
	BinaryMod
	BinaryPow
	BinaryConcat
	BinaryEq
	BinaryNotEq
	BinaryLt
	BinaryLe
	BinaryGt
	BinaryGe
	BinaryAnd
	BinaryOr
)

// ExprBinary is `lhs op rhs`.
type ExprBinary struct {
	Location Location
	Op       BinaryOp
	Lhs      Expr
	Rhs      Expr
}

// ExprTypeAssertion is `expr :: type`.
type ExprTypeAssertion struct {
	Location Location
	Expr     Expr
	Type     TypeExpr
}

// ExprIfElse is `if cond then t else f` (an expression, not a statement).
type ExprIfElse struct {
	Location  Location
	Condition Expr
	HasThen   bool
	True      Expr
	HasElse   bool
	False     Expr
}

// ExprInterpString is a backtick interpolated string with embedded
// expressions. Strings and Expressions are interleaved so that
// len(Strings) == len(Expressions)+1.
type ExprInterpString struct {
	Location    Location
	Strings     [][]byte
	Expressions []Expr
}

// ExprError is a placeholder emitted by the parser on syntax errors so
// downstream passes can still walk the tree.
type ExprError struct {
	Location  Location
	Expressions []Expr
	MessageIdx uint32
}

// ----------------------------------------------------------------------
// Statement nodes
// ----------------------------------------------------------------------

// StatBlock is `do block end` (and also serves as a top-level block).
type StatBlock = Block // alias so the parser can use either spelling.

// StatIf is `if cond then ... [elseif ...] [else ...] end`.
type StatIf struct {
	Location  Location
	Condition Expr
	ThenBody  *Block
	ElseBody  Stat // *Block or *StatIf for elseif; nil for absent else
	HasThen   bool
	HasEnd    bool
	ThenLoc   Location
	ElseLoc   Location
}

// StatWhile is `while cond do ... end`.
type StatWhile struct {
	Location  Location
	Condition Expr
	Body      *Block
	HasDo     bool
	DoLoc     Location
	HasEnd    bool
}

// StatRepeat is `repeat ... until cond`.
type StatRepeat struct {
	Location  Location
	Body      *Block
	Condition Expr
	HasUntil  bool
}

// StatBreak is `break`.
type StatBreak struct {
	Location Location
}

// StatContinue is `continue` (Luau extension; lexed as a contextual keyword).
type StatContinue struct {
	Location Location
}

// StatReturn is `return exprs`.
type StatReturn struct {
	Location Location
	List     []Expr
}

// StatExpr is a statement that is just an expression (typically a call).
type StatExpr struct {
	Location Location
	Expr     Expr
}

// Local is a declared local variable, used by ExprLocal and several
// statement kinds.
type Local struct {
	Name        string
	NameLoc     Location
	Annotation  TypeExpr // nil if not annotated
	Shadow      *Local   // points to the local this shadows, if any
	FunctionDepth uint32
	LoopDepth     uint32
}

// StatLocal is `local a, b, ... = expr1, expr2, ...`.
type StatLocal struct {
	Location Location
	Vars     []*Local
	Values   []Expr
	EqualsLoc Location
}

// StatFor is the numeric for loop.
type StatFor struct {
	Location Location
	Var      *Local
	From     Expr
	To       Expr
	Step     Expr // nil if absent
	Body     *Block
	HasDo    bool
	DoLoc    Location
	HasEnd   bool
}

// StatForIn is the generic for loop.
type StatForIn struct {
	Location Location
	Vars     []*Local
	Values   []Expr
	Body     *Block
	HasIn    bool
	InLoc    Location
	HasDo    bool
	DoLoc    Location
	HasEnd   bool
}

// StatAssign is `a, b, ... = expr1, expr2, ...`.
type StatAssign struct {
	Location Location
	Vars     []Expr
	Values   []Expr
}

// StatCompoundAssign is `a op= expr` (e.g., +=, -=, ..=).
type StatCompoundAssign struct {
	Location Location
	Op       BinaryOp
	Var      Expr
	Value    Expr
}

// StatFunction is `function a.b.c(args) body end` (a named function).
type StatFunction struct {
	Location Location
	Name     Expr // ExprGlobal / ExprIndexName / ExprIndexExpr chain
	Func     *ExprFunction
}

// StatLocalFunction is `local function name(args) body end`.
type StatLocalFunction struct {
	Location Location
	Name     *Local
	Func     *ExprFunction
}

// StatTypeAlias is `type Name = ...` or `export type Name = ...`.
type StatTypeAlias struct {
	Location     Location
	Name         string
	NameLoc      Location
	Generics     []GenericType
	GenericPacks []GenericTypePack
	Type         TypeExpr
	Exported     bool
}

// StatTypeFunction is a `type function` declaration (compile-time type fn).
type StatTypeFunction struct {
	Location Location
	Name     string
	NameLoc  Location
	Func     *ExprFunction
	Exported bool
}

// StatError is a placeholder emitted by the parser on errors.
type StatError struct {
	Location   Location
	Statements []Stat
	Expressions []Expr
	MessageIdx uint32
}

// ----------------------------------------------------------------------
// Type-annotation nodes
// ----------------------------------------------------------------------

// TypeReference is a named type, possibly with generic arguments.
type TypeReference struct {
	Location     Location
	Prefix       string // may be empty
	Name         string
	Parameters   []TypeOrPack
	HasParameters bool
}

// TypeOrPack discriminates a type argument that may be either a type or
// a type pack.
type TypeOrPack struct {
	Type     TypeExpr // exactly one of Type or Pack is non-nil
	Pack     TypePack
}

// TypeTable is `{ ... }` as a type annotation.
type TypeTable struct {
	Location Location
	Indexer  *TypeTableIndexer // nil if absent
	Props    []TypeTableProp
}

// TypeTableProp is a `name: T` member of a table type.
type TypeTableProp struct {
	Name     string
	NameLoc  Location
	Type     TypeExpr
}

// TypeTableIndexer is `[K]: V` in a table type.
type TypeTableIndexer struct {
	Location  Location
	IndexType TypeExpr
	ValueType TypeExpr
}

// TypeFunction is a function type: (args) -> ret.
type TypeFunction struct {
	Location     Location
	Generics     []GenericType
	GenericPacks []GenericTypePack
	ArgTypes     []TypeExpr
	ArgNames     []*Name // nil entries permitted; positional if all nil
	ArgVararg    TypePack // nil if absent
	ReturnType   TypePack
}

// TypeOptional is `T?`.
type TypeOptional struct {
	Location Location
	Inner    TypeExpr
}

// TypeUnion is `A | B | C`.
type TypeUnion struct {
	Location Location
	Types    []TypeExpr
}

// TypeIntersection is `A & B & C`.
type TypeIntersection struct {
	Location Location
	Types    []TypeExpr
}

// TypeGroup is `( T )` in a type annotation.
type TypeGroup struct {
	Location Location
	Inner    TypeExpr
}

// TypeSingletonBool is `true` / `false` as a singleton type.
type TypeSingletonBool struct {
	Location Location
	Value    bool
}

// TypeSingletonString is `"literal"` as a singleton type.
type TypeSingletonString struct {
	Location Location
	Value    []byte
}

// TypeTypeof is `typeof(expr)` as a type annotation.
type TypeTypeof struct {
	Location Location
	Expr     Expr
}

// TypeError is a placeholder emitted by the parser on errors.
type TypeError struct {
	Location   Location
	Types      []TypeExpr
	IsMissing  bool
	MessageIdx uint32
}

// TypePackExplicit is `(T1, T2, ...)` as an explicit type pack.
type TypePackExplicit struct {
	Location Location
	Types    []TypeExpr
	Tail     TypePack // nil if absent
}

// TypePackVariadic is `...T` (a variadic tail).
type TypePackVariadic struct {
	Location Location
	Inner    TypeExpr
}

// TypePackGeneric is `T...` (a generic type-pack reference).
type TypePackGeneric struct {
	Location Location
	Name     string
}

// ----------------------------------------------------------------------
// nodeMarker / exprMarker / statMarker / typeMarker implementations
// ----------------------------------------------------------------------

func (n *ExprGroup) Loc() Location           { return n.Location }
func (n *ExprConstantNil) Loc() Location     { return n.Location }
func (n *ExprConstantBool) Loc() Location    { return n.Location }
func (n *ExprConstantNumber) Loc() Location  { return n.Location }
func (n *ExprConstantInteger) Loc() Location { return n.Location }
func (n *ExprConstantString) Loc() Location  { return n.Location }
func (n *ExprLocal) Loc() Location           { return n.Location }
func (n *ExprGlobal) Loc() Location          { return n.Location }
func (n *ExprVarargs) Loc() Location         { return n.Location }
func (n *ExprCall) Loc() Location            { return n.Location }
func (n *ExprIndexName) Loc() Location       { return n.Location }
func (n *ExprIndexExpr) Loc() Location       { return n.Location }
func (n *ExprFunction) Loc() Location        { return n.Location }
func (n *ExprTable) Loc() Location           { return n.Location }
func (n *ExprUnary) Loc() Location           { return n.Location }
func (n *ExprBinary) Loc() Location          { return n.Location }
func (n *ExprTypeAssertion) Loc() Location   { return n.Location }
func (n *ExprIfElse) Loc() Location          { return n.Location }
func (n *ExprInterpString) Loc() Location    { return n.Location }
func (n *ExprError) Loc() Location           { return n.Location }

func (n *ExprGroup) nodeMarker()           {}
func (n *ExprConstantNil) nodeMarker()     {}
func (n *ExprConstantBool) nodeMarker()    {}
func (n *ExprConstantNumber) nodeMarker()  {}
func (n *ExprConstantInteger) nodeMarker() {}
func (n *ExprConstantString) nodeMarker()  {}
func (n *ExprLocal) nodeMarker()           {}
func (n *ExprGlobal) nodeMarker()          {}
func (n *ExprVarargs) nodeMarker()         {}
func (n *ExprCall) nodeMarker()            {}
func (n *ExprIndexName) nodeMarker()       {}
func (n *ExprIndexExpr) nodeMarker()       {}
func (n *ExprFunction) nodeMarker()        {}
func (n *ExprTable) nodeMarker()           {}
func (n *ExprUnary) nodeMarker()           {}
func (n *ExprBinary) nodeMarker()          {}
func (n *ExprTypeAssertion) nodeMarker()   {}
func (n *ExprIfElse) nodeMarker()          {}
func (n *ExprInterpString) nodeMarker()    {}
func (n *ExprError) nodeMarker()           {}

func (n *ExprGroup) exprMarker()           {}
func (n *ExprConstantNil) exprMarker()     {}
func (n *ExprConstantBool) exprMarker()    {}
func (n *ExprConstantNumber) exprMarker()  {}
func (n *ExprConstantInteger) exprMarker() {}
func (n *ExprConstantString) exprMarker()  {}
func (n *ExprLocal) exprMarker()           {}
func (n *ExprGlobal) exprMarker()          {}
func (n *ExprVarargs) exprMarker()         {}
func (n *ExprCall) exprMarker()            {}
func (n *ExprIndexName) exprMarker()       {}
func (n *ExprIndexExpr) exprMarker()       {}
func (n *ExprFunction) exprMarker()        {}
func (n *ExprTable) exprMarker()           {}
func (n *ExprUnary) exprMarker()           {}
func (n *ExprBinary) exprMarker()          {}
func (n *ExprTypeAssertion) exprMarker()   {}
func (n *ExprIfElse) exprMarker()          {}
func (n *ExprInterpString) exprMarker()    {}
func (n *ExprError) exprMarker()           {}

func (b *Block) Loc() Location { return b.Location }
func (b *Block) nodeMarker()   {}
func (b *Block) statMarker()   {}

func (n *StatIf) Loc() Location             { return n.Location }
func (n *StatWhile) Loc() Location          { return n.Location }
func (n *StatRepeat) Loc() Location         { return n.Location }
func (n *StatBreak) Loc() Location          { return n.Location }
func (n *StatContinue) Loc() Location       { return n.Location }
func (n *StatReturn) Loc() Location         { return n.Location }
func (n *StatExpr) Loc() Location           { return n.Location }
func (n *StatLocal) Loc() Location          { return n.Location }
func (n *StatFor) Loc() Location            { return n.Location }
func (n *StatForIn) Loc() Location          { return n.Location }
func (n *StatAssign) Loc() Location         { return n.Location }
func (n *StatCompoundAssign) Loc() Location { return n.Location }
func (n *StatFunction) Loc() Location       { return n.Location }
func (n *StatLocalFunction) Loc() Location  { return n.Location }
func (n *StatTypeAlias) Loc() Location      { return n.Location }
func (n *StatTypeFunction) Loc() Location   { return n.Location }
func (n *StatError) Loc() Location          { return n.Location }

func (n *StatIf) nodeMarker()             {}
func (n *StatWhile) nodeMarker()          {}
func (n *StatRepeat) nodeMarker()         {}
func (n *StatBreak) nodeMarker()          {}
func (n *StatContinue) nodeMarker()       {}
func (n *StatReturn) nodeMarker()         {}
func (n *StatExpr) nodeMarker()           {}
func (n *StatLocal) nodeMarker()          {}
func (n *StatFor) nodeMarker()            {}
func (n *StatForIn) nodeMarker()          {}
func (n *StatAssign) nodeMarker()         {}
func (n *StatCompoundAssign) nodeMarker() {}
func (n *StatFunction) nodeMarker()       {}
func (n *StatLocalFunction) nodeMarker()  {}
func (n *StatTypeAlias) nodeMarker()      {}
func (n *StatTypeFunction) nodeMarker()   {}
func (n *StatError) nodeMarker()          {}

func (n *StatIf) statMarker()             {}
func (n *StatWhile) statMarker()          {}
func (n *StatRepeat) statMarker()         {}
func (n *StatBreak) statMarker()          {}
func (n *StatContinue) statMarker()       {}
func (n *StatReturn) statMarker()         {}
func (n *StatExpr) statMarker()           {}
func (n *StatLocal) statMarker()          {}
func (n *StatFor) statMarker()            {}
func (n *StatForIn) statMarker()          {}
func (n *StatAssign) statMarker()         {}
func (n *StatCompoundAssign) statMarker() {}
func (n *StatFunction) statMarker()       {}
func (n *StatLocalFunction) statMarker()  {}
func (n *StatTypeAlias) statMarker()      {}
func (n *StatTypeFunction) statMarker()   {}
func (n *StatError) statMarker()          {}

func (n *TypeReference) Loc() Location       { return n.Location }
func (n *TypeTable) Loc() Location           { return n.Location }
func (n *TypeFunction) Loc() Location        { return n.Location }
func (n *TypeOptional) Loc() Location        { return n.Location }
func (n *TypeUnion) Loc() Location           { return n.Location }
func (n *TypeIntersection) Loc() Location    { return n.Location }
func (n *TypeGroup) Loc() Location           { return n.Location }
func (n *TypeSingletonBool) Loc() Location   { return n.Location }
func (n *TypeSingletonString) Loc() Location { return n.Location }
func (n *TypeTypeof) Loc() Location          { return n.Location }
func (n *TypeError) Loc() Location           { return n.Location }

func (n *TypeReference) nodeMarker()       {}
func (n *TypeTable) nodeMarker()           {}
func (n *TypeFunction) nodeMarker()        {}
func (n *TypeOptional) nodeMarker()        {}
func (n *TypeUnion) nodeMarker()           {}
func (n *TypeIntersection) nodeMarker()    {}
func (n *TypeGroup) nodeMarker()           {}
func (n *TypeSingletonBool) nodeMarker()   {}
func (n *TypeSingletonString) nodeMarker() {}
func (n *TypeTypeof) nodeMarker()          {}
func (n *TypeError) nodeMarker()           {}

func (n *TypeReference) typeMarker()       {}
func (n *TypeTable) typeMarker()           {}
func (n *TypeFunction) typeMarker()        {}
func (n *TypeOptional) typeMarker()        {}
func (n *TypeUnion) typeMarker()           {}
func (n *TypeIntersection) typeMarker()    {}
func (n *TypeGroup) typeMarker()           {}
func (n *TypeSingletonBool) typeMarker()   {}
func (n *TypeSingletonString) typeMarker() {}
func (n *TypeTypeof) typeMarker()          {}
func (n *TypeError) typeMarker()           {}

func (n *TypePackExplicit) Loc() Location { return n.Location }
func (n *TypePackVariadic) Loc() Location { return n.Location }
func (n *TypePackGeneric) Loc() Location  { return n.Location }

func (n *TypePackExplicit) nodeMarker() {}
func (n *TypePackVariadic) nodeMarker() {}
func (n *TypePackGeneric) nodeMarker()  {}

func (n *TypePackExplicit) typePackMarker() {}
func (n *TypePackVariadic) typePackMarker() {}
func (n *TypePackGeneric) typePackMarker()  {}
