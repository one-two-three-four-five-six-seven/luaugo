// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"github.com/one-two-three-four-five-six-seven/luaugo/internal/common"
	"github.com/one-two-three-four-five-six-seven/luaugo/internal/vmlog"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"
)

// execute.go: the main bytecode interpreter dispatch loop. Mirrors
// the structure of upstream lvmexecute.cpp luau_execute. Every opcode
// is handled in a `switch` arm; PC is advanced explicitly per case.
//
// Conventions:
//   - `code` is ci.cl.proto.Code, captured once per frame.
//   - `pc` is the integer index into code (NOT a pointer like upstream).
//   - `base` is ci.base, the index of R(0) in the thread stack.
//   - register access: rA, rB, rC return *value pointers; sR(A, v) stores.

// reframeStack re-extends L.stack so that the current Lua frame still
// has room for its declared register window after a callee shrank the
// backing slice. Call this immediately after any L.stack = L.stack[:L.top]
// inside an executing Lua frame; without it the next register access
// can panic with "index out of range".
//
// The function preserves the existing slice contents and never lowers
// L.top -- it only grows the slice up to base+MaxStackSize.
func reframeStack(L *stateImpl, base int, maxStack uint8) {
	needLen := base + int(maxStack)
	if len(L.stack) >= needLen {
		return
	}
	if cap(L.stack) >= needLen {
		L.stack = L.stack[:needLen]
		return
	}
	grow := make([]value, needLen, needLen+(needLen/2))
	copy(grow, L.stack)
	L.stack = grow
}

// executeProto runs the bytecode of ci until the function returns,
// yields, or raises an error. On normal return the results are placed
// per the calling convention (see RETURN below).
func executeProto(L *stateImpl, ci *callInfo) {
	for {
		// Re-fetch the live frame state on every reentry (after a
		// nested call to a Go function, the stack may have been
		// reallocated and slot pointers are invalid).
		cl := ci.cl
		p := cl.proto
		code := p.Code
		pc := ci.savedpc
		base := ci.base
		constants := getProtoCache(L.gs).constants[p]
		// On every frame entry / re-entry restore this frame's register
		// window. The previous frame may have shrunk L.stack to its own
		// L.top during its return; without this we'd panic on the next
		// register write into the current frame.
		reframeStack(L, base, p.MaxStackSize)

		// Inner dispatch loop.
	dispatch:
		for {
			if pc < 0 || pc >= len(code) {
				// fell off the end: implicit RETURN 0,0
				ci.savedpc = pc
				if returnFromFrame(L, ci, base, 0) {
					return
				}
				ci = L.currentFrame()
				break dispatch
			}
			insn := code[pc]
			pc++
			op := common.InsnOp(insn)
			a := common.InsnA(insn)

			switch op {
			case common.OpNop:
				// nothing

			case common.OpBreak:
				// Debugger break — for now equivalent to NOP.

			case common.OpLoadNil:
				L.stack[base+int(a)] = nilValue()

			case common.OpLoadB:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				L.stack[base+int(a)] = booleanValue(b != 0)
				pc += int(c)

			case common.OpLoadN:
				d := common.InsnD(insn)
				L.stack[base+int(a)] = numberValue(float64(d))

			case common.OpLoadK:
				d := common.InsnD(insn)
				L.stack[base+int(a)] = constants[d]

			case common.OpLoadKX:
				aux := code[pc]
				pc++
				L.stack[base+int(a)] = constants[aux]

			case common.OpMove:
				b := common.InsnB(insn)
				L.stack[base+int(a)] = L.stack[base+int(b)]

			case common.OpGetGlobal:
				aux := code[pc]
				pc++
				kv := constants[aux]
				if kv.tag != TString {
					L.runtimeError("GETGLOBAL key is not a string")
				}
				v, _ := cl.env.getStr(kv.gc.(*tString))
				L.stack[base+int(a)] = v

			case common.OpSetGlobal:
				aux := code[pc]
				pc++
				kv := constants[aux]
				if kv.tag != TString {
					L.runtimeError("SETGLOBAL key is not a string")
				}
				cl.env.setStr(L.gs, kv.gc.(*tString), L.stack[base+int(a)])

			case common.OpGetUpval:
				b := common.InsnB(insn)
				u := cl.upvalRefs[b]
				if u == nil {
					L.stack[base+int(a)] = nilValue()
				} else {
					L.stack[base+int(a)] = u.read()
				}

			case common.OpSetUpval:
				b := common.InsnB(insn)
				u := cl.upvalRefs[b]
				if u != nil {
					u.write(L.gs, L.stack[base+int(a)])
				}

			case common.OpCloseUpvals:
				L.closeUpvalsTo(base + int(a))

			case common.OpGetImport:
				d := common.InsnD(insn)
				_ = code[pc] // AUX is packed import id (also stored as light-userdata in K(D))
				aux := code[pc]
				pc++
				// We use AUX (packed) as authoritative.
				L.stack[base+int(a)] = getImport(L, cl.env, aux, constants)
				_ = d

			case common.OpGetTable:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				ci.savedpc = pc
				ci.top = L.top
				v := indexValue(L, L.stack[base+int(b)], L.stack[base+int(c)])
				L.stack[base+int(a)] = v

			case common.OpSetTable:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				ci.savedpc = pc
				ci.top = L.top
				newIndexValue(L, L.stack[base+int(b)], L.stack[base+int(c)], L.stack[base+int(a)])

			case common.OpGetTableKS:
				b := common.InsnB(insn)
				aux := code[pc]
				pc++
				kv := constants[aux]
				ci.savedpc = pc
				ci.top = L.top
				L.stack[base+int(a)] = indexValue(L, L.stack[base+int(b)], kv)

			case common.OpSetTableKS:
				b := common.InsnB(insn)
				aux := code[pc]
				pc++
				kv := constants[aux]
				ci.savedpc = pc
				ci.top = L.top
				newIndexValue(L, L.stack[base+int(b)], kv, L.stack[base+int(a)])

			case common.OpGetTableN:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				ci.savedpc = pc
				ci.top = L.top
				L.stack[base+int(a)] = indexValue(L, L.stack[base+int(b)], numberValue(float64(c)+1))

			case common.OpSetTableN:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				ci.savedpc = pc
				ci.top = L.top
				newIndexValue(L, L.stack[base+int(b)], numberValue(float64(c)+1), L.stack[base+int(a)])

			case common.OpNewClosure:
				d := common.InsnD(insn)
				if int(d) >= len(p.Protos) {
					L.runtimeError("NEWCLOSURE: invalid proto index")
				}
				childIdx := p.Protos[d]
				// Find the parent module from cl.proto via the cache.
				// We trust that constants are shared; protos are
				// accessed through the chain. Look up child in the
				// proto cache to find its module: simpler is to keep
				// a parent-module map. We use cl.env as env.
				module := findModuleForProto(L.gs, p)
				if module == nil {
					L.runtimeError("NEWCLOSURE: parent module not found")
				}
				if int(childIdx) >= len(module.Protos) {
					L.runtimeError("NEWCLOSURE: child proto index out of range")
				}
				child := module.Protos[childIdx]
				nc := newLClosure(L.gs, cl.env, child, int(child.NumUpvalues))
				// Consume the following CAPTURE instructions.
				for i := 0; i < int(child.NumUpvalues); i++ {
					capInsn := code[pc]
					pc++
					capOp := common.InsnOp(capInsn)
					if capOp != common.OpCapture {
						L.runtimeError("NEWCLOSURE: expected CAPTURE")
					}
					kind := common.CaptureKind(common.InsnA(capInsn))
					src := common.InsnB(capInsn)
					switch kind {
					case common.CaptureVal:
						uv := newUpVal(L.gs, L, -1)
						uv.closed = true
						uv.value = L.stack[base+int(src)]
						uv.owner = nil
						nc.upvalRefs[i] = uv
					case common.CaptureRef:
						nc.upvalRefs[i] = findOrCreateUpval(L, base+int(src))
					case common.CaptureUpval:
						nc.upvalRefs[i] = cl.upvalRefs[src]
					}
				}
				L.stack[base+int(a)] = closureValue(nc)
				// Safepoint: NEWCLOSURE allocates; nudge GC.
				L.gs.gcStep(0)

			case common.OpDupClosure:
				d := common.InsnD(insn)
				kv := constants[d]
				if kv.tag != TLightUserdata {
					L.runtimeError("DUPCLOSURE: invalid constant")
				}
				tag, ok := kv.ptr.(closureTag)
				if !ok {
					L.runtimeError("DUPCLOSURE: invalid closure tag")
				}
				module := findModuleForProto(L.gs, p)
				if module == nil || int(tag.protoIndex) >= len(module.Protos) {
					L.runtimeError("DUPCLOSURE: invalid proto reference")
				}
				child := module.Protos[tag.protoIndex]
				nc := newLClosure(L.gs, cl.env, child, int(child.NumUpvalues))
				// Following CAPTUREs only if there are upvalues.
				for i := 0; i < int(child.NumUpvalues); i++ {
					capInsn := code[pc]
					pc++
					capOp := common.InsnOp(capInsn)
					if capOp != common.OpCapture {
						L.runtimeError("DUPCLOSURE: expected CAPTURE")
					}
					kind := common.CaptureKind(common.InsnA(capInsn))
					src := common.InsnB(capInsn)
					switch kind {
					case common.CaptureVal:
						uv := newUpVal(L.gs, L, -1)
						uv.closed = true
						uv.value = L.stack[base+int(src)]
						uv.owner = nil
						nc.upvalRefs[i] = uv
					case common.CaptureRef:
						nc.upvalRefs[i] = findOrCreateUpval(L, base+int(src))
					case common.CaptureUpval:
						nc.upvalRefs[i] = cl.upvalRefs[src]
					}
				}
				L.stack[base+int(a)] = closureValue(nc)

			case common.OpNameCall:
				b := common.InsnB(insn)
				aux := code[pc]
				pc++
				kv := constants[aux]
				if kv.tag != TString {
					L.runtimeError("NAMECALL: key is not a string")
				}
				obj := L.stack[base+int(b)]
				// R(A+1) = R(B); R(A) = R(B)[K(AUX)]
				L.stack[base+int(a)+1] = obj
				ci.savedpc = pc
				ci.top = L.top
				L.stack[base+int(a)] = indexValue(L, obj, kv)

			case common.OpCall:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				nargs := int(b) - 1
				nresults := int(c) - 1
				if b == 0 {
					nargs = L.top - (base + int(a)) - 1
				}
				ci.savedpc = pc
				ci.top = L.top
				// Function is at base+a; args at base+a+1..
				funcIdx := base + int(a)
				if vmlog.Enabled("call") {
					vmlog.V("call", "OpCall funcIdx=%d nargs=%d nresults=%d L.top(pre)=%d len(stack)=%d",
						funcIdx, nargs, nresults, L.top, len(L.stack))
				}
				savedTop := ci.top
				L.callValue(funcIdx, nargs, nresults)
				if nresults != MultRet {
					// For fixed nresults, restore the caller's L.top
					// to its pre-call value. This matches upstream's
					// `L->top = ci->top` after `luaD_call`.
					// The actual return slots remain populated at
					// [funcIdx, funcIdx+nresults).
					L.top = savedTop
					if L.top > len(L.stack) {
						L.reserve(L.top - len(L.stack))
					}
					L.stack = L.stack[:L.top]
				}
				// Restore the parent frame's register window: a callee
				// that popped its stack down (Go callees in particular)
				// can leave the slice too short for the next register
				// write inside this Lua frame.
				reframeStack(L, base, p.MaxStackSize)
				if vmlog.Enabled("call") {
					vmlog.V("call", "OpCall post-return L.top=%d len(stack)=%d ra=%v",
						L.top, len(L.stack), L.stack[funcIdx])
				}

			case common.OpReturn:
				b := common.InsnB(insn)
				nresults := int(b) - 1
				if b == 0 {
					nresults = L.top - (base + int(a))
				}
				ci.savedpc = pc
				if returnFromFrame(L, ci, base+int(a), nresults) {
					// Frame stack popped to caller — but if this was
					// the outermost Execute frame, we return.
					return
				}
				// We continue in caller's frame.
				ci = L.currentFrame()
				break dispatch

			case common.OpJump:
				d := common.InsnD(insn)
				pc += int(d)

			case common.OpJumpBack:
				d := common.InsnD(insn)
				pc += int(d)
				// Safepoint.
				L.gs.gcStep(0)

			case common.OpJumpX:
				e := common.InsnE(insn)
				pc += int(e)

			case common.OpJumpIf:
				d := common.InsnD(insn)
				if !L.stack[base+int(a)].isFalse() {
					pc += int(d)
				}

			case common.OpJumpIfNot:
				d := common.InsnD(insn)
				if L.stack[base+int(a)].isFalse() {
					pc += int(d)
				}

			// Note: for the JUMP-IF-* ops, the jump offset D is relative
			// to the insn word (not the aux word). So when we take the
			// jump we must NOT additionally pc++ past the aux; the +D
			// includes the aux skip. When the condition is false we
			// pc++ once to skip the aux word and continue. This
			// matches upstream Luau's LOP_JUMPIF* dispatch.
			case common.OpJumpIfEq:
				d := common.InsnD(insn)
				aux := code[pc]
				if L.equalVal(L.stack[base+int(a)], L.stack[base+int(aux)]) {
					pc += int(d)
				} else {
					pc++
				}

			case common.OpJumpIfNotEq:
				d := common.InsnD(insn)
				aux := code[pc]
				if !L.equalVal(L.stack[base+int(a)], L.stack[base+int(aux)]) {
					pc += int(d)
				} else {
					pc++
				}

			case common.OpJumpIfLt:
				d := common.InsnD(insn)
				aux := code[pc]
				if L.lessThanVal(L.stack[base+int(a)], L.stack[base+int(aux)]) {
					pc += int(d)
				} else {
					pc++
				}

			case common.OpJumpIfNotLt:
				d := common.InsnD(insn)
				aux := code[pc]
				if !L.lessThanVal(L.stack[base+int(a)], L.stack[base+int(aux)]) {
					pc += int(d)
				} else {
					pc++
				}

			case common.OpJumpIfLe:
				d := common.InsnD(insn)
				aux := code[pc]
				if L.lessEqualVal(L.stack[base+int(a)], L.stack[base+int(aux)]) {
					pc += int(d)
				} else {
					pc++
				}

			case common.OpJumpIfNotLe:
				d := common.InsnD(insn)
				aux := code[pc]
				if !L.lessEqualVal(L.stack[base+int(a)], L.stack[base+int(aux)]) {
					pc += int(d)
				} else {
					pc++
				}

			case common.OpJumpXEqKNil:
				d := common.InsnD(insn)
				aux := code[pc]
				eq := L.stack[base+int(a)].tag == TNil
				if common.InsnAuxNot(aux) != 0 {
					eq = !eq
				}
				if eq {
					pc += int(d)
				} else {
					pc++
				}

			case common.OpJumpXEqKB:
				d := common.InsnD(insn)
				aux := code[pc]
				v := L.stack[base+int(a)]
				want := common.InsnAuxKB(aux) != 0
				eq := v.tag == TBoolean && v.bool_ == want
				if common.InsnAuxNot(aux) != 0 {
					eq = !eq
				}
				if eq {
					pc += int(d)
				} else {
					pc++
				}

			case common.OpJumpXEqKN:
				d := common.InsnD(insn)
				aux := code[pc]
				kv := constants[common.InsnAuxKV(aux)]
				v := L.stack[base+int(a)]
				eq := v.tag == TNumber && kv.tag == TNumber && v.num == kv.num
				if common.InsnAuxNot(aux) != 0 {
					eq = !eq
				}
				if eq {
					pc += int(d)
				} else {
					pc++
				}

			case common.OpJumpXEqKS:
				d := common.InsnD(insn)
				aux := code[pc]
				kv := constants[common.InsnAuxKV(aux)]
				v := L.stack[base+int(a)]
				eq := v.tag == TString && kv.tag == TString && v.gc == kv.gc
				if common.InsnAuxNot(aux) != 0 {
					eq = !eq
				}
				if eq {
					pc += int(d)
				} else {
					pc++
				}

			case common.OpAdd, common.OpSub, common.OpMul, common.OpDiv, common.OpMod, common.OpPow, common.OpIdiv:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				tm := opToTM(op)
				L.stack[base+int(a)] = L.doArith(tm, L.stack[base+int(b)], L.stack[base+int(c)])

			case common.OpAddK, common.OpSubK, common.OpMulK, common.OpDivK, common.OpModK, common.OpPowK, common.OpIdivK:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				tm := opToTM(op)
				L.stack[base+int(a)] = L.doArith(tm, L.stack[base+int(b)], constants[c])

			case common.OpSubRK:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				L.stack[base+int(a)] = L.doArith(TMSub, constants[b], L.stack[base+int(c)])

			case common.OpDivRK:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				L.stack[base+int(a)] = L.doArith(TMDiv, constants[b], L.stack[base+int(c)])

			case common.OpAnd:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				vb := L.stack[base+int(b)]
				if vb.isFalse() {
					L.stack[base+int(a)] = vb
				} else {
					L.stack[base+int(a)] = L.stack[base+int(c)]
				}

			case common.OpOr:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				vb := L.stack[base+int(b)]
				if !vb.isFalse() {
					L.stack[base+int(a)] = vb
				} else {
					L.stack[base+int(a)] = L.stack[base+int(c)]
				}

			case common.OpAndK:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				vb := L.stack[base+int(b)]
				if vb.isFalse() {
					L.stack[base+int(a)] = vb
				} else {
					L.stack[base+int(a)] = constants[c]
				}

			case common.OpOrK:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				vb := L.stack[base+int(b)]
				if !vb.isFalse() {
					L.stack[base+int(a)] = vb
				} else {
					L.stack[base+int(a)] = constants[c]
				}

			case common.OpConcat:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				// concatenate R(B)..R(C) inclusive, store at R(A)
				lo := base + int(b)
				hi := base + int(c)
				n := hi - lo + 1
				if vmlog.Enabled("concat") {
					for i := 0; i < n; i++ {
						vmlog.V("concat", "  in[%d] reg=%d tag=%v val=%v", i, b+uint8(i), L.stack[lo+i].tag, L.stack[lo+i])
					}
				}
				L.doConcat(lo, n)
				L.stack[base+int(a)] = L.stack[lo]
				if vmlog.Enabled("concat") {
					vmlog.V("concat", "  result tag=%v val=%v", L.stack[base+int(a)].tag, L.stack[base+int(a)])
				}

			case common.OpNot:
				b := common.InsnB(insn)
				L.stack[base+int(a)] = booleanValue(L.stack[base+int(b)].isFalse())

			case common.OpMinus:
				b := common.InsnB(insn)
				v := L.stack[base+int(b)]
				if v.tag == TNumber {
					L.stack[base+int(a)] = numberValue(-v.num)
				} else if v.tag == TVector {
					L.stack[base+int(a)] = valueFromVector(vectorFromValue(v).Neg())
				} else if n, ok := v.asNumber(); ok {
					L.stack[base+int(a)] = numberValue(-n)
				} else if r, ok := L.callUnaryTM(v, TMUnm); ok {
					L.stack[base+int(a)] = r
				} else {
					L.runtimeError("attempt to perform arithmetic (unm) on a " + v.tag.String() + " value")
				}

			case common.OpLength:
				b := common.InsnB(insn)
				L.stack[base+int(a)] = L.doLen(L.stack[base+int(b)])

			case common.OpNewTable:
				b := common.InsnB(insn)
				aux := code[pc]
				pc++
				narr := int(aux)
				nhash := 0
				if b > 0 {
					nhash = 1 << (b - 1)
				}
				L.stack[base+int(a)] = tableValue(newTable(L.gs, narr, nhash))
				L.gs.gcStep(0)

			case common.OpDupTable:
				d := common.InsnD(insn)
				kv := constants[d]
				t := newTable(L.gs, 0, 0)
				if kv.tag == TLightUserdata {
					switch tag := kv.ptr.(type) {
					case tableTemplateTag:
						// Pre-create slots for each declared key.
						_ = tag
					}
				}
				L.stack[base+int(a)] = tableValue(t)

			case common.OpSetList:
				b := common.InsnB(insn)
				c := common.InsnC(insn)
				aux := code[pc]
				pc++
				count := int(c) - 1
				if c == 0 {
					count = L.top - (base + int(b))
				}
				tv := L.stack[base+int(a)]
				if tv.tag != TTable {
					L.runtimeError("SETLIST: target is not a table")
				}
				t := tv.gc.(*table)
				startIdx := int(aux)
				for i := 0; i < count; i++ {
					t.setNum(L.gs, startIdx+i, L.stack[base+int(b)+i])
				}

			case common.OpForNPrep:
				d := common.InsnD(insn)
				// Layout: R(A)=limit, R(A+1)=step, R(A+2)=index
				limV := L.stack[base+int(a)]
				stepV := L.stack[base+int(a)+1]
				idxV := L.stack[base+int(a)+2]
				limN, ok1 := limV.asNumber()
				stepN, ok2 := stepV.asNumber()
				idxN, ok3 := idxV.asNumber()
				if !ok1 || !ok2 || !ok3 {
					L.runtimeError("'for' initial value must be a number")
				}
				L.stack[base+int(a)] = numberValue(limN)
				L.stack[base+int(a)+1] = numberValue(stepN)
				L.stack[base+int(a)+2] = numberValue(idxN)
				// Skip loop if already complete.
				cont := false
				if stepN > 0 {
					cont = idxN <= limN
				} else {
					cont = idxN >= limN
				}
				if !cont {
					pc += int(d)
				}

			case common.OpForNLoop:
				d := common.InsnD(insn)
				limN := L.stack[base+int(a)].num
				stepN := L.stack[base+int(a)+1].num
				idxN := L.stack[base+int(a)+2].num + stepN
				L.stack[base+int(a)+2] = numberValue(idxN)
				cont := false
				if stepN > 0 {
					cont = idxN <= limN
				} else {
					cont = idxN >= limN
				}
				if cont {
					// R(A+3) = idxN (user-visible "var")
					L.stack[base+int(a)+3] = numberValue(idxN)
					pc += int(d)
					L.gs.gcStep(0)
				}

			case common.OpForGLoop:
				// Upstream layout (lvmexecute.cpp LOP_FORGLOOP):
				//   insn = *pc++; aux = *pc;
				//   ... call ...
				//   pc += (control==nil) ? 1 : LUAU_INSN_D(insn);
				// Note: pc is NOT advanced past aux before the jump.
				// So our offset arithmetic must compute "from aux" not
				// "from the word after aux".
				d := common.InsnD(insn)
				aux := code[pc]
				// nvars = low byte of aux. The sign of aux encodes the
				// ipairs fast-path; we don't take that path here so
				// the count is just the magnitude.
				nvars := int(aux & 0xff)
				_ = aux & 0x80000000 // ipairs fast-path bit (ignored, fallback)
				if vmlog.Enabled("stack") {
					vmlog.V("stack", "OpForGLoop pre A=%d nvars=%d base=%d L.top=%d len(stack)=%d gen=%v",
						a, nvars, base, L.top, len(L.stack), L.stack[base+int(a)].tag)
				}

				// Builtin table iteration fast-path: FORGPREP planted
				// ra=nil, ra+1=table, ra+2=lightuserdata(index). Walk
				// the array then the hash portion. Mirrors upstream
				// LOP_FORGLOOP's "fast-path: builtin table iteration".
				gen := L.stack[base+int(a)]
				if gen.tag == TNil && base+int(a)+1 < len(L.stack) && L.stack[base+int(a)+1].tag == TTable {
					t := L.stack[base+int(a)+1].gc.(*table)
					index := iterPosFrom(L.stack[base+int(a)+2])
					sizearr := len(t.array)
					// Clear extra variables when nvars > 2 (we always
					// write the first two as key/value below).
					for i := 2; i < nvars; i++ {
						L.stack[base+int(a)+3+i] = nilValue()
					}
					// Advance index through array portion.
					emitted := false
					for index < sizearr {
						e := t.array[index]
						if e.tag != TNil {
							L.stack[base+int(a)+2] = value{tag: TLightUserdata, ptr: tableIterPos(index + 1)}
							L.stack[base+int(a)+3] = numberValue(float64(index + 1))
							L.stack[base+int(a)+4] = e
							pc += int(d)
							emitted = true
							break
						}
						index++
					}
					if !emitted {
						// Advance index through hash portion.
						hashStart := sizearr
						for index < hashStart+len(t.nodes) {
							n := &t.nodes[index-hashStart]
							if n.val.tag != TNil {
								L.stack[base+int(a)+2] = value{tag: TLightUserdata, ptr: tableIterPos(index + 1)}
								L.stack[base+int(a)+3] = n.key
								L.stack[base+int(a)+4] = n.val
								pc += int(d)
								emitted = true
								break
							}
							index++
						}
					}
					if !emitted {
						// Loop exit: skip aux.
						pc++
					}
					continue
				}
				// Stage the iterator call ABOVE the loop's register
				// window R(A)..R(A+(2+nvars)). Using L.top directly
				// was unsafe: a loop body's last operation can leave
				// L.top pointing *inside* R(A)..R(A+2), so the
				// subsequent push of [gen, state, control] would
				// clobber the iterator state itself — the bug
				// surfaced as "invalid key to 'next'" once R(A+2) was
				// overwritten with R(A)'s function value.
				//
				// Floor L.top at base+a+3+nvars (one past the user
				// variable window) before pushing so the staged call
				// lives in fresh slots. Results come back at the
				// staging position; we then copy them down into the
				// user-variable slots R(A+3..A+3+nvars-1).
				windowTop := base + int(a) + 3 + nvars
				if windowTop < base+int(a)+3 {
					windowTop = base + int(a) + 3
				}
				if L.top < windowTop {
					if windowTop > len(L.stack) {
						reframeStack(L, base, p.MaxStackSize)
						if windowTop > len(L.stack) {
							// Extend the slice (and underlying array
							// if needed) to cover the new top.
							if windowTop > cap(L.stack) {
								grow := make([]value, windowTop, windowTop+windowTop/2+16)
								copy(grow, L.stack)
								L.stack = grow
							} else {
								L.stack = L.stack[:windowTop]
							}
						}
					} else {
						L.stack = L.stack[:windowTop]
					}
					L.top = windowTop
				}
				saveTop := L.top
				L.push(L.stack[base+int(a)])
				L.push(L.stack[base+int(a)+1])
				L.push(L.stack[base+int(a)+2])
				ci.savedpc = pc
				ci.top = L.top
				L.callValue(saveTop, 2, nvars)
				// Restore parent frame's register window BEFORE
				// copying results: the call may have shrunk L.stack
				// to L.top (which sits at saveTop+nvars), making
				// R(A+3+i) at the upper end of the loop's register
				// window unaddressable. Without this, writing the
				// results panics with "index out of range" on the
				// first iteration that has many loop variables (see
				// tables.luau's `for i,v in pairs(a) do` over the
				// 9000-entry table).
				reframeStack(L, base, p.MaxStackSize)
				// Copy results into R(A+3..A+3+nvars-1)
				for i := 0; i < nvars; i++ {
					L.stack[base+int(a)+3+i] = L.stack[saveTop+i]
				}
				L.top = saveTop
				L.stack = L.stack[:saveTop]
				reframeStack(L, base, p.MaxStackSize)
				control := L.stack[base+int(a)+3]
				if vmlog.Enabled("stack") {
					vmlog.V("stack", "OpForGLoop post-call L.top=%d len(stack)=%d control=%v jumpback=%v",
						L.top, len(L.stack), control, control.tag != TNil)
				}
				if control.tag != TNil {
					// Continue: copy first return into control register
					// and jump back to the loop body. The D offset is
					// measured from the AUX word, so we add D directly
					// (pc is currently AT aux, not past it).
					L.stack[base+int(a)+2] = control
					pc += int(d)
				} else {
					// Loop exit: skip the AUX word.
					pc++
				}

			case common.OpForGPrep, common.OpForGPrepInext, common.OpForGPrepNext:
				d := common.InsnD(insn)
				// Generic for prep: jumps forward to the FORGLOOP.
				//
				// Upstream LOP_FORGPREP behaviour (lvmexecute.cpp):
				//   - R(A) is a function: leave as-is; FORGLOOP calls
				//     it as gen(state, control).
				//   - R(A) has __iter metamethod: invoke __iter(R(A))
				//     and overwrite (R(A), R(A+1), R(A+2)) with the
				//     three returned values.
				//   - R(A) has __call: leave as-is; FORGLOOP will call
				//     the table/userdata via __call.
				//   - R(A) is a plain table: set up the builtin table
				//     iteration: R(A)=nil, R(A+1)=table, R(A+2)=
				//     lightuserdata-holding-index-0. FORGLOOP detects
				//     ra=nil && ra+1=table and walks array+hash.
				//   - Otherwise: error.
				//
				// LOP_FORGPREP_INEXT / FORGPREP_NEXT (the ipairs/next
				// fast-paths) only validate the function form here;
				// FORGLOOP handles their builtin paths separately.
				ra := L.stack[base+int(a)]
				if ra.tag != TFunction {
					switch op {
					case common.OpForGPrepInext, common.OpForGPrepNext:
						// Builtin variants: only function generators
						// are valid here. (The table fast-path is gated
						// on safeenv at runtime, which we conservatively
						// treat as "off" — so anything non-function
						// must error before FORGLOOP.)
						if ra.tag != TTable && ra.tag != TUserdata {
							L.runtimeError("attempt to iterate over a " + ra.tag.String() + " value")
						}
					case common.OpForGPrep:
						if ra.tag == TTable || ra.tag == TUserdata {
							// Check for __iter / __call before falling
							// back to the builtin table iteration.
							iterMM := L.gs.getTagMethodForValue(ra, TMIter)
							callMM := L.gs.getTagMethodForValue(ra, TMCall)
							if iterMM.tag != TNil {
								// Invoke __iter(ra) and place the three
								// results into R(A), R(A+1), R(A+2).
								// Mirror upstream lvmexecute.cpp
								// LOP_FORGPREP: setobj2s(ra+1, ra);
								// setobj2s(ra, fn); call(ra, 3).
								base2 := base + int(a)
								// Need at least 3 result slots and an
								// extra one above (for the upcoming
								// FORGLOOP staging area).
								if need := base2 + 3; need > len(L.stack) {
									reframeStack(L, base, p.MaxStackSize)
									if need > len(L.stack) {
										L.reserve(need - L.top)
									}
								}
								L.stack[base2+1] = ra
								L.stack[base2] = iterMM
								savedTop := L.top
								L.top = base2 + 2
								if L.top > len(L.stack) {
									L.reserve(L.top - len(L.stack))
								} else {
									L.stack = L.stack[:L.top]
								}
								ci.savedpc = pc
								ci.top = L.top
								// Call __iter(self): 1 arg, 3 results
								// (iter, state, control).
								L.callValue(base2, 1, 3)
								reframeStack(L, base, p.MaxStackSize)
								L.top = savedTop
								if L.top > len(L.stack) {
									L.reserve(L.top - len(L.stack))
								} else {
									L.stack = L.stack[:L.top]
								}
								// Upstream guards: if __iter returned
								// nil at R(A) the FORGLOOP would try
								// to call a nil value with a confusing
								// stack; defer to that message rather
								// than synthesising a new one so the
								// conformance fixtures' pcall error
								// strings stay aligned.
								_ = L.stack[base2]
							} else if callMM.tag != TNil {
								// Leave R(A..A+2) as-is; FORGLOOP will
								// invoke __call on each iteration.
							} else if ra.tag == TTable {
								// Plain table: set up builtin iteration.
								// R(A) = nil, R(A+1) = the table,
								// R(A+2) = lightuserdata(index=0).
								base2 := base + int(a)
								L.stack[base2+1] = ra
								L.stack[base2] = nilValue()
								L.stack[base2+2] = value{tag: TLightUserdata, ptr: tableIterPos(0)}
							} else {
								// Userdata with no __iter and no __call
								// is not iterable.
								L.runtimeError("attempt to iterate over a " + ra.tag.String() + " value")
							}
						} else {
							L.runtimeError("attempt to iterate over a " + ra.tag.String() + " value")
						}
					}
				}
				pc += int(d)

			case common.OpGetVarargs:
				b := common.InsnB(insn)
				want := int(b) - 1
				varargs := ci.numVararg
				if want == -1 {
					want = varargs
					L.top = base + int(a) + want
					if L.top > len(L.stack) {
						L.reserve(L.top - len(L.stack))
					}
					L.stack = L.stack[:L.top]
				}
				reframeStack(L, base, p.MaxStackSize)
				for i := 0; i < want; i++ {
					if i < varargs {
						L.stack[base+int(a)+i] = L.stack[ci.varargBase+i]
					} else {
						if base+int(a)+i >= len(L.stack) {
							L.reserve(base + int(a) + i + 1 - L.top)
						}
						L.stack[base+int(a)+i] = nilValue()
					}
				}

			case common.OpPrepVarargs:
				// Already handled by callLua.

			case common.OpFastCall:
				// FASTCALL form: A=builtin id, C=skip-count to the
				// following CALL. Args/results are taken from the
				// CALL's registers.
				bfid := common.Builtin(a)
				skip := int(common.InsnC(insn))
				if pc+skip >= len(code) {
					L.runtimeError("FASTCALL: skip past end of code")
				}
				call := code[pc+skip]
				if common.InsnOp(call) != common.OpCall {
					L.runtimeError("FASTCALL: following insn is not CALL")
				}
				callA := common.InsnA(call)
				callB := common.InsnB(call)
				callC := common.InsnC(call)
				ra := base + int(callA)
				nparams := int(callB) - 1
				nresults := int(callC) - 1
				if callB == 0 {
					nparams = L.top - ra - 1
				}
				// Only the builtin's "safeenv" path; we treat all
				// envs as safe here (sandbox enforcement is on writes,
				// not reads, and the global table is shared).
				if nparams >= 1 && cl.env.safeenv {
					arg0 := L.stack[ra+1]
					// Gather additional args from ra+2..ra+nparams.
					var argsBuf [8]value
					var args []value
					if nparams-1 > 0 {
						if nparams-1 <= len(argsBuf) {
							args = argsBuf[:nparams-1]
						} else {
							args = make([]value, nparams-1)
						}
						for i := 0; i < nparams-1; i++ {
							args[i] = L.stack[ra+2+i]
						}
					}
					ci.savedpc = pc
					if n, ok := dispatchFastcall(L, bfid, ra, arg0, args, nresults, nparams); ok {
						// Adjust L.top for MULTRET; otherwise leave
						// it to the CALL convention.
						if nresults == MultRet {
							L.top = ra + n
							if L.top > len(L.stack) {
								L.reserve(L.top - len(L.stack))
							}
							L.stack = L.stack[:L.top]
						} else {
							// Pad results with nil up to nresults.
							for i := n; i < nresults; i++ {
								if ra+i >= len(L.stack) {
									L.reserve(ra + i + 1 - L.top)
								}
								L.stack[ra+i] = nilValue()
							}
						}
						// Restore parent's register window after the
						// fast path may have shrunk the slice.
						reframeStack(L, base, p.MaxStackSize)
						// Skip past the CALL.
						pc += skip + 1
						continue
					}
				}
				// Fall back: let the next CALL handle it.

			case common.OpFastCall1:
				bfid := common.Builtin(a)
				b := common.InsnB(insn)
				skip := int(common.InsnC(insn))
				if pc+skip >= len(code) {
					L.runtimeError("FASTCALL1: skip past end of code")
				}
				call := code[pc+skip]
				if common.InsnOp(call) != common.OpCall {
					L.runtimeError("FASTCALL1: following insn is not CALL")
				}
				callA := common.InsnA(call)
				callC := common.InsnC(call)
				ra := base + int(callA)
				nresults := int(callC) - 1
				if cl.env.safeenv {
					arg0 := L.stack[base+int(b)]
					ci.savedpc = pc
					if n, ok := dispatchFastcall(L, bfid, ra, arg0, nil, nresults, 1); ok {
						if nresults == MultRet {
							L.top = ra + n
							if L.top > len(L.stack) {
								L.reserve(L.top - len(L.stack))
							}
							L.stack = L.stack[:L.top]
						} else {
							for i := n; i < nresults; i++ {
								if ra+i >= len(L.stack) {
									L.reserve(ra + i + 1 - L.top)
								}
								L.stack[ra+i] = nilValue()
							}
						}
						pc += skip + 1
						continue
					}
				}

			case common.OpFastCall2:
				bfid := common.Builtin(a)
				b := common.InsnB(insn)
				skip := int(common.InsnC(insn)) - 1
				aux := code[pc]
				pc++
				if pc+skip >= len(code) {
					L.runtimeError("FASTCALL2: skip past end of code")
				}
				call := code[pc+skip]
				if common.InsnOp(call) != common.OpCall {
					L.runtimeError("FASTCALL2: following insn is not CALL")
				}
				callA := common.InsnA(call)
				callC := common.InsnC(call)
				ra := base + int(callA)
				nresults := int(callC) - 1
				if cl.env.safeenv {
					arg0 := L.stack[base+int(b)]
					args := [...]value{L.stack[base+int(aux&0xff)]}
					ci.savedpc = pc
					if n, ok := dispatchFastcall(L, bfid, ra, arg0, args[:], nresults, 2); ok {
						if nresults == MultRet {
							L.top = ra + n
							if L.top > len(L.stack) {
								L.reserve(L.top - len(L.stack))
							}
							L.stack = L.stack[:L.top]
						} else {
							for i := n; i < nresults; i++ {
								if ra+i >= len(L.stack) {
									L.reserve(ra + i + 1 - L.top)
								}
								L.stack[ra+i] = nilValue()
							}
						}
						pc += skip + 1
						continue
					}
				}

			case common.OpFastCall2K:
				bfid := common.Builtin(a)
				b := common.InsnB(insn)
				skip := int(common.InsnC(insn)) - 1
				aux := code[pc]
				pc++
				if pc+skip >= len(code) {
					L.runtimeError("FASTCALL2K: skip past end of code")
				}
				call := code[pc+skip]
				if common.InsnOp(call) != common.OpCall {
					L.runtimeError("FASTCALL2K: following insn is not CALL")
				}
				callA := common.InsnA(call)
				callC := common.InsnC(call)
				ra := base + int(callA)
				nresults := int(callC) - 1
				if cl.env.safeenv {
					if int(aux) >= len(constants) {
						L.runtimeError("FASTCALL2K: constant index out of range")
					}
					arg0 := L.stack[base+int(b)]
					args := [...]value{constants[aux]}
					ci.savedpc = pc
					if n, ok := dispatchFastcall(L, bfid, ra, arg0, args[:], nresults, 2); ok {
						if nresults == MultRet {
							L.top = ra + n
							if L.top > len(L.stack) {
								L.reserve(L.top - len(L.stack))
							}
							L.stack = L.stack[:L.top]
						} else {
							for i := n; i < nresults; i++ {
								if ra+i >= len(L.stack) {
									L.reserve(ra + i + 1 - L.top)
								}
								L.stack[ra+i] = nilValue()
							}
						}
						pc += skip + 1
						continue
					}
				}

			case common.OpFastCall3:
				bfid := common.Builtin(a)
				b := common.InsnB(insn)
				skip := int(common.InsnC(insn)) - 1
				aux := code[pc]
				pc++
				if pc+skip >= len(code) {
					L.runtimeError("FASTCALL3: skip past end of code")
				}
				call := code[pc+skip]
				if common.InsnOp(call) != common.OpCall {
					L.runtimeError("FASTCALL3: following insn is not CALL")
				}
				callA := common.InsnA(call)
				callC := common.InsnC(call)
				ra := base + int(callA)
				nresults := int(callC) - 1
				if cl.env.safeenv {
					arg0 := L.stack[base+int(b)]
					argA := common.InsnAuxA(aux)
					argB := common.InsnAuxB(aux)
					args := [...]value{
						L.stack[base+int(argA)],
						L.stack[base+int(argB)],
					}
					ci.savedpc = pc
					if n, ok := dispatchFastcall(L, bfid, ra, arg0, args[:], nresults, 3); ok {
						if nresults == MultRet {
							L.top = ra + n
							if L.top > len(L.stack) {
								L.reserve(L.top - len(L.stack))
							}
							L.stack = L.stack[:L.top]
						} else {
							for i := n; i < nresults; i++ {
								if ra+i >= len(L.stack) {
									L.reserve(ra + i + 1 - L.top)
								}
								L.stack[ra+i] = nilValue()
							}
						}
						pc += skip + 1
						continue
					}
				}

			case common.OpCoverage:
				// Coverage hit; just count by patching the instruction's E field.
				e := common.InsnE(insn)
				if e < (1<<23)-1 {
					code[pc-1] = (uint32(op) & 0xff) | uint32(uint32(e+1)<<8)
				}

			case common.OpCapture:
				// Stray CAPTURE — should have been consumed by NEWCLOSURE.

			case common.OpGetUdataKS, common.OpSetUdataKS, common.OpNameCallUdata:
				// v9 userdata accessor opcodes. Fall back to dispatch-by-key.
				b := common.InsnB(insn)
				aux := code[pc]
				pc++
				kvIdx := common.InsnAuxKV16(aux)
				if int(kvIdx) >= len(constants) {
					L.runtimeError("UDATAKS: constant index out of range")
				}
				kv := constants[kvIdx]
				ci.savedpc = pc
				ci.top = L.top
				switch op {
				case common.OpGetUdataKS:
					L.stack[base+int(a)] = indexValue(L, L.stack[base+int(b)], kv)
				case common.OpSetUdataKS:
					newIndexValue(L, L.stack[base+int(b)], kv, L.stack[base+int(a)])
				case common.OpNameCallUdata:
					obj := L.stack[base+int(b)]
					L.stack[base+int(a)+1] = obj
					L.stack[base+int(a)] = indexValue(L, obj, kv)
				}

			case common.OpNativeCall:
				// Pseudo opcode; should never execute.
				L.runtimeError("NATIVECALL is not supported")

			default:
				L.runtimeError("unimplemented opcode: " + op.String())
			}
		}
		// Outer loop re-fetches state and continues with the (now
		// updated) ci.
		_ = code
		_ = constants
		ci.savedpc = pc
	}
}

// returnFromFrame copies nresults values starting at resultBase into
// the caller's expected slots, pops ci, and returns true iff the
// outermost Execute frame returned (so the caller should bail out).
func returnFromFrame(L *stateImpl, ci *callInfo, resultBase, nresults int) bool {
	want := ci.numresults
	// Close any open upvals at or above the varargBase (the lowest
	// slot occupied by this frame, including its varargs). Closing at
	// ci.base would leave open upvals captured into the vararg region
	// (rare but legal: a parent frame can have an open upval at the
	// slot where this frame's varargs sit only via the
	// vararg-shift dance, but we still close to be safe).
	closeLevel := ci.base
	if ci.numVararg > 0 && ci.varargBase < closeLevel {
		closeLevel = ci.varargBase
	}
	L.closeUpvalsTo(closeLevel)
	// Destination is the function slot, which is one slot below the
	// vararg region (or below the fixed-arg region if there were no
	// varargs).
	funcSlot := ci.base - 1 - ci.numVararg
	keep := nresults
	if want != MultRet && nresults > want {
		keep = want
	}
	for i := 0; i < keep; i++ {
		L.stack[funcSlot+i] = L.stack[resultBase+i]
	}
	if want != MultRet {
		for i := keep; i < want; i++ {
			if funcSlot+i >= len(L.stack) {
				L.reserve(funcSlot + i + 1 - L.top)
			}
			L.stack[funcSlot+i] = nilValue()
		}
		L.top = funcSlot + want
	} else {
		L.top = funcSlot + keep
	}
	if L.top > cap(L.stack) {
		L.reserve(L.top - len(L.stack))
	}
	L.stack = L.stack[:L.top]
	isFresh := ci.flags&ciFresh != 0
	L.popFrame()
	return isFresh
}

// opToTM converts an arithmetic opcode to its metamethod tag.
func opToTM(op common.Opcode) TM {
	switch op {
	case common.OpAdd, common.OpAddK:
		return TMAdd
	case common.OpSub, common.OpSubK:
		return TMSub
	case common.OpMul, common.OpMulK:
		return TMMul
	case common.OpDiv, common.OpDivK:
		return TMDiv
	case common.OpMod, common.OpModK:
		return TMMod
	case common.OpPow, common.OpPowK:
		return TMPow
	case common.OpIdiv, common.OpIdivK:
		return TMIDiv
	}
	return 0
}

// getImport resolves an import packed id into a value by walking env
// using the constants table for string indices.
func getImport(L *stateImpl, env *table, packed uint32, constants []value) value {
	count := packed >> 30
	id0 := int(packed>>20) & 1023
	id1 := int(packed>>10) & 1023
	id2 := int(packed) & 1023
	var v value
	// Step 0: env[K(id0)]
	if int(id0) >= len(constants) {
		return nilValue()
	}
	k0 := constants[id0]
	if k0.tag != TString {
		return nilValue()
	}
	v, _ = env.getStr(k0.gc.(*tString))
	if count >= 2 && v.tag == TTable {
		if int(id1) >= len(constants) {
			return nilValue()
		}
		k1 := constants[id1]
		if k1.tag != TString {
			return nilValue()
		}
		v, _ = v.gc.(*table).getStr(k1.gc.(*tString))
		if count >= 3 && v.tag == TTable {
			if int(id2) >= len(constants) {
				return nilValue()
			}
			k2 := constants[id2]
			if k2.tag != TString {
				return nilValue()
			}
			v, _ = v.gc.(*table).getStr(k2.gc.(*tString))
		}
	}
	return v
}

// indexValue performs t[k] with __index metamethod fallback. Used by
// GETTABLE, GETTABLEKS, GETTABLEN, NAMECALL, and the API GetTable.
func indexValue(L *stateImpl, t, k value) value {
	for depth := 0; depth < 1000; depth++ {
		if t.tag == TTable {
			tt := t.gc.(*table)
			v := tt.get(k)
			if v.tag != TNil {
				return v
			}
			// Look for __index.
			if tt.metatable == nil {
				return nilValue()
			}
			mm := L.gs.getTagMethod(tt.metatable, TMIndex)
			if mm.tag == TNil {
				return nilValue()
			}
			if mm.tag == TFunction {
				return L.callIndexMeta(mm, t, k)
			}
			t = mm
			continue
		}
		// Built-in vector component access. Luau exposes .x/.y/.z/.w
		// on vector values, mirroring upstream Luau's `luaV_gettable`
		// fast path. We honor this before consulting any per-type
		// __index metatable so that scripts that read `v.x` work even
		// when no `vector` global has been set up by lib bindings.
		if t.tag == TVector && k.tag == TString {
			if r, ok := vectorComponent(t, k.gc.(*tString).str()); ok {
				return r
			}
		}
		// Non-table: invoke __index from per-type metatable.
		mm := L.gs.getTagMethodForValue(t, TMIndex)
		if mm.tag == TNil {
			// Specialised message for vectors with an unknown
			// component name, matching upstream's vector_index
			// (VM/src/lveclib.cpp): "attempt to index vector with
			// '<name>'". Falls back to the generic message for
			// non-string keys and other types.
			if t.tag == TVector && k.tag == TString {
				L.runtimeError("attempt to index vector with '" + k.gc.(*tString).str() + "'")
			}
			L.runtimeError("attempt to index a " + t.tag.String() + " value")
		}
		if mm.tag == TFunction {
			return L.callIndexMeta(mm, t, k)
		}
		t = mm
	}
	L.runtimeError("'__index' chain too long; possible loop")
	return nilValue()
}

// vectorComponent returns v's component selected by `name` (one of
// "x", "y", "z", "w" -- "X"/etc are intentionally NOT accepted, matching
// upstream Luau). The second result is false if name is not a valid
// component (so the caller can fall back to __index).
func vectorComponent(v value, name string) (value, bool) {
	if v.tag != TVector {
		return value{}, false
	}
	vec := vectorFromValue(v)
	switch name {
	case "x", "X":
		return numberValue(float64(vec.X)), true
	case "y", "Y":
		return numberValue(float64(vec.Y)), true
	case "z", "Z":
		return numberValue(float64(vec.Z)), true
	case "w", "W":
		// `.w` exists only when the VM is built 4-wide (matches
		// upstream's LUA_VECTOR_SIZE == 4 build).
		if VectorComponents == 4 {
			return numberValue(float64(vec.W)), true
		}
		return value{}, false
	}
	return value{}, false
}

func (L *stateImpl) callIndexMeta(mm, t, k value) value {
	// Metamethod calls must allocate ABOVE the caller's used
	// registers; otherwise the argument area overlaps the still-live
	// registers of the surrounding opcode (e.g. the LOADK /
	// GETGLOBAL slots that a CALL is about to consume).
	base := metamethodBase(L, L.top)
	savedTop := L.top
	savedLen := len(L.stack)
	if base > L.top {
		if needLen := base + 3; needLen > len(L.stack) {
			L.reserve(needLen - L.top)
		}
		L.top = base
	}
	L.push(mm)
	L.push(t)
	L.push(k)
	L.callValue(base, 2, 1)
	r := L.stack[base]
	if savedLen > L.top {
		L.stack = L.stack[:savedLen]
	}
	L.top = savedTop
	return r
}

// newIndexValue performs t[k] = v with __newindex metamethod fallback.
func newIndexValue(L *stateImpl, t, k, v value) {
	for depth := 0; depth < 1000; depth++ {
		if t.tag == TTable {
			tt := t.gc.(*table)
			// If key is already present (non-nil), assign without metamethod.
			if existing := tt.get(k); existing.tag != TNil || tt.metatable == nil {
				tt.set(L.gs, k, v)
				return
			}
			mm := L.gs.getTagMethod(tt.metatable, TMNewIndex)
			if mm.tag == TNil {
				tt.set(L.gs, k, v)
				return
			}
			if mm.tag == TFunction {
				callNewIndexMeta(L, mm, t, k, v)
				return
			}
			t = mm
			continue
		}
		mm := L.gs.getTagMethodForValue(t, TMNewIndex)
		if mm.tag == TNil {
			L.runtimeError("attempt to index a " + t.tag.String() + " value")
		}
		if mm.tag == TFunction {
			callNewIndexMeta(L, mm, t, k, v)
			return
		}
		t = mm
	}
	L.runtimeError("'__newindex' chain too long; possible loop")
}

// callNewIndexMeta invokes the __newindex metamethod mm with (t, k, v)
// preserving the surrounding Lua frame's L.top and stack length. The
// metamethod is called above the caller's used registers so its args
// can't alias live slots, and the caller's stack window is restored on
// return so the next opcode's register accesses remain in range.
func callNewIndexMeta(L *stateImpl, mm, t, k, v value) {
	base := metamethodBase(L, L.top)
	savedTop := L.top
	savedLen := len(L.stack)
	if base > L.top {
		if needLen := base + 4; needLen > len(L.stack) {
			L.reserve(needLen - L.top)
		}
		L.top = base
	}
	L.push(mm)
	L.push(t)
	L.push(k)
	L.push(v)
	L.callValue(base, 3, 0)
	if savedLen > len(L.stack) {
		if savedLen > cap(L.stack) {
			L.reserve(savedLen - len(L.stack))
		}
		L.stack = L.stack[:savedLen]
	}
	L.top = savedTop
}

// findOrCreateUpval returns the open upval at stack index `idx`,
// creating it (and linking it into the thread's open-upvals list) if
// missing.
func findOrCreateUpval(L *stateImpl, idx int) *upVal {
	// Search existing list (sorted by descending stackIndex).
	prev := (**upVal)(&L.openUpvals)
	for *prev != nil && (*prev).stackIndex >= idx {
		if (*prev).stackIndex == idx {
			return *prev
		}
		prev = &(*prev).openNext
	}
	u := newUpVal(L.gs, L, idx)
	u.openNext = *prev
	*prev = u
	return u
}

// findModuleForProto returns the *bytecode.Module that contains p, by
// searching the proto cache. We assume each proto is contained in at
// most one loaded module; the cache stores constants per proto so we
// recover the module by scanning the env map (slow path) or via a
// dedicated reverse map.
func findModuleForProto(g *globalState, p *bytecode.Proto) *bytecode.Module {
	cache := getProtoCache(g)
	for m := range cache.env {
		for _, mp := range m.Protos {
			if mp == p {
				return m
			}
		}
	}
	return nil
}

// tableIterPos encodes an iteration position into a light-userdata
// `ptr` field. Mirrors upstream's `setpvalue(ra + 2, ..., LU_TAG_ITERATOR)`
// use of a reinterpret-cast integer to indicate the next slot to inspect.
func tableIterPos(i int) any { return i }

// iterPosFrom recovers the iteration index stored by tableIterPos.
// Returns 0 if the slot does not carry an int payload (defensive).
func iterPosFrom(v value) int {
	if v.tag != TLightUserdata {
		return 0
	}
	if i, ok := v.ptr.(int); ok {
		return i
	}
	return 0
}
