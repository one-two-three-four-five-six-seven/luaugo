// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

// tablex.go: extension API for table introspection / mutation needed
// by the `table` standard library. These methods are additive to the
// Tier-1 contract (per STYLE.md: new exported symbols are allowed).
//
// All indices follow the standard Lua C API convention: positive
// indices are 1-based, negative indices count from the top.

// SetReadonly toggles the frozen flag of the table at idx. Frozen
// tables reject all writes with a runtime error. Mirrors upstream
// lua_setreadonly.
func (s *State) SetReadonly(idx int, ro bool) {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top || si.stack[i].tag != TTable {
		si.runtimeError("SetReadonly: value at index is not a table")
	}
	si.stack[i].gc.(*table).readonly = ro
}

// GetReadonly reports whether the table at idx is frozen. Mirrors
// upstream lua_getreadonly.
func (s *State) GetReadonly(idx int) bool {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top || si.stack[i].tag != TTable {
		return false
	}
	return si.stack[i].gc.(*table).readonly
}

// TableShape returns the underlying (arraySize, hashSize) of the
// table at idx. Stable across reads but changes whenever a rehash
// fires; sort_less uses this to detect "table modified during
// sorting" matching upstream's `t->sizearray != n` check.
func (s *State) TableShape(idx int) (arraySize, hashSize int) {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top || si.stack[i].tag != TTable {
		return 0, 0
	}
	t := si.stack[i].gc.(*table)
	return len(t.array), len(t.nodes)
}

// CloneTable pushes a shallow copy of the table at idx onto the
// stack. The clone preserves the metatable pointer but is never
// frozen, regardless of the source's frozen state. Mirrors upstream
// luaH_clone / table.clone.
func (s *State) CloneTable(idx int) {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top || si.stack[i].tag != TTable {
		si.runtimeError("CloneTable: value at index is not a table")
	}
	src := si.stack[i].gc.(*table)
	dst := newTable(si.gs, len(src.array), src.sizenode())
	// Copy array part.
	for j, v := range src.array {
		dst.array[j] = v
		if v.isCollectable() && v.gc != nil {
			si.gs.barrierTable(dst, v.gc)
		}
	}
	// Copy hash part via set so chains / mainPositions reflect the
	// destination's geometry.
	for j := range src.nodes {
		n := &src.nodes[j]
		if n.val.tag == TNil || n.key.tag == TNil {
			continue
		}
		dst.set(si.gs, n.key, n.val)
	}
	// Preserve metatable (shallow copy).
	dst.metatable = src.metatable
	// Clones are not frozen.
	dst.readonly = false
	si.push(tableValue(dst))
}

// ClearTable removes all entries from the table at idx (both the
// array and hash parts), preserving the metatable. Mirrors upstream
// luaH_clear / table.clear.
func (s *State) ClearTable(idx int) {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top || si.stack[i].tag != TTable {
		si.runtimeError("ClearTable: value at index is not a table")
	}
	t := si.stack[i].gc.(*table)
	if t.readonly {
		si.runtimeError("attempt to modify a readonly table")
	}
	for j := range t.array {
		t.array[j] = nilValue()
	}
	for j := range t.nodes {
		t.nodes[j].key = nilValue()
		t.nodes[j].val = nilValue()
		t.nodes[j].next = 0
	}
	t.lastfree = t.sizenode()
	t.tmcache = 0
}
