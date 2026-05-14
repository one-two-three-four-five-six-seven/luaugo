// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package compiler

import (
	"fmt"
	"math"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/common"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/ast"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"
)

// compile is the public entry point. We catch *CompileError panics and
// surface them as ordinary errors so callers get a clean Go API; any
// non-CompileError panic propagates and indicates a compiler bug.
func compile(prog *ast.Program, opts Options) (mod *bytecode.Module, err error) {
	if prog == nil || prog.Root == nil {
		return nil, &CompileError{Msg: "compiler: nil program"}
	}
	c := newCompiler(opts)
	defer func() {
		if r := recover(); r != nil {
			if ce, ok := r.(*CompileError); ok {
				mod = nil
				err = ce
				return
			}
			panic(r)
		}
	}()
	mainIdx := c.compileMainChunk(prog.Root)
	if c.err != nil {
		return nil, c.err
	}
	return c.builder.finalize(mainIdx), nil
}

// compiler is the per-invocation compiler state. It does not survive
// across Compile calls.
type compiler struct {
	opts    Options
	builder *builder
	err     *CompileError

	// funcStack maintains the per-function state for nested function
	// expressions. The top of the stack is the proto currently being
	// emitted.
	funcStack []*funcCtx

	// mutableGlobals is the set of globals that the compiler must
	// treat as potentially mutable (i.e. cannot use GETIMPORT).
	mutableGlobals map[string]bool
}

func newCompiler(opts Options) *compiler {
	mut := make(map[string]bool, len(opts.MutableGlobals))
	for _, name := range opts.MutableGlobals {
		mut[name] = true
	}
	return &compiler{
		opts:           opts,
		builder:        newBuilder(),
		mutableGlobals: mut,
	}
}

// funcCtx is the per-function context: the proto under construction
// plus locals, upvalues, loop labels, etc.
type funcCtx struct {
	pb *protoBuilder

	// locals maps *ast.Local → assigned register. A local is in scope
	// iff it has an entry here.
	locals map[*ast.Local]uint8
	// localStack records the order locals were declared in (used for
	// undeclaring on block exit and computing endpc for debug info).
	localStack []*ast.Local

	// upvals records upvalues this function captures, in the order the
	// outer NEWCLOSURE+CAPTURE sequence will use.
	upvals []upvalSlot

	// loop tracks the innermost loop's break and continue patch lists.
	loop *loopCtx

	// isVararg matches pb.isVararg; cached for fast tests.
	isVararg bool

	// scopeDepth counts active block scopes (0 = function body's outer).
	scopeDepth int

	// localScopeAt[i] holds the scope depth at which localStack[i] was
	// declared, so blockEnd can pop locals correctly.
	localScopeAt []int

	// node points back at the AST function expression (nil for the
	// main chunk).
	node *ast.ExprFunction
}

type upvalSlot struct {
	// kind: CaptureVal, CaptureRef, or CaptureUpval.
	kind common.CaptureKind
	// when kind == CaptureUpval: index into parent's upvals.
	// when kind == CaptureVal/Ref: register number in parent (locals
	// allocated before the inner function is created).
	index uint8
	// owner identifies the AST local for matching across scopes.
	owner *ast.Local
}

type loopCtx struct {
	parent       *loopCtx
	breakPCs     []int   // jump instruction PCs to patch to the end of the loop
	continuePCs  []int   // jump instruction PCs to patch to the loop's continue target
	continueIsSet bool
	continuePC   int    // jump target for continue (post-set)
	// localsAtEntry is the number of locals on the stack when the loop
	// body began; break and continue must CLOSEUPVALS to this level if
	// any captured locals are in the body.
	localsAtEntry int
	// requireClose tracks whether any local declared inside the loop
	// has been captured; if so break/continue must emit CLOSEUPVALS.
	requireClose bool

	// closeReg is the lowest register that holds a loop-scope local
	// (i.e. a local declared at loop entry such as the for-loop
	// variable, or the first body local for while). break/continue
	// emit a CLOSEUPVALS at this register before jumping so any
	// closures that captured a loop-scope local get a fresh upvalue
	// for the next iteration -- or are properly closed when we leave
	// the loop.
	closeReg uint8
	// breakClose / continueClose select whether break/continue emit
	// a CLOSEUPVALS at closeReg. For repeat...until, continue must
	// NOT close body locals because the until expression can still
	// reference them in scope.
	breakClose    bool
	continueClose bool
}

func (c *compiler) cur() *funcCtx { return c.funcStack[len(c.funcStack)-1] }

func (c *compiler) pushFunc(fc *funcCtx) {
	c.funcStack = append(c.funcStack, fc)
}

func (c *compiler) popFunc() *funcCtx {
	n := len(c.funcStack) - 1
	fc := c.funcStack[n]
	c.funcStack = c.funcStack[:n]
	return fc
}

func (c *compiler) raise(loc ast.Location, format string, args ...any) {
	panic(&CompileError{Location: loc, Msg: fmt.Sprintf(format, args...)})
}

// ----------------------------------------------------------------------
// Top-level / main chunk
// ----------------------------------------------------------------------

func (c *compiler) compileMainChunk(root *ast.Block) uint32 {
	fc := &funcCtx{
		pb:       newProtoBuilder(c.builder),
		locals:   make(map[*ast.Local]uint8),
		isVararg: true,
	}
	fc.pb.isVararg = true
	fc.pb.numParams = 0
	fc.pb.lineDefined = 0
	c.pushFunc(fc)

	// Main chunk is always vararg; emit PREPVARARGS with numparams=0.
	fc.pb.emitABC(common.OpPrepVarargs, 0, 0, 0)

	c.compileBlock(root)

	// If the block didn't terminate with a return, emit one.
	if !c.endsWithReturn(root) {
		fc.pb.emitReturn(0, 0)
	}
	c.popFunc()
	c.builder.module.Protos = append(c.builder.module.Protos, fc.pb.build(0, []byte{}))
	return uint32(len(c.builder.module.Protos) - 1)
}

func (c *compiler) endsWithReturn(blk *ast.Block) bool {
	if blk == nil || len(blk.Body) == 0 {
		return false
	}
	last := blk.Body[len(blk.Body)-1]
	switch last.(type) {
	case *ast.StatReturn, *ast.StatBreak, *ast.StatContinue:
		return true
	}
	return false
}

// ----------------------------------------------------------------------
// Block / statement compilation
// ----------------------------------------------------------------------

func (c *compiler) compileBlock(blk *ast.Block) {
	fc := c.cur()
	fc.scopeDepth++
	localsBefore := len(fc.localStack)
	topBefore := fc.pb.top

	for _, stat := range blk.Body {
		c.compileStat(stat)
	}

	// If this block declared any locals, emit CLOSEUPVALS to close any
	// upvalues that captured them by reference. Without this, when the
	// block's stack slots are reused (either by a sibling block, or by
	// the next iteration of an enclosing loop), the still-open upvalue
	// would alias the new occupant — producing both the classic
	// "all loop closures share the same variable" bug and, after a
	// stack reallocation, sporadic "nil where value expected" failures.
	// CLOSEUPVALS at A=topBefore is a no-op if no upvalue references
	// any slot >= topBefore, so emitting it unconditionally when the
	// block declared locals is safe and slightly conservative.
	declaredLocals := len(fc.localStack) > localsBefore
	if declaredLocals {
		// Skip if the last emitted instruction is a no-return terminator
		// (RETURN/BREAK/CONTINUE/JUMP at the same scope): those already
		// transfer control elsewhere and any close required has been
		// handled at the jump source.
		if !c.lastInsnIsUnreachableTerminator() {
			fc.pb.emitABC(common.OpCloseUpvals, topBefore, 0, 0)
		}
	}
	// Pop locals from the maps.
	for i := len(fc.localStack) - 1; i >= localsBefore; i-- {
		l := fc.localStack[i]
		delete(fc.locals, l)
	}
	fc.localStack = fc.localStack[:localsBefore]
	if localsBefore < len(fc.localScopeAt) {
		fc.localScopeAt = fc.localScopeAt[:localsBefore]
	}
	fc.pb.setTop(topBefore)
	fc.scopeDepth--
}

// lastInsnIsUnreachableTerminator reports whether the most recently
// emitted instruction is a RETURN; we don't bother to peek at JUMP
// because the back-edge of a loop has already taken care of close.
func (c *compiler) lastInsnIsUnreachableTerminator() bool {
	fc := c.cur()
	if len(fc.pb.code) == 0 {
		return false
	}
	last := fc.pb.code[len(fc.pb.code)-1]
	op := common.InsnOp(last)
	return op == common.OpReturn
}

func (c *compiler) compileStat(stat ast.Stat) {
	// Attribute every instruction emitted while compiling this
	// statement to its source line. AST positions are 0-based; the
	// bytecode line-info section is 1-based.
	if stat != nil {
		c.cur().pb.setLine(int(stat.Loc().Begin.Line) + 1)
	}
	switch s := stat.(type) {
	case *ast.Block:
		c.compileBlock(s)
	case *ast.StatReturn:
		c.compileReturn(s)
	case *ast.StatLocal:
		c.compileLocalDecl(s)
	case *ast.StatAssign:
		c.compileAssign(s)
	case *ast.StatCompoundAssign:
		c.compileCompoundAssign(s)
	case *ast.StatExpr:
		c.compileStatExpr(s)
	case *ast.StatIf:
		c.compileIf(s)
	case *ast.StatWhile:
		c.compileWhile(s)
	case *ast.StatRepeat:
		c.compileRepeat(s)
	case *ast.StatBreak:
		c.compileBreak(s)
	case *ast.StatContinue:
		c.compileContinue(s)
	case *ast.StatFor:
		c.compileFor(s)
	case *ast.StatForIn:
		c.compileForIn(s)
	case *ast.StatFunction:
		c.compileStatFunction(s)
	case *ast.StatLocalFunction:
		c.compileLocalFunction(s)
	case *ast.StatTypeAlias, *ast.StatTypeFunction:
		// Type declarations have no runtime effect.
		return
	case *ast.StatError:
		c.raise(s.Location, "compiler: parser error placeholder reached emission")
	default:
		c.raise(stat.Loc(), "compiler: unsupported statement %T", stat)
	}
}

func (c *compiler) compileReturn(s *ast.StatReturn) {
	fc := c.cur()
	if len(s.List) == 0 {
		fc.pb.emitReturn(0, 0)
		return
	}
	// Allocate contiguous registers for all return values.
	base := fc.pb.top
	if len(s.List) == 1 {
		// Could be a multi-return call expression; use B=0 (multret).
		if c.isMultRetExpr(s.List[0]) {
			c.compileExprMultRet(s.List[0], base)
			fc.pb.emitABC(common.OpReturn, base, 0, 0)
			fc.pb.setTop(base)
			return
		}
	}
	// Standard fixed-count return.
	for i, e := range s.List {
		isLast := i == len(s.List)-1
		if isLast && c.isMultRetExpr(e) {
			c.compileExprMultRet(e, base+uint8(i))
			fc.pb.emitABC(common.OpReturn, base, 0, 0)
			fc.pb.setTop(base)
			return
		}
		reg := fc.pb.reserveReg(1)
		c.compileExprToReg(e, reg)
	}
	fc.pb.emitReturn(base, len(s.List))
	fc.pb.setTop(base)
}

func (c *compiler) isMultRetExpr(e ast.Expr) bool {
	switch e.(type) {
	case *ast.ExprCall, *ast.ExprVarargs:
		return true
	}
	return false
}

func (c *compiler) compileLocalDecl(s *ast.StatLocal) {
	fc := c.cur()
	nvars := len(s.Vars)
	nvalues := len(s.Values)
	if nvalues == 0 {
		// Initialize all to nil.
		base := fc.pb.reserveReg(nvars)
		if nvars == 1 {
			fc.pb.emitABC(common.OpLoadNil, base, 0, 0)
		} else {
			// LOADNIL clears one register; emit one per local (or use
			// LOADNIL ranges? Upstream emits LOADNIL A with B holding
			// the count for v0..but opcode is "R(A) = nil" only.)
			for i := 0; i < nvars; i++ {
				fc.pb.emitABC(common.OpLoadNil, base+uint8(i), 0, 0)
			}
		}
		for i, l := range s.Vars {
			fc.locals[l] = base + uint8(i)
			fc.localStack = append(fc.localStack, l)
			fc.localScopeAt = append(fc.localScopeAt, fc.scopeDepth)
		}
		return
	}

	// Compile the value expressions into a contiguous block of registers
	// starting at the current top, then assign them to the locals.
	base := fc.pb.top
	for i, e := range s.Values {
		isLast := i == nvalues-1
		// If last expr is a multret expression and we need more values
		// than expressions, expand it.
		if isLast && nvars > nvalues && c.isMultRetExpr(e) {
			// Compile the call/vararg into base+i.. expecting nvars-i values.
			needed := nvars - i
			reg := fc.pb.reserveReg(needed)
			_ = reg
			c.compileExprMultRetExact(e, base+uint8(i), needed)
			break
		}
		reg := fc.pb.reserveReg(1)
		c.compileExprToReg(e, reg)
	}
	// If we have more values than vars, drop the extras.
	if nvalues > nvars {
		// Discard extra values: free those registers.
		extra := nvalues - nvars
		fc.pb.setTop(fc.pb.top - uint8(extra))
	}
	// If we have fewer values than vars (and no multret), pad with nils.
	if nvalues < nvars && !c.isMultRetExpr(s.Values[nvalues-1]) {
		for i := nvalues; i < nvars; i++ {
			reg := fc.pb.reserveReg(1)
			fc.pb.emitABC(common.OpLoadNil, reg, 0, 0)
		}
	}

	// Register the locals against the produced registers.
	for i, l := range s.Vars {
		fc.locals[l] = base + uint8(i)
		fc.localStack = append(fc.localStack, l)
		fc.localScopeAt = append(fc.localScopeAt, fc.scopeDepth)
	}
}

// compileAssign handles `a, b, c = e1, e2, e3`.
//
// Per Luau/Lua semantics, every LHS target's table and key
// sub-expressions must be evaluated BEFORE any of the LHS slots are
// written. Otherwise, writing to a local that is also used in a later
// LHS index expression (e.g. `a, b[a] = 43, -1`) would corrupt the
// index. We therefore pre-resolve every target into an lvalueDesc
// snapshot (allocating temps for `obj` and `key` of indexed targets)
// before compiling the RHS, then perform the assignments in order.
func (c *compiler) compileAssign(s *ast.StatAssign) {
	fc := c.cur()
	nvars := len(s.Vars)
	nvals := len(s.Values)

	// Single assignment fast path keeps the simpler/older codegen.
	if nvars == 1 && nvals == 1 {
		base := fc.pb.top
		tmp := fc.pb.reserveReg(1)
		c.compileExprToReg(s.Values[0], tmp)
		c.compileAssignTarget(s.Vars[0], tmp)
		fc.pb.setTop(base)
		return
	}

	// Step 1: pre-evaluate all LHS target addresses (table + key for
	// indexed targets). These temps live BELOW the RHS temps so they
	// are not perturbed by RHS compilation.
	base := fc.pb.top
	descs := make([]lvalueDesc, nvars)
	for i, target := range s.Vars {
		descs[i] = c.preEvalLValue(target)
	}

	// Step 2: compile all RHS values into a contiguous block of
	// temporaries (in declaration order), above the LHS temps.
	rhsBase := fc.pb.top
	for i, e := range s.Values {
		isLast := i == nvals-1
		if isLast && nvars > nvals && c.isMultRetExpr(e) {
			needed := nvars - i
			reg := fc.pb.reserveReg(needed)
			_ = reg
			c.compileExprMultRetExact(e, rhsBase+uint8(i), needed)
			break
		}
		reg := fc.pb.reserveReg(1)
		c.compileExprToReg(e, reg)
	}
	// Pad with nils as needed.
	if nvals < nvars && (nvals == 0 || !c.isMultRetExpr(s.Values[nvals-1])) {
		for i := nvals; i < nvars; i++ {
			reg := fc.pb.reserveReg(1)
			fc.pb.emitABC(common.OpLoadNil, reg, 0, 0)
		}
	}
	// Drop extras.
	if nvals > nvars {
		fc.pb.setTop(fc.pb.top - uint8(nvals-nvars))
	}

	// Step 3: assign each RHS temp to the (already-resolved) target.
	for i := 0; i < nvars; i++ {
		src := rhsBase + uint8(i)
		c.emitAssignFromDesc(s.Vars[i], descs[i], src)
	}
	fc.pb.setTop(base)
}

// lvalueDesc snapshots a multi-assignment LHS target after its
// addressing sub-expressions have been evaluated. The destination is
// frozen here so subsequent writes to other LHS slots cannot affect
// where this slot is written.
type lvalueDesc struct {
	kind     lvalueKind
	objReg   uint8  // for indexname/indexexpr/indexnum: register holding the table
	keyReg   uint8  // for indexexpr: register holding the key
	smallIdx uint8  // for indexnum: 1-based small int key in [1,256] (encoded as i)
	// objIsTemp/keyIsTemp record whether we allocated a fresh temp so
	// emitAssignFromDesc can release them later.
	objIsTemp bool
	keyIsTemp bool
}

type lvalueKind uint8

const (
	lvLocal lvalueKind = iota
	lvUpval
	lvGlobal
	lvIndexName
	lvIndexNum
	lvIndexExpr
)

// preEvalLValue resolves a LHS target and pre-computes any table/key
// registers needed to assign to it. Locals/upvalues/globals require no
// pre-evaluation. For indexed targets we allocate fresh temps and
// emit code to evaluate obj (and key) into them now -- this is what
// guarantees that an earlier LHS write to a local cannot disturb a
// later LHS index expression that read that local.
func (c *compiler) preEvalLValue(target ast.Expr) lvalueDesc {
	fc := c.cur()
	switch t := target.(type) {
	case *ast.ExprLocal:
		return lvalueDesc{kind: lvLocal}
	case *ast.ExprGlobal:
		return lvalueDesc{kind: lvGlobal}
	case *ast.ExprIndexName:
		obj := fc.pb.reserveReg(1)
		c.compileExprToReg(t.Expr, obj)
		return lvalueDesc{kind: lvIndexName, objReg: obj, objIsTemp: true}
	case *ast.ExprIndexExpr:
		// Specialize small int key to SETTABLEN: no key reg needed.
		if nlit, ok := t.Index.(*ast.ExprConstantNumber); ok {
			if idx, isInt := smallIndexLiteral(nlit.Value); isInt {
				obj := fc.pb.reserveReg(1)
				c.compileExprToReg(t.Expr, obj)
				return lvalueDesc{kind: lvIndexNum, objReg: obj, smallIdx: idx, objIsTemp: true}
			}
		}
		obj := fc.pb.reserveReg(1)
		c.compileExprToReg(t.Expr, obj)
		key := fc.pb.reserveReg(1)
		c.compileExprToReg(t.Index, key)
		return lvalueDesc{kind: lvIndexExpr, objReg: obj, keyReg: key, objIsTemp: true, keyIsTemp: true}
	default:
		c.raise(target.Loc(), "compiler: cannot assign to %T", target)
		return lvalueDesc{}
	}
}

// emitAssignFromDesc writes src into the location described by d.
// target is only consulted for fields not captured in d (local reg,
// upvalue id, global name string).
func (c *compiler) emitAssignFromDesc(target ast.Expr, d lvalueDesc, src uint8) {
	fc := c.cur()
	switch d.kind {
	case lvLocal:
		t := target.(*ast.ExprLocal)
		dst, ok := c.resolveLocalReg(t.Local)
		if ok && !t.Upvalue {
			if dst != src {
				fc.pb.emitABC(common.OpMove, dst, src, 0)
			}
			return
		}
		uid := c.resolveUpvalue(t.Local)
		fc.pb.emitABC(common.OpSetUpval, src, uid, 0)
	case lvGlobal:
		t := target.(*ast.ExprGlobal)
		sidx := fc.pb.addStringConstant(t.Name)
		fc.pb.emitABC(common.OpSetGlobal, src, 0, 0)
		fc.pb.emitAux(sidx)
	case lvIndexName:
		t := target.(*ast.ExprIndexName)
		sidx := fc.pb.addStringConstant(t.IndexName)
		fc.pb.emitABC(common.OpSetTableKS, src, d.objReg, 0)
		fc.pb.emitAux(sidx)
	case lvIndexNum:
		fc.pb.emitABC(common.OpSetTableN, src, d.objReg, d.smallIdx)
	case lvIndexExpr:
		fc.pb.emitABC(common.OpSetTable, src, d.objReg, d.keyReg)
	}
}

func (c *compiler) compileAssignTarget(target ast.Expr, src uint8) {
	fc := c.cur()
	switch t := target.(type) {
	case *ast.ExprLocal:
		dst, ok := c.resolveLocalReg(t.Local)
		if ok && !t.Upvalue {
			if dst != src {
				fc.pb.emitABC(common.OpMove, dst, src, 0)
			}
			return
		}
		// Upvalue.
		uid := c.resolveUpvalue(t.Local)
		fc.pb.emitABC(common.OpSetUpval, src, uid, 0)
	case *ast.ExprGlobal:
		sidx := fc.pb.addStringConstant(t.Name)
		fc.pb.emitABC(common.OpSetGlobal, src, 0, 0)
		fc.pb.emitAux(sidx)
	case *ast.ExprIndexName:
		// obj[name] = src
		objReg := c.compileExprAsReg(t.Expr, false)
		sidx := fc.pb.addStringConstant(t.IndexName)
		fc.pb.emitABC(common.OpSetTableKS, src, objReg, 0)
		fc.pb.emitAux(sidx)
		c.releaseTempIfTop(t.Expr, objReg)
	case *ast.ExprIndexExpr:
		objReg := c.compileExprAsReg(t.Expr, false)
		keyReg := c.compileExprAsReg(t.Index, false)
		// Specialize: small int key → SETTABLEN.
		if nlit, ok := t.Index.(*ast.ExprConstantNumber); ok {
			if i, isInt := smallIndexLiteral(nlit.Value); isInt {
				fc.pb.emitABC(common.OpSetTableN, src, objReg, uint8(i))
				c.releaseTempIfTop(t.Index, keyReg)
				c.releaseTempIfTop(t.Expr, objReg)
				return
			}
		}
		fc.pb.emitABC(common.OpSetTable, src, objReg, keyReg)
		c.releaseTempIfTop(t.Index, keyReg)
		c.releaseTempIfTop(t.Expr, objReg)
	default:
		c.raise(target.Loc(), "compiler: cannot assign to %T", target)
	}
}

func (c *compiler) compileCompoundAssign(s *ast.StatCompoundAssign) {
	fc := c.cur()
	base := fc.pb.top

	// Side-effect-correct lowering: evaluate the target's obj/key
	// sub-expressions exactly once, do GET into tmp, do the op, and
	// reuse the cached obj/key for the SET. conformance/basic.luau:976
	// has `res[(function() count += 1; return count end)()] += 5`,
	// which must invoke the index closure exactly once per compound
	// assignment.
	switch t := s.Var.(type) {
	case *ast.ExprIndexName:
		objReg := c.compileExprAsReg(t.Expr, false)
		sidx := fc.pb.addStringConstant(t.IndexName)
		// tmp = obj[name]
		tmp := fc.pb.reserveReg(1)
		fc.pb.emitABC(common.OpGetTableKS, tmp, objReg, 0)
		fc.pb.emitAux(sidx)
		// tmp = tmp op value
		c.compoundOp(s.Op, tmp, s.Value)
		// obj[name] = tmp
		fc.pb.emitABC(common.OpSetTableKS, tmp, objReg, 0)
		fc.pb.emitAux(sidx)
		fc.pb.setTop(base)
		return
	case *ast.ExprIndexExpr:
		objReg := c.compileExprAsReg(t.Expr, false)
		keyReg := c.compileExprAsReg(t.Index, false)
		tmp := fc.pb.reserveReg(1)
		// Specialise small int constant keys to GET/SETTABLEN where
		// possible -- saves an instruction and matches non-compound
		// assigns.
		if nlit, ok := t.Index.(*ast.ExprConstantNumber); ok {
			if i, isInt := smallIndexLiteral(nlit.Value); isInt {
				fc.pb.emitABC(common.OpGetTableN, tmp, objReg, uint8(i))
				c.compoundOp(s.Op, tmp, s.Value)
				fc.pb.emitABC(common.OpSetTableN, tmp, objReg, uint8(i))
				fc.pb.setTop(base)
				return
			}
		}
		fc.pb.emitABC(common.OpGetTable, tmp, objReg, keyReg)
		c.compoundOp(s.Op, tmp, s.Value)
		fc.pb.emitABC(common.OpSetTable, tmp, objReg, keyReg)
		fc.pb.setTop(base)
		return
	}

	// Fallback for local / upvalue / global targets: simple
	// load → op → store. No double-evaluation risk because these
	// have no sub-expressions.
	tmp := fc.pb.reserveReg(1)
	c.compileExprToReg(s.Var, tmp)
	c.compoundOp(s.Op, tmp, s.Value)
	c.compileAssignTarget(s.Var, tmp)
	fc.pb.setTop(base)
}

// compoundOp applies the op of a compound assignment to register tmp
// using rhs expression `value`, storing the result back into tmp. Used
// by all four target shapes of compileCompoundAssign.
func (c *compiler) compoundOp(op ast.BinaryOp, tmp uint8, value ast.Expr) {
	fc := c.cur()
	if op == ast.BinaryConcat {
		// OpConcat requires contiguous operand registers.
		rhs := fc.pb.reserveReg(1)
		c.compileExprToReg(value, rhs)
		fc.pb.emitABC(common.OpConcat, tmp, tmp, rhs)
		fc.pb.setTop(tmp + 1)
		return
	}
	rhsReg := c.compileExprAsReg(value, false)
	c.emitBinaryOp(op, tmp, tmp, rhsReg)
	c.releaseTempIfTop(value, rhsReg)
}

func (c *compiler) compileStatExpr(s *ast.StatExpr) {
	fc := c.cur()
	switch e := s.Expr.(type) {
	case *ast.ExprCall:
		// A call as a statement: compile as multret with 0 results.
		base := fc.pb.top
		c.compileCall(e, base, 0)
		fc.pb.setTop(base)
	default:
		// Per Lua grammar this should only ever be a call. Be lenient.
		base := fc.pb.top
		tmp := fc.pb.reserveReg(1)
		c.compileExprToReg(s.Expr, tmp)
		fc.pb.setTop(base)
	}
}

// ----------------------------------------------------------------------
// Control flow
// ----------------------------------------------------------------------

func (c *compiler) compileIf(s *ast.StatIf) {
	fc := c.cur()
	endPatches := []int{}

	// First branch.
	jmpToElse := c.compileCondJump(s.Condition, false /* jumpIf falsy */)
	c.compileBlock(s.ThenBody)

	if s.ElseBody != nil {
		// Jump past the else.
		jumpPC := fc.pb.pc()
		fc.pb.emitAD(common.OpJump, 0, 0)
		endPatches = append(endPatches, jumpPC)
		// Patch jmpToElse to here.
		c.patchJumpTo(jmpToElse, fc.pb.pc())
		// Compile else body.
		switch eb := s.ElseBody.(type) {
		case *ast.Block:
			c.compileBlock(eb)
		case *ast.StatIf:
			c.compileIf(eb)
		default:
			c.compileStat(eb.(ast.Stat))
		}
	} else {
		c.patchJumpTo(jmpToElse, fc.pb.pc())
	}
	for _, pc := range endPatches {
		c.patchJumpTo(pc, fc.pb.pc())
	}
}

// compileCondJump compiles cond and emits a conditional jump. If
// jumpIfTrue is false, the jump is taken when cond is FALSY. Returns
// the PC of the emitted jump instruction.
func (c *compiler) compileCondJump(cond ast.Expr, jumpIfTrue bool) int {
	fc := c.cur()
	// Evaluate condition into a temp.
	base := fc.pb.top
	r := fc.pb.reserveReg(1)
	c.compileExprToReg(cond, r)
	pc := fc.pb.pc()
	if jumpIfTrue {
		fc.pb.emitAD(common.OpJumpIf, r, 0)
	} else {
		fc.pb.emitAD(common.OpJumpIfNot, r, 0)
	}
	fc.pb.setTop(base)
	return pc
}

func (c *compiler) patchJumpTo(pc int, target int) {
	delta := int32(target - pc - 1)
	c.cur().pb.patchD(pc, delta)
}

func (c *compiler) compileWhile(s *ast.StatWhile) {
	fc := c.cur()
	loopStart := fc.pb.pc()

	// Compile condition; jump to end on falsy.
	jmpEnd := c.compileCondJump(s.Condition, false)

	// Set up loop context for break/continue. closeReg is the current
	// top: any locals declared inside the body live at >= top.
	prevLoop := fc.loop
	loop := &loopCtx{
		parent:        prevLoop,
		localsAtEntry: len(fc.localStack),
		closeReg:      fc.pb.top,
		breakClose:    true,
		continueClose: true,
	}
	fc.loop = loop

	c.compileBlock(s.Body)

	// Patch continue jumps to the back-edge.
	for _, pc := range loop.continuePCs {
		c.patchJumpTo(pc, fc.pb.pc())
	}

	// Emit JUMPBACK to loop start.
	backPC := fc.pb.pc()
	delta := int32(loopStart - backPC - 1)
	fc.pb.emitAD(common.OpJumpBack, 0, delta)

	// Patch end-jump.
	c.patchJumpTo(jmpEnd, fc.pb.pc())
	for _, pc := range loop.breakPCs {
		c.patchJumpTo(pc, fc.pb.pc())
	}
	fc.loop = prevLoop
}

func (c *compiler) compileRepeat(s *ast.StatRepeat) {
	fc := c.cur()
	loopStart := fc.pb.pc()

	prevLoop := fc.loop
	loop := &loopCtx{
		parent:        prevLoop,
		localsAtEntry: len(fc.localStack),
		closeReg:      fc.pb.top,
		// repeat...until: break closes body locals (we exit the loop)
		// but continue must NOT close them because the until
		// expression evaluated next can still reference them.
		breakClose:    true,
		continueClose: false,
	}
	fc.loop = loop

	// In Luau, the until expression can see locals defined in the body,
	// so we don't pop block locals until after the until check. We
	// implement this by inlining the block without a fresh compileBlock
	// scope boundary; instead we open the scope manually.
	fc.scopeDepth++
	localsBefore := len(fc.localStack)
	topBefore := fc.pb.top
	for _, stat := range s.Body.Body {
		c.compileStat(stat)
	}

	// Patch continue jumps to land at the until-check. Repeat..until
	// in Luau preserves body locals into the until expression (see
	// conformance/constructs.luau:253), so we do NOT close upvalues
	// here -- a closure captured by the body must remain attached to
	// its still-live stack slot when the until condition reads it.
	for _, pc := range loop.continuePCs {
		c.patchJumpTo(pc, fc.pb.pc())
	}

	// Compile until condition. Whether the loop exits or loops back,
	// we must CLOSEUPVALS before tearing down (or rebinding) the body
	// locals so that closures captured during this iteration don't
	// alias the slots that the next iteration -- or the post-loop
	// code -- will reuse. conformance/basic.luau:904 requires this
	// on the looping branch; the same closure-capture invariant
	// matters on the exit branch when the loop body's last captured
	// upvalue would otherwise survive into post-loop register reuse.
	r := fc.pb.reserveReg(1)
	c.compileExprToReg(s.Condition, r)
	fc.pb.setTop(fc.pb.top - 1)
	// JUMPIFNOT R, delta-to-loopStart -- loop back when the until
	// condition is false. (Emit BEFORE the CLOSEUPVALS so the body
	// locals are still live for the until expression; CLOSEUPVALS
	// itself only detaches upvalues, leaving the stack values intact,
	// so it doesn't matter that the read happened first.) The branch
	// target is loopStart, but CLOSEUPVALS must execute on EITHER
	// path before any subsequent register reuse, so we emit the
	// CLOSEUPVALS on the fall-through (exit) path and a separate
	// CLOSEUPVALS+JUMP on the back-edge.
	condPC := fc.pb.pc()
	fc.pb.emitAD(common.OpJumpIfNot, r, 0) // placeholder
	// Fall-through path: loop exits. Close captured upvalues.
	if fc.pb.top > topBefore {
		fc.pb.emitABC(common.OpCloseUpvals, topBefore, 0, 0)
	}
	exitPC := fc.pb.pc()
	fc.pb.emitAD(common.OpJump, 0, 0) // placeholder; jumps over the back-edge
	// Back-edge path: close upvalues and loop back to loopStart.
	c.patchJumpTo(condPC, fc.pb.pc())
	if fc.pb.top > topBefore {
		fc.pb.emitABC(common.OpCloseUpvals, topBefore, 0, 0)
	}
	backPC := fc.pb.pc()
	fc.pb.emitAD(common.OpJump, 0, int32(loopStart-backPC-1))
	// Past the back-edge: this is the loop-exit landing point.
	c.patchJumpTo(exitPC, fc.pb.pc())

	// Patch break jumps to here.
	for _, pc := range loop.breakPCs {
		c.patchJumpTo(pc, fc.pb.pc())
	}

	// Pop locals.
	for i := len(fc.localStack) - 1; i >= localsBefore; i-- {
		l := fc.localStack[i]
		delete(fc.locals, l)
	}
	fc.localStack = fc.localStack[:localsBefore]
	if localsBefore < len(fc.localScopeAt) {
		fc.localScopeAt = fc.localScopeAt[:localsBefore]
	}
	fc.pb.setTop(topBefore)
	fc.scopeDepth--

	fc.loop = prevLoop
}

func (c *compiler) compileBreak(s *ast.StatBreak) {
	fc := c.cur()
	if fc.loop == nil {
		c.raise(s.Location, "compiler: break outside of loop")
	}
	// Close any upvalues captured by loop-body locals before jumping
	// out of the loop -- otherwise their open upvalues would alias
	// stack slots reused by code after the loop.
	if fc.loop.breakClose {
		fc.pb.emitABC(common.OpCloseUpvals, fc.loop.closeReg, 0, 0)
	}
	pc := fc.pb.pc()
	fc.pb.emitAD(common.OpJump, 0, 0)
	fc.loop.breakPCs = append(fc.loop.breakPCs, pc)
}

func (c *compiler) compileContinue(s *ast.StatContinue) {
	fc := c.cur()
	if fc.loop == nil {
		c.raise(s.Location, "compiler: continue outside of loop")
	}
	if fc.loop.continueClose {
		fc.pb.emitABC(common.OpCloseUpvals, fc.loop.closeReg, 0, 0)
	}
	pc := fc.pb.pc()
	fc.pb.emitAD(common.OpJump, 0, 0)
	fc.loop.continuePCs = append(fc.loop.continuePCs, pc)
}

// compileFor handles numeric for: for v = init, limit[, step] do body end.
//
// Upstream Luau uses a 4-register layout for the numeric `for` loop:
//
//	R(A+0) = limit
//	R(A+1) = step
//	R(A+2) = internal index (VM-private; not a Lua local)
//	R(A+3) = user-visible loop variable (the Lua local)
//
// FORNPREP at A=base:
//   - coerces the three initial slots to numbers,
//   - jumps to end if the loop is already complete.
//
// FORNLOOP at A=base:
//   - adds R(A+1) to R(A+2) (the internal index),
//   - if still within [limit] (per sign of step), copies the updated
//     index into R(A+3) and jumps back to the body.
//
// Upstream seeds R(A+3) for the first iteration not via the VM but via
// a MOVE A+3, A+2 emitted at the top of the body. We mirror that
// pattern below: emit a MOVE immediately after FORNPREP so the body
// always sees a valid user-visible variable, regardless of whether the
// current iteration is the first (entered via FORNPREP) or a
// continuation (entered via the back-edge from FORNLOOP).
//
// This split is what makes assignments to the loop variable inside the
// body safe: the body sees the user-visible `R(A+3)` while the VM's
// canonical counter lives in `R(A+2)`. Our previous emitter folded the
// counter and the user variable into one slot (`base+2`), so user code
// like `for b=9,1,-2 do ... b = nil end` could clobber the counter and
// terminate the loop early (basic.luau:188).
func (c *compiler) compileFor(s *ast.StatFor) {
	fc := c.cur()
	base := fc.pb.reserveReg(4) // limit, step, index, var

	// Compile init into base+2 (internal index).
	c.compileExprToReg(s.From, base+2)
	// Compile limit into base+0.
	c.compileExprToReg(s.To, base)
	// Compile step into base+1; default 1.
	if s.Step != nil {
		c.compileExprToReg(s.Step, base+1)
	} else {
		fc.pb.emitAD(common.OpLoadN, base+1, 1)
	}

	// FORNPREP: A=base, D=jump-to-end. The VM also seeds R(A+3) with
	// the validated initial index, so on entry to the body the
	// user-visible variable is already populated.
	prepPC := fc.pb.pc()
	fc.pb.emitAD(common.OpForNPrep, base, 0)

	bodyPC := fc.pb.pc()
	// Seed the user-visible variable from the internal index for the
	// first iteration. FORNLOOP handles this for subsequent iterations
	// (it writes R(A+3) before taking the back-edge), but FORNPREP
	// itself does not, so without this MOVE the body would see a
	// stale slot on iteration 1.
	fc.pb.emitABC(common.OpMove, base+3, base+2, 0)
	// Bind the user-visible loop variable to base+3. The internal
	// index at base+2 is NOT a Lua local: it has no entry in
	// fc.locals, so name resolution for `b` (or any other identifier)
	// can never reach it.
	fc.locals[s.Var] = base + 3
	fc.localStack = append(fc.localStack, s.Var)
	fc.localScopeAt = append(fc.localScopeAt, fc.scopeDepth)

	prevLoop := fc.loop
	loop := &loopCtx{
		parent:        prevLoop,
		localsAtEntry: len(fc.localStack),
		// closeReg = base+3: the user-visible var is the only slot
		// in this loop's layout that a closure can legitimately
		// capture by name. The internal index at base+2 is invisible
		// to source code, so no upvalue can be open at that level.
		closeReg:      base + 3,
		breakClose:    true,
		continueClose: true,
	}
	fc.loop = loop

	c.compileBlock(s.Body)

	// Close any upvalues that captured the loop variable. Without
	// this, closures inside the body that capture `b` would all share
	// one upvalue across iterations -- breaking
	// `for i=1,N do a[i]=function() return i end`. CLOSEUPVALS at
	// A=base+3 is a no-op if no upvalue is open at that level.
	if !c.lastInsnIsUnreachableTerminator() {
		fc.pb.emitABC(common.OpCloseUpvals, base+3, 0, 0)
	}

	// Patch continue jumps to the FORNLOOP.
	loopPC := fc.pb.pc()
	for _, pc := range loop.continuePCs {
		c.patchJumpTo(pc, loopPC)
	}

	// FORNLOOP: A=base, D=jump-back-to-body.
	fc.pb.emitAD(common.OpForNLoop, base, int32(bodyPC-loopPC-1))

	// Patch FORNPREP to skip body when init violates termination.
	c.patchJumpTo(prepPC, fc.pb.pc())

	// Patch break jumps to here. compileBreak already emitted a
	// CLOSEUPVALS before the JUMP, so we don't need another one here.
	for _, pc := range loop.breakPCs {
		c.patchJumpTo(pc, fc.pb.pc())
	}

	// Pop the loop var.
	delete(fc.locals, s.Var)
	fc.localStack = fc.localStack[:len(fc.localStack)-1]
	fc.localScopeAt = fc.localScopeAt[:len(fc.localScopeAt)-1]
	fc.pb.setTop(base)
	fc.loop = prevLoop
}

// compileForIn handles generic for: for v1,v2,...,vN in e1,e2,e3 do body end.
//
// Upstream allocates [generator, state, index] (3 regs) followed by
// max(nvars, 2) variable slots so the ipairs/pairs fast path can assume
// at least two variables. FORGPREP at A=base jumps to the FORGLOOP at
// the loop back-edge; FORGLOOP's AUX has the variable count.
func (c *compiler) compileForIn(s *ast.StatForIn) {
	fc := c.cur()
	nvars := len(s.Vars)
	// 3 slots for iter/state/ctrl.
	base := fc.pb.reserveReg(3)

	nvals := len(s.Values)
	if nvals == 1 {
		// Single expression: typically a call returning (f, s, var)
		// triple. Compile as multret expecting 3 values.
		c.compileExprMultRetExact(s.Values[0], base, 3)
	} else {
		// Multiple expressions; compile each.
		for i, e := range s.Values {
			isLast := i == nvals-1
			if isLast && c.isMultRetExpr(e) && 3-i > 0 {
				c.compileExprMultRetExact(e, base+uint8(i), 3-i)
				break
			}
			if i < 3 {
				c.compileExprToReg(e, base+uint8(i))
			} else {
				// Evaluate for side-effects then discard.
				tmp := fc.pb.reserveReg(1)
				c.compileExprToReg(e, tmp)
				fc.pb.setTop(fc.pb.top - 1)
			}
		}
		// Fill missing slots with nil.
		if nvals < 3 {
			for i := nvals; i < 3; i++ {
				fc.pb.emitABC(common.OpLoadNil, base+uint8(i), 0, 0)
			}
		}
	}
	// Restore top to base+3 (compileCall may have left it lower).
	if fc.pb.top < base+3 {
		fc.pb.top = base + 3
		if fc.pb.top > fc.pb.maxStack {
			fc.pb.maxStack = fc.pb.top
		}
	}

	// Now reserve variable slots (at least 2 for ipairs/pairs fast path).
	varSlots := nvars
	if varSlots < 2 {
		varSlots = 2
	}
	fc.pb.reserveReg(varSlots)

	// FORGPREP: A=base, D=jump-to-FORGLOOP.
	prepPC := fc.pb.pc()
	fc.pb.emitAD(common.OpForGPrep, base, 0)

	bodyPC := fc.pb.pc()
	// Bind the vars in registers base+3..
	for i, v := range s.Vars {
		fc.locals[v] = base + 3 + uint8(i)
		fc.localStack = append(fc.localStack, v)
		fc.localScopeAt = append(fc.localScopeAt, fc.scopeDepth)
	}

	prevLoop := fc.loop
	loop := &loopCtx{
		parent:        prevLoop,
		localsAtEntry: len(fc.localStack),
		closeReg:      base + 3,
		breakClose:    true,
		continueClose: true,
	}
	fc.loop = loop

	c.compileBlock(s.Body)

	// Close any upvalues that captured the loop variables (>= base+3)
	// at the back-edge so each iteration's closures get a fresh
	// upvalue. Mirrors the numeric for-loop fix; see compileFor.
	if !c.lastInsnIsUnreachableTerminator() {
		fc.pb.emitABC(common.OpCloseUpvals, base+3, 0, 0)
	}

	// Patch continue to FORGLOOP.
	loopPC := fc.pb.pc()
	for _, pc := range loop.continuePCs {
		c.patchJumpTo(pc, loopPC)
	}

	// FORGLOOP: A=base, D=jump-back-to-body, AUX=varcount.
	fc.pb.emitAD(common.OpForGLoop, base, int32(bodyPC-loopPC-1))
	fc.pb.emitAux(uint32(nvars))

	// Patch FORGPREP to here (D=jump-to-FORGLOOP).
	c.patchJumpTo(prepPC, loopPC)

	// Patch breaks to after FORGLOOP. compileBreak already emitted a
	// CLOSEUPVALS for the captured loop vars before jumping.
	for _, pc := range loop.breakPCs {
		c.patchJumpTo(pc, fc.pb.pc())
	}

	// Pop loop vars.
	for range s.Vars {
		l := fc.localStack[len(fc.localStack)-1]
		delete(fc.locals, l)
		fc.localStack = fc.localStack[:len(fc.localStack)-1]
		fc.localScopeAt = fc.localScopeAt[:len(fc.localScopeAt)-1]
	}
	fc.pb.setTop(base)
	fc.loop = prevLoop
}

// ----------------------------------------------------------------------
// Functions
// ----------------------------------------------------------------------

func (c *compiler) compileLocalFunction(s *ast.StatLocalFunction) {
	fc := c.cur()
	// Allocate the local FIRST so that recursive calls resolve it.
	reg := fc.pb.reserveReg(1)
	fc.locals[s.Name] = reg
	fc.localStack = append(fc.localStack, s.Name)
	fc.localScopeAt = append(fc.localScopeAt, fc.scopeDepth)
	// Compile the function as a child proto.
	protoIdx, captures := c.compileFuncExpr(s.Func, s.Name.Name)
	c.emitNewClosure(reg, protoIdx, captures)
}

func (c *compiler) compileStatFunction(s *ast.StatFunction) {
	fc := c.cur()
	// Compile the function and store the resulting closure into the
	// target (which is some lvalue chain).
	debugName := ""
	if g, ok := s.Name.(*ast.ExprGlobal); ok {
		debugName = g.Name
	}
	protoIdx, captures := c.compileFuncExpr(s.Func, debugName)
	// Emit NEWCLOSURE into a temp, then assign.
	base := fc.pb.top
	tmp := fc.pb.reserveReg(1)
	c.emitNewClosure(tmp, protoIdx, captures)
	c.compileAssignTarget(s.Name, tmp)
	fc.pb.setTop(base)
}

// compileFuncExpr compiles an inner function and returns its child-proto
// index plus the captures the parent must emit via OpCapture.
func (c *compiler) compileFuncExpr(fn *ast.ExprFunction, debugName string) (uint32, []captureOp) {
	parent := c.cur()
	pb := newProtoBuilder(c.builder)
	pb.lineDefined = fn.Location.Begin.Line + 1
	if debugName != "" {
		pb.debugName = c.builder.internString(debugName)
	}
	pb.numParams = uint8(len(fn.Args))
	if fn.Self != nil {
		pb.numParams++
	}
	pb.isVararg = fn.Vararg

	fc := &funcCtx{
		pb:       pb,
		locals:   make(map[*ast.Local]uint8),
		isVararg: fn.Vararg,
		node:     fn,
	}
	c.pushFunc(fc)

	// Bind self if present.
	if fn.Self != nil {
		reg := pb.reserveReg(1)
		fc.locals[fn.Self] = reg
		fc.localStack = append(fc.localStack, fn.Self)
		fc.localScopeAt = append(fc.localScopeAt, 0)
	}
	for _, a := range fn.Args {
		reg := pb.reserveReg(1)
		fc.locals[a] = reg
		fc.localStack = append(fc.localStack, a)
		fc.localScopeAt = append(fc.localScopeAt, 0)
	}

	if fn.Vararg {
		pb.emitABC(common.OpPrepVarargs, pb.numParams, 0, 0)
	}

	c.compileBlock(fn.Body)
	if !c.endsWithReturn(fn.Body) {
		pb.emitReturn(0, 0)
	}

	captures := make([]captureOp, len(fc.upvals))
	for i, u := range fc.upvals {
		captures[i] = captureOp{kind: u.kind, index: u.index}
	}

	c.popFunc()
	c.builder.module.Protos = append(c.builder.module.Protos, pb.build(uint8(len(fc.upvals)), []byte{}))
	protoIdx := uint32(len(c.builder.module.Protos) - 1)
	_ = parent
	return protoIdx, captures
}

type captureOp struct {
	kind  common.CaptureKind
	index uint8
}

func (c *compiler) emitNewClosure(target uint8, protoIdx uint32, captures []captureOp) {
	fc := c.cur()
	// Register the child proto index in our local protos array.
	idx := uint32(0)
	found := false
	for i, p := range fc.pb.childProtos {
		if p == protoIdx {
			idx = uint32(i)
			found = true
			break
		}
	}
	if !found {
		idx = uint32(len(fc.pb.childProtos))
		fc.pb.childProtos = append(fc.pb.childProtos, protoIdx)
	}
	if idx > 32767 {
		c.raise(ast.Location{}, "compiler: too many child protos")
	}
	fc.pb.emitAD(common.OpNewClosure, target, int32(idx))
	for _, cap := range captures {
		fc.pb.emitABC(common.OpCapture, uint8(cap.kind), cap.index, 0)
	}
}

// ----------------------------------------------------------------------
// Expression compilation
// ----------------------------------------------------------------------

// compileExprToReg compiles e and stores its value into target. The
// caller has already reserved target.
func (c *compiler) compileExprToReg(e ast.Expr, target uint8) {
	fc := c.cur()
	switch x := e.(type) {
	case *ast.ExprGroup:
		c.compileExprToReg(x.Expr, target)
	case *ast.ExprConstantNil:
		fc.pb.emitABC(common.OpLoadNil, target, 0, 0)
	case *ast.ExprConstantBool:
		b := uint8(0)
		if x.Value {
			b = 1
		}
		fc.pb.emitABC(common.OpLoadB, target, b, 0)
	case *ast.ExprConstantNumber:
		if i, ok := smallIntLiteral(x.Value); ok {
			fc.pb.emitAD(common.OpLoadN, target, int32(i))
		} else {
			cid := fc.pb.addNumberConstant(x.Value)
			fc.pb.emitLoadK(target, cid)
		}
	case *ast.ExprConstantInteger:
		// Use number constant for compatibility.
		v := float64(x.Value)
		if i, ok := smallIntLiteral(v); ok {
			fc.pb.emitAD(common.OpLoadN, target, int32(i))
		} else {
			cid := fc.pb.addNumberConstant(v)
			fc.pb.emitLoadK(target, cid)
		}
	case *ast.ExprConstantString:
		cid := fc.pb.addStringConstant(string(x.Value))
		fc.pb.emitLoadK(target, cid)
	case *ast.ExprLocal:
		c.compileLocalRead(x, target)
	case *ast.ExprGlobal:
		sidx := fc.pb.addStringConstant(x.Name)
		fc.pb.emitABC(common.OpGetGlobal, target, 0, 0)
		fc.pb.emitAux(sidx)
	case *ast.ExprVarargs:
		// In to-single-reg context, only the first vararg value.
		fc.pb.emitABC(common.OpGetVarargs, target, 2, 0)
	case *ast.ExprCall:
		c.compileCallToReg(x, target)
	case *ast.ExprIndexName:
		c.compileIndexName(x, target)
	case *ast.ExprIndexExpr:
		c.compileIndexExpr(x, target)
	case *ast.ExprFunction:
		protoIdx, captures := c.compileFuncExpr(x, x.DebugName)
		c.emitNewClosure(target, protoIdx, captures)
	case *ast.ExprTable:
		c.compileTable(x, target)
	case *ast.ExprUnary:
		c.compileUnary(x, target)
	case *ast.ExprBinary:
		c.compileBinary(x, target)
	case *ast.ExprIfElse:
		c.compileIfElseExpr(x, target)
	case *ast.ExprTypeAssertion:
		c.compileExprToReg(x.Expr, target)
	case *ast.ExprInterpString:
		c.compileInterpString(x, target)
	default:
		c.raise(e.Loc(), "compiler: unsupported expression %T", e)
	}
}

func (c *compiler) compileLocalRead(x *ast.ExprLocal, target uint8) {
	fc := c.cur()
	if reg, ok := c.resolveLocalReg(x.Local); ok && !x.Upvalue {
		if reg != target {
			fc.pb.emitABC(common.OpMove, target, reg, 0)
		}
		return
	}
	uid := c.resolveUpvalue(x.Local)
	fc.pb.emitABC(common.OpGetUpval, target, uid, 0)
}

// resolveLocalReg returns the register of l if it is bound in the
// current function. Returns (_, false) if l is captured from an outer
// function (caller should use resolveUpvalue).
func (c *compiler) resolveLocalReg(l *ast.Local) (uint8, bool) {
	fc := c.cur()
	r, ok := fc.locals[l]
	return r, ok
}

// resolveUpvalue ensures the current function (and every intermediate
// function) has captured l, and returns the upvalue slot in the current
// function. Walks the function stack to find l.
func (c *compiler) resolveUpvalue(l *ast.Local) uint8 {
	// Locate the function that owns l.
	owner := -1
	for i := len(c.funcStack) - 1; i >= 0; i-- {
		if _, ok := c.funcStack[i].locals[l]; ok {
			owner = i
			break
		}
	}
	if owner == -1 {
		c.raise(l.NameLoc, "compiler: unresolved local %q", l.Name)
	}
	// Add the local as a capture in funcStack[owner+1], then propagate
	// upvals through intermediate functions, finally returning the slot
	// in the current function.
	prevIdx := uint8(0)
	for depth := owner + 1; depth <= len(c.funcStack)-1; depth++ {
		fc := c.funcStack[depth]
		// Look for an existing slot.
		found := -1
		for j, u := range fc.upvals {
			if u.owner == l {
				found = j
				break
			}
		}
		if found >= 0 {
			prevIdx = uint8(found)
			continue
		}
		var slot upvalSlot
		if depth == owner+1 {
			// Capture directly from the owner's locals.
			reg := c.funcStack[owner].locals[l]
			slot = upvalSlot{kind: common.CaptureRef, index: reg, owner: l}
		} else {
			slot = upvalSlot{kind: common.CaptureUpval, index: prevIdx, owner: l}
		}
		fc.upvals = append(fc.upvals, slot)
		prevIdx = uint8(len(fc.upvals) - 1)
	}
	return prevIdx
}

func (c *compiler) compileIndexName(x *ast.ExprIndexName, target uint8) {
	fc := c.cur()
	// obj[name]: emit GETTABLEKS.
	objReg := c.compileExprAsReg(x.Expr, false)
	sidx := fc.pb.addStringConstant(x.IndexName)
	fc.pb.emitABC(common.OpGetTableKS, target, objReg, 0)
	fc.pb.emitAux(sidx)
	c.releaseTempIfTop(x.Expr, objReg)
}

func (c *compiler) compileIndexExpr(x *ast.ExprIndexExpr, target uint8) {
	fc := c.cur()
	// Specialize for small integer literal keys.
	if nlit, ok := x.Index.(*ast.ExprConstantNumber); ok {
		if i, isInt := smallIndexLiteral(nlit.Value); isInt {
			objReg := c.compileExprAsReg(x.Expr, false)
			fc.pb.emitABC(common.OpGetTableN, target, objReg, uint8(i))
			c.releaseTempIfTop(x.Expr, objReg)
			return
		}
	}
	// Specialize for string literal keys.
	if slit, ok := x.Index.(*ast.ExprConstantString); ok {
		objReg := c.compileExprAsReg(x.Expr, false)
		sidx := fc.pb.addStringConstant(string(slit.Value))
		fc.pb.emitABC(common.OpGetTableKS, target, objReg, 0)
		fc.pb.emitAux(sidx)
		c.releaseTempIfTop(x.Expr, objReg)
		return
	}
	objReg := c.compileExprAsReg(x.Expr, false)
	keyReg := c.compileExprAsReg(x.Index, false)
	fc.pb.emitABC(common.OpGetTable, target, objReg, keyReg)
	c.releaseTempIfTop(x.Index, keyReg)
	c.releaseTempIfTop(x.Expr, objReg)
}

// compileExprAsReg compiles e and returns the register holding its
// value. If e is a local read with no side effects, returns the local's
// register directly (no copy). Otherwise allocates a fresh temp at the
// top of the stack. The caller should call releaseTempIfTop when done.
func (c *compiler) compileExprAsReg(e ast.Expr, _ bool) uint8 {
	fc := c.cur()
	if lx, ok := e.(*ast.ExprLocal); ok && !lx.Upvalue {
		if r, ok2 := c.resolveLocalReg(lx.Local); ok2 {
			return r
		}
	}
	if g, ok := e.(*ast.ExprGroup); ok {
		return c.compileExprAsReg(g.Expr, false)
	}
	tmp := fc.pb.reserveReg(1)
	c.compileExprToReg(e, tmp)
	return tmp
}

// releaseTempIfTop frees the register r if it was a temporary at the
// top of the register stack.
func (c *compiler) releaseTempIfTop(e ast.Expr, r uint8) {
	fc := c.cur()
	// Only free if r was the top-most allocated register and was
	// not bound as a local.
	if r+1 != fc.pb.top {
		return
	}
	// If e is a local, we did not allocate a new temp.
	if lx, ok := e.(*ast.ExprLocal); ok && !lx.Upvalue {
		if _, ok2 := c.resolveLocalReg(lx.Local); ok2 {
			return
		}
	}
	if _, ok := e.(*ast.ExprGroup); ok {
		// Treat like a temp anyway; conservatively free.
	}
	fc.pb.setTop(r)
}

func (c *compiler) compileTable(x *ast.ExprTable, target uint8) {
	fc := c.cur()
	// Count array entries and record entries.
	arrayN := 0
	hashN := 0
	for _, it := range x.Items {
		if it.Kind == ast.TableItemList {
			arrayN++
		} else {
			hashN++
		}
	}

	// Detect pure record table with string keys → DUPTABLE.
	allStringKeys := hashN > 0 && arrayN == 0
	if allStringKeys {
		keys := make([]uint32, 0, len(x.Items))
		for _, it := range x.Items {
			if it.Kind != ast.TableItemRecord {
				allStringKeys = false
				break
			}
			// Record uses an implicit Name; the Key field should be a
			// ConstantString.
			if cs, ok := it.Key.(*ast.ExprConstantString); ok {
				keys = append(keys, fc.pb.addStringConstant(string(cs.Value)))
			} else {
				allStringKeys = false
				break
			}
		}
		if allStringKeys && len(keys) > 0 {
			tidx := fc.pb.addTableConstant(keys)
			fc.pb.emitAD(common.OpDupTable, target, int32(tidx))
			// Now assign each key.
			for i, it := range x.Items {
				cs := it.Key.(*ast.ExprConstantString)
				vReg := c.compileExprAsReg(it.Value, false)
				sidx := fc.pb.addStringConstant(string(cs.Value))
				_ = keys[i]
				fc.pb.emitABC(common.OpSetTableKS, vReg, target, 0)
				fc.pb.emitAux(sidx)
				c.releaseTempIfTop(it.Value, vReg)
			}
			return
		}
	}

	// NEWTABLE: A=target, B=hash_size_hint, AUX=array_size_hint.
	// B is log2 hash size; we encode it as exact count for simplicity
	// (upstream's hint is a hash mask exponent but VM accepts any).
	hashHint := uint8(0)
	if hashN > 0 {
		// log2(ceil) of hashN.
		hashHint = uint8(ceilLog2(hashN))
	}
	fc.pb.emitABC(common.OpNewTable, target, hashHint, 0)
	fc.pb.emitAux(uint32(arrayN))

	// Set record-style entries.
	for _, it := range x.Items {
		switch it.Kind {
		case ast.TableItemRecord:
			if cs, ok := it.Key.(*ast.ExprConstantString); ok {
				vReg := c.compileExprAsReg(it.Value, false)
				sidx := fc.pb.addStringConstant(string(cs.Value))
				fc.pb.emitABC(common.OpSetTableKS, vReg, target, 0)
				fc.pb.emitAux(sidx)
				c.releaseTempIfTop(it.Value, vReg)
			} else {
				c.raise(it.Value.Loc(), "compiler: record table key must be a string literal")
			}
		case ast.TableItemGeneral:
			kReg := c.compileExprAsReg(it.Key, false)
			vReg := c.compileExprAsReg(it.Value, false)
			fc.pb.emitABC(common.OpSetTable, vReg, target, kReg)
			c.releaseTempIfTop(it.Value, vReg)
			c.releaseTempIfTop(it.Key, kReg)
		}
	}

	// Array-style entries via SETLIST. Emit in chunks of up to 16 to
	// keep register pressure low and stay within SETLIST's 8-bit C field.
	if arrayN > 0 {
		const chunkSize = 16
		base := fc.pb.top
		// Reserve a fixed chunk window.
		chunk := chunkSize
		if chunk > arrayN {
			chunk = arrayN
		}
		fc.pb.reserveReg(chunk)
		current := 0
		arrayIndex := uint32(1)
		multret := false
		for i, it := range x.Items {
			if it.Kind != ast.TableItemList {
				continue
			}
			// Flush chunk if full.
			if current == chunk {
				fc.pb.emitABC(common.OpSetList, target, base, uint8(current+1))
				fc.pb.emitAux(arrayIndex)
				arrayIndex += uint32(current)
				current = 0
			}
			isLastItem := i == len(x.Items)-1
			if isLastItem && c.isMultRetExpr(it.Value) {
				c.compileExprMultRet(it.Value, base+uint8(current))
				current++
				multret = true
				break
			}
			c.compileExprToReg(it.Value, base+uint8(current))
			current++
		}
		if current > 0 {
			if multret {
				fc.pb.emitABC(common.OpSetList, target, base, 0)
			} else {
				fc.pb.emitABC(common.OpSetList, target, base, uint8(current+1))
			}
			fc.pb.emitAux(arrayIndex)
		}
		fc.pb.setTop(base)
	}
}

func ceilLog2(n int) int {
	if n <= 1 {
		return 0
	}
	r := 0
	v := n - 1
	for v > 0 {
		r++
		v >>= 1
	}
	return r
}

func (c *compiler) compileUnary(x *ast.ExprUnary, target uint8) {
	fc := c.cur()
	srcReg := c.compileExprAsReg(x.Operand, false)
	switch x.Op {
	case ast.UnaryMinus:
		fc.pb.emitABC(common.OpMinus, target, srcReg, 0)
	case ast.UnaryNot:
		fc.pb.emitABC(common.OpNot, target, srcReg, 0)
	case ast.UnaryLen:
		fc.pb.emitABC(common.OpLength, target, srcReg, 0)
	}
	c.releaseTempIfTop(x.Operand, srcReg)
}

func (c *compiler) compileBinary(x *ast.ExprBinary, target uint8) {
	fc := c.cur()

	switch x.Op {
	case ast.BinaryAnd, ast.BinaryOr:
		c.compileShortCircuit(x, target)
		return
	case ast.BinaryConcat:
		c.compileConcat(x, target)
		return
	}

	// Comparison: produce a boolean.
	if isCompareOp(x.Op) {
		c.compileCompareToBool(x, target)
		return
	}

	// Arithmetic.
	// Try constant-on-right specialization (ADDK et al).
	if cn, ok := x.Rhs.(*ast.ExprConstantNumber); ok && isArithOp(x.Op) {
		// We need the rhs as a constant K.
		cid := fc.pb.addNumberConstant(cn.Value)
		if cid < 256 {
			lhsReg := c.compileExprAsReg(x.Lhs, false)
			c.emitBinaryOpK(x.Op, target, lhsReg, uint8(cid))
			c.releaseTempIfTop(x.Lhs, lhsReg)
			return
		}
	}
	lhsReg := c.compileExprAsReg(x.Lhs, false)
	rhsReg := c.compileExprAsReg(x.Rhs, false)
	c.emitBinaryOp(x.Op, target, lhsReg, rhsReg)
	c.releaseTempIfTop(x.Rhs, rhsReg)
	c.releaseTempIfTop(x.Lhs, lhsReg)
}

func isArithOp(op ast.BinaryOp) bool {
	switch op {
	case ast.BinaryAdd, ast.BinarySub, ast.BinaryMul, ast.BinaryDiv,
		ast.BinaryMod, ast.BinaryPow, ast.BinaryFloorDiv:
		return true
	}
	return false
}

func isCompareOp(op ast.BinaryOp) bool {
	switch op {
	case ast.BinaryEq, ast.BinaryNotEq, ast.BinaryLt, ast.BinaryLe, ast.BinaryGt, ast.BinaryGe:
		return true
	}
	return false
}

func (c *compiler) emitBinaryOp(op ast.BinaryOp, target, b, cc uint8) {
	fc := c.cur()
	var oc common.Opcode
	switch op {
	case ast.BinaryAdd:
		oc = common.OpAdd
	case ast.BinarySub:
		oc = common.OpSub
	case ast.BinaryMul:
		oc = common.OpMul
	case ast.BinaryDiv:
		oc = common.OpDiv
	case ast.BinaryMod:
		oc = common.OpMod
	case ast.BinaryPow:
		oc = common.OpPow
	case ast.BinaryFloorDiv:
		oc = common.OpIdiv
	default:
		c.raise(ast.Location{}, "compiler: unsupported binary op %d", op)
	}
	fc.pb.emitABC(oc, target, b, cc)
}

func (c *compiler) emitBinaryOpK(op ast.BinaryOp, target, b, kIdx uint8) {
	fc := c.cur()
	var oc common.Opcode
	switch op {
	case ast.BinaryAdd:
		oc = common.OpAddK
	case ast.BinarySub:
		oc = common.OpSubK
	case ast.BinaryMul:
		oc = common.OpMulK
	case ast.BinaryDiv:
		oc = common.OpDivK
	case ast.BinaryMod:
		oc = common.OpModK
	case ast.BinaryPow:
		oc = common.OpPowK
	case ast.BinaryFloorDiv:
		oc = common.OpIdivK
	default:
		// Fall back to non-K path.
		c.raise(ast.Location{}, "compiler: emitBinaryOpK with non-arith op")
	}
	fc.pb.emitABC(oc, target, b, kIdx)
}

func (c *compiler) compileShortCircuit(x *ast.ExprBinary, target uint8) {
	fc := c.cur()
	// AND: target = lhs; if target falsy, skip rhs. OR: opposite.
	c.compileExprToReg(x.Lhs, target)
	jumpPC := fc.pb.pc()
	if x.Op == ast.BinaryAnd {
		fc.pb.emitAD(common.OpJumpIfNot, target, 0)
	} else {
		fc.pb.emitAD(common.OpJumpIf, target, 0)
	}
	c.compileExprToReg(x.Rhs, target)
	c.patchJumpTo(jumpPC, fc.pb.pc())
}

func (c *compiler) compileConcat(x *ast.ExprBinary, target uint8) {
	fc := c.cur()
	// Flatten right-associative concat chain.
	exprs := []ast.Expr{x.Lhs}
	cur := x.Rhs
	for {
		if bx, ok := cur.(*ast.ExprBinary); ok && bx.Op == ast.BinaryConcat {
			exprs = append(exprs, bx.Lhs)
			cur = bx.Rhs
			continue
		}
		exprs = append(exprs, cur)
		break
	}
	// CONCAT requires its operands to be in contiguous registers.
	base := fc.pb.top
	for _, e := range exprs {
		r := fc.pb.reserveReg(1)
		c.compileExprToReg(e, r)
	}
	last := base + uint8(len(exprs)-1)
	fc.pb.emitABC(common.OpConcat, target, base, last)
	fc.pb.setTop(base)
}

func (c *compiler) compileCompareToBool(x *ast.ExprBinary, target uint8) {
	fc := c.cur()
	// Pattern: emit conditional jump that lands on LOADB target,1,1 (skip) and LOADB target,0,0 (fall through).
	jpc := c.compileCondCompareJump(x, true /* jump-if-true */)
	// "True" path: load false (since we *didn't* take the jump).
	fc.pb.emitABC(common.OpLoadB, target, 0, 1) // LOADB target, 0, jump+1 (skip the LOADB 1)
	// True landing: load true.
	c.patchJumpTo(jpc, fc.pb.pc())
	fc.pb.emitABC(common.OpLoadB, target, 1, 0)
}

// compileCondCompareJump emits a JUMPIFEQ/LE/LT (or the NOT variant) for
// the comparison x. Returns the PC of the conditional jump for patching.
// If jumpIfTrue is true, the jump is taken when the comparison holds.
func (c *compiler) compileCondCompareJump(x *ast.ExprBinary, jumpIfTrue bool) int {
	fc := c.cur()
	// Normalize Gt/Ge to Lt/Le with swapped operands.
	lhs, rhs := x.Lhs, x.Rhs
	op := x.Op
	switch op {
	case ast.BinaryGt:
		op = ast.BinaryLt
		lhs, rhs = rhs, lhs
	case ast.BinaryGe:
		op = ast.BinaryLe
		lhs, rhs = rhs, lhs
	}

	lhsReg := c.compileExprAsReg(lhs, false)
	rhsReg := c.compileExprAsReg(rhs, false)

	pc := fc.pb.pc()
	var oc common.Opcode
	switch op {
	case ast.BinaryEq:
		if jumpIfTrue {
			oc = common.OpJumpIfEq
		} else {
			oc = common.OpJumpIfNotEq
		}
	case ast.BinaryNotEq:
		if jumpIfTrue {
			oc = common.OpJumpIfNotEq
		} else {
			oc = common.OpJumpIfEq
		}
	case ast.BinaryLt:
		if jumpIfTrue {
			oc = common.OpJumpIfLt
		} else {
			oc = common.OpJumpIfNotLt
		}
	case ast.BinaryLe:
		if jumpIfTrue {
			oc = common.OpJumpIfLe
		} else {
			oc = common.OpJumpIfNotLe
		}
	}
	fc.pb.emitAD(oc, lhsReg, 0)
	fc.pb.emitAux(uint32(rhsReg))
	c.releaseTempIfTop(rhs, rhsReg)
	c.releaseTempIfTop(lhs, lhsReg)
	return pc
}

func (c *compiler) compileIfElseExpr(x *ast.ExprIfElse, target uint8) {
	fc := c.cur()
	jmp := c.compileCondJump(x.Condition, false)
	c.compileExprToReg(x.True, target)
	endJmp := fc.pb.pc()
	fc.pb.emitAD(common.OpJump, 0, 0)
	c.patchJumpTo(jmp, fc.pb.pc())
	c.compileExprToReg(x.False, target)
	c.patchJumpTo(endJmp, fc.pb.pc())
}

func (c *compiler) compileInterpString(x *ast.ExprInterpString, target uint8) {
	// Luau backtick interpolation desugars (per the Luau spec) to
	// string.format using the `%*` "tostring" specifier, so every
	// interpolated expression must be converted via tostring(). Our
	// simpler implementation emits a CONCAT chain, but each non-
	// literal slot is funneled through `tostring(expr)` first so that
	// booleans, numbers, tables, etc. concatenate cleanly. Otherwise
	// `assertEq(\`true = {true}\`, ...)` would fail with
	// "attempt to concatenate a boolean value".
	fc := c.cur()
	base := fc.pb.top
	// We need tostring(); fetch it via GETGLOBAL "tostring" once.
	tostrSidx := fc.pb.addStringConstant("tostring")
	for i := 0; i < len(x.Strings); i++ {
		r := fc.pb.reserveReg(1)
		cid := fc.pb.addStringConstant(string(x.Strings[i]))
		fc.pb.emitLoadK(r, cid)
		if i < len(x.Expressions) {
			// Reserve [r2 = tostring slot, r3 = arg slot]; call
			// tostring(expr) leaving the result at r2.
			r2 := fc.pb.reserveReg(1)
			r3 := fc.pb.reserveReg(1)
			fc.pb.emitABC(common.OpGetGlobal, r2, 0, 0)
			fc.pb.emitAux(tostrSidx)
			c.compileExprToReg(x.Expressions[i], r3)
			fc.pb.emitABC(common.OpCall, r2, 2, 2) // 1 arg, 1 result
			// After CALL, r2 holds the string. r3 still occupies a
			// slot but is dead -- free it.
			fc.pb.setTop(r3)
		}
	}
	count := uint8(fc.pb.top - base)
	if count == 0 {
		fc.pb.emitABC(common.OpLoadNil, target, 0, 0)
		return
	}
	if count == 1 {
		fc.pb.emitABC(common.OpMove, target, base, 0)
		fc.pb.setTop(base)
		return
	}
	fc.pb.emitABC(common.OpConcat, target, base, base+count-1)
	fc.pb.setTop(base)
}

// ----------------------------------------------------------------------
// Calls
// ----------------------------------------------------------------------

func (c *compiler) compileCallToReg(x *ast.ExprCall, target uint8) {
	fc := c.cur()
	// Compile call to top of stack with 1 expected return; copy into target.
	base := fc.pb.top
	c.compileCall(x, base, 1)
	if base != target {
		fc.pb.emitABC(common.OpMove, target, base, 0)
	}
	fc.pb.setTop(base)
}

// compileCall emits a call into base, base+1, ... and expects nresults
// values back. If nresults is -1, multret (B=0 in CALL's C field).
func (c *compiler) compileCall(x *ast.ExprCall, base uint8, nresults int) {
	fc := c.cur()
	// Method call?
	if x.Self {
		// `obj:method(args)`. Upstream's layout:
		//   compile obj into base+1 (so NAMECALL B=base, A=base writes
		//   method into base and self into base+1).
		//   compile args into base+2, base+3, ...
		//   emit NAMECALL A=base B=base immediately before CALL.
		// Note: NAMECALL MUST be the instruction right before CALL,
		// so args must be evaluated and placed first.
		ix, ok := x.Func.(*ast.ExprIndexName)
		if !ok || ix.Op != ':' {
			c.raise(x.Location, "compiler: method call with non-IndexName func")
		}
		fc.pb.setTop(base)
		// Reserve base (method) and base+1 (self).
		_ = fc.pb.reserveReg(2)
		// Compile obj into base+1 (NAMECALL reads from B and writes B+1=A+1=base+1).
		// Actually NAMECALL does R(A) = R(B):K, R(A+1) = R(B). So if we
		// compile obj into base (or any reg) and set B=that, NAMECALL
		// writes A=base=method and A+1=base+1=self. We choose B=base
		// (so NAMECALL R[base]=R[base]:"m"; R[base+1]=R[base]). Compile
		// obj into base.
		c.compileExprToReg(ix.Expr, base)
		// Compile args INTO contiguous regs base+2, base+3, ...
		nargs := 1 // self counts
		multret := false
		for i, arg := range x.Args {
			isLast := i == len(x.Args)-1
			if isLast && c.isMultRetExpr(arg) {
				r := fc.pb.reserveReg(1)
				c.compileExprMultRet(arg, r)
				multret = true
				break
			}
			r := fc.pb.reserveReg(1)
			c.compileExprToReg(arg, r)
			nargs++
		}
		// Emit NAMECALL IMMEDIATELY before CALL.
		sidx := fc.pb.addStringConstant(ix.IndexName)
		fc.pb.emitABC(common.OpNameCall, base, base, 0)
		fc.pb.emitAux(sidx)
		if multret {
			fc.pb.emitABC(common.OpCall, base, 0, uint8(nresults+1))
		} else {
			fc.pb.emitABC(common.OpCall, base, uint8(nargs+1), uint8(nresults+1))
		}
		if nresults >= 0 {
			fc.pb.setTop(base + uint8(nresults))
		}
		return
	}

	// Regular call: compile func into base, then args into base+1, ...
	fc.pb.setTop(base)
	_ = fc.pb.reserveReg(1)
	c.compileExprToReg(x.Func, base)
	nargs := 0
	for i, arg := range x.Args {
		isLast := i == len(x.Args)-1
		if isLast && c.isMultRetExpr(arg) {
			r := fc.pb.reserveReg(1)
			c.compileExprMultRet(arg, r)
			fc.pb.emitABC(common.OpCall, base, 0, uint8(nresults+1))
			fc.pb.setTop(base + uint8(maxInt(nresults, 0)))
			return
		}
		r := fc.pb.reserveReg(1)
		c.compileExprToReg(arg, r)
		nargs++
	}
	fc.pb.emitABC(common.OpCall, base, uint8(nargs+1), uint8(nresults+1))
	fc.pb.setTop(base + uint8(maxInt(nresults, 0)))
}

// compileExprMultRet compiles e into target.. expecting multret. Used
// when the receiver wants all values (e.g. last arg of a call,
// last return value).
func (c *compiler) compileExprMultRet(e ast.Expr, target uint8) {
	fc := c.cur()
	switch x := e.(type) {
	case *ast.ExprCall:
		c.compileCall(x, target, -1)
	case *ast.ExprVarargs:
		fc.pb.emitABC(common.OpGetVarargs, target, 0, 0)
	default:
		c.compileExprToReg(e, target)
	}
}

// compileExprMultRetExact compiles e into target.. expecting exactly
// nvals results.
func (c *compiler) compileExprMultRetExact(e ast.Expr, target uint8, nvals int) {
	fc := c.cur()
	switch x := e.(type) {
	case *ast.ExprCall:
		c.compileCall(x, target, nvals)
	case *ast.ExprVarargs:
		fc.pb.emitABC(common.OpGetVarargs, target, uint8(nvals+1), 0)
	default:
		c.compileExprToReg(e, target)
		// pad with nil
		for i := 1; i < nvals; i++ {
			fc.pb.emitABC(common.OpLoadNil, target+uint8(i), 0, 0)
		}
	}
}

// ----------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------

// smallIntLiteral returns (i, true) if v is an integer in int16 range
// (i.e. fits in LOADN's D field).
func smallIntLiteral(v float64) (int16, bool) {
	if math.Floor(v) != v {
		return 0, false
	}
	if v < math.MinInt16 || v > math.MaxInt16 {
		return 0, false
	}
	return int16(v), true
}

// smallIndexLiteral returns (i, true) if v is an integer in [1, 256]
// (fits in GETTABLEN/SETTABLEN's C field which is C+1 indexed).
func smallIndexLiteral(v float64) (uint8, bool) {
	if math.Floor(v) != v {
		return 0, false
	}
	if v < 1 || v > 256 {
		return 0, false
	}
	return uint8(v - 1), true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
