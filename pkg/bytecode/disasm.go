// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package bytecode

import (
	"fmt"
	"strings"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/common"
)

// disassemble renders the full Module in a human-readable form: each
// proto's body indented under a `function <name>(...)` header. The
// format is informational only; consumers should not rely on its exact
// shape.
func disassemble(m *Module) string {
	if m == nil {
		return ""
	}
	var b strings.Builder
	for i, p := range m.Protos {
		marker := ""
		if uint32(i) == m.MainProto {
			marker = " ; main"
		}
		fmt.Fprintf(&b, "; proto %d%s\n", i, marker)
		b.WriteString(disassembleProto(m, p))
		b.WriteByte('\n')
	}
	return b.String()
}

// disassembleProto renders a single proto. The first line is the
// upstream-style header; subsequent lines are one decoded instruction
// each, indented with four spaces.
func disassembleProto(m *Module, p *Proto) string {
	if p == nil {
		return ""
	}

	var b strings.Builder
	name := "?"
	if m != nil && p.DebugName != 0 && int(p.DebugName) <= len(m.Strings) {
		name = m.Strings[p.DebugName-1]
	}

	params := make([]string, 0, p.NumParams)
	for i := uint8(0); i < p.NumParams; i++ {
		params = append(params, fmt.Sprintf("R%d", i))
	}
	if p.IsVararg != 0 {
		params = append(params, "...")
	}
	fmt.Fprintf(&b, "function %s(%s)\n", name, strings.Join(params, ", "))

	for pc := 0; pc < len(p.Code); {
		insn := p.Code[pc]
		op := common.InsnOp(insn)
		var aux uint32
		hasAux := op.HasAux() && pc+1 < len(p.Code)
		if hasAux {
			aux = p.Code[pc+1]
		}
		line := formatInsn(pc, insn, aux, op, hasAux)
		fmt.Fprintf(&b, "    %s\n", line)
		if hasAux {
			pc += 2
		} else {
			pc++
		}
	}
	return b.String()
}

// formatInsn renders a single decoded instruction. The decoding chooses
// among the ABC, AD, and E forms based on the opcode, mirroring the
// LUAU_INSN_* accessors from upstream.
func formatInsn(pc int, insn, aux uint32, op common.Opcode, hasAux bool) string {
	a := common.InsnA(insn)
	bField := common.InsnB(insn)
	c := common.InsnC(insn)
	d := common.InsnD(insn)
	e := common.InsnE(insn)

	var body string
	switch op {
	// E-form: 24-bit signed immediate.
	case common.OpJumpX, common.OpCoverage:
		body = fmt.Sprintf("E=%d", e)

	// AD-form jumps: A is reg, D is signed offset.
	case common.OpJump, common.OpJumpBack,
		common.OpJumpIf, common.OpJumpIfNot,
		common.OpJumpIfEq, common.OpJumpIfLe, common.OpJumpIfLt,
		common.OpJumpIfNotEq, common.OpJumpIfNotLe, common.OpJumpIfNotLt,
		common.OpForNPrep, common.OpForNLoop,
		common.OpForGLoop, common.OpForGPrep,
		common.OpForGPrepInext, common.OpForGPrepNext,
		common.OpJumpXEqKNil, common.OpJumpXEqKB,
		common.OpJumpXEqKN, common.OpJumpXEqKS:
		body = fmt.Sprintf("A=%d D=%d", a, d)

	// AD-form: register and 16-bit signed payload (constant index, child id, literal, ...).
	case common.OpLoadN, common.OpLoadK,
		common.OpNewClosure, common.OpDupTable, common.OpDupClosure:
		body = fmt.Sprintf("A=%d D=%d", a, d)

	default:
		body = fmt.Sprintf("A=%d B=%d C=%d", a, bField, c)
	}

	if hasAux {
		body += fmt.Sprintf(" AUX=0x%08x", aux)
	}
	return fmt.Sprintf("%04d  %-14s %s", pc, op.String(), body)
}
