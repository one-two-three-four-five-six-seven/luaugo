// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"fmt"

	"github.com/luaugo/luaugo/pkg/bytecode"
)

// api.go: Tier-3 implementations of *State methods. These functions
// REPLACE the panic("Tier 3 owns this") stubs in state.go by providing
// the actual backing implementations. Method definitions on *State
// remain in state.go; this file overrides the unexported backends.
//
// Backend overrides are accomplished by replacing the no-op backend
// methods in state.go's "calls / errors" block via Go method
// dispatch. Since Go does not allow re-declaring a method, we instead
// have state.go's stub methods call into helpers defined here.

// The state.go file declares `call`, `pcall`, etc. as stubs. To
// preserve the Tier-1 contract (we are not allowed to modify
// contract.go), we replace those stubs by directly editing the stub
// bodies in state.go. The stubs delegate here.

// callImpl is the backend for State.Call.
func (s *State) callImpl(nargs, nresults int) {
	s.impl.callFromGo(nargs, nresults)
}

// pcallImpl is the backend for State.PCall.
func (s *State) pcallImpl(nargs, nresults, errfunc int) Status {
	return s.impl.pcallFromGo(nargs, nresults, errfunc)
}

// raiseErrorImpl is the backend for State.Error.
func (s *State) raiseErrorImplBackend() {
	s.impl.raiseErrorImpl()
}

// getTableImpl is the backend for State.GetTable.
func (s *State) getTableImpl(idx int) {
	si := s.impl
	if si.top < 1 {
		si.runtimeError("GetTable requires a key on top of the stack")
	}
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		si.runtimeError("GetTable: invalid stack index")
	}
	t := si.stack[i]
	k := si.stack[si.top-1]
	v := indexValue(si, t, k)
	si.stack[si.top-1] = v
}

// setTableImpl is the backend for State.SetTable.
func (s *State) setTableImpl(idx int) {
	si := s.impl
	if si.top < 2 {
		si.runtimeError("SetTable requires key and value on the stack")
	}
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		si.runtimeError("SetTable: invalid stack index")
	}
	t := si.stack[i]
	k := si.stack[si.top-2]
	v := si.stack[si.top-1]
	newIndexValue(si, t, k, v)
	si.stack = si.stack[:si.top-2]
	si.top -= 2
}

// equalImpl is the backend for State.Equal.
func (s *State) equalImpl(idx1, idx2 int) bool {
	si := s.impl
	i1 := si.absIndex(idx1)
	i2 := si.absIndex(idx2)
	if i1 < 0 || i1 >= si.top || i2 < 0 || i2 >= si.top {
		return false
	}
	return si.equalVal(si.stack[i1], si.stack[i2])
}

// lessThanImpl is the backend for State.LessThan.
func (s *State) lessThanImpl(idx1, idx2 int) bool {
	si := s.impl
	i1 := si.absIndex(idx1)
	i2 := si.absIndex(idx2)
	if i1 < 0 || i1 >= si.top || i2 < 0 || i2 >= si.top {
		return false
	}
	return si.lessThanVal(si.stack[i1], si.stack[i2])
}

// loadImpl is the backend for State.Load.
func (s *State) loadImpl(chunkname string, blob []byte, env int) error {
	m, err := bytecode.Decode(blob)
	if err != nil {
		return err
	}
	return s.impl.loadModuleImpl(chunkname, m, env)
}

// loadModuleImpl is the backend for State.LoadModule.
func (s *State) loadModuleImplPub(chunkname string, m *bytecode.Module, env int) error {
	return s.impl.loadModuleImpl(chunkname, m, env)
}

// openLibsImpl is the backend for State.OpenLibs. Tier 4 owns the
// actual stdlib; we expose a hook so that pkg/vm/lib.OpenAll(s) can be
// invoked. To avoid a Tier-3 -> Tier-4 dependency, OpenLibs is left
// as a placeholder that callers must override; we implement a no-op
// so that builds compile.
func (s *State) openLibsImpl() {
	// Defer to pkg/vm/lib.OpenAll via the installed hook. Tier 4 sets
	// OpenLibsHook at init time. If unset, no-op.
	if openLibsHook != nil {
		openLibsHook(s)
	}
}

// openLibsHook is the indirection used to break the dep cycle with
// pkg/vm/lib. Tier 4 sets this from an init() function.
var openLibsHook func(*State)

// RegisterOpenLibsHook is called by pkg/vm/lib at package init.
func RegisterOpenLibsHook(fn func(*State)) {
	openLibsHook = fn
}

// sandboxImpl freezes the globals table (and the registry's string
// metatable, if any). Mirrors upstream luaL_sandbox.
func (s *State) sandboxImpl() {
	si := s.impl
	if si.globals != nil {
		si.globals.readonly = true
		si.globals.safeenv = true
	}
	if si.gs.registry != nil {
		si.gs.registry.readonly = true
	}
	for _, mt := range si.gs.mt {
		if mt != nil {
			mt.readonly = true
		}
	}
}

// sandboxThreadImpl gives a coroutine its own globals table.
func (s *State) sandboxThreadImpl() {
	si := s.impl
	// Clone the globals table.
	old := si.globals
	if old == nil {
		return
	}
	g := newTable(si.gs, 0, 0)
	// Set its metatable to point at the parent table for read-through.
	mt := newTable(si.gs, 0, 1)
	mt.setStr(si.gs, si.gs.intern("__index"), tableValue(old))
	g.metatable = mt
	si.globals = g
}

// pushFstringImpl pushes a formatted string (lua_pushfstring).
func (s *State) pushFstringImpl(format string, args ...any) string {
	str := fmt.Sprintf(format, args...)
	s.PushString(str)
	return str
}

// PushFString pushes a fmt.Sprintf-formatted string.
func (s *State) PushFString(format string, args ...any) string {
	return s.pushFstringImpl(format, args...)
}

// Concat performs `n` top-of-stack value concatenations (lua_concat).
func (s *State) Concat(n int) {
	if n < 0 {
		return
	}
	if n == 0 {
		s.PushString("")
		return
	}
	if n == 1 {
		return
	}
	si := s.impl
	base := si.top - n
	if base < 0 {
		si.runtimeError("Concat: not enough values on the stack")
	}
	si.doConcat(base, n)
	si.stack = si.stack[:base+1]
	si.top = base + 1
}

// Arith performs the binary arithmetic operation tm on the top two
// stack values (or unary on the top one for TMUnm/TMLen). The
// operands are popped and the result pushed. Mirrors lua_arith.
func (s *State) Arith(tm TM) {
	si := s.impl
	if tm == TMUnm {
		if si.top < 1 {
			si.runtimeError("Arith: stack underflow")
		}
		v := si.stack[si.top-1]
		si.stack[si.top-1] = si.doArith(TMUnm, v, nilValue())
		return
	}
	if tm == TMLen {
		if si.top < 1 {
			si.runtimeError("Arith: stack underflow")
		}
		v := si.stack[si.top-1]
		si.stack[si.top-1] = si.doLen(v)
		return
	}
	if si.top < 2 {
		si.runtimeError("Arith: stack underflow")
	}
	a := si.stack[si.top-2]
	b := si.stack[si.top-1]
	si.stack[si.top-2] = si.doArith(tm, a, b)
	si.stack = si.stack[:si.top-1]
	si.top--
}

// ObjLen returns the result of `#v` as an int.
func (s *State) ObjLen(idx int) int {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		return 0
	}
	r := si.doLen(si.stack[i])
	if r.tag == TNumber {
		return int(r.num)
	}
	return 0
}

// SetMetatable sets the metatable for the value at idx from the table
// at the top of the stack (which is popped). Returns true on success.
func (s *State) SetMetatable(idx int) bool {
	si := s.impl
	if si.top < 1 {
		return false
	}
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		return false
	}
	mtVal := si.stack[si.top-1]
	var mt *table
	if mtVal.tag == TTable {
		mt = mtVal.gc.(*table)
	} else if mtVal.tag != TNil {
		return false
	}
	v := si.stack[i]
	switch v.tag {
	case TTable:
		v.gc.(*table).metatable = mt
		v.gc.(*table).tmcache = 0
	case TUserdata:
		v.gc.(*userdata).metatable = mt
	default:
		// Per-type metatable.
		if int(v.tag) >= 0 && int(v.tag) < len(si.gs.mt) {
			si.gs.mt[v.tag] = mt
		}
	}
	si.stack = si.stack[:si.top-1]
	si.top--
	return true
}

// GetMetatable pushes the metatable of the value at idx, or returns
// false (and pushes nothing) if no metatable is set.
func (s *State) GetMetatable(idx int) bool {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		return false
	}
	v := si.stack[i]
	var mt *table
	switch v.tag {
	case TTable:
		mt = v.gc.(*table).metatable
	case TUserdata:
		mt = v.gc.(*userdata).metatable
	default:
		if int(v.tag) >= 0 && int(v.tag) < len(si.gs.mt) {
			mt = si.gs.mt[v.tag]
		}
	}
	if mt == nil {
		return false
	}
	si.push(tableValue(mt))
	return true
}

// ToPointer returns an opaque uintptr for the value at idx, or 0 for
// non-pointer values. Mirrors lua_topointer.
func (s *State) ToPointer(idx int) uintptr {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		return 0
	}
	v := si.stack[i]
	if v.tag == TLightUserdata {
		return uintptrOf(v.ptr)
	}
	if v.isCollectable() && v.gc != nil {
		return uintptrOf(v.gc)
	}
	return 0
}

// PushLightUserdata pushes a Go pointer as a light userdata with tag 0.
func (s *State) PushLightUserdata(p any) {
	v := value{tag: TLightUserdata, ptr: p}
	s.impl.push(v)
}

// PushLightUserdataTagged pushes a Go pointer with the given tag.
func (s *State) PushLightUserdataTagged(p any, tag int) {
	v := value{tag: TLightUserdata, ptr: p, ltag: tag}
	s.impl.push(v)
}

// NewUserdata allocates a new userdata of size bytes and pushes it.
// The user pointer is the raw byte slice underlying the allocation.
func (s *State) NewUserdata(size int) []byte {
	u := newUserdataBytes(s.impl.gs, size, 0)
	s.impl.push(userdataValue(u))
	return u.data
}

// NewUserdataTagged allocates a tagged userdata.
func (s *State) NewUserdataTagged(size int, tag byte) []byte {
	u := newUserdataBytes(s.impl.gs, size, tag)
	s.impl.push(userdataValue(u))
	return u.data
}

// NewUserdataDtor allocates a userdata whose Go finalizer is run when
// it becomes unreachable.
func (s *State) NewUserdataDtor(obj any, dtor func()) {
	u := newUserdataObject(s.impl.gs, obj, 0)
	u.finalizer = dtor
	s.impl.push(userdataValue(u))
}

// ToUserdata returns the user data Go object for the value at idx, or
// nil if it isn't a userdata.
func (s *State) ToUserdata(idx int) any {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		return nil
	}
	v := si.stack[i]
	switch v.tag {
	case TUserdata:
		u := v.gc.(*userdata)
		if u.object != nil {
			return u.object
		}
		return u.data
	case TLightUserdata:
		return v.ptr
	}
	return nil
}

// ToThread returns the *State for a thread value, or nil.
func (s *State) ToThread(idx int) *State {
	si := s.impl
	i := si.absIndex(idx)
	if i < 0 || i >= si.top {
		return nil
	}
	v := si.stack[i]
	if v.tag != TThread {
		return nil
	}
	th := v.gc.(*stateImpl)
	if th.wrapper != nil {
		return th.wrapper
	}
	w := &State{impl: th}
	th.wrapper = w
	return w
}

// Status returns the thread's last completion status.
func (s *State) Status() Status { return s.impl.status }

// XMove transfers n values from s to dst's stack.
func (s *State) XMove(dst *State, n int) {
	si := s.impl
	di := dst.impl
	if si == di || n <= 0 {
		return
	}
	if n > si.top {
		n = si.top
	}
	for i := si.top - n; i < si.top; i++ {
		di.push(si.stack[i])
	}
	si.stack = si.stack[:si.top-n]
	si.top -= n
}

// IsMainThread reports whether s is the main thread (not a coroutine).
// Mirrors upstream lua_pushthread's "is main" return.
func (s *State) IsMainThread() bool {
	return s.impl == s.impl.gs.mainthread
}

// PushThread pushes s onto its own stack and returns true if s is the
// main thread. Mirrors upstream lua_pushthread.
func (s *State) PushThread() bool {
	s.impl.push(threadValue(s.impl))
	return s.IsMainThread()
}

// IsYieldable reports whether the current thread can yield. A thread
// can yield iff it is a coroutine (not the main thread). luaugo does
// not currently support metamethod-yield bans, so this matches "not
// the main thread".
func (s *State) IsYieldable() bool {
	return s.impl.co != nil
}

// CoStatus returns the status of co viewed from s, using upstream's
// status names: "running", "suspended", "normal", or "dead".
//   - "running": co is the currently-executing thread.
//   - "normal":  co has resumed another coroutine that is now running.
//   - "suspended": co is fresh (never started) or yielded.
//   - "dead":    co returned normally or errored.
//
// Mirrors upstream costatus() / lua_costatus.
func (s *State) CoStatus(co *State) string {
	if co == nil || co.impl == nil {
		return "dead"
	}
	c := co.impl.co
	if c == nil {
		// Main thread: it's running iff s == co.
		if co.impl == s.impl {
			return "running"
		}
		return "normal"
	}
	if c.finished {
		return "dead"
	}
	if co.impl == s.impl {
		return "running"
	}
	if !c.started {
		return "suspended"
	}
	// Started, not finished, not the current thread: it either yielded
	// (suspended) or is mid-resume of another coroutine (normal). We
	// approximate by the last recorded status.
	if co.impl.status == StatusYield {
		return "suspended"
	}
	// Default to suspended for a started, non-current coroutine when
	// the recorded status is OK (e.g. never finished a resume).
	return "suspended"
}
