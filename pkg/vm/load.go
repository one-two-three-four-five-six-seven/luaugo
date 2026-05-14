// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"fmt"

	"github.com/luaugo/luaugo/pkg/bytecode"
)

// load.go: bytecode loading. Mirrors upstream lvmload.cpp luau_load.
//
// The Module produced by pkg/bytecode is already decoded so this file
// is much shorter than the upstream version — we don't have to parse a
// blob. We do, however, replicate the upstream semantics for:
//   - resolving string constants into interned *tString pointers
//   - rewriting ConstantClosureEntry into pre-built *closure (DupClosure)
//   - linking ConstantImportEntry references (lazy at execute time)
//   - allocating one *closure for the main proto and pushing it.

// loadModuleImpl is the workhorse behind State.LoadModule and
// State.Load (the latter decodes first, then delegates here).
func (s *stateImpl) loadModuleImpl(chunkname string, m *bytecode.Module, env int) error {
	if m == nil {
		return fmt.Errorf("vm: nil module")
	}
	// Resolve env table.
	var envT *table
	if env > 0 {
		i := s.absIndex(env)
		if i < 0 || i >= s.top || s.stack[i].tag != TTable {
			return fmt.Errorf("vm: load: env index %d does not refer to a table", env)
		}
		envT = s.stack[i].gc.(*table)
	} else {
		envT = s.globals
	}

	// Intern strings into a per-module *tString slice. Index 0 means
	// "no string", index N maps to m.Strings[N-1].
	strs := make([]*tString, len(m.Strings)+1)
	for i, str := range m.Strings {
		strs[i+1] = s.gs.intern(str)
	}

	// We need to handle ConstantClosureEntry which references protos
	// by index. Pre-create *closure values for each proto first; then
	// fill in constants (which may reference closures back).
	if int(m.MainProto) >= len(m.Protos) {
		return fmt.Errorf("vm: load: main proto index %d out of range", m.MainProto)
	}

	// Resolve proto constants. Each proto carries a parallel
	// resolvedConstants []value alongside the original Constants list.
	// We thread that via a side table because Proto is in the
	// bytecode package and we don't want to mutate it.
	cache := getProtoCache(s.gs)

	for _, p := range m.Protos {
		if _, ok := cache.constants[p]; ok {
			continue
		}
		cs := make([]value, len(p.Constants))
		for i, c := range p.Constants {
			cs[i] = resolveConstant(s.gs, c, strs, m)
		}
		cache.constants[p] = cs
	}

	// Build closures for ConstantClosure entries so that DUPCLOSURE
	// has a pre-built target. We do this lazily on first execution
	// since the env table is fixed once we know it. For now we record
	// the env on the cache; per upstream luau_load, all child protos
	// share the main proto's env.
	cache.env[m] = envT
	cache.chunkname[m] = chunkname

	main := m.Protos[m.MainProto]
	mainCl := newLClosure(s.gs, envT, main, int(main.NumUpvalues))
	// Initialize all upvals to closed nil so any GETUPVAL before SET
	// returns nil.
	for i := range mainCl.upvalRefs {
		uv := newUpVal(s.gs, s, -1)
		uv.closed = true
		uv.value = nilValue()
		uv.owner = nil
		mainCl.upvalRefs[i] = uv
	}

	s.push(closureValue(mainCl))
	return nil
}

// resolveConstant converts a bytecode.Constant entry into the runtime
// `value` it represents. Closure entries are resolved at execute time
// by DUPCLOSURE (we record only the proto index).
func resolveConstant(g *globalState, c bytecode.Constant, strs []*tString, m *bytecode.Module) value {
	switch x := c.(type) {
	case bytecode.ConstantNilEntry:
		return nilValue()
	case bytecode.ConstantBooleanEntry:
		return booleanValue(x.Value)
	case bytecode.ConstantNumberEntry:
		return numberValue(x.Value)
	case bytecode.ConstantIntegerEntry:
		return numberValue(float64(x.Value))
	case bytecode.ConstantStringEntry:
		if x.Index == 0 {
			return stringValue(g.intern(""))
		}
		if int(x.Index) < len(strs) {
			return stringValue(strs[x.Index])
		}
		return nilValue()
	case bytecode.ConstantVectorEntry:
		return vectorValue(x.X, x.Y, x.Z, x.W)
	case bytecode.ConstantImportEntry:
		// Resolution is deferred to GETIMPORT.
		v := value{tag: TLightUserdata}
		v.ptr = importTag{packed: x.Packed}
		return v
	case bytecode.ConstantClosureEntry:
		// Record the proto index for DUPCLOSURE.
		v := value{tag: TLightUserdata}
		v.ptr = closureTag{protoIndex: x.ProtoIndex}
		return v
	case bytecode.ConstantTableEntry:
		// Build a template table. Keys are constant-table indices
		// pointing at string/number constants whose value becomes the
		// table key. Used by NEWTABLE/SETLIST templates.
		v := value{tag: TLightUserdata}
		v.ptr = tableTemplateTag{keys: x.Keys}
		return v
	case bytecode.ConstantTableWithConstantsEntry:
		v := value{tag: TLightUserdata}
		v.ptr = tableTemplateTag{pairs: x.Pairs}
		return v
	}
	return nilValue()
}

// importTag is the placeholder value type stored in a Proto's constant
// table for ConstantImportEntry. The execute loop reads the packed
// path at GETIMPORT time.
type importTag struct {
	packed uint32
}

// closureTag is the placeholder for ConstantClosureEntry.
type closureTag struct {
	protoIndex uint32
}

// tableTemplateTag is the placeholder for ConstantTableEntry /
// ConstantTableWithConstantsEntry.
type tableTemplateTag struct {
	keys  []uint32
	pairs []bytecode.ConstantTablePair
}

// ---- proto cache --------------------------------------------------------

// protoCache stores per-module / per-proto bookkeeping derived at load
// time: the resolved constants slice (so we don't recompute), the env
// table, and a memoised cloned-closure for DUPCLOSURE.
type protoCache struct {
	constants map[*bytecode.Proto][]value
	env       map[*bytecode.Module]*table
	chunkname map[*bytecode.Module]string
}

func newProtoCache() *protoCache {
	return &protoCache{
		constants: make(map[*bytecode.Proto][]value),
		env:       make(map[*bytecode.Module]*table),
		chunkname: make(map[*bytecode.Module]string),
	}
}

// getProtoCache returns g's proto cache, creating it lazily. We attach
// it to the globalState via a side map keyed on the global pointer.
var pcaches = map[*globalState]*protoCache{}

func getProtoCache(g *globalState) *protoCache {
	if c, ok := pcaches[g]; ok {
		return c
	}
	c := newProtoCache()
	pcaches[g] = c
	return c
}
