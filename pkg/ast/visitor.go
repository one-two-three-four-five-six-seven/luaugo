// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package ast

// visitor.go implements Walk, which descends an AST and invokes
// Visitor methods on every node.

func walk(v Visitor, n any) {
	switch x := n.(type) {
	case nil:
		return
	case *Program:
		if x != nil && x.Root != nil {
			walk(v, x.Root)
		}
	case *Block:
		if x == nil {
			return
		}
		// Visit block-as-statement for symmetry with upstream when called on Stat.
		for _, s := range x.Body {
			walk(v, s)
		}
	case Stat:
		walkStat(v, x)
	case Expr:
		walkExpr(v, x)
	case TypeExpr:
		walkType(v, x)
	case TypePack:
		walkTypePack(v, x)
	}
}

func walkStat(v Visitor, s Stat) {
	if s == nil || !v.VisitStat(s) {
		return
	}
	switch n := s.(type) {
	case *Block:
		for _, st := range n.Body {
			walkStat(v, st)
		}
	case *StatIf:
		walkExpr(v, n.Condition)
		if n.ThenBody != nil {
			walkStat(v, n.ThenBody)
		}
		if n.ElseBody != nil {
			walkStat(v, n.ElseBody)
		}
	case *StatWhile:
		walkExpr(v, n.Condition)
		walkStat(v, n.Body)
	case *StatRepeat:
		walkStat(v, n.Body)
		walkExpr(v, n.Condition)
	case *StatBreak, *StatContinue:
		// no children
	case *StatReturn:
		for _, e := range n.List {
			walkExpr(v, e)
		}
	case *StatExpr:
		walkExpr(v, n.Expr)
	case *StatLocal:
		for _, l := range n.Vars {
			if l != nil && l.Annotation != nil {
				walkType(v, l.Annotation)
			}
		}
		for _, e := range n.Values {
			walkExpr(v, e)
		}
	case *StatFor:
		if n.Var != nil && n.Var.Annotation != nil {
			walkType(v, n.Var.Annotation)
		}
		walkExpr(v, n.From)
		walkExpr(v, n.To)
		if n.Step != nil {
			walkExpr(v, n.Step)
		}
		walkStat(v, n.Body)
	case *StatForIn:
		for _, l := range n.Vars {
			if l != nil && l.Annotation != nil {
				walkType(v, l.Annotation)
			}
		}
		for _, e := range n.Values {
			walkExpr(v, e)
		}
		walkStat(v, n.Body)
	case *StatAssign:
		for _, lhs := range n.Vars {
			walkExpr(v, lhs)
		}
		for _, rhs := range n.Values {
			walkExpr(v, rhs)
		}
	case *StatCompoundAssign:
		walkExpr(v, n.Var)
		walkExpr(v, n.Value)
	case *StatFunction:
		walkExpr(v, n.Name)
		walkExpr(v, n.Func)
	case *StatLocalFunction:
		walkExpr(v, n.Func)
	case *StatTypeAlias:
		walkType(v, n.Type)
	case *StatTypeFunction:
		walkExpr(v, n.Func)
	case *StatError:
		for _, e := range n.Expressions {
			walkExpr(v, e)
		}
		for _, st := range n.Statements {
			walkStat(v, st)
		}
	}
}

func walkExpr(v Visitor, e Expr) {
	if e == nil || !v.VisitExpr(e) {
		return
	}
	switch n := e.(type) {
	case *ExprGroup:
		walkExpr(v, n.Expr)
	case *ExprConstantNil, *ExprConstantBool, *ExprConstantNumber, *ExprConstantInteger, *ExprConstantString, *ExprLocal, *ExprGlobal, *ExprVarargs:
		// leaves
	case *ExprCall:
		walkExpr(v, n.Func)
		for _, a := range n.Args {
			walkExpr(v, a)
		}
	case *ExprIndexName:
		walkExpr(v, n.Expr)
	case *ExprIndexExpr:
		walkExpr(v, n.Expr)
		walkExpr(v, n.Index)
	case *ExprFunction:
		for _, l := range n.Args {
			if l != nil && l.Annotation != nil {
				walkType(v, l.Annotation)
			}
		}
		if n.VarargAnnot != nil {
			walkTypePack(v, n.VarargAnnot)
		}
		if n.ReturnAnnot != nil {
			walkTypePack(v, n.ReturnAnnot)
		}
		if n.Body != nil {
			walkStat(v, n.Body)
		}
	case *ExprTable:
		for _, it := range n.Items {
			if it.Key != nil {
				walkExpr(v, it.Key)
			}
			if it.Value != nil {
				walkExpr(v, it.Value)
			}
		}
	case *ExprUnary:
		walkExpr(v, n.Operand)
	case *ExprBinary:
		walkExpr(v, n.Lhs)
		walkExpr(v, n.Rhs)
	case *ExprTypeAssertion:
		walkExpr(v, n.Expr)
		walkType(v, n.Type)
	case *ExprIfElse:
		walkExpr(v, n.Condition)
		walkExpr(v, n.True)
		walkExpr(v, n.False)
	case *ExprInterpString:
		for _, sub := range n.Expressions {
			walkExpr(v, sub)
		}
	case *ExprError:
		for _, sub := range n.Expressions {
			walkExpr(v, sub)
		}
	}
}

func walkType(v Visitor, t TypeExpr) {
	if t == nil || !v.VisitType(t) {
		return
	}
	switch n := t.(type) {
	case *TypeReference:
		for _, p := range n.Parameters {
			if p.Type != nil {
				walkType(v, p.Type)
			}
			if p.Pack != nil {
				walkTypePack(v, p.Pack)
			}
		}
	case *TypeTable:
		for _, prop := range n.Props {
			walkType(v, prop.Type)
		}
		if n.Indexer != nil {
			walkType(v, n.Indexer.IndexType)
			walkType(v, n.Indexer.ValueType)
		}
	case *TypeFunction:
		for _, p := range n.ArgTypes {
			walkType(v, p)
		}
		if n.ArgVararg != nil {
			walkTypePack(v, n.ArgVararg)
		}
		if n.ReturnType != nil {
			walkTypePack(v, n.ReturnType)
		}
	case *TypeOptional:
		walkType(v, n.Inner)
	case *TypeUnion:
		for _, p := range n.Types {
			walkType(v, p)
		}
	case *TypeIntersection:
		for _, p := range n.Types {
			walkType(v, p)
		}
	case *TypeGroup:
		walkType(v, n.Inner)
	case *TypeSingletonBool, *TypeSingletonString:
		// leaf
	case *TypeTypeof:
		walkExpr(v, n.Expr)
	case *TypeError:
		for _, p := range n.Types {
			walkType(v, p)
		}
	}
}

func walkTypePack(v Visitor, tp TypePack) {
	if tp == nil || !v.VisitTypePack(tp) {
		return
	}
	switch n := tp.(type) {
	case *TypePackExplicit:
		for _, t := range n.Types {
			walkType(v, t)
		}
		if n.Tail != nil {
			walkTypePack(v, n.Tail)
		}
	case *TypePackVariadic:
		walkType(v, n.Inner)
	case *TypePackGeneric:
		// leaf
	}
}
