// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package ast

// AST -> JSON serialization mirroring upstream's AstJsonEncoder
// (.upstream/Analysis/src/AstJsonEncoder.cpp). Every node type
// produces an object of shape
//
//	{ "type": "AstFooBar", "location": "L,C - L,C", ...fields... }
//
// with fields in the upstream-defined order. Positions are encoded in
// the upstream format "line,column" (0-based, as stored in
// ast.Position). The encoder is the backend for `luau-ast` and any
// future tooling that needs a stable JSON projection of a parsed
// program.
//
// The output is intentionally NOT routed through encoding/json: the
// upstream encoder hand-writes a specific layout (no field reordering,
// raw NaN/Infinity tokens) that downstream consumers (e.g. the Luau
// language tests) depend on. We match that byte stream as closely as
// possible.

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// ToJSON returns a JSON projection of program's root block, wrapped
// with a `commentLocations` sidecar so the output mirrors upstream's
// `toJson(AstNode*, const std::vector<Comment>&)` overload
// (AstJsonEncoder.cpp:1562-1571).
func ToJSON(p *Program) string {
	var b strings.Builder
	e := jsonEncoder{w: &b}
	e.raw(`{"root":`)
	e.writeBlock(p.Root)
	e.raw(`,"commentLocations":[`)
	for i, loc := range p.CommentLocs {
		if i > 0 {
			e.raw(",")
		}
		e.raw(`{"location":`)
		e.writeLocation(loc)
		// Upstream tracks the lexeme kind (Comment / BlockComment /
		// BrokenComment); our AST stores only locations because the
		// parser already discarded the lexical category, so we emit
		// "Comment" unconditionally. Tools that distinguish block vs
		// inline comments based on JSON shape should re-tokenize.
		e.raw(`,"type":"Comment"}`)
	}
	e.raw("]}")
	return b.String()
}

// ToJSONNode returns a JSON projection of a single node, without the
// program-level commentLocations wrapper. Mirrors upstream's
// `toJson(AstNode*)` overload (AstJsonEncoder.cpp:1555-1560).
func ToJSONNode(n Node) string {
	var b strings.Builder
	e := jsonEncoder{w: &b}
	e.writeNode(n)
	return b.String()
}

// jsonEncoder writes the upstream-shaped JSON stream into w.
type jsonEncoder struct {
	w io.Writer
}

func (e *jsonEncoder) raw(s string) { _, _ = io.WriteString(e.w, s) }

func (e *jsonEncoder) writeString(s string) {
	// Match upstream's writeString in AstJsonEncoder.cpp:177-205:
	// JSON-escape backslash, double quote, control characters, and
	// surrogate-half codepoints. UTF-8 high bytes are passed through.
	e.raw(`"`)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\':
			e.raw(`\\`)
		case c == '"':
			e.raw(`\"`)
		case c == '\n':
			e.raw(`\n`)
		case c == '\r':
			e.raw(`\r`)
		case c == '\t':
			e.raw(`\t`)
		case c == '\b':
			e.raw(`\b`)
		case c == '\f':
			e.raw(`\f`)
		case c < 0x20:
			fmt.Fprintf(e.w, `\u%04x`, c)
		default:
			e.w.Write([]byte{c})
		}
	}
	e.raw(`"`)
}

func (e *jsonEncoder) writeBool(b bool) {
	if b {
		e.raw("true")
	} else {
		e.raw("false")
	}
}

// writeNumber mirrors upstream's write(double) (AstJsonEncoder.cpp:108-132):
// emits raw `Infinity` / `-Infinity` / `NaN` for non-finite values, and
// "%.17g" for everything else. JSON-strict consumers must post-process
// the non-finite tokens, but the Luau test suite depends on this exact
// output.
func (e *jsonEncoder) writeNumber(d float64) {
	switch {
	case math.IsInf(d, 1):
		e.raw("Infinity")
	case math.IsInf(d, -1):
		e.raw("-Infinity")
	case math.IsNaN(d):
		e.raw("NaN")
	default:
		// strconv with -1 precision gives shortest round-trip; we
		// switch to %.17g style with 17 significant digits to match
		// upstream's snprintf("%.17g") exactly.
		e.raw(strconv.FormatFloat(d, 'g', 17, 64))
	}
}

func (e *jsonEncoder) writeInt(i int64)  { e.raw(strconv.FormatInt(i, 10)) }
func (e *jsonEncoder) writeUint(u uint64) { e.raw(strconv.FormatUint(u, 10)) }

// writePosition emits "line,column" (no quotes). Used as a building
// block of writeLocation.
func (e *jsonEncoder) writePosition(p Position) {
	e.writeUint(uint64(p.Line))
	e.raw(",")
	e.writeUint(uint64(p.Column))
}

// writeLocation emits "L,C - L,C" as a JSON string literal.
func (e *jsonEncoder) writeLocation(loc Location) {
	e.raw(`"`)
	e.writePosition(loc.Begin)
	e.raw(" - ")
	e.writePosition(loc.End)
	e.raw(`"`)
}

// field emits `"name":` (the colon, no value). The caller is
// responsible for serializing the value and any leading comma.
func (e *jsonEncoder) field(name string, leadComma bool) {
	if leadComma {
		e.raw(",")
	}
	e.writeString(name)
	e.raw(":")
}

// header opens a node object with `{"type":"AstFoo","location":"..."`.
// It returns true so the caller can pass it as the "comma needed for
// the next field" flag.
func (e *jsonEncoder) header(typeName string, loc Location) bool {
	e.raw(`{"type":`)
	e.writeString(typeName)
	e.raw(`,"location":`)
	e.writeLocation(loc)
	return true
}

// ---------------------------------------------------------------------------
// Node dispatch
// ---------------------------------------------------------------------------

// writeNode emits any AST node by switching on its concrete type. The
// schema mirrors AstJsonEncoder.cpp; node-specific helpers below are
// invoked from this central dispatcher so that recursive children
// share the same machinery.
func (e *jsonEncoder) writeNode(n Node) {
	if n == nil {
		e.raw("null")
		return
	}
	switch x := n.(type) {
	// Expressions
	case *ExprGroup:
		c := e.header("AstExprGroup", x.Location)
		e.field("expr", c)
		e.writeNode(x.Expr)
		e.raw("}")
	case *ExprConstantNil:
		e.header("AstExprConstantNil", x.Location)
		e.raw("}")
	case *ExprConstantBool:
		c := e.header("AstExprConstantBool", x.Location)
		e.field("value", c)
		e.writeBool(x.Value)
		e.raw("}")
	case *ExprConstantNumber:
		c := e.header("AstExprConstantNumber", x.Location)
		e.field("value", c)
		e.writeNumber(x.Value)
		e.raw("}")
	case *ExprConstantInteger:
		c := e.header("AstExprConstantInteger", x.Location)
		e.field("value", c)
		e.writeInt(x.Value)
		e.raw("}")
	case *ExprConstantString:
		c := e.header("AstExprConstantString", x.Location)
		e.field("value", c)
		e.writeString(string(x.Value))
		e.raw("}")
	case *ExprLocal:
		c := e.header("AstExprLocal", x.Location)
		e.field("local", c)
		e.writeLocal(x.Local)
		e.raw("}")
	case *ExprGlobal:
		c := e.header("AstExprGlobal", x.Location)
		e.field("global", c)
		e.writeString(x.Name)
		e.raw("}")
	case *ExprVarargs:
		e.header("AstExprVarargs", x.Location)
		e.raw("}")
	case *ExprCall:
		c := e.header("AstExprCall", x.Location)
		e.field("func", c)
		e.writeNode(x.Func)
		e.field("args", true)
		e.writeNodeArray(exprsToNodes(x.Args))
		e.field("self", true)
		e.writeBool(x.Self)
		e.field("argLocation", true)
		e.writeLocation(x.ArgLocation)
		e.raw("}")
	case *ExprIndexName:
		c := e.header("AstExprIndexName", x.Location)
		e.field("expr", c)
		e.writeNode(x.Expr)
		e.field("index", true)
		e.writeString(x.IndexName)
		e.field("indexLocation", true)
		e.writeLocation(x.IndexLoc)
		e.field("op", true)
		// Upstream stores op as a single char; we encode that as a
		// one-char JSON string so the field shape matches.
		e.writeString(string(x.Op))
		e.raw("}")
	case *ExprIndexExpr:
		c := e.header("AstExprIndexExpr", x.Location)
		e.field("expr", c)
		e.writeNode(x.Expr)
		e.field("index", true)
		e.writeNode(x.Index)
		e.raw("}")
	case *ExprFunction:
		e.writeExprFunction(x)
	case *ExprTable:
		c := e.header("AstExprTable", x.Location)
		e.field("items", c)
		e.raw("[")
		for i, item := range x.Items {
			if i > 0 {
				e.raw(",")
			}
			e.writeTableItem(item)
		}
		e.raw("]")
		e.raw("}")
	case *ExprUnary:
		c := e.header("AstExprUnary", x.Location)
		e.field("op", c)
		e.writeUnaryOp(x.Op)
		e.field("expr", true)
		e.writeNode(x.Operand)
		e.raw("}")
	case *ExprBinary:
		c := e.header("AstExprBinary", x.Location)
		e.field("op", c)
		e.writeBinaryOp(x.Op)
		e.field("left", true)
		e.writeNode(x.Lhs)
		e.field("right", true)
		e.writeNode(x.Rhs)
		e.raw("}")
	case *ExprTypeAssertion:
		c := e.header("AstExprTypeAssertion", x.Location)
		e.field("expr", c)
		e.writeNode(x.Expr)
		e.field("annotation", true)
		e.writeNode(x.Type)
		e.raw("}")
	case *ExprIfElse:
		c := e.header("AstExprIfElse", x.Location)
		e.field("condition", c)
		e.writeNode(x.Condition)
		e.field("hasThen", true)
		e.writeBool(x.HasThen)
		e.field("trueExpr", true)
		e.writeNode(x.True)
		e.field("hasElse", true)
		e.writeBool(x.HasElse)
		e.field("falseExpr", true)
		e.writeNode(x.False)
		e.raw("}")
	case *ExprInterpString:
		c := e.header("AstExprInterpString", x.Location)
		e.field("strings", c)
		e.raw("[")
		for i, s := range x.Strings {
			if i > 0 {
				e.raw(",")
			}
			e.writeString(string(s))
		}
		e.raw("]")
		e.field("expressions", true)
		e.writeNodeArray(exprsToNodes(x.Expressions))
		e.raw("}")
	case *ExprError:
		c := e.header("AstExprError", x.Location)
		e.field("expressions", c)
		e.writeNodeArray(exprsToNodes(x.Expressions))
		e.field("messageIndex", true)
		e.writeUint(uint64(x.MessageIdx))
		e.raw("}")

	// Statements
	case *Block:
		e.writeBlock(x)
	case *StatIf:
		c := e.header("AstStatIf", x.Location)
		e.field("condition", c)
		e.writeNode(x.Condition)
		e.field("thenbody", true)
		e.writeBlock(x.ThenBody)
		if x.ElseBody != nil {
			e.field("elsebody", true)
			e.writeNode(x.ElseBody)
		}
		e.field("hasThen", true)
		e.writeBool(x.HasThen)
		e.raw("}")
	case *StatWhile:
		c := e.header("AstStatWhile", x.Location)
		e.field("condition", c)
		e.writeNode(x.Condition)
		e.field("body", true)
		e.writeBlock(x.Body)
		e.field("hasDo", true)
		e.writeBool(x.HasDo)
		e.raw("}")
	case *StatRepeat:
		c := e.header("AstStatRepeat", x.Location)
		e.field("condition", c)
		e.writeNode(x.Condition)
		e.field("body", true)
		e.writeBlock(x.Body)
		e.raw("}")
	case *StatBreak:
		e.header("AstStatBreak", x.Location)
		e.raw("}")
	case *StatContinue:
		e.header("AstStatContinue", x.Location)
		e.raw("}")
	case *StatReturn:
		c := e.header("AstStatReturn", x.Location)
		e.field("list", c)
		e.writeNodeArray(exprsToNodes(x.List))
		e.raw("}")
	case *StatExpr:
		c := e.header("AstStatExpr", x.Location)
		e.field("expr", c)
		e.writeNode(x.Expr)
		e.raw("}")
	case *StatLocal:
		c := e.header("AstStatLocal", x.Location)
		e.field("vars", c)
		e.raw("[")
		for i, v := range x.Vars {
			if i > 0 {
				e.raw(",")
			}
			e.writeLocal(v)
		}
		e.raw("]")
		e.field("values", true)
		e.writeNodeArray(exprsToNodes(x.Values))
		e.raw("}")
	case *StatFor:
		c := e.header("AstStatFor", x.Location)
		e.field("var", c)
		e.writeLocal(x.Var)
		e.field("from", true)
		e.writeNode(x.From)
		e.field("to", true)
		e.writeNode(x.To)
		if x.Step != nil {
			e.field("step", true)
			e.writeNode(x.Step)
		}
		e.field("body", true)
		e.writeBlock(x.Body)
		e.field("hasDo", true)
		e.writeBool(x.HasDo)
		e.raw("}")
	case *StatForIn:
		c := e.header("AstStatForIn", x.Location)
		e.field("vars", c)
		e.raw("[")
		for i, v := range x.Vars {
			if i > 0 {
				e.raw(",")
			}
			e.writeLocal(v)
		}
		e.raw("]")
		e.field("values", true)
		e.writeNodeArray(exprsToNodes(x.Values))
		e.field("body", true)
		e.writeBlock(x.Body)
		e.field("hasIn", true)
		e.writeBool(x.HasIn)
		e.field("hasDo", true)
		e.writeBool(x.HasDo)
		e.raw("}")
	case *StatAssign:
		c := e.header("AstStatAssign", x.Location)
		e.field("vars", c)
		e.writeNodeArray(exprsToNodes(x.Vars))
		e.field("values", true)
		e.writeNodeArray(exprsToNodes(x.Values))
		e.raw("}")
	case *StatCompoundAssign:
		c := e.header("AstStatCompoundAssign", x.Location)
		e.field("op", c)
		e.writeBinaryOp(x.Op)
		e.field("var", true)
		e.writeNode(x.Var)
		e.field("value", true)
		e.writeNode(x.Value)
		e.raw("}")
	case *StatFunction:
		c := e.header("AstStatFunction", x.Location)
		e.field("name", c)
		e.writeNode(x.Name)
		e.field("func", true)
		e.writeNode(x.Func)
		e.raw("}")
	case *StatLocalFunction:
		c := e.header("AstStatLocalFunction", x.Location)
		e.field("name", c)
		e.writeLocal(x.Name)
		e.field("func", true)
		e.writeNode(x.Func)
		e.raw("}")
	case *StatTypeAlias:
		c := e.header("AstStatTypeAlias", x.Location)
		e.field("name", c)
		e.writeString(x.Name)
		e.field("generics", true)
		e.writeGenerics(x.Generics)
		e.field("genericPacks", true)
		e.writeGenericPacks(x.GenericPacks)
		e.field("value", true)
		e.writeNode(x.Type)
		e.field("exported", true)
		e.writeBool(x.Exported)
		e.raw("}")
	case *StatTypeFunction:
		// Upstream's StatTypeFunction was renamed to
		// AstStatDeclareFunction in newer releases; we emit it under
		// the legacy name so consumers that switch on `type` find a
		// stable string. The body proto is encoded under "func".
		c := e.header("AstStatTypeFunction", x.Location)
		e.field("name", c)
		e.writeString(x.Name)
		e.field("func", true)
		e.writeNode(x.Func)
		e.field("exported", true)
		e.writeBool(x.Exported)
		e.raw("}")
	case *StatError:
		c := e.header("AstStatError", x.Location)
		e.field("expressions", c)
		e.writeNodeArray(exprsToNodes(x.Expressions))
		e.field("statements", true)
		e.writeNodeArray(statsToNodes(x.Statements))
		e.raw("}")

	// Type annotation nodes
	case *TypeReference:
		c := e.header("AstTypeReference", x.Location)
		if x.Prefix != "" {
			e.field("prefix", c)
			e.writeString(x.Prefix)
			c = true
		}
		e.field("name", c)
		e.writeString(x.Name)
		e.field("nameLocation", true)
		e.writeLocation(x.Location)
		e.field("parameters", true)
		e.raw("[")
		for i, p := range x.Parameters {
			if i > 0 {
				e.raw(",")
			}
			e.writeTypeOrPack(p)
		}
		e.raw("]")
		e.raw("}")
	case *TypeTable:
		c := e.header("AstTypeTable", x.Location)
		e.field("props", c)
		e.raw("[")
		for i, p := range x.Props {
			if i > 0 {
				e.raw(",")
			}
			e.writeTableProp(p)
		}
		e.raw("]")
		e.field("indexer", true)
		e.writeTableIndexer(x.Indexer)
		e.raw("}")
	case *TypeFunction:
		c := e.header("AstTypeFunction", x.Location)
		e.field("generics", c)
		e.writeGenerics(x.Generics)
		e.field("genericPacks", true)
		e.writeGenericPacks(x.GenericPacks)
		e.field("argTypes", true)
		e.raw("[")
		for i, t := range x.ArgTypes {
			if i > 0 {
				e.raw(",")
			}
			e.writeNode(t)
		}
		e.raw("]")
		e.field("argNames", true)
		e.raw("[")
		for i, nm := range x.ArgNames {
			if i > 0 {
				e.raw(",")
			}
			if nm == nil {
				e.raw("null")
			} else {
				e.raw(`{"name":`)
				e.writeString(nm.Name)
				e.raw(`,"location":`)
				e.writeLocation(nm.Location)
				e.raw("}")
			}
		}
		e.raw("]")
		e.field("returnTypes", true)
		e.writeNode(x.ReturnType)
		e.raw("}")
	case *TypeTypeof:
		c := e.header("AstTypeTypeof", x.Location)
		e.field("expr", c)
		e.writeNode(x.Expr)
		e.raw("}")
	case *TypeOptional:
		e.header("AstTypeOptional", x.Location)
		e.raw("}")
	case *TypeUnion:
		c := e.header("AstTypeUnion", x.Location)
		e.field("types", c)
		e.raw("[")
		for i, t := range x.Types {
			if i > 0 {
				e.raw(",")
			}
			e.writeNode(t)
		}
		e.raw("]}")
	case *TypeIntersection:
		c := e.header("AstTypeIntersection", x.Location)
		e.field("types", c)
		e.raw("[")
		for i, t := range x.Types {
			if i > 0 {
				e.raw(",")
			}
			e.writeNode(t)
		}
		e.raw("]}")
	case *TypeGroup:
		c := e.header("AstTypeGroup", x.Location)
		e.field("inner", c)
		e.writeNode(x.Inner)
		e.raw("}")
	case *TypeSingletonBool:
		c := e.header("AstTypeSingletonBool", x.Location)
		e.field("value", c)
		e.writeBool(x.Value)
		e.raw("}")
	case *TypeSingletonString:
		c := e.header("AstTypeSingletonString", x.Location)
		e.field("value", c)
		e.writeString(string(x.Value))
		e.raw("}")
	case *TypeError:
		c := e.header("AstTypeError", x.Location)
		e.field("types", c)
		e.raw("[")
		for i, t := range x.Types {
			if i > 0 {
				e.raw(",")
			}
			e.writeNode(t)
		}
		e.raw("]")
		e.field("messageIndex", true)
		e.writeUint(uint64(x.MessageIdx))
		e.raw("}")
	case *TypePackExplicit:
		c := e.header("AstTypePackExplicit", x.Location)
		e.field("typeList", c)
		e.raw(`{"type":"AstTypeList","types":[`)
		for i, t := range x.Types {
			if i > 0 {
				e.raw(",")
			}
			e.writeNode(t)
		}
		e.raw("]")
		if x.Tail != nil {
			e.raw(`,"tailType":`)
			e.writeNode(x.Tail)
		}
		e.raw("}}")
	case *TypePackVariadic:
		c := e.header("AstTypePackVariadic", x.Location)
		e.field("variadicType", c)
		e.writeNode(x.Inner)
		e.raw("}")
	case *TypePackGeneric:
		c := e.header("AstTypePackGeneric", x.Location)
		e.field("genericName", c)
		e.writeString(x.Name)
		e.raw("}")
	default:
		// Unknown node type. Emit a sentinel so downstream tooling
		// can flag it instead of crashing; this should never trigger
		// in well-formed code.
		e.raw(`{"type":"Unknown"}`)
	}
}

// writeBlock emits an AstStatBlock with explicit `hasEnd` + `body`
// fields. Mirrors AstJsonEncoder.cpp:689-712.
func (e *jsonEncoder) writeBlock(b *Block) {
	if b == nil {
		e.raw("null")
		return
	}
	e.header("AstStatBlock", b.Location)
	e.raw(`,"hasEnd":`)
	e.writeBool(b.HasEnd)
	e.raw(`,"body":[`)
	for i, s := range b.Body {
		if i > 0 {
			e.raw(",")
		}
		e.writeNode(s)
	}
	e.raw("]}")
}

// writeLocal serializes a Local (declared variable). Upstream's shape:
// { "luauType": T?, "name": "x", "type": "AstLocal", "location": ... }.
// We emit `luauType` only when an annotation is present; `null`
// otherwise -- matching the encoder check at AstJsonEncoder.cpp:241-244.
func (e *jsonEncoder) writeLocal(l *Local) {
	if l == nil {
		e.raw("null")
		return
	}
	e.raw(`{"luauType":`)
	if l.Annotation != nil {
		e.writeNode(l.Annotation)
	} else {
		e.raw("null")
	}
	e.raw(`,"name":`)
	e.writeString(l.Name)
	e.raw(`,"type":"AstLocal","location":`)
	e.writeLocation(l.NameLoc)
	e.raw("}")
}

// writeExprFunction handles function-expression literals, including
// optional self / vararg-annotation slots.
func (e *jsonEncoder) writeExprFunction(x *ExprFunction) {
	c := e.header("AstExprFunction", x.Location)
	e.field("attributes", c)
	e.raw("[")
	for i, a := range x.Attributes {
		if i > 0 {
			e.raw(",")
		}
		e.raw(`{"type":"AstAttr","location":`)
		e.writeLocation(a.Location)
		e.raw(`,"name":`)
		e.writeString(a.Name)
		e.raw("}")
	}
	e.raw("]")
	e.field("generics", true)
	e.writeGenerics(x.Generics)
	e.field("genericPacks", true)
	e.writeGenericPacks(x.GenericPacks)
	if x.Self != nil {
		e.field("self", true)
		e.writeLocal(x.Self)
	}
	e.field("args", true)
	e.raw("[")
	for i, a := range x.Args {
		if i > 0 {
			e.raw(",")
		}
		e.writeLocal(a)
	}
	e.raw("]")
	if x.ReturnAnnot != nil {
		e.field("returnAnnotation", true)
		e.writeNode(x.ReturnAnnot)
	}
	e.field("vararg", true)
	e.writeBool(x.Vararg)
	e.field("varargLocation", true)
	e.writeLocation(x.VarargLoc)
	if x.VarargAnnot != nil {
		e.field("varargAnnotation", true)
		e.writeNode(x.VarargAnnot)
	}
	e.field("body", true)
	e.writeBlock(x.Body)
	e.field("functionDepth", true)
	e.writeUint(uint64(x.FunctionDepth))
	e.field("debugname", true)
	e.writeString(x.DebugName)
	e.raw("}")
}

func (e *jsonEncoder) writeTableItem(it TableItem) {
	e.raw(`{"type":"AstExprTableItem","kind":`)
	switch it.Kind {
	case TableItemList:
		e.writeString("item")
	case TableItemRecord:
		e.writeString("record")
	case TableItemGeneral:
		e.writeString("general")
	default:
		e.writeString("item")
	}
	if it.Kind != TableItemList {
		e.raw(`,"key":`)
		e.writeNode(it.Key)
	}
	e.raw(`,"value":`)
	e.writeNode(it.Value)
	e.raw("}")
}

// writeUnaryOp emits an upstream-canonical op token ("Not"/"Minus"/"Len").
func (e *jsonEncoder) writeUnaryOp(op UnaryOp) {
	switch op {
	case UnaryNot:
		e.writeString("Not")
	case UnaryMinus:
		e.writeString("Minus")
	case UnaryLen:
		e.writeString("Len")
	default:
		e.writeString("Unknown")
	}
}

// writeBinaryOp emits an upstream-canonical op token; mirrors the
// switch in AstJsonEncoder.cpp:608-647.
func (e *jsonEncoder) writeBinaryOp(op BinaryOp) {
	var s string
	switch op {
	case BinaryAdd:
		s = "Add"
	case BinarySub:
		s = "Sub"
	case BinaryMul:
		s = "Mul"
	case BinaryDiv:
		s = "Div"
	case BinaryFloorDiv:
		s = "FloorDiv"
	case BinaryMod:
		s = "Mod"
	case BinaryPow:
		s = "Pow"
	case BinaryConcat:
		s = "Concat"
	case BinaryNotEq:
		s = "CompareNe"
	case BinaryEq:
		s = "CompareEq"
	case BinaryLt:
		s = "CompareLt"
	case BinaryLe:
		s = "CompareLe"
	case BinaryGt:
		s = "CompareGt"
	case BinaryGe:
		s = "CompareGe"
	case BinaryAnd:
		s = "And"
	case BinaryOr:
		s = "Or"
	default:
		s = "Unknown"
	}
	e.writeString(s)
}

func (e *jsonEncoder) writeGenerics(gs []GenericType) {
	e.raw("[")
	for i, g := range gs {
		if i > 0 {
			e.raw(",")
		}
		e.raw(`{"type":"AstGenericType","name":`)
		e.writeString(g.Name)
		if g.Default != nil {
			e.raw(`,"luauType":`)
			e.writeNode(g.Default)
		}
		e.raw("}")
	}
	e.raw("]")
}

func (e *jsonEncoder) writeGenericPacks(gps []GenericTypePack) {
	e.raw("[")
	for i, g := range gps {
		if i > 0 {
			e.raw(",")
		}
		e.raw(`{"type":"AstGenericTypePack","name":`)
		e.writeString(g.Name)
		if g.Default != nil {
			e.raw(`,"luauType":`)
			e.writeNode(g.Default)
		}
		e.raw("}")
	}
	e.raw("]")
}

func (e *jsonEncoder) writeTableProp(p TypeTableProp) {
	e.raw(`{"name":`)
	e.writeString(p.Name)
	e.raw(`,"type":"AstTableProp","location":`)
	e.writeLocation(p.NameLoc)
	e.raw(`,"propType":`)
	e.writeNode(p.Type)
	e.raw("}")
}

func (e *jsonEncoder) writeTableIndexer(ti *TypeTableIndexer) {
	if ti == nil {
		e.raw("null")
		return
	}
	e.raw(`{"location":`)
	e.writeLocation(ti.Location)
	e.raw(`,"indexType":`)
	e.writeNode(ti.IndexType)
	e.raw(`,"resultType":`)
	e.writeNode(ti.ValueType)
	e.raw("}")
}

func (e *jsonEncoder) writeTypeOrPack(t TypeOrPack) {
	if t.Type != nil {
		e.writeNode(t.Type)
		return
	}
	if t.Pack != nil {
		e.writeNode(t.Pack)
		return
	}
	e.raw("null")
}

func (e *jsonEncoder) writeNodeArray(nodes []Node) {
	e.raw("[")
	for i, n := range nodes {
		if i > 0 {
			e.raw(",")
		}
		e.writeNode(n)
	}
	e.raw("]")
}

// exprsToNodes / statsToNodes adapt the typed slices in the AST to the
// Node-typed input expected by writeNodeArray. The runtime conversion
// is cheap because each element is already a pointer.
func exprsToNodes(es []Expr) []Node {
	if len(es) == 0 {
		return nil
	}
	out := make([]Node, len(es))
	for i, e := range es {
		out[i] = e
	}
	return out
}

func statsToNodes(ss []Stat) []Node {
	if len(ss) == 0 {
		return nil
	}
	out := make([]Node, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
