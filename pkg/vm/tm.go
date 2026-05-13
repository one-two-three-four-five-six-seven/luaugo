// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

// TM identifies a Lua tag (meta) method. Values match upstream
// ltm.h enum TMS exactly so a Tier 3 interpreter can reuse them as
// shift amounts for the tmcache bitmask.
type TM int

const (
	TMIndex TM = iota
	TMNewIndex
	TMMode
	TMNameCall
	TMCall
	TMIter
	TMLen
	TMEq

	TMAdd
	TMSub
	TMMul
	TMDiv
	TMIDiv
	TMMod
	TMPow
	TMUnm

	TMLt
	TMLe
	TMConcat
	TMType
	TMMetatable

	TMCount
)

// TMNames lists the canonical metamethod names in TM order. Library
// code (and tests) may index this slice directly.
var TMNames = [TMCount]string{
	"__index",
	"__newindex",
	"__mode",
	"__namecall",
	"__call",
	"__iter",
	"__len",
	"__eq",
	"__add",
	"__sub",
	"__mul",
	"__div",
	"__idiv",
	"__mod",
	"__pow",
	"__unm",
	"__lt",
	"__le",
	"__concat",
	"__type",
	"__metatable",
}

// initTM populates the global tmname[] array with interned strings for
// every metamethod, matching upstream luaT_init.
func (g *globalState) initTM() {
	for i := 0; i < int(TMCount); i++ {
		g.tmname[i] = g.intern(TMNames[i])
		// Mark these strings fixed: they outlive every GC cycle.
		g.tmname[i].marked |= gcFixedBit
	}
}

// getTagMethod returns the metamethod for `event` from `mt`, or a nil
// value if absent. The tmcache bitmask is updated on miss so subsequent
// queries can answer with no hash lookup. Matches upstream luaT_gettm.
func (g *globalState) getTagMethod(mt *table, event TM) value {
	if mt == nil {
		return nilValue()
	}
	// Fast: cache says absent.
	if event < 8 && mt.tmcache&(1<<event) != 0 {
		return nilValue()
	}
	v, _ := mt.getStr(g.tmname[event])
	if v.tag == TNil && event < 8 {
		mt.tmcache |= 1 << event
	}
	return v
}

// getTagMethodForValue returns the metamethod for v's type, including
// per-type "default" metatables stored in globalState.mt.
func (g *globalState) getTagMethodForValue(v value, event TM) value {
	var mt *table
	switch v.tag {
	case TTable:
		mt = v.gc.(*table).metatable
	case TUserdata:
		mt = v.gc.(*userdata).metatable
	default:
		if int(v.tag) >= 0 && int(v.tag) < len(g.mt) {
			mt = g.mt[v.tag]
		}
	}
	return g.getTagMethod(mt, event)
}
