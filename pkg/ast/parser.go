// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package ast

// parser.go is a Go port of Luau's Ast/src/Parser.cpp. It implements a
// recursive-descent parser for the Luau grammar including type
// annotations, generic type parameters, attributes, string
// interpolation, continue, if-else expressions, type-assertion (::),
// compound assignments, and method calls.

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ----------------------------------------------------------------------
// Driver: parse / parseExpr
// ----------------------------------------------------------------------

func parse(chunkname string, source []byte, opts ParseOptions) *ParseResult {
	p := newParser(chunkname, source, opts)
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(*ParseError); ok {
				p.errors = append(p.errors, *pe)
			} else {
				p.errors = append(p.errors, ParseError{Location: p.curLoc(), Msg: fmt.Sprint(r)})
			}
		}
	}()
	root := p.parseChunk()
	lines := int(p.lex.cur.Location.End.Line) + 1
	if len(source) > 0 && source[len(source)-1] != '\n' {
		lines++
	}
	return &ParseResult{
		Program: &Program{
			Root:        root,
			Lines:       lines,
			HotComments: p.hotComments,
			CommentLocs: p.commentLocs,
		},
		Errors:   p.errors,
		Comments: p.captured,
	}
}

func parseExpr(chunkname string, source []byte, opts ParseOptions) (Expr, []ParseError) {
	p := newParser(chunkname, source, opts)
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(*ParseError); ok {
				p.errors = append(p.errors, *pe)
			}
		}
	}()
	expr := p.parseExpr(0)
	return expr, p.errors
}

// ----------------------------------------------------------------------
// Parser state
// ----------------------------------------------------------------------

type parser struct {
	chunkname string
	lex       *luauLexer
	opts      ParseOptions

	errors      []ParseError
	hotComments []HotComment
	commentLocs []Location
	captured    []Token

	headerActive bool

	functionStack []parserFunc
	typeFuncDepth int

	localMap   map[string]*Local
	localStack []*Local

	recursion int
}

type parserFunc struct {
	vararg    bool
	loopDepth int
}

func newParser(chunkname string, source []byte, opts ParseOptions) *parser {
	p := &parser{
		chunkname:    chunkname,
		opts:         opts,
		lex:          newLexer(chunkname, source).(*luauLexer),
		localMap:     make(map[string]*Local),
		headerActive: true,
	}
	p.functionStack = append(p.functionStack, parserFunc{vararg: true})
	// Always skip comments at lexer level so the parser sees clean tokens;
	// we capture them in nextLexeme.
	p.lex.skipComments = true
	p.nextLexeme()
	p.headerActive = false
	return p
}

// nextLexeme advances the lexer by one token, capturing comments and hot
// comments along the way. Mirrors Parser::nextLexeme in upstream.
func (p *parser) nextLexeme() {
	// We want comments delivered too, so temporarily disable skipping.
	p.lex.skipComments = false
	tok := p.lex.Next()
	for tok.Kind == TokenComment || tok.Kind == TokenBlockComment || tok.Kind == TokenBrokenComment {
		if p.opts.CaptureComments {
			p.captured = append(p.captured, tok)
		}
		p.commentLocs = append(p.commentLocs, tok.Location)
		if tok.Kind == TokenBrokenComment {
			break
		}
		if tok.Kind == TokenComment && len(tok.Value) > 0 && tok.Value[0] == '!' {
			text := tok.Value
			end := len(text)
			for end > 0 && isSpaceByte(text[end-1]) {
				end--
			}
			content := string(text[1:end])
			p.hotComments = append(p.hotComments, HotComment{
				Header:   p.headerActive,
				Location: tok.Location,
				Content:  content,
			})
		}
		tok = p.lex.Next()
	}
	p.lex.skipComments = true
}

func (p *parser) cur() Token { return p.lex.cur }

func (p *parser) curLoc() Location { return p.lex.cur.Location }

func (p *parser) prevLoc() Location { return p.lex.prev }

func (p *parser) lookahead() Token { return p.lex.lookahead() }

// ----------------------------------------------------------------------
// Error reporting
// ----------------------------------------------------------------------

func (p *parser) report(loc Location, format string, args ...any) {
	p.errors = append(p.errors, ParseError{Location: loc, Msg: fmt.Sprintf(format, args...)})
}

func (p *parser) reportName(context string) {
	if context != "" {
		p.report(p.curLoc(), "Incorrect identifier in %s", context)
	} else {
		p.report(p.curLoc(), "Incorrect identifier")
	}
}

func (p *parser) raiseError(loc Location, format string, args ...any) {
	pe := &ParseError{Location: loc, Msg: fmt.Sprintf(format, args...)}
	panic(pe)
}

// ----------------------------------------------------------------------
// Expect / consume
// ----------------------------------------------------------------------

func (p *parser) expectAndConsume(kind TokenKind, context string) bool {
	if p.cur().Kind == kind {
		p.nextLexeme()
		return true
	}
	p.report(p.curLoc(), "Expected %s when parsing %s, got %s", tokenKindString(kind), context, tokenString(p.cur()))
	return false
}

func (p *parser) expectAndConsumeChar(ch byte, context string) bool {
	return p.expectAndConsume(TokenKind(ch), context)
}

func (p *parser) expectMatchAndConsume(kind TokenKind, _ Token) bool {
	if p.cur().Kind == kind {
		p.nextLexeme()
		return true
	}
	p.report(p.curLoc(), "Expected %s (to close), got %s", tokenKindString(kind), tokenString(p.cur()))
	return false
}

func (p *parser) expectMatchEndAndConsume(kind TokenKind, beginTok Token) bool {
	return p.expectMatchAndConsume(kind, beginTok)
}

func tokenString(t Token) string {
	switch t.Kind {
	case TokenEof:
		return "<eof>"
	case TokenName, TokenAttribute:
		if len(t.Value) > 0 {
			return fmt.Sprintf("'%s'", string(t.Value))
		}
		return "identifier"
	case TokenNumber:
		return "number"
	case TokenQuotedString, TokenRawString, TokenInterpStringSimple:
		return "string"
	}
	return tokenKindString(t.Kind)
}

func tokenKindString(k TokenKind) string {
	if k < CharEnd && k > 0 {
		return fmt.Sprintf("'%c'", byte(k))
	}
	switch k {
	case TokenEqual:
		return "'=='"
	case TokenLessEqual:
		return "'<='"
	case TokenGreaterEqual:
		return "'>='"
	case TokenNotEqual:
		return "'~='"
	case TokenDot2:
		return "'..'"
	case TokenDot3:
		return "'...'"
	case TokenSkinnyArrow:
		return "'->'"
	case TokenDoubleColon:
		return "'::'"
	case TokenFloorDiv:
		return "'//'"
	case TokenAddAssign:
		return "'+='"
	case TokenSubAssign:
		return "'-='"
	case TokenMulAssign:
		return "'*='"
	case TokenDivAssign:
		return "'/='"
	case TokenFloorDivAssign:
		return "'//='"
	case TokenModAssign:
		return "'%='"
	case TokenPowAssign:
		return "'^='"
	case TokenConcatAssign:
		return "'..='"
	case TokenName:
		return "name"
	case TokenAnd:
		return "'and'"
	case TokenBreak:
		return "'break'"
	case TokenDo:
		return "'do'"
	case TokenElse:
		return "'else'"
	case TokenElseif:
		return "'elseif'"
	case TokenEnd:
		return "'end'"
	case TokenFalse:
		return "'false'"
	case TokenFor:
		return "'for'"
	case TokenFunction:
		return "'function'"
	case TokenIf:
		return "'if'"
	case TokenIn:
		return "'in'"
	case TokenLocal:
		return "'local'"
	case TokenNil:
		return "'nil'"
	case TokenNot:
		return "'not'"
	case TokenOr:
		return "'or'"
	case TokenRepeat:
		return "'repeat'"
	case TokenReturn:
		return "'return'"
	case TokenThen:
		return "'then'"
	case TokenTrue:
		return "'true'"
	case TokenUntil:
		return "'until'"
	case TokenWhile:
		return "'while'"
	}
	return "<token>"
}

// ----------------------------------------------------------------------
// Local stack
// ----------------------------------------------------------------------

func (p *parser) saveLocals() int { return len(p.localStack) }

func (p *parser) restoreLocals(off int) {
	for i := len(p.localStack) - 1; i >= off; i-- {
		l := p.localStack[i]
		if l.Shadow != nil {
			p.localMap[l.Name] = l.Shadow
		} else {
			delete(p.localMap, l.Name)
		}
	}
	p.localStack = p.localStack[:off]
}

func (p *parser) pushLocal(name *Name, annotation TypeExpr) *Local {
	prev := p.localMap[name.Name]
	l := &Local{
		Name:          name.Name,
		NameLoc:       name.Location,
		Annotation:    annotation,
		Shadow:        prev,
		FunctionDepth: uint32(len(p.functionStack) - 1),
		LoopDepth:     uint32(p.functionStack[len(p.functionStack)-1].loopDepth),
	}
	p.localMap[name.Name] = l
	p.localStack = append(p.localStack, l)
	return l
}

// ----------------------------------------------------------------------
// Top-level: parseChunk / parseBlock
// ----------------------------------------------------------------------

func (p *parser) parseChunk() *Block {
	body := p.parseBlock()
	if p.cur().Kind != TokenEof {
		p.report(p.curLoc(), "Expected end of file, got %s", tokenString(p.cur()))
	}
	return body
}

func blockFollow(t Token) bool {
	switch t.Kind {
	case TokenEof, TokenElse, TokenElseif, TokenEnd, TokenUntil:
		return true
	}
	return false
}

func (p *parser) parseBlock() *Block {
	off := p.saveLocals()
	b := p.parseBlockNoScope()
	p.restoreLocals(off)
	return b
}

func (p *parser) parseBlockNoScope() *Block {
	var body []Stat
	prevEnd := p.prevLoc().End
	for !blockFollow(p.cur()) {
		stat := p.parseStat()
		if p.cur().Kind == TokenKind(';') {
			p.nextLexeme()
		}
		body = append(body, stat)
		if isStatLast(stat) {
			break
		}
	}
	loc := Location{Begin: prevEnd, End: p.curLoc().Begin}
	return &Block{Location: loc, Body: body}
}

func isStatLast(s Stat) bool {
	switch s.(type) {
	case *StatBreak, *StatContinue, *StatReturn:
		return true
	}
	return false
}

// ----------------------------------------------------------------------
// Statements
// ----------------------------------------------------------------------

func (p *parser) parseStat() Stat {
	switch p.cur().Kind {
	case TokenIf:
		return p.parseIf()
	case TokenWhile:
		return p.parseWhile()
	case TokenDo:
		return p.parseDo()
	case TokenFor:
		return p.parseFor()
	case TokenRepeat:
		return p.parseRepeat()
	case TokenFunction:
		return p.parseFunctionStat(nil)
	case TokenLocal:
		return p.parseLocal(nil)
	case TokenReturn:
		return p.parseReturn()
	case TokenBreak:
		return p.parseBreak()
	case TokenAttribute, TokenAttributeOpen:
		return p.parseAttributeStat()
	}

	start := p.curLoc()
	expr := p.parsePrimaryExpr(true)

	if _, ok := expr.(*ExprCall); ok {
		return &StatExpr{Location: expr.Loc(), Expr: expr}
	}

	if p.cur().Kind == TokenKind(',') || p.cur().Kind == TokenKind('=') {
		return p.parseAssignment(expr)
	}

	if op, ok := parseCompoundOp(p.cur()); ok {
		return p.parseCompoundAssignment(expr, op)
	}

	// Context-sensitive keywords like 'type', 'continue', 'export type', 'declare'
	if g, ok := expr.(*ExprGlobal); ok {
		switch g.Name {
		case "type":
			return p.parseTypeAlias(expr.Loc(), false)
		case "continue":
			return &StatContinue{Location: expr.Loc()}
		case "export":
			if p.cur().Kind == TokenName && string(p.cur().Value) == "type" {
				p.nextLexeme()
				return p.parseTypeAlias(expr.Loc(), true)
			}
		case "declare":
			if p.opts.AllowDeclarationSyntax {
				return p.parseDeclaration(expr.Loc(), nil)
			}
		}
	}

	// Skip token if no progress
	if start == p.curLoc() {
		p.nextLexeme()
	}
	p.report(expr.Loc(), "Incomplete statement: expected assignment or a function call")
	return &StatError{Location: expr.Loc(), Expressions: []Expr{expr}}
}

func (p *parser) parseIf() Stat {
	start := p.curLoc()
	p.nextLexeme() // if / elseif

	cond := p.parseExpr(0)

	thenTok := p.cur()
	var thenLoc Location
	if p.expectAndConsume(TokenThen, "if statement") {
		thenLoc = thenTok.Location
	}

	thenBody := p.parseBlock()
	var elseBody Stat
	var elseLoc Location
	end := start

	if p.cur().Kind == TokenElseif {
		thenBody.HasEnd = true
		elseLoc = p.curLoc()
		elseBody = p.parseIf()
		end = elseBody.Loc()
	} else {
		matchTok := thenTok
		if p.cur().Kind == TokenElse {
			thenBody.HasEnd = true
			elseLoc = p.curLoc()
			matchTok = p.cur()
			p.nextLexeme()
			elseB := p.parseBlock()
			elseB.Location.Begin = matchTok.Location.End
			elseBody = elseB
		}
		end = p.curLoc()
		hasEnd := p.expectMatchEndAndConsume(TokenEnd, matchTok)
		if elseBody != nil {
			if blk, ok := elseBody.(*Block); ok {
				blk.HasEnd = hasEnd
			}
		} else {
			thenBody.HasEnd = hasEnd
		}
	}

	return &StatIf{
		Location:  Location{Begin: start.Begin, End: end.End},
		Condition: cond,
		ThenBody:  thenBody,
		ElseBody:  elseBody,
		HasThen:   thenLoc != Location{},
		HasEnd:    thenBody.HasEnd,
		ThenLoc:   thenLoc,
		ElseLoc:   elseLoc,
	}
}

func (p *parser) parseWhile() Stat {
	start := p.curLoc()
	p.nextLexeme()
	cond := p.parseExpr(0)
	matchDo := p.cur()
	hasDo := p.expectAndConsume(TokenDo, "while loop")
	p.functionStack[len(p.functionStack)-1].loopDepth++
	body := p.parseBlock()
	p.functionStack[len(p.functionStack)-1].loopDepth--
	end := p.curLoc()
	hasEnd := p.expectMatchEndAndConsume(TokenEnd, matchDo)
	body.HasEnd = hasEnd
	return &StatWhile{
		Location:  Location{Begin: start.Begin, End: end.End},
		Condition: cond,
		Body:      body,
		HasDo:     hasDo,
		DoLoc:     matchDo.Location,
		HasEnd:    hasEnd,
	}
}

func (p *parser) parseRepeat() Stat {
	start := p.curLoc()
	matchRep := p.cur()
	p.nextLexeme()
	off := p.saveLocals()
	p.functionStack[len(p.functionStack)-1].loopDepth++
	body := p.parseBlockNoScope()
	p.functionStack[len(p.functionStack)-1].loopDepth--
	hasUntil := p.expectMatchEndAndConsume(TokenUntil, matchRep)
	body.HasEnd = hasUntil
	cond := p.parseExpr(0)
	p.restoreLocals(off)
	return &StatRepeat{
		Location:  Location{Begin: start.Begin, End: cond.Loc().End},
		Body:      body,
		Condition: cond,
		HasUntil:  hasUntil,
	}
}

func (p *parser) parseDo() Stat {
	start := p.curLoc()
	matchDo := p.cur()
	p.nextLexeme()
	body := p.parseBlock()
	body.Location.Begin = start.Begin
	endLoc := p.curLoc()
	body.HasEnd = p.expectMatchEndAndConsume(TokenEnd, matchDo)
	if body.HasEnd {
		body.Location.End = endLoc.End
	}
	return body
}

func (p *parser) parseBreak() Stat {
	loc := p.curLoc()
	p.nextLexeme()
	if p.functionStack[len(p.functionStack)-1].loopDepth == 0 {
		p.report(loc, "break statement must be inside a loop")
	}
	return &StatBreak{Location: loc}
}

func (p *parser) parseFor() Stat {
	start := p.curLoc()
	p.nextLexeme()
	bind := p.parseBinding(false)

	if p.cur().Kind == TokenKind('=') {
		p.nextLexeme()
		from := p.parseExpr(0)
		p.expectAndConsumeChar(',', "index range")
		to := p.parseExpr(0)
		var step Expr
		if p.cur().Kind == TokenKind(',') {
			p.nextLexeme()
			step = p.parseExpr(0)
		}
		matchDo := p.cur()
		hasDo := p.expectAndConsume(TokenDo, "for loop")
		off := p.saveLocals()
		p.functionStack[len(p.functionStack)-1].loopDepth++
		v := p.pushLocal(&bind.name, bind.annotation)
		body := p.parseBlock()
		p.functionStack[len(p.functionStack)-1].loopDepth--
		p.restoreLocals(off)
		end := p.curLoc()
		hasEnd := p.expectMatchEndAndConsume(TokenEnd, matchDo)
		body.HasEnd = hasEnd
		return &StatFor{
			Location: Location{Begin: start.Begin, End: end.End},
			Var:      v,
			From:     from,
			To:       to,
			Step:     step,
			Body:     body,
			HasDo:    hasDo,
			DoLoc:    matchDo.Location,
			HasEnd:   hasEnd,
		}
	}

	bindings := []parserBinding{bind}
	if p.cur().Kind == TokenKind(',') {
		p.nextLexeme()
		p.parseBindingList(&bindings, false)
	}

	inLoc := p.curLoc()
	hasIn := p.expectAndConsume(TokenIn, "for loop")
	var values []Expr
	p.parseExprList(&values)
	matchDo := p.cur()
	hasDo := p.expectAndConsume(TokenDo, "for loop")
	off := p.saveLocals()
	p.functionStack[len(p.functionStack)-1].loopDepth++
	var vars []*Local
	for i := range bindings {
		vars = append(vars, p.pushLocal(&bindings[i].name, bindings[i].annotation))
	}
	body := p.parseBlock()
	p.functionStack[len(p.functionStack)-1].loopDepth--
	p.restoreLocals(off)
	end := p.curLoc()
	hasEnd := p.expectMatchEndAndConsume(TokenEnd, matchDo)
	body.HasEnd = hasEnd
	return &StatForIn{
		Location: Location{Begin: start.Begin, End: end.End},
		Vars:     vars,
		Values:   values,
		Body:     body,
		HasIn:    hasIn,
		InLoc:    inLoc,
		HasDo:    hasDo,
		DoLoc:    matchDo.Location,
		HasEnd:   hasEnd,
	}
}

func (p *parser) parseReturn() Stat {
	start := p.curLoc()
	p.nextLexeme()
	var list []Expr
	if !blockFollow(p.cur()) && p.cur().Kind != TokenKind(';') {
		p.parseExprList(&list)
	}
	end := start
	if len(list) > 0 {
		end = list[len(list)-1].Loc()
	}
	return &StatReturn{Location: Location{Begin: start.Begin, End: end.End}, List: list}
}

// ----------------------------------------------------------------------
// Function statements / local / attribute stat
// ----------------------------------------------------------------------

func (p *parser) parseFunctionName() (Expr, bool, string) {
	debugName := ""
	if p.cur().Kind == TokenName {
		debugName = string(p.cur().Value)
	}
	expr := p.parseNameExpr("function name")
	hasSelf := false

	for p.cur().Kind == TokenKind('.') {
		opPos := p.curLoc().Begin
		p.nextLexeme()
		nm := p.parseName("field name")
		debugName = nm.Name
		expr = &ExprIndexName{
			Location:    Location{Begin: expr.Loc().Begin, End: nm.Location.End},
			Expr:        expr,
			IndexName:   nm.Name,
			IndexLoc:    nm.Location,
			OperatorLoc: opPos,
			Op:          '.',
		}
	}
	if p.cur().Kind == TokenKind(':') {
		opPos := p.curLoc().Begin
		p.nextLexeme()
		nm := p.parseName("method name")
		debugName = nm.Name
		expr = &ExprIndexName{
			Location:    Location{Begin: expr.Loc().Begin, End: nm.Location.End},
			Expr:        expr,
			IndexName:   nm.Name,
			IndexLoc:    nm.Location,
			OperatorLoc: opPos,
			Op:          ':',
		}
		hasSelf = true
	}
	return expr, hasSelf, debugName
}

func (p *parser) parseFunctionStat(attributes []Attribute) Stat {
	start := p.curLoc()
	if len(attributes) > 0 {
		start = attributes[0].Location
	}
	matchFunc := p.cur()
	p.nextLexeme()
	name, hasSelf, debugName := p.parseFunctionName()
	body := p.parseFunctionBody(hasSelf, matchFunc, debugName, nil, attributes)
	return &StatFunction{
		Location: Location{Begin: start.Begin, End: body.Loc().End},
		Name:     name,
		Func:     body,
	}
}

func (p *parser) parseAttributeStat() Stat {
	attrs := p.parseAttributes()
	switch p.cur().Kind {
	case TokenFunction:
		return p.parseFunctionStat(attrs)
	case TokenLocal:
		return p.parseLocal(attrs)
	case TokenName:
		if p.opts.AllowDeclarationSyntax && string(p.cur().Value) == "declare" {
			expr := p.parsePrimaryExpr(true)
			return p.parseDeclaration(expr.Loc(), attrs)
		}
	}
	p.report(p.curLoc(), "Expected 'function', 'local function' or 'declare function' after attribute, got %s", tokenString(p.cur()))
	return &StatError{Location: p.curLoc()}
}

func (p *parser) parseLocal(attributes []Attribute) Stat {
	start := p.curLoc()
	if len(attributes) > 0 {
		start = attributes[0].Location
	}
	p.nextLexeme() // local
	if p.cur().Kind == TokenFunction {
		matchFunc := p.cur()
		p.nextLexeme()
		if matchFunc.Location.Begin.Line == start.Begin.Line {
			matchFunc.Location.Begin.Column = start.Begin.Column
		}
		nm := p.parseName("variable name")
		funLocal := p.pushLocal(&nm, nil)
		body := p.parseFunctionBodyWithLocal(false, matchFunc, nm.Name, attributes, nil)
		return &StatLocalFunction{
			Location: Location{Begin: start.Begin, End: body.Loc().End},
			Name:     funLocal,
			Func:     body,
		}
	}

	if len(attributes) != 0 {
		p.report(p.curLoc(), "Expected 'function' after local declaration with attribute")
	}

	var bindings []parserBinding
	p.parseBindingList(&bindings, false)
	var values []Expr
	var eqLoc Location
	if p.cur().Kind == TokenKind('=') {
		eqLoc = p.curLoc()
		p.nextLexeme()
		p.parseExprList(&values)
	}
	var vars []*Local
	for i := range bindings {
		vars = append(vars, p.pushLocal(&bindings[i].name, bindings[i].annotation))
	}
	end := p.prevLoc()
	if len(values) > 0 {
		end = values[len(values)-1].Loc()
	}
	return &StatLocal{
		Location:  Location{Begin: start.Begin, End: end.End},
		Vars:      vars,
		Values:    values,
		EqualsLoc: eqLoc,
	}
}

// ----------------------------------------------------------------------
// Attributes
// ----------------------------------------------------------------------

func (p *parser) parseAttributes() []Attribute {
	var attrs []Attribute
	for p.cur().Kind == TokenAttribute || p.cur().Kind == TokenAttributeOpen {
		p.parseAttribute(&attrs)
	}
	return attrs
}

func (p *parser) parseAttribute(out *[]Attribute) {
	if p.cur().Kind == TokenAttribute {
		loc := p.curLoc()
		name := string(p.cur().Value)
		p.nextLexeme()
		*out = append(*out, Attribute{Location: loc, Name: name})
		return
	}
	// @[ ... ]
	open := p.cur()
	p.nextLexeme()
	if p.cur().Kind != TokenKind(']') {
		for {
			nm := p.parseName("attribute name")
			if p.cur().Kind == TokenRawString || p.cur().Kind == TokenQuotedString ||
				p.cur().Kind == TokenKind('{') || p.cur().Kind == TokenKind('(') {
				_, argsLoc, _ := p.parseCallList()
				*out = append(*out, Attribute{
					Location: Location{Begin: nm.Location.Begin, End: argsLoc.End},
					Name:     nm.Name,
				})
			} else {
				*out = append(*out, Attribute{Location: nm.Location, Name: nm.Name})
			}
			if p.cur().Kind == TokenKind(',') {
				p.nextLexeme()
			} else {
				break
			}
		}
	} else {
		p.report(Location{Begin: open.Location.Begin, End: p.curLoc().End}, "Attribute list cannot be empty")
	}
	p.expectMatchAndConsume(TokenKind(']'), open)
}

// ----------------------------------------------------------------------
// Declarations (used when AllowDeclarationSyntax)
// ----------------------------------------------------------------------

func (p *parser) parseDeclaration(start Location, _ []Attribute) Stat {
	// Minimal support: we skip / report depending on what's there.
	// Fixtures don't use declarations, but we provide a stub to keep the
	// parser robust if someone enables AllowDeclarationSyntax.
	p.report(start, "declaration syntax not supported in this build")
	// Try to recover by skipping the line.
	for p.cur().Kind != TokenEof && p.cur().Location.Begin.Line == start.Begin.Line {
		p.nextLexeme()
	}
	return &StatError{Location: start}
}

// ----------------------------------------------------------------------
// Type alias / type function
// ----------------------------------------------------------------------

func (p *parser) parseTypeAlias(start Location, exported bool) Stat {
	if p.cur().Kind == TokenFunction {
		return p.parseTypeFunction(start, exported)
	}
	nm := p.parseName("type name")
	generics, genericPacks := p.parseGenericTypeList(true)
	p.expectAndConsumeChar('=', "type alias")
	ty := p.parseType(false)
	return &StatTypeAlias{
		Location:     Location{Begin: start.Begin, End: ty.Loc().End},
		Name:         nm.Name,
		NameLoc:      nm.Location,
		Generics:     generics,
		GenericPacks: genericPacks,
		Type:         ty,
		Exported:     exported,
	}
}

func (p *parser) parseTypeFunction(start Location, exported bool) Stat {
	matchFn := p.cur()
	p.nextLexeme() // function
	nm := p.parseName("type function name")
	oldDepth := p.typeFuncDepth
	p.typeFuncDepth = len(p.functionStack)
	body := p.parseFunctionBody(false, matchFn, nm.Name, nil, nil)
	p.typeFuncDepth = oldDepth
	return &StatTypeFunction{
		Location: Location{Begin: start.Begin, End: body.Loc().End},
		Name:     nm.Name,
		NameLoc:  nm.Location,
		Func:     body,
		Exported: exported,
	}
}

// ----------------------------------------------------------------------
// Bindings and function body
// ----------------------------------------------------------------------

type parserBinding struct {
	name       Name
	annotation TypeExpr
}

func (p *parser) parseBinding(_ bool) parserBinding {
	nm := p.parseNameOpt("variable name")
	if nm == nil {
		nm = &Name{Name: "error", Location: p.curLoc()}
	}
	ann := p.parseOptionalType()
	return parserBinding{name: *nm, annotation: ann}
}

// parseBindingList returns: vararg present, vararg location, vararg annot
func (p *parser) parseBindingList(out *[]parserBinding, allowDot3 bool) (bool, Location, TypePack) {
	for {
		if p.cur().Kind == TokenDot3 && allowDot3 {
			loc := p.curLoc()
			p.nextLexeme()
			var tailAnn TypePack
			if p.cur().Kind == TokenKind(':') {
				p.nextLexeme()
				tailAnn = p.parseVariadicArgumentTypePack()
			}
			return true, loc, tailAnn
		}
		*out = append(*out, p.parseBinding(false))
		if p.cur().Kind != TokenKind(',') {
			break
		}
		p.nextLexeme()
	}
	return false, Location{}, nil
}

func (p *parser) parseOptionalType() TypeExpr {
	if p.cur().Kind == TokenKind(':') {
		p.nextLexeme()
		return p.parseType(false)
	}
	return nil
}

func (p *parser) parseFunctionBody(hasSelf bool, matchFunc Token, debugName string, localName *Name, attributes []Attribute) *ExprFunction {
	return p.parseFunctionBodyWithLocal(hasSelf, matchFunc, debugName, attributes, localName)
}

func (p *parser) parseFunctionBodyWithLocal(hasSelf bool, matchFunc Token, debugName string, attributes []Attribute, _ *Name) *ExprFunction {
	start := matchFunc.Location
	if len(attributes) > 0 {
		start = attributes[0].Location
	}

	generics, genericPacks := p.parseGenericTypeList(false)
	matchParen := p.cur()
	p.expectAndConsumeChar('(', "function")

	var args []parserBinding
	vararg := false
	var varargLoc Location
	var varargAnn TypePack

	if p.cur().Kind != TokenKind(')') {
		vararg, varargLoc, varargAnn = p.parseBindingList(&args, true)
	}

	var argLoc Location
	if matchParen.Kind == TokenKind('(') && p.cur().Kind == TokenKind(')') {
		argLoc = Location{Begin: matchParen.Location.Begin, End: p.curLoc().End}
	}
	p.expectMatchAndConsume(TokenKind(')'), matchParen)

	retAnn := p.parseOptionalReturnType()

	// New function scope
	off := p.saveLocals()
	p.functionStack = append(p.functionStack, parserFunc{vararg: vararg})

	var selfLocal *Local
	if hasSelf {
		selfName := Name{Name: "self", Location: start}
		selfLocal = p.pushLocal(&selfName, nil)
	}
	var argLocals []*Local
	for i := range args {
		argLocals = append(argLocals, p.pushLocal(&args[i].name, args[i].annotation))
	}

	body := p.parseBlock()

	p.functionStack = p.functionStack[:len(p.functionStack)-1]
	p.restoreLocals(off)

	end := p.curLoc()
	hasEnd := p.expectMatchEndAndConsume(TokenEnd, matchFunc)
	body.HasEnd = hasEnd

	return &ExprFunction{
		Location:      Location{Begin: start.Begin, End: end.End},
		Attributes:    attributes,
		Generics:      generics,
		GenericPacks:  genericPacks,
		Self:          selfLocal,
		Args:          argLocals,
		ArgLocation:   argLoc,
		Vararg:        vararg,
		VarargLoc:     varargLoc,
		VarargAnnot:   varargAnn,
		ReturnAnnot:   retAnn,
		Body:          body,
		FunctionDepth: uint32(len(p.functionStack)),
		DebugName:     debugName,
		HasEnd:        hasEnd,
	}
}

// ----------------------------------------------------------------------
// Assignment / compound assignment
// ----------------------------------------------------------------------

func (p *parser) parseAssignment(initial Expr) Stat {
	if !isLValue(initial) {
		p.report(initial.Loc(), "Assigned expression must be a variable or a field")
	}
	vars := []Expr{initial}
	for p.cur().Kind == TokenKind(',') {
		p.nextLexeme()
		e := p.parsePrimaryExpr(true)
		if !isLValue(e) {
			p.report(e.Loc(), "Assigned expression must be a variable or a field")
		}
		vars = append(vars, e)
	}
	p.expectAndConsumeChar('=', "assignment")
	var values []Expr
	p.parseExprList(&values)
	end := initial.Loc()
	if len(values) > 0 {
		end = values[len(values)-1].Loc()
	}
	return &StatAssign{
		Location: Location{Begin: initial.Loc().Begin, End: end.End},
		Vars:     vars,
		Values:   values,
	}
}

func (p *parser) parseCompoundAssignment(initial Expr, op BinaryOp) Stat {
	if !isLValue(initial) {
		p.report(initial.Loc(), "Assigned expression must be a variable or a field")
	}
	p.nextLexeme()
	v := p.parseExpr(0)
	return &StatCompoundAssign{
		Location: Location{Begin: initial.Loc().Begin, End: v.Loc().End},
		Op:       op,
		Var:      initial,
		Value:    v,
	}
}

func isLValue(e Expr) bool {
	switch e.(type) {
	case *ExprLocal, *ExprGlobal, *ExprIndexExpr, *ExprIndexName:
		return true
	}
	return false
}

// ----------------------------------------------------------------------
// Expressions
// ----------------------------------------------------------------------

// binaryPriority matches upstream Parser.cpp's table in parseExpr.
var binaryPriority = [16][2]uint8{
	{6, 6},   // Add
	{6, 6},   // Sub
	{7, 7},   // Mul
	{7, 7},   // Div
	{7, 7},   // FloorDiv
	{7, 7},   // Mod
	{10, 9},  // Pow (right-assoc)
	{5, 4},   // Concat (right-assoc)
	{3, 3},   // Eq
	{3, 3},   // NotEq
	{3, 3},   // Lt
	{3, 3},   // Le
	{3, 3},   // Gt
	{3, 3},   // Ge
	{2, 2},   // And
	{1, 1},   // Or
}

const unaryPriority = 8

func (p *parser) parseExpr(limit uint8) Expr {
	start := p.curLoc()
	var expr Expr
	if uop, ok := parseUnaryOp(p.cur()); ok {
		p.nextLexeme()
		sub := p.parseExpr(unaryPriority)
		expr = &ExprUnary{
			Location: Location{Begin: start.Begin, End: sub.Loc().End},
			Op:       uop,
			Operand:  sub,
		}
	} else {
		expr = p.parseAssertionExpr()
	}
	op, ok := parseBinaryOp(p.cur())
	for ok && binaryPriority[op][0] > limit {
		p.nextLexeme()
		next := p.parseExpr(binaryPriority[op][1])
		expr = &ExprBinary{
			Location: Location{Begin: start.Begin, End: next.Loc().End},
			Op:       op,
			Lhs:      expr,
			Rhs:      next,
		}
		op, ok = parseBinaryOp(p.cur())
	}
	return expr
}

func parseUnaryOp(t Token) (UnaryOp, bool) {
	switch t.Kind {
	case TokenNot:
		return UnaryNot, true
	case TokenKind('-'):
		return UnaryMinus, true
	case TokenKind('#'):
		return UnaryLen, true
	}
	return 0, false
}

func parseBinaryOp(t Token) (BinaryOp, bool) {
	switch t.Kind {
	case TokenKind('+'):
		return BinaryAdd, true
	case TokenKind('-'):
		return BinarySub, true
	case TokenKind('*'):
		return BinaryMul, true
	case TokenKind('/'):
		return BinaryDiv, true
	case TokenFloorDiv:
		return BinaryFloorDiv, true
	case TokenKind('%'):
		return BinaryMod, true
	case TokenKind('^'):
		return BinaryPow, true
	case TokenDot2:
		return BinaryConcat, true
	case TokenNotEqual:
		return BinaryNotEq, true
	case TokenEqual:
		return BinaryEq, true
	case TokenKind('<'):
		return BinaryLt, true
	case TokenLessEqual:
		return BinaryLe, true
	case TokenKind('>'):
		return BinaryGt, true
	case TokenGreaterEqual:
		return BinaryGe, true
	case TokenAnd:
		return BinaryAnd, true
	case TokenOr:
		return BinaryOr, true
	}
	return 0, false
}

func parseCompoundOp(t Token) (BinaryOp, bool) {
	switch t.Kind {
	case TokenAddAssign:
		return BinaryAdd, true
	case TokenSubAssign:
		return BinarySub, true
	case TokenMulAssign:
		return BinaryMul, true
	case TokenDivAssign:
		return BinaryDiv, true
	case TokenFloorDivAssign:
		return BinaryFloorDiv, true
	case TokenModAssign:
		return BinaryMod, true
	case TokenPowAssign:
		return BinaryPow, true
	case TokenConcatAssign:
		return BinaryConcat, true
	}
	return 0, false
}

func (p *parser) parseAssertionExpr() Expr {
	start := p.curLoc()
	e := p.parseSimpleExpr()
	if p.cur().Kind == TokenDoubleColon {
		p.nextLexeme()
		ann := p.parseType(false)
		return &ExprTypeAssertion{
			Location: Location{Begin: start.Begin, End: ann.Loc().End},
			Expr:     e,
			Type:     ann,
		}
	}
	return e
}

func (p *parser) parseSimpleExpr() Expr {
	start := p.curLoc()
	var attrs []Attribute
	if p.cur().Kind == TokenAttribute || p.cur().Kind == TokenAttributeOpen {
		attrs = p.parseAttributes()
		if p.cur().Kind != TokenFunction {
			p.report(start, "Expected 'function' declaration after attribute")
		}
	}

	switch p.cur().Kind {
	case TokenNil:
		p.nextLexeme()
		return &ExprConstantNil{Location: start}
	case TokenTrue:
		p.nextLexeme()
		return &ExprConstantBool{Location: start, Value: true}
	case TokenFalse:
		p.nextLexeme()
		return &ExprConstantBool{Location: start, Value: false}
	case TokenFunction:
		matchFunc := p.cur()
		p.nextLexeme()
		return p.parseFunctionBody(false, matchFunc, "", nil, attrs)
	case TokenNumber:
		return p.parseNumber()
	case TokenRawString, TokenQuotedString, TokenInterpStringSimple:
		return p.parseString()
	case TokenInterpStringBegin:
		return p.parseInterpString()
	case TokenBrokenString:
		p.nextLexeme()
		p.report(start, "Malformed string")
		return &ExprError{Location: start}
	case TokenBrokenInterpDoubleBrace:
		p.nextLexeme()
		p.report(start, "Double braces in interpolated strings are not allowed")
		return &ExprError{Location: start}
	case TokenDot3:
		if p.functionStack[len(p.functionStack)-1].vararg {
			p.nextLexeme()
			return &ExprVarargs{Location: start}
		}
		p.nextLexeme()
		p.report(start, "Cannot use '...' outside of a vararg function")
		return &ExprError{Location: start}
	case TokenKind('{'):
		return p.parseTableConstructor()
	case TokenIf:
		return p.parseIfElseExpr()
	}
	return p.parsePrimaryExpr(false)
}

// ----------------------------------------------------------------------
// Primary / prefix
// ----------------------------------------------------------------------

func (p *parser) parsePrefixExpr() Expr {
	if p.cur().Kind == TokenKind('(') {
		start := p.curLoc().Begin
		matchParen := p.cur()
		p.nextLexeme()
		e := p.parseExpr(0)
		end := p.curLoc().End
		if p.cur().Kind != TokenKind(')') {
			p.expectMatchAndConsume(TokenKind(')'), matchParen)
			end = p.prevLoc().End
		} else {
			p.nextLexeme()
		}
		return &ExprGroup{Location: Location{Begin: start, End: end}, Expr: e}
	}
	return p.parseNameExpr("expression")
}

func (p *parser) parseNameExpr(context string) Expr {
	nm := p.parseNameOpt(context)
	if nm == nil {
		return &ExprError{Location: p.curLoc()}
	}
	if local, ok := p.localMap[nm.Name]; ok && local != nil {
		if int(local.FunctionDepth) < p.typeFuncDepth {
			p.report(p.curLoc(), "Type function cannot reference outer local '%s'", local.Name)
		}
		return &ExprLocal{
			Location: nm.Location,
			Local:    local,
			Upvalue:  int(local.FunctionDepth) != len(p.functionStack)-1,
		}
	}
	return &ExprGlobal{Location: nm.Location, Name: nm.Name}
}

func (p *parser) parsePrimaryExpr(asStatement bool) Expr {
	start := p.curLoc().Begin
	expr := p.parsePrefixExpr()
	for {
		switch p.cur().Kind {
		case TokenKind('.'):
			opPos := p.curLoc().Begin
			p.nextLexeme()
			idx := p.parseIndexName("", opPos)
			expr = &ExprIndexName{
				Location:    Location{Begin: start, End: idx.Location.End},
				Expr:        expr,
				IndexName:   idx.Name,
				IndexLoc:    idx.Location,
				OperatorLoc: opPos,
				Op:          '.',
			}
		case TokenKind('['):
			matchBr := p.cur()
			p.nextLexeme()
			idx := p.parseExpr(0)
			end := p.curLoc().End
			p.expectMatchAndConsume(TokenKind(']'), matchBr)
			expr = &ExprIndexExpr{
				Location: Location{Begin: start, End: end},
				Expr:     expr,
				Index:    idx,
			}
		case TokenKind(':'):
			expr = p.parseMethodCall(start, expr)
		case TokenKind('('):
			if !asStatement && expr.Loc().End.Line != p.curLoc().Begin.Line {
				p.report(p.curLoc(), "Ambiguous syntax; use ';' to separate statements")
				return expr
			}
			expr = p.parseFunctionArgs(expr, false)
		case TokenKind('{'), TokenRawString, TokenQuotedString, TokenInterpStringSimple:
			expr = p.parseFunctionArgs(expr, false)
		case TokenKind('<'):
			// Could be explicit type instantiation: f<<T>>(...)
			if p.lookahead().Kind == TokenKind('<') {
				expr = p.parseExplicitTypeInstantiationExpr(start, expr)
			} else {
				return expr
			}
		default:
			return expr
		}
	}
}

func (p *parser) parseMethodCall(start Position, expr Expr) Expr {
	opPos := p.curLoc().Begin
	p.nextLexeme()
	idx := p.parseIndexName("method name", opPos)
	fn := &ExprIndexName{
		Location:    Location{Begin: start, End: idx.Location.End},
		Expr:        expr,
		IndexName:   idx.Name,
		IndexLoc:    idx.Location,
		OperatorLoc: opPos,
		Op:          ':',
	}
	// Optional explicit type instantiation: a:method<<T>>(...)
	if p.cur().Kind == TokenKind('<') && p.lookahead().Kind == TokenKind('<') {
		p.parseTypeInstantiationExpr()
	}
	return p.parseFunctionArgs(fn, true)
}

func (p *parser) parseExplicitTypeInstantiationExpr(_ Position, base Expr) Expr {
	p.parseTypeInstantiationExpr()
	// We don't have a dedicated AST node for explicit type instantiation in the
	// contract; the contract pushes type-arg info into ExprCall. For now we
	// just keep the base expression - downstream calls will consume the args.
	return base
}

// parseTypeInstantiationExpr parses <<T1, T2, ...>> and discards the
// parsed type list (the contract doesn't expose explicit instantiations).
// The lexer presents '<<' as two consecutive '<' tokens. We consume the
// first '<' here, let parseTypeParamsRaw consume the second '<' and the
// matching first '>', then we consume the trailing '>'.
func (p *parser) parseTypeInstantiationExpr() {
	begin := p.cur()
	p.nextLexeme() // first <
	_ = p.parseTypeParamsRaw()
	p.expectMatchAndConsume(TokenKind('>'), begin)
}

// ----------------------------------------------------------------------
// Function args / call list / table constructor
// ----------------------------------------------------------------------

func (p *parser) parseExprList(out *[]Expr) {
	*out = append(*out, p.parseExpr(0))
	for p.cur().Kind == TokenKind(',') {
		p.nextLexeme()
		if p.cur().Kind == TokenKind(')') {
			p.report(p.curLoc(), "Expected expression after ',' but got ')'")
			break
		}
		*out = append(*out, p.parseExpr(0))
	}
}

func (p *parser) parseCallList() ([]Expr, Location, Location) {
	switch p.cur().Kind {
	case TokenKind('('):
		argStart := p.curLoc().End
		matchParen := p.cur()
		p.nextLexeme()
		var args []Expr
		if p.cur().Kind != TokenKind(')') {
			p.parseExprList(&args)
		}
		end := p.curLoc()
		p.expectMatchAndConsume(TokenKind(')'), matchParen)
		return args, Location{Begin: argStart, End: end.End}, matchParen.Location
	case TokenKind('{'):
		argStart := p.curLoc().End
		e := p.parseTableConstructor()
		return []Expr{e}, Location{Begin: argStart, End: p.prevLoc().End}, e.Loc()
	default:
		loc := p.curLoc()
		e := p.parseString()
		return []Expr{e}, loc, e.Loc()
	}
}

func (p *parser) parseFunctionArgs(fn Expr, self bool) Expr {
	switch p.cur().Kind {
	case TokenKind('('):
		argStart := p.curLoc().End
		if fn.Loc().End.Line != p.curLoc().Begin.Line {
			// Ambiguity warning; recover by continuing.
		}
		matchParen := p.cur()
		p.nextLexeme()
		var args []Expr
		if p.cur().Kind != TokenKind(')') {
			p.parseExprList(&args)
		}
		end := p.curLoc()
		argEnd := end.End
		p.expectMatchAndConsume(TokenKind(')'), matchParen)
		return &ExprCall{
			Location:    Location{Begin: fn.Loc().Begin, End: end.End},
			Func:        fn,
			Args:        args,
			Self:        self,
			ArgLocation: Location{Begin: argStart, End: argEnd},
		}
	case TokenKind('{'):
		argStart := p.curLoc().End
		e := p.parseTableConstructor()
		argEnd := p.prevLoc().End
		return &ExprCall{
			Location:    Location{Begin: fn.Loc().Begin, End: e.Loc().End},
			Func:        fn,
			Args:        []Expr{e},
			Self:        self,
			ArgLocation: Location{Begin: argStart, End: argEnd},
		}
	case TokenRawString, TokenQuotedString, TokenInterpStringSimple:
		loc := p.curLoc()
		e := p.parseString()
		return &ExprCall{
			Location:    Location{Begin: fn.Loc().Begin, End: e.Loc().End},
			Func:        fn,
			Args:        []Expr{e},
			Self:        self,
			ArgLocation: loc,
		}
	}
	p.report(p.curLoc(), "Expected '(', '{' or <string> when parsing function call, got %s", tokenString(p.cur()))
	return &ExprError{Location: fn.Loc(), Expressions: []Expr{fn}}
}

func (p *parser) parseTableConstructor() Expr {
	start := p.curLoc()
	matchBrace := p.cur()
	p.expectAndConsumeChar('{', "table literal")
	var items []TableItem
	lastIndent := uint32(0)
	for p.cur().Kind != TokenKind('}') {
		lastIndent = p.curLoc().Begin.Column
		if p.cur().Kind == TokenKind('[') {
			matchBr := p.cur()
			p.nextLexeme()
			key := p.parseExpr(0)
			p.expectMatchAndConsume(TokenKind(']'), matchBr)
			p.expectAndConsumeChar('=', "table field")
			val := p.parseExpr(0)
			items = append(items, TableItem{Kind: TableItemGeneral, Key: key, Value: val})
		} else if p.cur().Kind == TokenName && p.lookahead().Kind == TokenKind('=') {
			nm := p.parseName("table field")
			p.expectAndConsumeChar('=', "table field")
			key := &ExprConstantString{
				Location: nm.Location,
				Value:    []byte(nm.Name),
				Quote:    QuoteDouble,
			}
			val := p.parseExpr(0)
			if fn, ok := val.(*ExprFunction); ok && fn.DebugName == "" {
				fn.DebugName = nm.Name
			}
			items = append(items, TableItem{Kind: TableItemRecord, Key: key, Value: val})
		} else {
			e := p.parseExpr(0)
			items = append(items, TableItem{Kind: TableItemList, Value: e})
		}
		if p.cur().Kind == TokenKind(',') || p.cur().Kind == TokenKind(';') {
			p.nextLexeme()
		} else if (p.cur().Kind == TokenKind('[') || p.cur().Kind == TokenName) && p.curLoc().Begin.Column == lastIndent {
			p.report(p.curLoc(), "Expected ',' after table constructor element")
		} else if p.cur().Kind != TokenKind('}') {
			break
		}
	}
	end := p.curLoc()
	if !p.expectMatchAndConsume(TokenKind('}'), matchBrace) {
		end = p.prevLoc()
	}
	return &ExprTable{Location: Location{Begin: start.Begin, End: end.End}, Items: items}
}

func (p *parser) parseIfElseExpr() Expr {
	start := p.curLoc()
	p.nextLexeme() // if / elseif
	cond := p.parseExpr(0)
	hasThen := p.expectAndConsume(TokenThen, "if then else expression")
	trueExpr := p.parseExpr(0)
	var falseExpr Expr
	hasElse := false
	if p.cur().Kind == TokenElseif {
		hasElse = true
		falseExpr = p.parseIfElseExpr()
	} else {
		hasElse = p.expectAndConsume(TokenElse, "if then else expression")
		falseExpr = p.parseExpr(0)
	}
	return &ExprIfElse{
		Location:  Location{Begin: start.Begin, End: falseExpr.Loc().End},
		Condition: cond,
		HasThen:   hasThen,
		True:      trueExpr,
		HasElse:   hasElse,
		False:     falseExpr,
	}
}

// ----------------------------------------------------------------------
// Names
// ----------------------------------------------------------------------

func (p *parser) parseNameOpt(context string) *Name {
	if p.cur().Kind != TokenName {
		p.reportName(context)
		return nil
	}
	n := &Name{Name: string(p.cur().Value), Location: p.curLoc()}
	p.nextLexeme()
	return n
}

func (p *parser) parseName(context string) Name {
	if n := p.parseNameOpt(context); n != nil {
		return *n
	}
	loc := p.curLoc()
	loc.End = loc.Begin
	return Name{Name: "error", Location: loc}
}

func (p *parser) parseIndexName(context string, previous Position) Name {
	if n := p.parseNameOpt(context); n != nil {
		return *n
	}
	// Accept reserved keyword on same line as incomplete name.
	cur := p.cur()
	if cur.Kind >= TokenAnd && cur.Kind <= TokenWhile && cur.Location.Begin.Line == previous.Line {
		n := Name{Name: string(cur.Value), Location: cur.Location}
		p.nextLexeme()
		return n
	}
	loc := p.curLoc()
	loc.End = loc.Begin
	return Name{Name: "error", Location: loc}
}

// ----------------------------------------------------------------------
// Generic type list / type params
// ----------------------------------------------------------------------

func (p *parser) parseGenericTypeList(withDefaults bool) ([]GenericType, []GenericTypePack) {
	var names []GenericType
	var packs []GenericTypePack
	if p.cur().Kind != TokenKind('<') {
		return names, packs
	}
	begin := p.cur()
	p.nextLexeme()
	seenPack := false
	seenDefault := false
	for {
		nameLoc := p.curLoc()
		nm := p.parseName("")
		if p.cur().Kind == TokenDot3 || seenPack {
			seenPack = true
			if p.cur().Kind != TokenDot3 {
				p.report(p.curLoc(), "Generic types come before generic type packs")
			} else {
				p.nextLexeme()
			}
			if withDefaults && p.cur().Kind == TokenKind('=') {
				seenDefault = true
				p.nextLexeme()
				var tp TypePack
				if shouldParseTypePack(p) {
					tp = p.parseTypePack()
				} else {
					t, pk := p.parseSimpleTypeOrPack()
					if t != nil {
						p.report(t.Loc(), "Expected type pack after '=', got type")
					}
					tp = pk
				}
				packs = append(packs, GenericTypePack{Location: nameLoc, Name: nm.Name, Default: tp})
			} else {
				if seenDefault {
					p.report(p.curLoc(), "Expected default type pack after type pack name")
				}
				packs = append(packs, GenericTypePack{Location: nameLoc, Name: nm.Name})
			}
		} else {
			if withDefaults && p.cur().Kind == TokenKind('=') {
				seenDefault = true
				p.nextLexeme()
				dt := p.parseType(false)
				names = append(names, GenericType{Location: nameLoc, Name: nm.Name, Default: dt})
			} else {
				if seenDefault {
					p.report(p.curLoc(), "Expected default type after type name")
				}
				names = append(names, GenericType{Location: nameLoc, Name: nm.Name})
			}
		}
		if p.cur().Kind == TokenKind(',') {
			p.nextLexeme()
			if p.cur().Kind == TokenKind('>') {
				p.report(p.curLoc(), "Expected type after ',' but got '>'")
				break
			}
		} else {
			break
		}
	}
	p.expectMatchAndConsume(TokenKind('>'), begin)
	return names, packs
}

// parseTypeParamsRaw is used for type instantiations and type references.
// Returns a slice of TypeOrPack.
func (p *parser) parseTypeParamsRaw() []TypeOrPack {
	var params []TypeOrPack
	if p.cur().Kind != TokenKind('<') {
		return params
	}
	begin := p.cur()
	p.nextLexeme()
	for {
		if shouldParseTypePack(p) {
			tp := p.parseTypePack()
			params = append(params, TypeOrPack{Pack: tp})
		} else if p.cur().Kind == TokenKind('(') {
			bLoc := p.curLoc()
			ty, tp := p.parseSimpleType(true, false)
			if tp != nil {
				// If single-element typepack and next is type-follow, treat as group type
				if explicit, ok := tp.(*TypePackExplicit); ok && explicit.Tail == nil && len(explicit.Types) == 1 && isTypeFollow(p.cur().Kind) {
					inner := explicit.Types[0]
					grp := &TypeGroup{Location: inner.Loc(), Inner: inner}
					params = append(params, TypeOrPack{Type: p.parseTypeSuffix(grp, bLoc)})
				} else {
					params = append(params, TypeOrPack{Pack: tp})
				}
			} else {
				params = append(params, TypeOrPack{Type: p.parseTypeSuffix(ty, bLoc)})
			}
		} else if p.cur().Kind == TokenKind('>') && len(params) == 0 {
			break
		} else {
			params = append(params, TypeOrPack{Type: p.parseType(false)})
		}
		if p.cur().Kind == TokenKind(',') {
			p.nextLexeme()
		} else {
			break
		}
	}
	p.expectMatchAndConsume(TokenKind('>'), begin)
	return params
}

// ----------------------------------------------------------------------
// Types
// ----------------------------------------------------------------------

func shouldParseTypePack(p *parser) bool {
	if p.cur().Kind == TokenDot3 {
		return true
	}
	if p.cur().Kind == TokenName && p.lookahead().Kind == TokenDot3 {
		return true
	}
	return false
}

func isTypeFollow(k TokenKind) bool {
	return k == TokenKind('|') || k == TokenKind('?') || k == TokenKind('&')
}

func (p *parser) parseType(inDecl bool) TypeExpr {
	begin := p.curLoc()
	var ty TypeExpr
	if p.cur().Kind != TokenKind('|') && p.cur().Kind != TokenKind('&') {
		ty, _ = p.parseSimpleType(false, inDecl)
	}
	return p.parseTypeSuffix(ty, begin)
}

func (p *parser) parseTypeSuffix(ty TypeExpr, begin Location) TypeExpr {
	var parts []TypeExpr
	if ty != nil {
		parts = append(parts, ty)
	}
	isUnion := false
	isInter := false
	optionalCount := 0
	loc := begin
	for {
		c := p.cur().Kind
		if c == TokenKind('|') {
			p.nextLexeme()
			sub, _ := p.parseSimpleType(false, false)
			parts = append(parts, sub)
			isUnion = true
		} else if c == TokenKind('?') {
			loc2 := p.curLoc()
			p.nextLexeme()
			parts = append(parts, &TypeOptional{Location: loc2})
			optionalCount++
			isUnion = true
		} else if c == TokenKind('&') {
			p.nextLexeme()
			sub, _ := p.parseSimpleType(false, false)
			parts = append(parts, sub)
			isInter = true
		} else if c == TokenDot3 {
			p.report(p.curLoc(), "Unexpected '...' after type annotation")
			p.nextLexeme()
		} else {
			break
		}
	}
	if len(parts) == 1 && !isUnion && !isInter {
		return parts[0]
	}
	if isUnion && isInter {
		p.report(loc, "Mixing union and intersection types is not allowed; consider wrapping in parentheses.")
		return &TypeError{Location: Location{Begin: loc.Begin, End: parts[len(parts)-1].Loc().End}, Types: parts}
	}
	loc.End = parts[len(parts)-1].Loc().End
	if isUnion {
		return &TypeUnion{Location: loc, Types: parts}
	}
	return &TypeIntersection{Location: loc, Types: parts}
}

func (p *parser) parseSimpleType(allowPack bool, inDecl bool) (TypeExpr, TypePack) {
	start := p.curLoc()
	switch p.cur().Kind {
	case TokenAttribute, TokenAttributeOpen:
		if !inDecl {
			p.report(start, "attributes are not allowed in declaration context")
		}
		_ = p.parseAttributes()
		return p.parseFunctionType(allowPack)
	case TokenNil:
		p.nextLexeme()
		return &TypeReference{Location: start, Name: "nil"}, nil
	case TokenTrue:
		p.nextLexeme()
		return &TypeSingletonBool{Location: start, Value: true}, nil
	case TokenFalse:
		p.nextLexeme()
		return &TypeSingletonBool{Location: start, Value: false}, nil
	case TokenRawString, TokenQuotedString:
		raw := p.cur().Value
		quote := p.cur().Quote
		p.nextLexeme()
		var val []byte
		if p.lex != nil {
			if p.lex.cur.Kind == TokenRawString {
				val = fixupMultilineString(raw)
			} else {
				v, ok := fixupQuotedString(raw)
				if !ok {
					p.report(start, "String literal contains malformed escape sequence")
					return &TypeError{Location: start}, nil
				}
				val = v
			}
		}
		_ = quote
		return &TypeSingletonString{Location: start, Value: val}, nil
	case TokenInterpStringBegin, TokenInterpStringSimple:
		_ = p.parseInterpString()
		p.report(start, "Interpolated string literals cannot be used as types")
		return &TypeError{Location: start}, nil
	case TokenBrokenString:
		p.nextLexeme()
		p.report(start, "Malformed string")
		return &TypeError{Location: start}, nil
	case TokenName:
		var prefix string
		nm := p.parseName("type name")
		if p.cur().Kind == TokenKind('.') {
			opPos := p.curLoc().Begin
			p.nextLexeme()
			prefix = nm.Name
			nm = p.parseIndexName("field name", opPos)
		} else if p.cur().Kind == TokenDot3 {
			p.report(p.curLoc(), "Unexpected '...' after type name; type pack is not allowed in this context")
			p.nextLexeme()
		} else if nm.Name == "typeof" {
			typeofBegin := p.cur()
			p.expectAndConsumeChar('(', "typeof type")
			e := p.parseExpr(0)
			end := p.curLoc()
			p.expectMatchAndConsume(TokenKind(')'), typeofBegin)
			return &TypeTypeof{Location: Location{Begin: start.Begin, End: end.End}, Expr: e}, nil
		}
		hasParams := false
		var params []TypeOrPack
		if p.cur().Kind == TokenKind('<') {
			hasParams = true
			params = p.parseTypeParamsRaw()
		}
		end := p.prevLoc()
		return &TypeReference{
			Location:      Location{Begin: start.Begin, End: end.End},
			Prefix:        prefix,
			Name:          nm.Name,
			Parameters:    params,
			HasParameters: hasParams,
		}, nil
	case TokenKind('{'):
		return p.parseTableType(inDecl), nil
	case TokenKind('('), TokenKind('<'):
		return p.parseFunctionType(allowPack)
	case TokenFunction:
		p.nextLexeme()
		p.report(start, "Using 'function' as a type is not supported")
		return &TypeError{Location: start}, nil
	}
	p.report(Location{Begin: p.prevLoc().End, End: start.End}, "Expected type, got %s", tokenString(p.cur()))
	return &TypeError{Location: Location{Begin: p.prevLoc().End, End: start.Begin}, IsMissing: true}, nil
}

func (p *parser) parseTableType(inDecl bool) TypeExpr {
	start := p.curLoc()
	matchBr := p.cur()
	p.expectAndConsumeChar('{', "table type")
	var props []TypeTableProp
	var indexer *TypeTableIndexer
	_ = inDecl
	isArray := false
	for p.cur().Kind != TokenKind('}') {
		// Skip access modifiers (read/write) before name (not before '[' or ':').
		if p.cur().Kind == TokenName && p.lookahead().Kind != TokenKind(':') {
			n := string(p.cur().Value)
			if n == "read" || n == "write" {
				p.nextLexeme()
			}
		}
		if p.cur().Kind == TokenKind('[') {
			beginBr := p.cur()
			p.nextLexeme()
			if (p.cur().Kind == TokenRawString || p.cur().Kind == TokenQuotedString) && p.lookahead().Kind == TokenKind(']') {
				keyTok := p.cur()
				var keyBytes []byte
				if keyTok.Kind == TokenRawString {
					keyBytes = fixupMultilineString(keyTok.Value)
				} else {
					b, ok := fixupQuotedString(keyTok.Value)
					if !ok {
						p.report(beginBr.Location, "String literal contains malformed escape")
					}
					keyBytes = b
				}
				keyLoc := p.curLoc()
				p.nextLexeme() // string
				p.expectMatchAndConsume(TokenKind(']'), beginBr)
				p.expectAndConsumeChar(':', "table field")
				t := p.parseType(false)
				props = append(props, TypeTableProp{Name: string(keyBytes), NameLoc: keyLoc, Type: t})
			} else {
				idxType := p.parseType(false)
				p.expectMatchAndConsume(TokenKind(']'), beginBr)
				p.expectAndConsumeChar(':', "table field")
				valType := p.parseType(false)
				if indexer != nil {
					p.report(idxType.Loc(), "Cannot have more than one table indexer")
				} else {
					indexer = &TypeTableIndexer{
						Location:  Location{Begin: beginBr.Location.Begin, End: valType.Loc().End},
						IndexType: idxType,
						ValueType: valType,
					}
				}
			}
		} else if len(props) == 0 && indexer == nil && !(p.cur().Kind == TokenName && p.lookahead().Kind == TokenKind(':')) {
			t := p.parseType(false)
			isArray = true
			idx := &TypeReference{Location: t.Loc(), Name: "number"}
			indexer = &TypeTableIndexer{Location: t.Loc(), IndexType: idx, ValueType: t}
			break
		} else {
			nm := p.parseNameOpt("table field")
			if nm == nil {
				break
			}
			p.expectAndConsumeChar(':', "table field")
			t := p.parseType(inDecl)
			props = append(props, TypeTableProp{Name: nm.Name, NameLoc: nm.Location, Type: t})
		}
		if p.cur().Kind == TokenKind(',') || p.cur().Kind == TokenKind(';') {
			p.nextLexeme()
		} else if p.cur().Kind != TokenKind('}') {
			break
		}
	}
	end := p.curLoc()
	if !p.expectMatchAndConsume(TokenKind('}'), matchBr) {
		end = p.prevLoc()
	}
	_ = isArray
	return &TypeTable{Location: Location{Begin: start.Begin, End: end.End}, Props: props, Indexer: indexer}
}

func (p *parser) parseFunctionType(allowPack bool) (TypeExpr, TypePack) {
	begin := p.cur()
	forceFn := p.cur().Kind == TokenKind('<')
	generics, genericPacks := p.parseGenericTypeList(false)
	parameterStart := p.cur()
	p.expectAndConsumeChar('(', "function parameters")
	var params []TypeExpr
	var names []*Name
	var varargAnn TypePack
	if p.cur().Kind != TokenKind(')') {
		varargAnn = p.parseTypeList(&params, &names)
	}
	closeLoc := p.curLoc()
	p.expectMatchAndConsume(TokenKind(')'), parameterStart)
	if len(names) > 0 {
		forceFn = true
	}
	returnIntro := p.cur().Kind == TokenSkinnyArrow || p.cur().Kind == TokenKind(':')
	if len(params) == 1 && varargAnn == nil && !forceFn && !returnIntro {
		if allowPack {
			node := &TypePackExplicit{Location: begin.Location, Types: params}
			return nil, node
		}
		return &TypeGroup{Location: Location{Begin: parameterStart.Location.Begin, End: closeLoc.End}, Inner: params[0]}, nil
	}
	if !forceFn && !returnIntro && allowPack {
		node := &TypePackExplicit{Location: begin.Location, Types: params, Tail: varargAnn}
		return nil, node
	}
	// function type tail
	return p.parseFunctionTypeTail(begin, generics, genericPacks, params, names, varargAnn), nil
}

func (p *parser) parseFunctionTypeTail(begin Token, generics []GenericType, genericPacks []GenericTypePack, params []TypeExpr, names []*Name, varargAnn TypePack) TypeExpr {
	if p.cur().Kind == TokenKind(':') {
		p.report(p.curLoc(), "Return types in function types are written after '->' instead of ':'")
		p.nextLexeme()
	} else if p.cur().Kind != TokenSkinnyArrow && len(generics) == 0 && len(genericPacks) == 0 && len(params) == 0 {
		p.report(Location{Begin: begin.Location.Begin, End: p.prevLoc().End}, "Expected '->' after '()' when parsing function type; did you mean 'nil'?")
		return &TypeReference{Location: begin.Location, Name: "nil"}
	} else {
		p.expectAndConsume(TokenSkinnyArrow, "function type")
	}
	ret := p.parseReturnType()
	return &TypeFunction{
		Location:     Location{Begin: begin.Location.Begin, End: ret.Loc().End},
		Generics:     generics,
		GenericPacks: genericPacks,
		ArgTypes:     params,
		ArgNames:     names,
		ArgVararg:    varargAnn,
		ReturnType:   ret,
	}
}

func (p *parser) parseOptionalReturnType() TypePack {
	if p.cur().Kind == TokenKind(':') || p.cur().Kind == TokenSkinnyArrow {
		if p.cur().Kind == TokenSkinnyArrow {
			p.report(p.curLoc(), "Function return type annotations are written after ':' instead of '->'")
		}
		p.nextLexeme()
		r := p.parseReturnType()
		if p.cur().Kind == TokenKind(',') {
			p.report(p.curLoc(), "Expected a statement, got ','; did you forget to wrap the list of return types in parentheses?")
			p.nextLexeme()
		}
		return r
	}
	return nil
}

func (p *parser) parseReturnType() TypePack {
	begin := p.cur()
	if p.cur().Kind != TokenKind('(') {
		if shouldParseTypePack(p) {
			return p.parseTypePack()
		}
		t := p.parseType(false)
		return &TypePackExplicit{Location: t.Loc(), Types: []TypeExpr{t}}
	}
	p.nextLexeme()
	var params []TypeExpr
	var names []*Name
	var varargAnn TypePack
	if p.cur().Kind != TokenKind(')') {
		varargAnn = p.parseTypeList(&params, &names)
	}
	loc := Location{Begin: begin.Location.Begin, End: p.curLoc().End}
	p.expectMatchAndConsume(TokenKind(')'), begin)

	if p.cur().Kind != TokenSkinnyArrow && len(names) == 0 {
		if len(params) == 1 {
			var inner TypeExpr
			if varargAnn == nil {
				inner = &TypeGroup{Location: loc, Inner: params[0]}
			} else {
				inner = params[0]
			}
			ret := p.parseTypeSuffix(inner, begin.Location)
			endPos := loc.End
			if len(params) == 1 {
				endPos = loc.End
			}
			_ = endPos
			return &TypePackExplicit{Location: Location{Begin: loc.Begin, End: ret.Loc().End}, Types: []TypeExpr{ret}, Tail: varargAnn}
		}
		return &TypePackExplicit{Location: loc, Types: params, Tail: varargAnn}
	}
	tail := p.parseFunctionTypeTail(begin, nil, nil, params, names, varargAnn)
	return &TypePackExplicit{Location: Location{Begin: loc.Begin, End: tail.Loc().End}, Types: []TypeExpr{tail}}
}

func (p *parser) parseTypeList(types *[]TypeExpr, names *[]*Name) TypePack {
	for {
		if shouldParseTypePack(p) {
			return p.parseTypePack()
		}
		if p.cur().Kind == TokenName && p.lookahead().Kind == TokenKind(':') {
			for len(*names) < len(*types) {
				*names = append(*names, nil)
			}
			nm := Name{Name: string(p.cur().Value), Location: p.curLoc()}
			*names = append(*names, &nm)
			p.nextLexeme()
			p.expectAndConsumeChar(':', "")
		} else if len(*names) > 0 {
			*names = append(*names, nil)
		}
		*types = append(*types, p.parseType(false))
		if p.cur().Kind != TokenKind(',') {
			break
		}
		p.nextLexeme()
		if p.cur().Kind == TokenKind(')') {
			p.report(p.curLoc(), "Expected type after ',' but got ')'")
			break
		}
	}
	return nil
}

func (p *parser) parseSimpleTypeOrPack() (TypeExpr, TypePack) {
	begin := p.curLoc()
	ty, tp := p.parseSimpleType(true, false)
	if tp != nil {
		return nil, tp
	}
	return p.parseTypeSuffix(ty, begin), nil
}

func (p *parser) parseTypePack() TypePack {
	if p.cur().Kind == TokenDot3 {
		start := p.curLoc()
		p.nextLexeme()
		inner := p.parseType(false)
		return &TypePackVariadic{Location: Location{Begin: start.Begin, End: inner.Loc().End}, Inner: inner}
	}
	if p.cur().Kind == TokenName && p.lookahead().Kind == TokenDot3 {
		nm := p.parseName("generic name")
		end := p.curLoc()
		p.expectAndConsume(TokenDot3, "generic type pack annotation")
		return &TypePackGeneric{Location: Location{Begin: nm.Location.Begin, End: end.End}, Name: nm.Name}
	}
	return nil
}

func (p *parser) parseVariadicArgumentTypePack() TypePack {
	if p.cur().Kind == TokenName && p.lookahead().Kind == TokenDot3 {
		nm := p.parseName("generic name")
		end := p.curLoc()
		p.expectAndConsume(TokenDot3, "generic type pack annotation")
		return &TypePackGeneric{Location: Location{Begin: nm.Location.Begin, End: end.End}, Name: nm.Name}
	}
	ann := p.parseType(false)
	return &TypePackVariadic{Location: ann.Loc(), Inner: ann}
}

// ----------------------------------------------------------------------
// String / number literal parsing
// ----------------------------------------------------------------------

func (p *parser) parseString() Expr {
	loc := p.curLoc()
	tok := p.cur()
	var val []byte
	switch tok.Kind {
	case TokenQuotedString, TokenInterpStringSimple:
		b, ok := fixupQuotedString(tok.Value)
		if !ok {
			p.nextLexeme()
			p.report(loc, "String literal contains malformed escape sequence")
			return &ExprError{Location: loc}
		}
		val = b
	case TokenRawString:
		val = fixupMultilineString(tok.Value)
	}
	p.nextLexeme()
	return &ExprConstantString{Location: loc, Value: val, Quote: tok.Quote}
}

func (p *parser) parseInterpString() Expr {
	startLoc := p.curLoc()
	endLoc := p.curLoc()
	var strs [][]byte
	var exprs []Expr

	for {
		cur := p.cur()
		if cur.Kind != TokenInterpStringBegin && cur.Kind != TokenInterpStringMid &&
			cur.Kind != TokenInterpStringEnd && cur.Kind != TokenInterpStringSimple {
			break
		}
		endLoc = cur.Location
		b, ok := fixupQuotedString(cur.Value)
		if !ok {
			p.nextLexeme()
			p.report(Location{Begin: startLoc.Begin, End: endLoc.End}, "Interpolated string literal contains malformed escape sequence")
			return &ExprError{Location: Location{Begin: startLoc.Begin, End: endLoc.End}}
		}
		strs = append(strs, b)
		p.nextLexeme()
		if cur.Kind == TokenInterpStringEnd || cur.Kind == TokenInterpStringSimple {
			break
		}
		switch p.cur().Kind {
		case TokenInterpStringMid, TokenInterpStringEnd:
			p.nextLexeme()
			exprs = append(exprs, &ExprError{Location: endLoc})
		case TokenBrokenString:
			p.nextLexeme()
			exprs = append(exprs, &ExprError{Location: endLoc})
		default:
			exprs = append(exprs, p.parseExpr(0))
		}
		switch p.cur().Kind {
		case TokenInterpStringBegin, TokenInterpStringMid, TokenInterpStringEnd:
			// loop
		case TokenBrokenInterpDoubleBrace:
			p.nextLexeme()
			p.report(endLoc, "Double braces are not permitted within interpolated strings; did you mean '\\{'?")
			return &ExprInterpString{Location: Location{Begin: startLoc.Begin, End: endLoc.End}, Strings: strs, Expressions: exprs}
		case TokenBrokenString:
			p.nextLexeme()
			fallthrough
		case TokenEof:
			p.report(p.prevLoc(), "Malformed interpolated string; did you forget to add a '`'?")
			return &ExprInterpString{Location: Location{Begin: startLoc.Begin, End: p.prevLoc().End}, Strings: strs, Expressions: exprs}
		default:
			p.report(endLoc, "Malformed interpolated string, got %s", tokenString(p.cur()))
			return &ExprInterpString{Location: Location{Begin: startLoc.Begin, End: endLoc.End}, Strings: strs, Expressions: exprs}
		}
	}

	return &ExprInterpString{
		Location:    Location{Begin: startLoc.Begin, End: endLoc.End},
		Strings:     strs,
		Expressions: exprs,
	}
}

func (p *parser) parseNumber() Expr {
	start := p.curLoc()
	raw := p.cur().Value
	// Strip underscores
	s := string(raw)
	if strings.ContainsRune(s, '_') {
		s = strings.ReplaceAll(s, "_", "")
	}
	p.nextLexeme()
	if len(s) == 0 {
		p.report(start, "Malformed number")
		return &ExprError{Location: start}
	}
	// Integer literal suffix 'i'
	if s[len(s)-1] == 'i' {
		body := s[:len(s)-1]
		var (
			value int64
			err   error
		)
		switch {
		case strings.HasPrefix(body, "0x") || strings.HasPrefix(body, "0X"):
			u, perr := strconv.ParseUint(body[2:], 16, 64)
			if perr != nil {
				err = perr
			}
			value = int64(u)
		case strings.HasPrefix(body, "0b") || strings.HasPrefix(body, "0B"):
			u, perr := strconv.ParseUint(body[2:], 2, 64)
			if perr != nil {
				err = perr
			}
			value = int64(u)
		default:
			value, err = strconv.ParseInt(body, 10, 64)
		}
		if err != nil {
			p.report(start, "Malformed integer")
			return &ExprError{Location: start}
		}
		return &ExprConstantInteger{Location: start, Value: value}
	}
	// Float / regular number
	v, kind := parseLuauNumber(s)
	if kind == NumberMalformed {
		p.report(start, "Malformed number")
		return &ExprError{Location: start}
	}
	return &ExprConstantNumber{Location: start, Value: v, Parse: kind}
}

func parseLuauNumber(s string) (float64, NumberParseResult) {
	if len(s) == 0 {
		return 0, NumberMalformed
	}
	// Binary literal
	if len(s) > 2 && s[0] == '0' && (s[1] == 'b' || s[1] == 'B') {
		u, err := strconv.ParseUint(s[2:], 2, 64)
		if err != nil {
			return 0, NumberMalformed
		}
		return float64(u), NumberOk
	}
	// Hex literal
	if len(s) > 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		u, err := strconv.ParseUint(s[2:], 16, 64)
		if err != nil {
			return 0, NumberMalformed
		}
		return float64(u), NumberOk
	}
	// Decimal float
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		if math.IsInf(v, 0) {
			return v, NumberOk
		}
		return 0, NumberMalformed
	}
	return v, NumberOk
}
