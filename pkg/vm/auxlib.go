// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import "fmt"

// auxlib.go: luaL_* helpers exposed as *State methods. (Filename is
// auxlib.go rather than aux.go because "aux" is a reserved DOS device
// name on Windows.) Names use the pattern LCheckX / LOptX / LError /
// LRegister consistent with the Tier-3 contract.

// LError raises a Lua error with a formatted message prefixed by the
// caller's source location (when available).
func (s *State) LError(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	// Prefix with source location if we have a Lua frame.
	if where := s.Where(1); where != "" {
		msg = where + msg
	}
	s.impl.runtimeError(msg)
}

// LTypeError raises a type-mismatch error for argument argn.
func (s *State) LTypeError(argn int, tname string) {
	got := s.Type(argn)
	s.LError("invalid argument #%d (%s expected, got %s)", argn, tname, got.String())
}

// LArgError raises a generic argument error.
func (s *State) LArgError(argn int, extramsg string) {
	s.LError("invalid argument #%d (%s)", argn, extramsg)
}

// LCheckType verifies the value at argn has type t; raises otherwise.
func (s *State) LCheckType(argn int, t Type) {
	if s.Type(argn) != t {
		s.LTypeError(argn, t.String())
	}
}

// LCheckString returns the string value at argn or raises.
func (s *State) LCheckString(argn int) string {
	v, ok := s.ToString(argn)
	if !ok {
		s.LTypeError(argn, "string")
	}
	return v
}

// LCheckNumber returns the numeric value at argn or raises.
func (s *State) LCheckNumber(argn int) float64 {
	v, ok := s.ToNumber(argn)
	if !ok {
		s.LTypeError(argn, "number")
	}
	return v
}

// LCheckInteger returns the integer value at argn or raises.
func (s *State) LCheckInteger(argn int) int64 {
	v, ok := s.ToInteger(argn)
	if !ok {
		s.LTypeError(argn, "integer")
	}
	return v
}

// LCheckBoolean returns the boolean value at argn or raises if absent.
func (s *State) LCheckBoolean(argn int) bool {
	if s.IsNoneOrNil(argn) {
		s.LTypeError(argn, "boolean")
	}
	return s.ToBoolean(argn)
}

// LCheckUserdata returns the userdata Go value at argn with the
// expected metatable name.
func (s *State) LCheckUserdata(argn int, tname string) any {
	v := s.ToUserdata(argn)
	if v == nil {
		s.LTypeError(argn, tname)
	}
	return v
}

// LOptString returns the string at argn or def if not present.
func (s *State) LOptString(argn int, def string) string {
	if s.IsNoneOrNil(argn) {
		return def
	}
	return s.LCheckString(argn)
}

// LOptNumber returns the number at argn or def if not present.
func (s *State) LOptNumber(argn int, def float64) float64 {
	if s.IsNoneOrNil(argn) {
		return def
	}
	return s.LCheckNumber(argn)
}

// LOptInteger returns the integer at argn or def if not present.
func (s *State) LOptInteger(argn int, def int64) int64 {
	if s.IsNoneOrNil(argn) {
		return def
	}
	return s.LCheckInteger(argn)
}

// LRegister registers a list of name->GoFunction pairs as fields of
// the table at the top of the stack. The table is left on the stack.
func (s *State) LRegister(funcs map[string]GoFunction) {
	if s.Top() == 0 || s.Type(-1) != TTable {
		s.LError("LRegister requires a table on top of the stack")
	}
	for name, fn := range funcs {
		s.PushGoFunction(fn, 0)
		s.SetField(-2, name)
	}
}

// LRegisterList is like LRegister but preserves order via a slice.
type LFnEntry struct {
	Name string
	Fn   GoFunction
}

// LRegisterList registers entries in order.
func (s *State) LRegisterList(entries []LFnEntry) {
	if s.Top() == 0 || s.Type(-1) != TTable {
		s.LError("LRegisterList requires a table on top of the stack")
	}
	for _, e := range entries {
		s.PushGoFunction(e.Fn, 0)
		s.SetField(-2, e.Name)
	}
}

// LFindTable navigates a dotted path under the table at idx, creating
// any missing subtables. Returns "" on success or the first segment
// that already exists as a non-table.
func (s *State) LFindTable(idx int, fname string, sizehint int) string {
	s.PushValue(idx)
	start := 0
	for i := 0; i <= len(fname); i++ {
		if i == len(fname) || fname[i] == '.' {
			seg := fname[start:i]
			s.PushString(seg)
			s.RawGet(-2)
			if s.IsNil(-1) {
				s.Pop(1)
				s.CreateTable(0, sizehint)
				s.PushString(seg)
				s.PushValue(-2)
				s.RawSet(-4)
			} else if !s.IsTable(-1) {
				s.Pop(2)
				return seg
			}
			s.Remove(-2)
			start = i + 1
		}
	}
	return ""
}

// LNewMetatable creates a new metatable in the registry under `name`.
// Returns true if the metatable was created (first time), false if it
// already existed. The metatable is left on the stack.
func (s *State) LNewMetatable(name string) bool {
	s.GetRegistryField(name)
	if !s.IsNil(-1) {
		return false
	}
	s.Pop(1)
	s.NewTable()
	s.PushValue(-1)
	s.SetRegistryField(name)
	return true
}

// LGetMetafield pushes mt[event] for the value at idx and returns true,
// or returns false (no push) if absent.
func (s *State) LGetMetafield(idx int, event string) bool {
	if !s.GetMetatable(idx) {
		return false
	}
	s.PushString(event)
	s.RawGet(-2)
	if s.IsNil(-1) {
		s.Pop(2)
		return false
	}
	s.Remove(-2) // remove metatable
	return true
}

// LCheckStack ensures n more slots are available.
func (s *State) LCheckStack(n int, msg string) {
	if !s.CheckStack(n) {
		s.LError("stack overflow (%s)", msg)
	}
}

// Where returns "<chunk>:<line>: " or "" if no source info available.
func (s *State) Where(level int) string {
	// Walk frames from the innermost outward.
	si := s.impl
	idx := len(si.frames) - 1 - level
	if idx < 0 || idx >= len(si.frames) {
		return ""
	}
	ci := si.frames[idx]
	if ci.cl == nil || ci.cl.isGo || ci.cl.proto == nil {
		return ""
	}
	p := ci.cl.proto
	line := lineForPC(p, ci.savedpc-1)
	src := chunkNameForProto(si.gs, p)
	if src == "" {
		src = "?"
	}
	if line > 0 {
		return fmt.Sprintf("%s:%d: ", src, line)
	}
	return ""
}

// LCallMeta calls the named metamethod of the value at idx with one
// argument (the value itself) and pushes the result. Returns true if
// the metamethod existed.
func (s *State) LCallMeta(idx int, event string) bool {
	if !s.LGetMetafield(idx, event) {
		return false
	}
	s.PushValue(idx)
	s.Call(1, 1)
	return true
}

// LToLString converts the value at idx to a string, invoking
// __tostring if present.
func (s *State) LToLString(idx int) string {
	if s.LCallMeta(idx, "__tostring") {
		v, _ := s.ToString(-1)
		s.Remove(-1)
		return v
	}
	switch s.Type(idx) {
	case TString, TNumber, TInteger:
		v, _ := s.ToString(idx)
		return v
	case TBoolean:
		if s.ToBoolean(idx) {
			return "true"
		}
		return "false"
	case TNil:
		return "nil"
	}
	return fmt.Sprintf("%s: 0x%x", s.Type(idx).String(), s.ToPointer(idx))
}

// GetRegistryField pushes registry[name].
func (s *State) GetRegistryField(name string) {
	si := s.impl
	key := si.gs.intern(name)
	v, _ := si.gs.registry.getStr(key)
	si.push(v)
}

// SetRegistryField sets registry[name] from the top of the stack.
func (s *State) SetRegistryField(name string) {
	si := s.impl
	if si.top < 1 {
		si.runtimeError("SetRegistryField requires a value on the stack")
	}
	key := si.gs.intern(name)
	v := si.stack[si.top-1]
	si.gs.registry.setStr(si.gs, key, v)
	si.stack = si.stack[:si.top-1]
	si.top--
}
