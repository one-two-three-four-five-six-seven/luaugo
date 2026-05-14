// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"github.com/luaugo/luaugo/internal/common"
	"github.com/luaugo/luaugo/pkg/bytecode"
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
				L.callValue(funcIdx, nargs, nresults)
				if nresults != MultRet {
					L.top = funcIdx + nresults
					if L.top > len(L.stack) {
						L.reserve(L.top - len(L.stack))
					}
					L.stack = L.stack[:L.top]
				}
				// After the call, the callee may have shrunk the
				// backing slice below the parent's MaxStackSize area
				// (this is the case for any Go function that pops
				// values during its execution). Subsequent register
				// writes inside this Lua frame need those slots back,
				// so we re-extend the slice to base+MaxStackSize
				// without touching L.top (which still records the
				// post-return free slot for multret) and without
				// zeroing surviving register values.
				if needLen := base + int(cl.proto.MaxStackSize); len(L.stack) < needLen {
					if cap(L.stack) < needLen {
						grow := make([]value, needLen, needLen+(needLen/2))
						copy(grow, L.stack)
						L.stack = grow
					} else {
						L.stack = L.stack[:needLen]
					}
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
				L.doConcat(lo, n)
				L.stack[base+int(a)] = L.stack[lo]

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
				d := common.InsnD(insn)
				aux := code[pc]
				pc++
				// Layout: R(A)=generator, R(A+1)=state, R(A+2)=control, R(A+3..)=vars
				// FORGLOOP calls generator(state, control), assigns the returns to vars,
				// sets control = first var, and jumps back if control != nil.
				nvars := int(aux & 0xff)
				_ = aux & 0x80000000 // ipairs fast-path bit (ignored, fallback)
				// Build call: push generator, state, control.
				saveTop := L.top
				L.push(L.stack[base+int(a)])
				L.push(L.stack[base+int(a)+1])
				L.push(L.stack[base+int(a)+2])
				ci.savedpc = pc
				ci.top = L.top
				L.callValue(saveTop, 2, nvars)
				// Copy results into R(A+3..A+3+nvars-1)
				for i := 0; i < nvars; i++ {
					L.stack[base+int(a)+3+i] = L.stack[saveTop+i]
				}
				L.top = saveTop
				L.stack = L.stack[:saveTop]
				control := L.stack[base+int(a)+3]
				if control.tag != TNil {
					L.stack[base+int(a)+2] = control
					pc += int(d)
				}

			case common.OpForGPrep, common.OpForGPrepInext, common.OpForGPrepNext:
				d := common.InsnD(insn)
				// Generic for prep: jumps forward to the FORGLOOP.
				// We do minimal work (matching upstream fallback path).
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
	// Close any open upvals at or above ci.base.
	L.closeUpvalsTo(ci.base)
	// Destination is ci.base - 1 (the function slot).
	funcSlot := ci.base - 1
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
		// Non-table: invoke __index from per-type metatable.
		mm := L.gs.getTagMethodForValue(t, TMIndex)
		if mm.tag == TNil {
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
				base := L.top
				L.push(mm)
				L.push(t)
				L.push(k)
				L.push(v)
				L.callValue(base, 3, 0)
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
			if savedLen > L.top {
				L.stack = L.stack[:savedLen]
			}
			L.top = savedTop
			return
		}
		t = mm
	}
	L.runtimeError("'__newindex' chain too long; possible loop")
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
