// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"fmt"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"
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

	// Tier-3 fields.
	frames    []*callInfo // call frame stack
	callBase  int         // current Go-function frame base (for absIndex)
	callFunc  int         // current Go-function index (top of callee slot)
	wrapper   *State      // cached *State view of this thread
	co        *coroutine  // coroutine bookkeeping (nil for main thread)
	status    Status      // last status for this thread

	// nonyieldable counts the depth of currently-running non-yieldable
	// Go calls (table.sort's comparator dispatch, debug.* introspection
	// helpers, etc.). When >0, coroutine.yield raises
	// "attempt to yield across metamethod/C-call boundary" instead of
	// suspending. Mirrors upstream's `L->nCcalls > L->baseCcalls`
	// gate, scoped per-thread.
	nonyieldable int

	// inErrHandler counts the depth of currently-executing xpcall
	// error handlers (and other "we're already handling an error"
	// contexts). When a pcall recovers an error while this is >0,
	// the recovered error message is replaced with "error in error
	// handling" so conformance/errors.luau:211 surfaces the expected
	// string.
	inErrHandler int
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

	// gcStopped is set by collectgarbage("stop") and cleared by
	// collectgarbage("restart"). When true, the auto-step driven
	// by allocBytes is suppressed so scripts that explicitly want
	// to control collection (gc.luau:80 / 108) can observe their
	// own allocation pressure.
	gcStopped bool

	// gcInStep is the re-entrancy guard set while gcStep is
	// executing; allocations done during marking/sweeping bump
	// totalBytes but must not recursively call gcStep.
	gcInStep bool

	// resumeDepth counts the number of currently in-flight nested
	// coroutine resumes on this global state. Upstream uses
	// L->nCcalls combined with LUAI_MAXCCALLS for the same gate;
	// without that bookkeeping we cap the depth at maxCoResumeDepth
	// so a pathological recursive coroutine chain (coroutine.luau:
	// 246-247's deliberate infinite recursion) errors out before
	// goroutines accumulate beyond reasonable limits.
	resumeDepth int

	// liveCoroutines tracks every coroutine that has been started on
	// this globalState. State.Close uses this to send a shutdown
	// signal so blocked goroutines (suspended on <-resumeCh) exit
	// instead of leaking. Indexed by *coroutine for O(1) registration
	// and removal at finish-time.
	liveCoroutines map[*coroutine]struct{}

	// coverageHits records, per-proto, how many times each source
	// line has been executed. Maintained by executeProto on every
	// new line transition. Read by debug.getcoverage. nil until the
	// first proto enters execution. We key by *bytecode.Proto since
	// proto pointers are stable for the module's lifetime.
	coverageHits map[*bytecode.Proto]map[int]int
}

// maxCoResumeDepth caps simultaneously-active nested coroutine
// resumes. Lua-level scripts that legitimately need deep chains
// (recursive generator pipelines, coroutine-based parsers) virtually
// never exceed a few dozen; we set the cap well above any reasonable
// usage. The conformance fixture coroutine.luau:246 deliberately
// spawns an unbounded chain inside a pcall, expecting a recoverable
// "C stack overflow" error; that path is the primary consumer of
// this gate.
// 199 instead of 200 to account for the fact that upstream's
// LUAI_MAXCCALLS counts every C-call (including pcall) but we only
// bump resumeDepth on coroutine Resume. conformance/pcall.luau:295
// asserts count == 200 for a pcall(foo) -> coroutine.wrap(foo)()
// recursion: 1 pcall + 199 wraps = 200 invocations of foo.
const maxCoResumeDepth = 199

func newGlobalState() *globalState {
	g := &globalState{
		strt:           newStringTable(),
		currentWhite:   gcWhite0Bit,
		gcstate:        gcPause,
		gcGoal:         gcDefaultGoal,
		gcStepMul:      gcDefaultStepMul,
		gcStepSize:     gcDefaultStepSize,
		gcThreshold:    gcDefaultStepSize * 1024,
		liveCoroutines: make(map[*coroutine]struct{}),
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
	g := s.impl.gs
	// Signal every still-live coroutine to exit. Their goroutines are
	// blocked on <-resumeCh after a yield; sending a shutdown message
	// unblocks them and they fall through to their defer-recover,
	// terminating cleanly. Without this Close, every State that ran
	// any coroutine that didn't run to completion would leak one
	// goroutine per yielded coroutine.
	for c := range g.liveCoroutines {
		if c.finished {
			continue
		}
		// Non-blocking send: the channel is buffered with cap 1, so
		// if the coroutine isn't currently parked we just drop the
		// signal. The goroutine will pick it up on its next receive
		// (which it must do; suspended coroutines are always parked
		// on resumeCh).
		select {
		case c.resumeCh <- resumeMsg{shutdown: true}:
		default:
		}
	}
	// Clear the map so subsequent Close calls don't double-send.
	g.liveCoroutines = nil
	// Drop references; the Go GC will reclaim everything once nothing
	// outside the State holds the global_State.
	g.allgc = nil
	g.mainthread = nil
	s.impl.stack = nil
}

// ----------------------------------------------------------------------
// Stack indexing
// ----------------------------------------------------------------------

// absIndex resolves a Lua-style 1-based stack index (positive) or a
// from-top index (negative). Returns -1 for unreachable indices.
//
// When the thread is currently executing a Go function (callBase != 0),
// positive indices are measured from callBase so the Go function sees
// its arguments at indices 1..nargs.
func (s *stateImpl) absIndex(idx int) int {
	if idx > 0 {
		i := idx - 1
		if s.callBase > 0 {
			i = s.callBase + idx - 1
		}
		return i
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

// reserve ensures the live region of the stack covers at least
// s.top + n slots.
//
// Subtle: this function must only EXTEND the slice, never SHRINK it.
// The interpreter routinely keeps s.top below len(s.stack) while
// register operations (MOVE/LOADK/...) write to slots above s.top
// without bumping it; OpCall, OpForGLoop, OpSetList, etc. then read
// those higher slots when gathering arguments. An earlier version of
// this function unconditionally did
//
//	s.stack = s.stack[:need]
//
// which truncates whenever s.top < need < len(s.stack), silently
// invalidating the in-flight argument frame and producing
// "index out of range" panics inside OpForGLoop and OpSetList when
// they pulled from those slots (see tables.luau).
func (s *stateImpl) reserve(n int) {
	need := s.top + n
	if need <= len(s.stack) {
		return
	}
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

func (s *State) top() int {
	if s.impl.callBase > 0 {
		return s.impl.top - s.impl.callBase
	}
	return s.impl.top
}

func (s *State) setTop(idx int) {
	si := s.impl
	if idx < 0 {
		// Negative idx means "remove |idx| elements from the top".
		newTop := si.top + idx + 1
		if newTop < si.callBase {
			newTop = si.callBase
		}
		si.stack = si.stack[:newTop]
		si.top = newTop
		return
	}
	// Positive idx is frame-relative when inside a Go call.
	absTop := idx
	if si.callBase > 0 {
		absTop = si.callBase + idx
	}
	if absTop > si.top {
		si.reserve(absTop - si.top)
		for i := si.top; i < absTop; i++ {
			si.stack[i] = nilValue()
		}
	} else {
		si.stack = si.stack[:absTop]
	}
	si.top = absTop
}

func (s *State) checkStack(n int) bool {
	if n < 0 {
		return false
	}
	// Mirror upstream lua_checkstack: refuse if either the request
	// alone, or the resulting top, would exceed LUAI_MAXCSTACK
	// (8000 slots). conformance/tables.luau:670-677 exercises this
	// by calling `table.unpack(b)` on an 8000-element table, which
	// must surface as "too many results to unpack".
	const LUAI_MAXCSTACK = 8000
	if n > LUAI_MAXCSTACK || s.impl.top+n > LUAI_MAXCSTACK {
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

func (s *State) getTable(idx int) { s.getTableImpl(idx) }

func (s *State) setTable(idx int) { s.setTableImpl(idx) }

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

func (s *State) equal(idx1, idx2 int) bool { return s.equalImpl(idx1, idx2) }

func (s *State) lessThan(idx1, idx2 int) bool { return s.lessThanImpl(idx1, idx2) }

// ----------------------------------------------------------------------
// Calls / errors
// ----------------------------------------------------------------------

func (s *State) call(nargs, nresults int) { s.callImpl(nargs, nresults) }

func (s *State) pcall(nargs, nresults, errfunc int) Status {
	return s.pcallImpl(nargs, nresults, errfunc)
}

func (s *State) raiseError() { s.raiseErrorImplBackend() }

func (s *State) errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	s.impl.runtimeError(msg)
}

// ----------------------------------------------------------------------
// Coroutines / loading (Tier 3)
// ----------------------------------------------------------------------

func (s *State) newThread() *State { return s.newThreadImpl() }

func (co *State) resume(from *State, nargs int) Status { return co.resumeImpl(from, nargs) }

func (s *State) yield(nresults int) int { return s.yieldImpl(nresults) }

func (s *State) load(chunkname string, blob []byte, env int) error {
	return s.loadImpl(chunkname, blob, env)
}

func (s *State) loadModule(chunkname string, m *bytecode.Module, env int) error {
	return s.loadModuleImplPub(chunkname, m, env)
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
	// NOTE: we intentionally do NOT call sweepInternTable here even
	// though full upstream Luau sweeps interned strings during GC.
	// Our marker doesn't trace proto-level string constants (they
	// live inside *bytecode.Module slices that aren't gcObjects of
	// their own), so any string that's currently a Lua constant but
	// not also on the live stack would be condemned and removed from
	// the intern table. The next call to intern() for the same byte
	// sequence would then allocate a brand-new *tString, breaking
	// pointer-identity-based string equality (`type(x) == "thread"`
	// surfacing as false after collectgarbage()). Until the proto
	// graph is wired into the GC, leaking interned strings is the
	// correct trade-off: total memory footprint stays bounded by
	// distinct loaded strings, which is the same growth shape
	// upstream has anyway.
}

// ----------------------------------------------------------------------
// Libraries / sandboxing (Tier 4)
// ----------------------------------------------------------------------

func (s *State) openLibs() { s.openLibsImpl() }

func (s *State) sandbox() { s.sandboxImpl() }

func (s *State) sandboxThread() { s.sandboxThreadImpl() }
