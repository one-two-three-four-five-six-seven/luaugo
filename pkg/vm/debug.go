// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"fmt"
	"strings"

	"github.com/luaugo/luaugo/pkg/bytecode"
)

// debug.go: traceback and debug.info support.

// TraceBack returns a multi-line traceback string starting from frame
// level (0 = innermost). msg is prepended verbatim. Mirrors upstream
// debug.traceback semantics.
func (s *State) TraceBack(level int, msg string) string {
	var b strings.Builder
	if msg != "" {
		b.WriteString(msg)
		b.WriteByte('\n')
	}
	b.WriteString("stack traceback:")
	frames := s.impl.frames
	for i := len(frames) - 1 - level; i >= 0; i-- {
		ci := frames[i]
		b.WriteByte('\n')
		b.WriteByte('\t')
		if ci.cl == nil {
			b.WriteString("?")
			continue
		}
		if ci.cl.isGo {
			b.WriteString("[Go]: in ?")
			continue
		}
		p := ci.cl.proto
		name := "?"
		_ = p
		line := lineForPC(p, ci.savedpc-1)
		if line > 0 {
			b.WriteString(fmt.Sprintf("[Lua]:%d: in function %s", line, name))
		} else {
			b.WriteString(fmt.Sprintf("[Lua]: in function %s", name))
		}
	}
	return b.String()
}

// DebugInfo returns basic frame info for the frame `level` deep.
type DebugInfo struct {
	Source      string
	Line        int
	What        string // "Go" or "Lua"
	Name        string
	Currentline int
}

// GetInfo returns a DebugInfo for the frame `level` deep. Returns
// (info, true) if the frame exists.
func (s *State) GetInfo(level int) (DebugInfo, bool) {
	frames := s.impl.frames
	idx := len(frames) - 1 - level
	if idx < 0 || idx >= len(frames) {
		return DebugInfo{}, false
	}
	ci := frames[idx]
	di := DebugInfo{}
	if ci.cl == nil {
		return di, true
	}
	if ci.cl.isGo {
		di.What = "Go"
		return di, true
	}
	di.What = "Lua"
	p := ci.cl.proto
	di.Source = "?"
	di.Line = int(p.LineDefined)
	di.Currentline = lineForPC(p, ci.savedpc-1)
	return di, true
}

func lineForPC(p *bytecode.Proto, pc int) int {
	// We don't decode LineInfo here (the encoding is delta+abs).
	// Tier 4 may add a proper decoder.
	_ = p
	_ = pc
	return 0
}
