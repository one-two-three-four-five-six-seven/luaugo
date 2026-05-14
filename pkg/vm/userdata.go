// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

// userdata mirrors upstream Udata (lobject.h / ludata.h). It packages
// an opaque byte payload (or arbitrary Go value) with a host-defined
// tag and an optional metatable.
//
// The optional finalizer field captures upstream's __gc dispatch in a
// way that's natural for Go: when the GC sweeps a dead userdata it
// invokes f() if non-nil. The Lua-level __gc metamethod (when bound to
// a closure) is invoked by the higher-level finalisation pass and
// translated into this Go callback.
//
// UTagProxy is the sentinel tag stored on userdata returned by
// newproxy. Upstream uses UTAG_PROXY (LUA_UTAG_LIMIT+1 == 129) so the
// __type metamethod is intentionally ignored for proxies -- otherwise
// scripts could spoof the typeof() of arbitrary Go-side userdata.
// We pick 129 to match upstream's numeric value, though only the
// distinction "==UTagProxy or not" matters here.
const UTagProxy byte = 129

type userdata struct {
	gcHeader

	tag       byte
	metatable *table

	// Either of these may be set, but not both. data is used for
	// byte-payload userdata (the upstream `char data[]` style); object
	// is used for Go-side struct backing.
	data   []byte
	object any

	// userValue is the "user value" slot returned by lua_getuservalue.
	// It is a regular Lua value the host may attach to the userdata.
	userValue value

	finalizer func()
}

func newUserdataBytes(g *globalState, size int, tag byte) *userdata {
	u := &userdata{tag: tag, data: make([]byte, size), userValue: nilValue()}
	g.gcInit(u, TUserdata, memSizeUdataHdr+size)
	return u
}

func newUserdataObject(g *globalState, obj any, tag byte) *userdata {
	u := &userdata{tag: tag, object: obj, userValue: nilValue()}
	g.gcInit(u, TUserdata, memSizeUdataHdr+16)
	return u
}
