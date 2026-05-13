// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"fmt"

	"github.com/luaugo/luaugo/pkg/bytecode"
)

// stateImpl is the concrete implementation that hides behind *State.
// Conceptually it pairs upstream's `lua_State` (per-thread fields) with
// a back-pointer to the `global_State` shared by all threads.
//
// stateImpl is a GC object (TThread) so coroutines participate in the
// tri-colour invariant.
type stateImpl struct {
	gcHeader

	// Per-thread fields.
	stack      []value
	top        int          // first free slot (stack[0..top-1] is live)
	globals    *table       // _G (per Lua state; sandboxes may swap)
	openUpvals *upVal       // linked list head
	gs         *globalState // never nil while the thread is alive
	closed     bool         // set by Close
}

// globalState is the shared "global_State" referenced by every thread
// belonging to the same VM instance. Owning multiple State pointers
// (e.g. coroutines) into the same VM amounts to multiple stateImpl
// values whose `gs` pointers compare equal.
type globalState struct {
	mainthread *stateImpl
	allgc      gcObject // doubly-linked list of all GC objects

	strt *stringTable

	// Global tables.
	registry *table
	mt       [TBuffer + 1]*table // metatables per type
	ttname   [TBuffer + 1]*tString
	tmname   [TMCount]*tString

	// GC fields.
	currentWhite uint8
	gcstate      int8
	gray         gcObject
	grayAgain    gcObject
	weak         gcObject
	sweepCursor  gcObject

	totalBytes  uint64
	gcThreshold uint64
	gcGoal      int
	gcStepMul   int
	gcStepSize  int
}

func newGlobalState() *globalState {
	g := &globalState{
		strt:         newStringTable(),
		currentWhite: gcWhite0Bit,
		gcstate:      gcPause,
		gcGoal:       gcDefaultGoal,
		gcStepMul:    gcDefaultStepMul,
		gcStepSize:   gcDefaultStepSize,
		gcThreshold:  gcDefaultStepSize * 1024,
	}
	return g
}

// newState (lowercase) is invoked by NewState in contract.go. It
// builds a fresh global_State, a single main thread, and seeds the
// metamethod-name table.
func newState() *State {
	g := newGlobalState()
	s := &stateImpl{
		stack: make([]value, 0, 64),
		gs:    g,
	}
	// Manually link s into allgc (gcInit pattern, but the global state
	// isn't quite ready yet — we still need to mint the globals table
	// after allgc has a thread root).
	g.gcInit(s, TThread, memSizeThreadHdr)
	g.mainthread = s

	// Pre-intern metamethod names.
	g.initTM()
	// Globals.
	s.globals = newTable(g, 0, 0)
	// Registry.
	g.registry = newTable(g, 0, 0)

	return &State{impl: s}
}

func (s *State) close() {
	if s.impl == nil || s.impl.closed {
		return
	}
	s.impl.closed = true
	// Drop references; the Go GC will reclaim everything once nothing
	// outside the State holds the global_State.
	g := s.impl.gs
	g.allgc = nil
	g.mainthread = nil
	s.impl.stack = nil
}

// ----------------------------------------------------------------------
// Stack indexing
// ----------------------------------------------------------------------

// absIndex resolves a Lua-style 1-based stack index (positive) or a
// from-top index (negative). Returns -1 for unreachable indices.
func (s *stateImpl) absIndex(idx int) int {
	if idx > 0 {
		return idx - 1 // convert to 0-based
	}
	if idx < 0 {
		i := s.top + idx
		if i < 0 {
			return -1
		}
		return i
	}
	return -1 // 0 is invalid as a stack index
}

func (s *stateImpl) checkIndex(idx int) int {
	i := s.absIndex(idx)
	if i < 0 || i >= s.top {
		// Tier-2 contract: out-of-range indices yield "no value" reads
		// and panic on writes. We mark this with a sentinel by
		// returning -1 and letting the caller decide.
		return -1
	}
	return i
}

func (s *stateImpl) reserve(n int) {
	need := s.top + n
	if cap(s.stack) >= need {
		s.stack = s.stack[:need]
		return
	}
	ns := need + (need / 2)
	if ns < 16 {
		ns = 16
	}
	grow := make([]value, need, ns)
	copy(grow, s.stack)
	s.stack = grow
}

func (s *stateImpl) push(v value) {
	s.reserve(1)
	s.stack[s.top] = v
	s.top++
}

// ----------------------------------------------------------------------
// State method backends — exported public methods on *State call these.
// ----------------------------------------------------------------------

func (s *State) top() int { return s.impl.top }

func (s *State) setTop(idx int) {
	si := s.impl
	if idx < 0 {
		// Negative idx means "remove |idx| elements from the top".
		newTop := si.top + idx + 1
		if newTop < 0 {
			newTop = 0
		}
		si.stack = si.stack[:newTop]
		si.top = newTop
		return
	}
	if idx > si.top {
		si.reserve(idx - si.top)
		for i := si.top; i < idx; i++ {
			si.stack[i] = nilValue()
		}
	} else {
		si.stack = si.stack[:idx]
	}
	si.top = idx
}

func (s *State) checkStack(n int) bool {
	if n < 0 {
		return false
	}
	s.impl.reserve(n)
	return true
}

func (s *State) typeAt(idx int) Type {
	i := s.impl.absIndex(idx)
	if i < 0 || i >= s.impl.top {
		return TNone
	}
	return s.impl.stack[i].tag
}

func (s *State) pushNil()           { s.impl.push(nilValue()) }
func (s *State) pushBoolean(b bool) { s.impl.push(booleanValue(b)) }
func (s *State) pushNumber(n float64) {
	s.impl.push(numberValue(n))
}
func (s *State) pushInteger(n int64) {
	// See contract: v0.720 stores all numbers as float64. The TInteger
	// tag is reserved but no stack value has that tag.
	s.impl.push(numberValue(float64(n)))
}
func (s *State) pushString(v string) {
	s.impl.push(stringValue(s.impl.gs.intern(v)))
}
func (s *State) pushVector(x, y, z, w float32) {
	if VectorComponents == 3 {
		w = 0
	}
	s.impl.push(vectorValue(x, y, z, w))
}

func (s *State) pushGoFunction(fn GoFunction, n int) {
	si := s.impl
	if n > si.top {
		panic("vm: not enough values on the stack for upvalues")
	}
	c := newCClosure(si.gs, si.globals, fn, n)
	// Pop n values into the closure's upvalues, top last.
	base := si.top - n
	for i := 0; i < n; i++ {
		c.upvals[i] = si.stack[base+i]
	}
	si.stack = si.stack[:base]
	si.top = base
	si.push(closureValue(c))
}

func (s *State) pushValue(idx int) {
	i := s.impl.absIndex(idx)
	if i < 0 || i >= s.impl.top {
		s.impl.push(nilValue())
		return
	}
	s.impl.push(s.impl.stack[i])
}

func (s *State) toBoolean(idx int) bool {
	i := s.impl.absIndex(idx)
	if i < 0 || i >= s.impl.top {
		return false
	}
	return !s.impl.stack[i].isFalse()
}

func (s *State) toNumber(idx int) (float64, bool) {
	i := s.impl.absIndex(idx)
	if i < 0 || i >= s.impl.top {
		return 0, false
	}
	return s.impl.stack[i].asNumber()
}

func (s *State) toInteger(idx int) (int64, bool) {
	i := s.impl.absIndex(idx)
	if i < 0 || i >= s.impl.top {
		return 0, false
	}
	return s.impl.stack[i].asInteger()
}

func (s *State) toString(idx int) (string, bool) {
	i := s.impl.absIndex(idx)
	if i < 0 || i >= s.impl.top {
		return "", false
	}
	out, ok := s.impl.stack[i].asString()
	if ok && s.impl.stack[i].tag == TNumber {
		// Per Lua semantics, ToString on a number value also mutates
		// the slot to hold the interned string so subsequent reads see
		// a string. Mirrors upstream lua_tolstring's side effect.
		s.impl.stack[i] = stringValue(s.impl.gs.intern(out))
	}
	return out, ok
}

func (s *State) toVector(idx int) (float32, float32, float32, float32, bool) {
	i := s.impl.absIndex(idx)
	if i < 0 || i >= s.impl.top {
		return 0, 0, 0, 0, false
	}
	v := s.impl.stack[i]
	if v.tag != TVector {
		return 0, 0, 0, 0, false
	}
	return v.vec[0], v.vec[1], v.vec[2], v.vec[3], true
}

func (s *State) isString(idx int) bool {
	t := s.typeAt(idx)
	return t == TString || t == TNumber
}

func (s *State) isNumber(idx int) bool {
	i := s.impl.absIndex(idx)
	if i < 0 || i >= s.impl.top {
		return false
	}
	_, ok := s.impl.stack[i].asNumber()
	return ok
}

func (s *State) remove(idx int) {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		return
	}
	copy(si.stack[i:], si.stack[i+1:si.top])
	si.top--
	si.stack = si.stack[:si.top]
}

func (s *State) insert(idx int) {
	si := s.impl
	if si.top == 0 {
		return
	}
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		return
	}
	top := si.stack[si.top-1]
	copy(si.stack[i+1:si.top], si.stack[i:si.top-1])
	si.stack[i] = top
}

func (s *State) replace(idx int) {
	si := s.impl
	if si.top == 0 {
		return
	}
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		return
	}
	top := si.stack[si.top-1]
	si.stack[i] = top
	si.top--
	si.stack = si.stack[:si.top]
}

// ----------------------------------------------------------------------
// Tables
// ----------------------------------------------------------------------

func (s *State) newTable(narr, nrec int) {
	t := newTable(s.impl.gs, narr, nrec)
	s.impl.push(tableValue(t))
}

func (s *State) getTable(idx int) {
	panic("vm: GetTable: Tier 3 owns this (needs metamethod dispatch)")
}

func (s *State) setTable(idx int) {
	panic("vm: SetTable: Tier 3 owns this (needs metamethod dispatch)")
}

func (s *State) getField(idx int, name string) {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top || si.stack[i].tag != TTable {
		si.push(nilValue())
		return
	}
	t := si.stack[i].gc.(*table)
	key := si.gs.intern(name)
	v, _ := t.getStr(key)
	si.push(v)
}

func (s *State) setField(idx int, name string) {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top || si.stack[i].tag != TTable {
		panic(luaError{message: "attempt to index a non-table"})
	}
	if si.top < 1 {
		panic("vm: SetField requires a value on top of the stack")
	}
	t := si.stack[i].gc.(*table)
	key := si.gs.intern(name)
	v := si.stack[si.top-1]
	t.setStr(si.gs, key, v)
	si.stack = si.stack[:si.top-1]
	si.top--
}

func (s *State) rawGet(idx int) {
	si := s.impl
	i := si.absIndex(idx)
	if si.top < 1 {
		panic("vm: RawGet requires a key on top of the stack")
	}
	if i < 0 || i >= si.top || si.stack[i].tag != TTable {
		si.stack[si.top-1] = nilValue()
		return
	}
	t := si.stack[i].gc.(*table)
	key := si.stack[si.top-1]
	si.stack[si.top-1] = t.get(key)
}

func (s *State) rawSet(idx int) {
	si := s.impl
	i := si.absIndex(idx)
	if si.top < 2 {
		panic("vm: RawSet requires a key and value on the stack")
	}
	if i < 0 || i >= si.top || si.stack[i].tag != TTable {
		panic(luaError{message: "attempt to index a non-table"})
	}
	t := si.stack[i].gc.(*table)
	key := si.stack[si.top-2]
	val := si.stack[si.top-1]
	t.set(si.gs, key, val)
	si.stack = si.stack[:si.top-2]
	si.top -= 2
}

func (s *State) rawGetI(idx, n int) {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top || si.stack[i].tag != TTable {
		si.push(nilValue())
		return
	}
	t := si.stack[i].gc.(*table)
	si.push(t.getNum(n))
}

func (s *State) rawSetI(idx, n int) {
	si := s.impl
	i := si.absIndex(idx)
	if si.top < 1 {
		panic("vm: RawSetI requires a value on top of the stack")
	}
	if i < 0 || i >= si.top || si.stack[i].tag != TTable {
		panic(luaError{message: "attempt to index a non-table"})
	}
	t := si.stack[i].gc.(*table)
	v := si.stack[si.top-1]
	t.setNum(si.gs, n, v)
	si.stack = si.stack[:si.top-1]
	si.top--
}

func (s *State) next(idx int) bool {
	si := s.impl
	i := si.absIndex(idx)
	if si.top < 1 {
		panic("vm: Next requires a key on top of the stack")
	}
	if i < 0 || i >= si.top || si.stack[i].tag != TTable {
		return false
	}
	t := si.stack[i].gc.(*table)
	key := si.stack[si.top-1]
	k, v, ok := t.next(key)
	if !ok {
		// Pop the iteration key.
		si.stack = si.stack[:si.top-1]
		si.top--
		return false
	}
	si.stack[si.top-1] = k
	si.push(v)
	return true
}

func (s *State) length(idx int) {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		si.push(numberValue(0))
		return
	}
	v := si.stack[i]
	switch v.tag {
	case TString:
		si.push(numberValue(float64(v.gc.(*tString).len())))
	case TTable:
		si.push(numberValue(float64(v.gc.(*table).rawLen())))
	case TBuffer:
		si.push(numberValue(float64(v.gc.(*buffer).Len())))
	default:
		// Without metamethods (Tier 2), other types contribute 0. Tier
		// 3 will route through __len.
		si.push(numberValue(0))
	}
}

func (s *State) rawEqual(idx1, idx2 int) bool {
	si := s.impl
	i1 := si.absIndex(idx1)
	i2 := si.absIndex(idx2)
	if i1 < 0 || i1 >= si.top || i2 < 0 || i2 >= si.top {
		return false
	}
	return rawEqual(si.stack[i1], si.stack[i2])
}

func (s *State) equal(idx1, idx2 int) bool {
	panic("vm: Equal: Tier 3 owns this (needs __eq dispatch)")
}

func (s *State) lessThan(idx1, idx2 int) bool {
	panic("vm: LessThan: Tier 3 owns this (needs __lt dispatch)")
}

// ----------------------------------------------------------------------
// Calls / errors
// ----------------------------------------------------------------------

func (s *State) call(int, int) {
	panic("vm: Call: Tier 3 owns this (bytecode interpreter)")
}

func (s *State) pcall(int, int, int) Status {
	panic("vm: PCall: Tier 3 owns this (bytecode interpreter)")
}

func (s *State) raiseError() {
	panic("vm: Error: Tier 3 owns this (longjmp-style unwinding)")
}

func (s *State) errorf(format string, args ...any) {
	panic(luaError{message: fmt.Sprintf(format, args...)})
}

// ----------------------------------------------------------------------
// Coroutines / loading (Tier 3)
// ----------------------------------------------------------------------

func (s *State) newThread() *State {
	panic("vm: NewThread: Tier 3 owns this")
}

func (co *State) resume(*State, int) Status {
	panic("vm: Resume: Tier 3 owns this")
}

func (s *State) yield(int) int {
	panic("vm: Yield: Tier 3 owns this")
}

func (s *State) load(string, []byte, int) error {
	panic("vm: Load: Tier 3 owns this")
}

func (s *State) loadModule(string, *bytecode.Module, int) error {
	panic("vm: LoadModule: Tier 3 owns this")
}

// ----------------------------------------------------------------------
// Globals
// ----------------------------------------------------------------------

func (s *State) getGlobal(name string) {
	si := s.impl
	key := si.gs.intern(name)
	v, _ := si.globals.getStr(key)
	si.push(v)
}

func (s *State) setGlobal(name string) {
	si := s.impl
	if si.top < 1 {
		panic("vm: SetGlobal requires a value on top of the stack")
	}
	key := si.gs.intern(name)
	v := si.stack[si.top-1]
	si.globals.setStr(si.gs, key, v)
	si.stack = si.stack[:si.top-1]
	si.top--
}

// ----------------------------------------------------------------------
// GC
// ----------------------------------------------------------------------

func (s *State) gcInfo() int {
	// Upstream gcinfo() returns kilobytes.
	return int(s.impl.gs.totalBytes / 1024)
}

func (s *State) collectGarbage() {
	s.impl.gs.fullGC()
	// Sweep any dead strings out of the intern table now that the
	// allgc list has the bookkeeping done.
	s.impl.gs.sweepInternTable()
}

// ----------------------------------------------------------------------
// Libraries / sandboxing (Tier 4)
// ----------------------------------------------------------------------

func (s *State) openLibs() {
	panic("vm: OpenLibs: Tier 4 owns this")
}

func (s *State) sandbox() {
	panic("vm: Sandbox: Tier 3 owns this")
}

func (s *State) sandboxThread() {
	panic("vm: SandboxThread: Tier 3 owns this")
}
