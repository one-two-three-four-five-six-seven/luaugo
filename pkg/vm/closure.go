// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import "github.com/luaugo/luaugo/pkg/bytecode"

// closure mirrors upstream Closure (lobject.h). A single Go struct
// represents both Lua and Go ("C") closures distinguished by the isGo
// flag, matching upstream's discriminated union.
//
// The variant fields are:
//
//   - Lua closure: proto + upvalRefs (slice of *upVal pointing at open
//     stack slots or closed values).
//   - Go closure: goFn + upvals (slice of plain values captured by
//     value at construction time).
type closure struct {
	gcHeader

	isGo  bool
	env   *table
	proto *bytecode.Proto

	// Go closures
	goFn   GoFunction
	upvals []value

	// Lua closures
	upvalRefs []*upVal
}

func newLClosure(g *globalState, env *table, p *bytecode.Proto, nup int) *closure {
	c := &closure{env: env, proto: p, upvalRefs: make([]*upVal, nup)}
	size := memSizeClosureHdr + nup*8
	g.gcInit(c, TFunction, size)
	return c
}

func newCClosure(g *globalState, env *table, fn GoFunction, nup int) *closure {
	c := &closure{isGo: true, env: env, goFn: fn, upvals: make([]value, nup)}
	for i := range c.upvals {
		c.upvals[i] = nilValue()
	}
	size := memSizeClosureHdr + nup*memSizeTValue
	g.gcInit(c, TFunction, size)
	return c
}

// upVal mirrors upstream UpVal. An open upval points into the owning
// thread's stack (via stack+stackIndex so it survives stack
// reallocations); a closed upval holds the value inline.
type upVal struct {
	gcHeader

	closed bool
	value  value

	// Open-state bookkeeping.
	owner      *stateImpl
	stackIndex int // index into owner.stack while open
	openNext   *upVal
}

func newUpVal(g *globalState, owner *stateImpl, stackIndex int) *upVal {
	u := &upVal{owner: owner, stackIndex: stackIndex}
	g.gcInit(u, TUpval, memSizeUpVal)
	return u
}

// open returns the current value of u: either the stack slot it points
// at, or the closed-in copy.
func (u *upVal) open() bool { return !u.closed }
func (u *upVal) read() value {
	if u.closed {
		return u.value
	}
	return u.owner.stack[u.stackIndex]
}

func (u *upVal) write(g *globalState, v value) {
	if u.closed {
		u.value = v
		if v.isCollectable() && v.gc != nil {
			g.barrier(u, v.gc)
		}
		return
	}
	u.owner.stack[u.stackIndex] = v
}

// close transitions u from open to closed, copying its current stack
// value into its own storage. The caller is responsible for unlinking
// u from the thread's open-upvals list.
func (u *upVal) close() {
	if u.closed {
		return
	}
	u.value = u.owner.stack[u.stackIndex]
	u.closed = true
	u.owner = nil
	u.openNext = nil
}
