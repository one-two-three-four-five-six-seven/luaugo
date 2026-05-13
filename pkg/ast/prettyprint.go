// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package ast

// prettyprint.go produces a debugging-friendly s-expression-style dump
// of an AST. It is not source-faithful.

import (
	"fmt"
	"io"
	"strings"
)

func prettyPrint(w io.Writer, n Node) error {
	var b strings.Builder
	ppNode(&b, n, 0)
	_, err := io.WriteString(w, b.String())
	return err
}

func indent(b *strings.Builder, depth int) {
	for i := 0; i < depth; i++ {
		b.WriteString("  ")
	}
}

func ppNode(b *strings.Builder, n Node, depth int) {
	if n == nil {
		indent(b, depth)
		b.WriteString("(nil)\n")
		return
	}
	switch x := n.(type) {
	case *Block:
		ppBlock(b, x, depth)
	case Stat:
		ppStat(b, x, depth)
	case Expr:
		ppExpr(b, x, depth)
	case TypeExpr:
		ppType(b, x, depth)
	case TypePack:
		ppTypePack(b, x, depth)
	default:
		indent(b, depth)
		fmt.Fprintf(b, "(unknown %T)\n", n)
	}
}

func ppBlock(b *strings.Builder, blk *Block, depth int) {
	indent(b, depth)
	b.WriteString("(block\n")
	for _, s := range blk.Body {
		ppStat(b, s, depth+1)
	}
	indent(b, depth)
	b.WriteString(")\n")
}

func ppStat(b *strings.Builder, s Stat, depth int) {
	indent(b, depth)
	switch x := s.(type) {
	case *Block:
		b.WriteString("(do\n")
		for _, st := range x.Body {
			ppStat(b, st, depth+1)
		}
		indent(b, depth)
		b.WriteString(")\n")
	case *StatIf:
		b.WriteString("(if\n")
		ppExpr(b, x.Condition, depth+1)
		ppStat(b, x.ThenBody, depth+1)
		if x.ElseBody != nil {
			ppStat(b, x.ElseBody, depth+1)
		}
		indent(b, depth)
		b.WriteString(")\n")
	case *StatWhile:
		b.WriteString("(while\n")
		ppExpr(b, x.Condition, depth+1)
		ppStat(b, x.Body, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *StatRepeat:
		b.WriteString("(repeat\n")
		ppStat(b, x.Body, depth+1)
		ppExpr(b, x.Condition, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *StatBreak:
		b.WriteString("(break)\n")
	case *StatContinue:
		b.WriteString("(continue)\n")
	case *StatReturn:
		b.WriteString("(return\n")
		for _, e := range x.List {
			ppExpr(b, e, depth+1)
		}
		indent(b, depth)
		b.WriteString(")\n")
	case *StatExpr:
		b.WriteString("(stat-expr\n")
		ppExpr(b, x.Expr, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *StatLocal:
		b.WriteString("(local")
		for _, v := range x.Vars {
			fmt.Fprintf(b, " %s", v.Name)
		}
		b.WriteString("\n")
		for _, e := range x.Values {
			ppExpr(b, e, depth+1)
		}
		indent(b, depth)
		b.WriteString(")\n")
	case *StatFor:
		fmt.Fprintf(b, "(for %s\n", x.Var.Name)
		ppExpr(b, x.From, depth+1)
		ppExpr(b, x.To, depth+1)
		if x.Step != nil {
			ppExpr(b, x.Step, depth+1)
		}
		ppStat(b, x.Body, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *StatForIn:
		b.WriteString("(for-in")
		for _, v := range x.Vars {
			fmt.Fprintf(b, " %s", v.Name)
		}
		b.WriteString("\n")
		for _, e := range x.Values {
			ppExpr(b, e, depth+1)
		}
		ppStat(b, x.Body, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *StatAssign:
		b.WriteString("(assign\n")
		for _, e := range x.Vars {
			ppExpr(b, e, depth+1)
		}
		for _, e := range x.Values {
			ppExpr(b, e, depth+1)
		}
		indent(b, depth)
		b.WriteString(")\n")
	case *StatCompoundAssign:
		fmt.Fprintf(b, "(compound %d\n", x.Op)
		ppExpr(b, x.Var, depth+1)
		ppExpr(b, x.Value, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *StatFunction:
		b.WriteString("(function-stat\n")
		ppExpr(b, x.Name, depth+1)
		ppExpr(b, x.Func, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *StatLocalFunction:
		fmt.Fprintf(b, "(local-function %s\n", x.Name.Name)
		ppExpr(b, x.Func, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *StatTypeAlias:
		fmt.Fprintf(b, "(type-alias %s)\n", x.Name)
	case *StatTypeFunction:
		fmt.Fprintf(b, "(type-function %s)\n", x.Name)
	case *StatError:
		b.WriteString("(stat-error)\n")
	default:
		fmt.Fprintf(b, "(unknown-stat %T)\n", s)
	}
}

func ppExpr(b *strings.Builder, e Expr, depth int) {
	indent(b, depth)
	switch x := e.(type) {
	case *ExprGroup:
		b.WriteString("(group\n")
		ppExpr(b, x.Expr, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *ExprConstantNil:
		b.WriteString("(nil)\n")
	case *ExprConstantBool:
		fmt.Fprintf(b, "(bool %v)\n", x.Value)
	case *ExprConstantNumber:
		fmt.Fprintf(b, "(number %g)\n", x.Value)
	case *ExprConstantInteger:
		fmt.Fprintf(b, "(integer %d)\n", x.Value)
	case *ExprConstantString:
		fmt.Fprintf(b, "(string %q)\n", string(x.Value))
	case *ExprLocal:
		fmt.Fprintf(b, "(local-ref %s)\n", x.Local.Name)
	case *ExprGlobal:
		fmt.Fprintf(b, "(global-ref %s)\n", x.Name)
	case *ExprVarargs:
		b.WriteString("(...)\n")
	case *ExprCall:
		b.WriteString("(call\n")
		ppExpr(b, x.Func, depth+1)
		for _, a := range x.Args {
			ppExpr(b, a, depth+1)
		}
		indent(b, depth)
		b.WriteString(")\n")
	case *ExprIndexName:
		fmt.Fprintf(b, "(index-name %c%s\n", x.Op, x.IndexName)
		ppExpr(b, x.Expr, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *ExprIndexExpr:
		b.WriteString("(index-expr\n")
		ppExpr(b, x.Expr, depth+1)
		ppExpr(b, x.Index, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *ExprFunction:
		b.WriteString("(function\n")
		if x.Body != nil {
			ppBlock(b, x.Body, depth+1)
		}
		indent(b, depth)
		b.WriteString(")\n")
	case *ExprTable:
		b.WriteString("(table\n")
		for _, it := range x.Items {
			indent(b, depth+1)
			b.WriteString("(item")
			if it.Key != nil {
				b.WriteString("\n")
				ppExpr(b, it.Key, depth+2)
				ppExpr(b, it.Value, depth+2)
				indent(b, depth+1)
			} else {
				b.WriteString("\n")
				ppExpr(b, it.Value, depth+2)
				indent(b, depth+1)
			}
			b.WriteString(")\n")
		}
		indent(b, depth)
		b.WriteString(")\n")
	case *ExprUnary:
		fmt.Fprintf(b, "(unary %d\n", x.Op)
		ppExpr(b, x.Operand, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *ExprBinary:
		fmt.Fprintf(b, "(binary %d\n", x.Op)
		ppExpr(b, x.Lhs, depth+1)
		ppExpr(b, x.Rhs, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *ExprTypeAssertion:
		b.WriteString("(type-assertion\n")
		ppExpr(b, x.Expr, depth+1)
		ppType(b, x.Type, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *ExprIfElse:
		b.WriteString("(if-else-expr\n")
		ppExpr(b, x.Condition, depth+1)
		ppExpr(b, x.True, depth+1)
		ppExpr(b, x.False, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *ExprInterpString:
		b.WriteString("(interp-string\n")
		for i, s := range x.Strings {
			indent(b, depth+1)
			fmt.Fprintf(b, "(part %q)\n", string(s))
			if i < len(x.Expressions) {
				ppExpr(b, x.Expressions[i], depth+1)
			}
		}
		indent(b, depth)
		b.WriteString(")\n")
	case *ExprError:
		b.WriteString("(expr-error)\n")
	default:
		fmt.Fprintf(b, "(unknown-expr %T)\n", e)
	}
}

func ppType(b *strings.Builder, t TypeExpr, depth int) {
	indent(b, depth)
	switch x := t.(type) {
	case *TypeReference:
		if x.Prefix != "" {
			fmt.Fprintf(b, "(type-ref %s.%s)\n", x.Prefix, x.Name)
		} else {
			fmt.Fprintf(b, "(type-ref %s)\n", x.Name)
		}
	case *TypeTable:
		b.WriteString("(type-table)\n")
	case *TypeFunction:
		b.WriteString("(type-function)\n")
	case *TypeOptional:
		b.WriteString("(type-optional\n")
		ppType(b, x.Inner, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *TypeUnion:
		b.WriteString("(type-union\n")
		for _, p := range x.Types {
			ppType(b, p, depth+1)
		}
		indent(b, depth)
		b.WriteString(")\n")
	case *TypeIntersection:
		b.WriteString("(type-intersection\n")
		for _, p := range x.Types {
			ppType(b, p, depth+1)
		}
		indent(b, depth)
		b.WriteString(")\n")
	case *TypeGroup:
		b.WriteString("(type-group\n")
		ppType(b, x.Inner, depth+1)
		indent(b, depth)
		b.WriteString(")\n")
	case *TypeSingletonBool:
		fmt.Fprintf(b, "(type-bool %v)\n", x.Value)
	case *TypeSingletonString:
		fmt.Fprintf(b, "(type-string %q)\n", string(x.Value))
	case *TypeTypeof:
		b.WriteString("(typeof)\n")
	case *TypeError:
		b.WriteString("(type-error)\n")
	default:
		fmt.Fprintf(b, "(unknown-type %T)\n", t)
	}
}

func ppTypePack(b *strings.Builder, tp TypePack, depth int) {
	indent(b, depth)
	switch x := tp.(type) {
	case *TypePackExplicit:
		b.WriteString("(type-pack-explicit)\n")
		_ = x
	case *TypePackVariadic:
		b.WriteString("(type-pack-variadic)\n")
	case *TypePackGeneric:
		fmt.Fprintf(b, "(type-pack-generic %s)\n", x.Name)
	default:
		fmt.Fprintf(b, "(unknown-type-pack %T)\n", tp)
	}
}
