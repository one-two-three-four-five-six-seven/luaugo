// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

// Package vm is the Luau virtual machine: state, garbage collector,
// bytecode loader, interpreter, and Lua C API equivalents. luaugo's API
// uses Go-idiomatic naming, but the semantics mirror upstream Luau
// (and, transitively, Lua 5.x) exactly. See `tools/UPSTREAM_MAP.md` for
// upstream sources of truth.
package vm

import "github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"

// ----------------------------------------------------------------------
// Type tags
// ----------------------------------------------------------------------

// Type identifies the dynamic type of a Lua value. Numeric values match
// upstream enum lua_Type exactly so they can be returned from the
// Lua C API without translation.
type Type int8

const (
	// TNone is the sentinel returned for indices that refer to no value
	// (for example, beyond the top of the stack). It is *not* a valid
	// type for a value held in memory.
	TNone Type = -1

	TNil           Type = 0 // must be 0 (used by isnoneornil)
	TBoolean       Type = 1 // must be 1 (used by isfalse)
	TLightUserdata Type = 2
	TNumber        Type = 3
	TInteger       Type = 4
	TVector        Type = 5
	TString        Type = 6 // last value type; all types above this are GC types

	TTable    Type = 7
	TFunction Type = 8
	TUserdata Type = 9
	TThread   Type = 10
	TBuffer   Type = 11

	// TProto / TUpval / TDeadKey are internal types not exposed to Lua
	// code; they are never the result of type()/typeof().
	TProto   Type = 12
	TUpval   Type = 13
	TDeadKey Type = 14
)

// String returns the lower-case type name used by Lua's `type()` and
// `typeof()` for v.
func (t Type) String() string {
	switch t {
	case TNone:
		return "no value"
	case TNil:
		return "nil"
	case TBoolean:
		return "boolean"
	case TLightUserdata, TUserdata:
		return "userdata"
	case TNumber, TInteger:
		return "number"
	case TVector:
		return "vector"
	case TString:
		return "string"
	case TTable:
		return "table"
	case TFunction:
		return "function"
	case TThread:
		return "thread"
	case TBuffer:
		return "buffer"
	}
	return "unknown"
}

// IsGC reports whether values of type t are garbage-collected.
// Mirrors upstream's `iscollectable(o)` macro (`tt >= LUA_TSTRING`),
// which means strings ARE collectable.
func (t Type) IsGC() bool { return t >= TString }

// ----------------------------------------------------------------------
// Status codes
// ----------------------------------------------------------------------

// Status is the result of a protected call or resume. Values match
// upstream enum thread_status.
type Status int8

const (
	StatusOK         Status = 0 // call completed successfully
	StatusYield      Status = 1 // call suspended via coroutine.yield
	StatusErrRun     Status = 2 // runtime error
	StatusErrSyntax  Status = 3 // syntax error during loading (unused; load returns own error)
	StatusErrMem     Status = 4 // memory allocation error (unused in Go; included for API compat)
	StatusErrErr     Status = 5 // error in error-handling function
	StatusBreak      Status = 6 // hit a debugger breakpoint
)

// ----------------------------------------------------------------------
// Error reporting
// ----------------------------------------------------------------------

// Error is the interface implemented by Lua-visible errors. Error
// values flow through pcall/xpcall without going through Go's panic /
// recover machinery.
type Error interface {
	error
	// LuaValue returns the value passed to `error(...)`. It may be a
	// string, a table, or any other Lua value.
	LuaValue() any
}

// ----------------------------------------------------------------------
// State
// ----------------------------------------------------------------------

// State is a single Lua thread plus its shared global state. Multiple
// State values may share a single global state when they represent
// coroutines of the same Lua VM.
type State struct {
	// Concrete fields are filled in by Tier 2 implementation; this
	// contract intentionally exposes State by reference only.
	impl *stateImpl
}

// NewState creates a fresh Lua VM with an empty global table and no
// standard libraries opened. Call vm.OpenLibs (or individual
// pkg/vm/lib.OpenX functions) to populate the standard library.
func NewState() *State { return newState() }

// Close releases resources held by the state. After Close the state
// must not be used.
func (s *State) Close() { s.close() }

// ----------------------------------------------------------------------
// Stack manipulation (Lua C API equivalents)
// ----------------------------------------------------------------------

// Top returns the index of the topmost stack slot. Returns 0 when the
// stack is empty.
func (s *State) Top() int { return s.top() }

// SetTop sets the stack pointer to idx, filling new slots with nil if
// idx > current top.
func (s *State) SetTop(idx int) { s.setTop(idx) }

// Pop removes n values from the top of the stack.
func (s *State) Pop(n int) { s.SetTop(s.Top() - n) }

// CheckStack ensures the stack has room for at least n more slots.
func (s *State) CheckStack(n int) bool { return s.checkStack(n) }

// Type returns the type of the value at idx, or TNone if idx is empty.
func (s *State) Type(idx int) Type { return s.typeAt(idx) }

// PushNil pushes a nil value.
func (s *State) PushNil() { s.pushNil() }

// PushBoolean pushes a boolean.
func (s *State) PushBoolean(v bool) { s.pushBoolean(v) }

// PushNumber pushes a float64 number.
func (s *State) PushNumber(v float64) { s.pushNumber(v) }

// PushInteger pushes an int64 integer.
func (s *State) PushInteger(v int64) { s.pushInteger(v) }

// PushString pushes a string.
func (s *State) PushString(v string) { s.pushString(v) }

// PushVector pushes a vector value. w is ignored unless the VM is
// configured for 4-wide vectors.
func (s *State) PushVector(x, y, z, w float32) { s.pushVector(x, y, z, w) }

// PushGoFunction pushes a Go function as a Lua callable.
type GoFunction func(s *State) int

// PushGoFunction pushes fn as a Lua function. n is the number of
// upvalues captured from the top of the stack.
func (s *State) PushGoFunction(fn GoFunction, n int) { s.pushGoFunction(fn, n) }

// PushValue pushes a copy of the value at idx onto the top of the stack.
func (s *State) PushValue(idx int) { s.pushValue(idx) }

// ToBoolean returns the truthiness of the value at idx.
func (s *State) ToBoolean(idx int) bool { return s.toBoolean(idx) }

// ToNumber returns the value at idx as a float64. The second return is
// true iff the value could be coerced to a number.
func (s *State) ToNumber(idx int) (float64, bool) { return s.toNumber(idx) }

// ToInteger returns the value at idx as an int64.
func (s *State) ToInteger(idx int) (int64, bool) { return s.toInteger(idx) }

// ToString returns the value at idx as a string. Numbers and integers
// are coerced; other types yield ("", false).
func (s *State) ToString(idx int) (string, bool) { return s.toString(idx) }

// ToVector returns the value at idx as a 4-tuple of float32.
func (s *State) ToVector(idx int) (x, y, z, w float32, ok bool) { return s.toVector(idx) }

// IsNil reports whether the value at idx is nil.
func (s *State) IsNil(idx int) bool { return s.Type(idx) == TNil }

// IsNoneOrNil reports whether idx is empty or holds nil.
func (s *State) IsNoneOrNil(idx int) bool {
	t := s.Type(idx)
	return t == TNone || t == TNil
}

// IsString reports whether the value at idx is a string (or coercible).
func (s *State) IsString(idx int) bool { return s.isString(idx) }

// IsNumber reports whether the value at idx is a number or integer
// (or coercible from a string).
func (s *State) IsNumber(idx int) bool { return s.isNumber(idx) }

// IsTable reports whether the value at idx is a table.
func (s *State) IsTable(idx int) bool { return s.Type(idx) == TTable }

// IsFunction reports whether the value at idx is a function.
func (s *State) IsFunction(idx int) bool { return s.Type(idx) == TFunction }

// Remove removes the element at idx, shifting later elements down.
func (s *State) Remove(idx int) { s.remove(idx) }

// Insert moves the top element to idx, shifting later elements up.
func (s *State) Insert(idx int) { s.insert(idx) }

// Replace replaces the value at idx with the top element (which is
// popped).
func (s *State) Replace(idx int) { s.replace(idx) }

// ----------------------------------------------------------------------
// Table operations
// ----------------------------------------------------------------------

// NewTable pushes a new empty table.
func (s *State) NewTable() { s.newTable(0, 0) }

// CreateTable pushes a new table with preallocated array and hash parts.
func (s *State) CreateTable(narr, nrec int) { s.newTable(narr, nrec) }

// GetTable performs t[k] where t is at idx and k is at the top of the
// stack; result replaces k.
func (s *State) GetTable(idx int) { s.getTable(idx) }

// SetTable performs t[k]=v where t is at idx, k and v are the top two
// stack slots, both of which are popped.
func (s *State) SetTable(idx int) { s.setTable(idx) }

// GetField pushes t[name] where t is at idx.
func (s *State) GetField(idx int, name string) { s.getField(idx, name) }

// SetField sets t[name]=v where t is at idx and v is the top of the
// stack (which is popped).
func (s *State) SetField(idx int, name string) { s.setField(idx, name) }

// RawGet performs t[k] without invoking metamethods.
func (s *State) RawGet(idx int) { s.rawGet(idx) }

// RawSet performs t[k]=v without invoking metamethods.
func (s *State) RawSet(idx int) { s.rawSet(idx) }

// RawGetI pushes t[n] where n is an integer key, without metamethods.
func (s *State) RawGetI(idx int, n int) { s.rawGetI(idx, n) }

// RawSetI sets t[n]=v with an integer key, without metamethods. v is
// the top of the stack and is popped.
func (s *State) RawSetI(idx int, n int) { s.rawSetI(idx, n) }

// Next pops a key from the stack and pushes the next key/value pair
// from the table at idx. Returns false when iteration is complete (in
// which case nothing is pushed).
func (s *State) Next(idx int) bool { return s.next(idx) }

// Length pushes the result of `#t` where t is at idx.
func (s *State) Length(idx int) { s.length(idx) }

// RawEqual performs a raw equality test without metamethods.
func (s *State) RawEqual(idx1, idx2 int) bool { return s.rawEqual(idx1, idx2) }

// Equal performs equality including __eq metamethods.
func (s *State) Equal(idx1, idx2 int) bool { return s.equal(idx1, idx2) }

// LessThan performs <, including __lt metamethods.
func (s *State) LessThan(idx1, idx2 int) bool { return s.lessThan(idx1, idx2) }

// ----------------------------------------------------------------------
// Calls
// ----------------------------------------------------------------------

// Call calls the function at the top of the stack with nargs arguments
// pushed above it, expecting nresults return values (or -1 to keep all).
// A runtime error propagates as a Go panic; use PCall for protected
// calls.
func (s *State) Call(nargs, nresults int) { s.call(nargs, nresults) }

// PCall calls the function in protected mode. errfunc is the stack
// index of an error handler, or 0 for none. Returns a Status; on
// non-OK status the error value is on the top of the stack.
func (s *State) PCall(nargs, nresults, errfunc int) Status { return s.pcall(nargs, nresults, errfunc) }

// Error raises a Lua error with the value on top of the stack.
func (s *State) Error() { s.raiseError() }

// Errorf is a convenience that pushes fmt.Sprintf-formatted text and
// then raises an error.
func (s *State) Errorf(format string, args ...any) { s.errorf(format, args...) }

// ----------------------------------------------------------------------
// Coroutines
// ----------------------------------------------------------------------

// NewThread creates a new coroutine sharing globals with s. The new
// thread is pushed onto s's stack.
func (s *State) NewThread() *State { return s.newThread() }

// Resume runs the coroutine until it yields, returns, or errors.
// nargs values from the top of from's stack are passed to the
// coroutine. Returns the new Status. On StatusOK or StatusYield the
// returned/yielded values are on the coroutine's stack.
func (co *State) Resume(from *State, nargs int) Status { return co.resume(from, nargs) }

// Yield suspends the current coroutine, returning nresults values to
// the resumer. Must be called from within a Go function executing
// inside a coroutine.
func (s *State) Yield(nresults int) int { return s.yield(nresults) }

// ----------------------------------------------------------------------
// Loading
// ----------------------------------------------------------------------

// Load parses a bytecode blob (as produced by bytecode.Encode or
// upstream `luau --compile=binary`) and pushes the resulting closure
// onto the stack. env is the environment table index (0 for default).
func (s *State) Load(chunkname string, blob []byte, env int) error {
	return s.load(chunkname, blob, env)
}

// LoadModule loads an already-decoded bytecode.Module without going
// through serialization.
func (s *State) LoadModule(chunkname string, m *bytecode.Module, env int) error {
	return s.loadModule(chunkname, m, env)
}

// ----------------------------------------------------------------------
// Globals
// ----------------------------------------------------------------------

// GetGlobal pushes the value of the named global.
func (s *State) GetGlobal(name string) { s.getGlobal(name) }

// SetGlobal sets the named global to the top of the stack (which is
// popped).
func (s *State) SetGlobal(name string) { s.setGlobal(name) }

// Register registers a Go function as a global named name.
func (s *State) Register(name string, fn GoFunction) {
	s.PushGoFunction(fn, 0)
	s.SetGlobal(name)
}

// ----------------------------------------------------------------------
// Garbage collection
// ----------------------------------------------------------------------

// GCInfo returns the current heap size in kilobytes, matching Lua's
// `gcinfo()` builtin.
func (s *State) GCInfo() int { return s.gcInfo() }

// CollectGarbage runs a full garbage collection cycle.
func (s *State) CollectGarbage() { s.collectGarbage() }

// GCStep advances the garbage collector by approximately `work` units
// of internal work. Returns true if the current GC cycle finished
// during this step. Mirrors upstream lua_gc(LUA_GCSTEP).
func (s *State) GCStep(work int) bool { return s.impl.gs.gcStep(work) }

// SetGCStopped suspends or resumes the automatic GC stepping driven
// by allocations. Mirrors lua_gc(LUA_GCSTOP) / LUA_GCRESTART.
// CollectGarbage and explicit GCStep calls bypass this flag.
func (s *State) SetGCStopped(stopped bool) { s.impl.gs.gcStopped = stopped }

// GCStopped reports whether the GC is currently suspended. Mirrors
// lua_gc(LUA_GCISRUNNING) (inverted).
func (s *State) GCStopped() bool { return s.impl.gs.gcStopped }

// ----------------------------------------------------------------------
// Library opening
// ----------------------------------------------------------------------

// OpenLibs opens the standard library set (equivalent to upstream
// luaL_openlibs). Individual libraries can also be opened via the
// pkg/vm/lib package.
func (s *State) OpenLibs() { s.openLibs() }

// Sandbox locks down the global table so that subsequent assignments to
// builtin globals raise an error. Mirrors upstream luaL_sandbox.
func (s *State) Sandbox() { s.sandbox() }

// SandboxThread sandboxes a coroutine so it has its own globals table
// distinct from the parent state. Mirrors upstream luaL_sandboxthread.
func (s *State) SandboxThread() { s.sandboxThread() }
