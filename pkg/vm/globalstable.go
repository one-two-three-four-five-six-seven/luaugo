// Copyright (c) luaugo contributors. Licensed under the MIT License.

package vm

// PushGlobalsTable pushes the thread's actual globals table onto the
// stack. This mirrors upstream Luau's `lua_pushvalue(L,
// LUA_GLOBALSINDEX)`: callers that need a real reference to the
// globals (so that arbitrary keys can be read and written through it,
// matching Luau's `_G` semantics) use this rather than constructing a
// proxy table.
//
// This is intentionally not part of the contract.go API surface; it
// is a runtime helper used by the standard library to bind `_G` to
// the actual globals table so that scripts like
// `for i=1,10000 do _G[i] = i end` behave the same as on upstream
// Luau.
func (s *State) PushGlobalsTable() {
	s.impl.push(tableValue(s.impl.globals))
}
