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

// debug.go: traceback and debug.info support. Mirrors the
// upstream-exposed pieces of ldebug.cpp / lapi.cpp that Luau's debug
// library consumes (luaL_traceback, lua_getinfo).

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
		src := chunkNameForProto(s.impl.gs, p)
		if src == "" {
			src = "[Lua]"
		}
		line := lineForPC(p, ci.savedpc-1)
		name := protoName(s.impl.gs, p)
		if name == "" {
			name = "?"
		}
		if line > 0 {
			b.WriteString(fmt.Sprintf("%s:%d: in function %s", src, line, name))
		} else {
			b.WriteString(fmt.Sprintf("%s: in function %s", src, name))
		}
	}
	return b.String()
}

// DebugInfo returns basic frame info for the frame `level` deep.
type DebugInfo struct {
	Source      string // chunkname (as passed to Load)
	Line        int    // proto's LineDefined
	What        string // "Go", "Lua", or "" (unknown)
	Name        string // best-effort function name
	Currentline int    // current source line (0 if unavailable)
	NumParams   int    // proto.NumParams (Lua frames)
	IsVararg    bool   // proto.IsVararg != 0 (Lua frames)
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
		di.Source = "[Go]"
		di.Currentline = -1
		return di, true
	}
	di.What = "Lua"
	p := ci.cl.proto
	di.Source = chunkNameForProto(s.impl.gs, p)
	if di.Source == "" {
		di.Source = "?"
	}
	di.Line = int(p.LineDefined)
	di.Currentline = lineForPC(p, ci.savedpc-1)
	di.Name = protoName(s.impl.gs, p)
	di.NumParams = int(p.NumParams)
	di.IsVararg = p.IsVararg != 0
	return di, true
}

// PushFunc pushes the function value of the frame `level` deep onto
// the stack. Returns true if the frame exists. Mirrors the 'f' arm of
// upstream lua_getinfo.
func (s *State) PushFunc(level int) bool {
	frames := s.impl.frames
	idx := len(frames) - 1 - level
	if idx < 0 || idx >= len(frames) {
		return false
	}
	ci := frames[idx]
	if ci.cl == nil {
		s.impl.push(nilValue())
		return true
	}
	s.impl.push(closureValue(ci.cl))
	return true
}

// chunkNameForProto looks up the chunkname recorded at load time for
// the module that contains p. Returns "" if not found.
func chunkNameForProto(g *globalState, p *bytecode.Proto) string {
	cache := getProtoCache(g)
	for m, name := range cache.chunkname {
		for _, mp := range m.Protos {
			if mp == p {
				return name
			}
		}
	}
	return ""
}

// protoName returns the proto's DebugName as a string, or "" if absent.
func protoName(g *globalState, p *bytecode.Proto) string {
	if p == nil || p.DebugName == 0 {
		return ""
	}
	cache := getProtoCache(g)
	for m := range cache.chunkname {
		if int(p.DebugName) <= len(m.Strings) {
			for _, mp := range m.Protos {
				if mp == p {
					return m.Strings[p.DebugName-1]
				}
			}
		}
	}
	return ""
}

// lineForPC returns the source line for the instruction at pc in p, or
// 0 if line info is not available. Mirrors upstream luaG_getline:
//
//	abslineinfo[pc >> linegaplog2] + lineinfo[pc]
//
// NOTE: pkg/bytecode currently stores both arrays as the raw delta
// values emitted by upstream (rather than the cumulative values
// upstream's lvmload computes at decode time). The arithmetic below
// therefore matches upstream when callers populate AbsLineInfo with
// already-summed absolute lines (as the upstream loader does in
// memory), which is the form luaugo's hand-built test fixtures use.
// Modules produced by the luaugo bytecode decoder will yield a smaller
// value -- see the contract bug noted in pkg/bytecode/decoder.go.
func lineForPC(p *bytecode.Proto, pc int) int {
	if p == nil || p.LineInfo == nil {
		return 0
	}
	li := p.LineInfo
	if pc < 0 || pc >= len(li.LineInfo) {
		return 0
	}
	gap := pc >> li.LineGapLog2
	if gap < 0 || gap >= len(li.AbsLineInfo) {
		return 0
	}
	return int(li.AbsLineInfo[gap]) + int(li.LineInfo[pc])
}
