// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

// Incremental tri-colour mark-and-sweep collector, mirroring the
// invariants of upstream lgc.cpp:
//
//   - Every Lua-visible heap object implements gcObject and lives on
//     the global `allgc` doubly-linked list. The Go GC will not free it
//     while it is on this list because the list itself is reachable.
//   - The collector cycles between two "white" colours so an object
//     allocated mid-cycle is recognisable as fresh and not swept this
//     round.
//   - A *forward* write barrier is invoked whenever a black object's
//     reference field is overwritten with a white child: the child is
//     promoted to grey so the invariant "black never points to white"
//     is preserved.
//   - The collector marches through five states (pause, propagate,
//     propagateagain, atomic, sweep). Step granularity is bounded by
//     `gcStepSize` so a single call doesn't pause for the whole heap.

// GC colour bits (lgc.h layout).
const (
	gcWhite0Bit = 1 << 0
	gcWhite1Bit = 1 << 1
	gcBlackBit  = 1 << 2
	gcFixedBit  = 1 << 3

	gcWhiteBits = gcWhite0Bit | gcWhite1Bit
)

// GC states.
const (
	gcPause int8 = iota
	gcPropagate
	gcPropagateAgain
	gcAtomic
	gcSweep
)

// Default GC tunables (lgc.h LUAI_GC*).
const (
	gcDefaultGoal     = 200 // percent
	gcDefaultStepMul  = 200 // percent
	gcDefaultStepSize = 1024
)

// gcObject is implemented by every type the collector can manage.
// Concrete implementors embed gcHeader to satisfy this interface.
type gcObject interface {
	gcHead() *gcHeader
}

// gcHeader is the per-object bookkeeping that every collectable type
// embeds. It corresponds to upstream's CommonHeader (tt, marked, memcat)
// plus the all-objects doubly linked list that we use in place of
// upstream's page allocator.
type gcHeader struct {
	tt     Type // value type (kept in sync with the value carrier's tag)
	marked uint8

	// Doubly-linked list intrusive pointers. The owning global state's
	// `allgc` list is rooted at globalState.allgc; the list is circular
	// in neither direction (NULL terminates).
	prev gcObject
	next gcObject

	// Greyed-list link used while this object is on the gray, gray2,
	// or weak list during a collection cycle.
	graylink gcObject

	// Approximate size charged to the GC accounting when this object
	// is freed. Set at allocation time.
	size int
}

func (h *gcHeader) gcHead() *gcHeader { return h }

// gcInit initialises a freshly-allocated GC object: it is born the
// "current" white colour, has its tag set, is appended to allgc, and
// its size is added to the heap budget.
func (g *globalState) gcInit(o gcObject, tt Type, size int) {
	h := o.gcHead()
	h.tt = tt
	h.marked = g.currentWhite & gcWhiteBits
	h.size = size

	// Append to allgc head.
	h.prev = nil
	h.next = g.allgc
	if g.allgc != nil {
		g.allgc.gcHead().prev = o
	}
	g.allgc = o

	g.allocBytes(size)
}

// gcLink removes o from the allgc list. Only called by the sweep phase.
func (g *globalState) gcUnlink(o gcObject) {
	h := o.gcHead()
	if h.prev != nil {
		h.prev.gcHead().next = h.next
	} else {
		g.allgc = h.next
	}
	if h.next != nil {
		h.next.gcHead().prev = h.prev
	}
	h.prev = nil
	h.next = nil
}

func isWhite(o gcObject) bool { return o.gcHead().marked&gcWhiteBits != 0 }
func isBlack(o gcObject) bool { return o.gcHead().marked&gcBlackBit != 0 }
func isGray(o gcObject) bool  { return o.gcHead().marked&(gcWhiteBits|gcBlackBit) == 0 }
func isFixed(o gcObject) bool { return o.gcHead().marked&gcFixedBit != 0 }

func otherWhite(g *globalState) uint8 { return (g.currentWhite ^ gcWhiteBits) & gcWhiteBits }

// isDead reports whether the value at o has been condemned by the
// collector in the current cycle (its colour is the "other" white).
func isDead(g *globalState, o gcObject) bool {
	return o.gcHead().marked&(gcWhiteBits|gcFixedBit) == otherWhite(g)
}

// makeGray transitions o from white to gray and pushes it onto g.gray.
// Already-gray and already-black objects are left alone.
func (g *globalState) makeGray(o gcObject) {
	h := o.gcHead()
	if h.marked&gcWhiteBits == 0 {
		return
	}
	// Clear white bits; do NOT set black yet.
	h.marked &^= gcWhiteBits
	h.graylink = g.gray
	g.gray = o
}

// markValue marks the GC payload of v (if any).
func (g *globalState) markValue(v value) {
	if v.isCollectable() && v.gc != nil {
		g.markObject(v.gc)
	}
}

// markObject promotes o to gray if it was white.
func (g *globalState) markObject(o gcObject) {
	if o == nil {
		return
	}
	g.makeGray(o)
}

// barrier is the forward write barrier. Call it AFTER assigning child
// into a reference field of parent. If parent is already black and
// child is still white, the invariant requires we re-grey parent (or
// promote child). Following upstream luaC_barrierf we promote the
// child: that prevents repeated barrier hits when the same parent is
// mutated many times.
func (g *globalState) barrier(parent, child gcObject) {
	if parent == nil || child == nil {
		return
	}
	if !isBlack(parent) || !isWhite(child) {
		return
	}
	// Promote child to gray. We must NOT do this if the collector has
	// already passed the propagate phase: during sweep there is no
	// invariant to preserve and the grey list is dormant.
	if g.gcstate == gcPropagate || g.gcstate == gcPropagateAgain || g.gcstate == gcAtomic {
		g.makeGray(child)
	}
}

// barrierTable is the back barrier for tables: we revert the table to
// grey so it gets re-traversed before the cycle ends, matching upstream
// luaC_barrierback (which uses the back barrier for performance: tables
// are mutated more often than other objects).
func (g *globalState) barrierTable(t *table, child gcObject) {
	if t == nil || child == nil {
		return
	}
	if !isBlack(t) || !isWhite(child) {
		return
	}
	if g.gcstate == gcPropagate || g.gcstate == gcPropagateAgain || g.gcstate == gcAtomic {
		// Demote table back to gray and re-queue.
		h := t.gcHead()
		h.marked &^= gcBlackBit
		h.graylink = g.grayAgain
		g.grayAgain = t
	}
}

// -- traversal ----------------------------------------------------------

// propagateMark dequeues a single grey object, marks each child grey,
// then turns the object black. Returns the approximate byte cost of
// the work performed so the step loop can amortise.
func (g *globalState) propagateMark() int {
	o := g.gray
	if o == nil {
		return 0
	}
	h := o.gcHead()
	g.gray = h.graylink
	h.graylink = nil

	switch obj := o.(type) {
	case *tString:
		// Strings have no children. Just blacken.
	case *table:
		g.traverseTable(obj)
	case *closure:
		g.traverseClosure(obj)
	case *upVal:
		if obj.closed {
			g.markValue(obj.value)
		}
		// open upvals point into a stack slot; the thread roots that.
	case *userdata:
		if obj.metatable != nil {
			g.markObject(obj.metatable)
		}
		if obj.userValue.isCollectable() && obj.userValue.gc != nil {
			g.markObject(obj.userValue.gc)
		}
	case *buffer:
		// no GC children
	case *stateImpl:
		g.traverseThread(obj)
	}

	// Blacken.
	h.marked |= gcBlackBit
	return h.size
}

func (g *globalState) traverseTable(t *table) {
	// Inspect __mode for weak semantics.
	mode := tableMode(t)
	if mode != "" {
		// Defer to atomic clear pass: enqueue onto weak list and stay
		// grey. Marking the keys/values is conditional on the mode.
		t.weakMode = mode
		t.gcHead().graylink = g.weak
		g.weak = t
		// Mark only the side that is NOT weak.
		weakK := containsByte(mode, 'k')
		weakV := containsByte(mode, 'v')
		if t.metatable != nil {
			g.markObject(t.metatable)
		}
		for i := range t.array {
			if !weakV {
				g.markValue(t.array[i])
			}
		}
		for i := range t.nodes {
			n := &t.nodes[i]
			if n.key.tag == TNil || n.val.tag == TNil {
				continue
			}
			if !weakK {
				g.markValue(n.key)
			}
			if !weakV {
				g.markValue(n.val)
			}
		}
		// Re-grey: don't blacken — atomic will revisit.
		t.gcHead().marked &^= gcBlackBit
		return
	}
	if t.metatable != nil {
		g.markObject(t.metatable)
	}
	for i := range t.array {
		g.markValue(t.array[i])
	}
	for i := range t.nodes {
		n := &t.nodes[i]
		g.markValue(n.key)
		g.markValue(n.val)
	}
}

func tableMode(t *table) string {
	if t.metatable == nil {
		return ""
	}
	v, _ := t.metatable.getStr(t.metatable.intern("__mode"))
	s, ok := v.asString()
	if !ok {
		return ""
	}
	return s
}

func containsByte(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}

func (g *globalState) traverseClosure(c *closure) {
	if c.env != nil {
		g.markObject(c.env)
	}
	for i := range c.upvals {
		g.markValue(c.upvals[i])
	}
	for i := range c.upvalRefs {
		if c.upvalRefs[i] != nil {
			g.markObject(c.upvalRefs[i])
		}
	}
}

func (g *globalState) traverseThread(th *stateImpl) {
	// Mark globals.
	if th.globals != nil {
		g.markObject(th.globals)
	}
	// Mark the live portion of the stack.
	for i := 0; i < th.top; i++ {
		g.markValue(th.stack[i])
	}
	// Mark open upvalues.
	uv := th.openUpvals
	for uv != nil {
		g.markObject(uv)
		uv = uv.openNext
	}
}

// markRoots seeds the grey list with everything reachable from the
// roots. Called once when transitioning pause -> propagate.
func (g *globalState) markRoots() {
	g.gray = nil
	g.grayAgain = nil
	g.weak = nil

	if g.mainthread != nil {
		g.markObject(g.mainthread)
	}
	if g.registry != nil {
		g.markObject(g.registry)
	}
	for _, mt := range g.mt {
		if mt != nil {
			g.markObject(mt)
		}
	}
	for _, ts := range g.tmname {
		if ts != nil {
			g.markObject(ts)
		}
	}
	for _, ts := range g.ttname {
		if ts != nil {
			g.markObject(ts)
		}
	}
}

// atomic does the work that must happen with no mutator interference:
// drain the grey list, then drain grayAgain, then clear weak slots,
// then flip the white colour for the next cycle.
func (g *globalState) atomic() {
	for g.gray != nil {
		g.propagateMark()
	}
	// Now process grayAgain (objects re-greyed during propagate via
	// the back barrier).
	g.gray = g.grayAgain
	g.grayAgain = nil
	for g.gray != nil {
		g.propagateMark()
	}

	// Clear unreachable entries from weak tables.
	g.clearWeak()

	// Flip white.
	g.currentWhite = otherWhite(g)
}

func (g *globalState) clearWeak() {
	for o := g.weak; o != nil; {
		next := o.gcHead().graylink
		o.gcHead().graylink = nil

		t, ok := o.(*table)
		if !ok {
			o = next
			continue
		}
		weakK := containsByte(t.weakMode, 'k')
		weakV := containsByte(t.weakMode, 'v')

		for i := range t.array {
			v := t.array[i]
			if weakV && v.isCollectable() && v.gc != nil && isWhite(v.gc) {
				t.array[i] = nilValue()
			}
		}
		for i := range t.nodes {
			n := &t.nodes[i]
			if n.key.tag == TNil || n.val.tag == TNil {
				continue
			}
			drop := false
			if weakK && n.key.isCollectable() && n.key.gc != nil && isWhite(n.key.gc) {
				drop = true
			}
			if weakV && n.val.isCollectable() && n.val.gc != nil && isWhite(n.val.gc) {
				drop = true
			}
			if drop {
				n.val = nilValue()
			}
		}
		o = next
	}
	g.weak = nil
}

// sweepStep walks at most `budget` allgc entries, freeing those that
// are dead (other-white and not fixed) and recolouring survivors to the
// new current white. Returns the number of entries inspected.
func (g *globalState) sweepStep(budget int) int {
	n := 0
	o := g.sweepCursor
	for o != nil && n < budget {
		next := o.gcHead().next
		if isDead(g, o) {
			// Userdata with __gc: enqueue for finalisation (we run it
			// immediately during sweep, since this is a Tier-2 shim).
			if u, ok := o.(*userdata); ok && u.finalizer != nil {
				u.finalizer()
				u.finalizer = nil
			}
			g.freeBytes(o.gcHead().size)
			g.gcUnlink(o)
		} else {
			// Recolour to current white.
			h := o.gcHead()
			h.marked &^= gcWhiteBits | gcBlackBit
			h.marked |= g.currentWhite & gcWhiteBits
		}
		o = next
		n++
	}
	g.sweepCursor = o
	return n
}

// step advances the GC by approximately `work` units. Returns true if
// the current cycle finished.
//
// When work is 0 the call is treated as a "throttled tick": we only
// advance the collector if memory pressure (totalBytes) has crossed
// the configured threshold. This keeps tight per-iteration call sites
// (NEWCLOSURE, FORNLOOP, ...) cheap when the program isn't actually
// allocating GC objects, while still driving the collector forward
// when long-running scripts grow the heap.
func (g *globalState) gcStep(work int) bool {
	if work <= 0 {
		// Pause when we're idle and below the trigger.
		if g.gcstate == gcPause && g.totalBytes < g.gcThreshold {
			return false
		}
		work = gcDefaultStepSize
	}
	switch g.gcstate {
	case gcPause:
		g.markRoots()
		g.gcstate = gcPropagate
		return false
	case gcPropagate:
		for work > 0 && g.gray != nil {
			work -= g.propagateMark()
		}
		if g.gray == nil {
			g.gcstate = gcPropagateAgain
		}
		return false
	case gcPropagateAgain:
		// Re-mark the main thread to capture stack mutations during
		// propagate.
		if g.mainthread != nil {
			g.makeGray(g.mainthread)
		}
		for work > 0 && g.gray != nil {
			work -= g.propagateMark()
		}
		g.gcstate = gcAtomic
		return false
	case gcAtomic:
		g.atomic()
		g.sweepCursor = g.allgc
		g.gcstate = gcSweep
		return false
	case gcSweep:
		// Each sweep step inspects up to (work / 32) objects.
		inspected := g.sweepStep(work/32 + 1)
		_ = inspected
		if g.sweepCursor == nil {
			g.gcstate = gcPause
			// Set new GC threshold: a fraction over current size.
			g.gcThreshold = g.totalBytes + uint64(int(g.totalBytes)*g.gcStepMul/100)
			if g.gcThreshold < gcDefaultStepSize {
				g.gcThreshold = gcDefaultStepSize
			}
			return true
		}
		return false
	}
	return false
}

// fullGC runs a complete cycle synchronously.
func (g *globalState) fullGC() {
	// Finish any in-progress cycle.
	for g.gcstate != gcPause {
		g.gcStep(1 << 30)
	}
	// Start and run a fresh one.
	for !g.gcStep(1 << 30) {
	}
}
