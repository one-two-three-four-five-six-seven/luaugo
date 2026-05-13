// Copyright (c) luaugo contributors. Licensed under the MIT License.

package vm

import (
	"fmt"

	"github.com/luaugo/luaugo/pkg/bytecode"
)

// Tier-1 placeholders for Tier 2 / Tier 3 VM agents. All exported
// methods on *State are routed through these unexported methods so the
// contract can be locked while implementation details remain free to
// change.

type stateImpl struct {
	// Concrete state machinery is filled in by Tier 2/3.
	closed bool
}

func newState() *State { return &State{impl: &stateImpl{}} }

func (s *State) close()               { s.impl.closed = true }
func (s *State) top() int             { panic("vm: Top not yet implemented") }
func (s *State) setTop(int)           { panic("vm: SetTop not yet implemented") }
func (s *State) checkStack(int) bool  { return true }
func (s *State) typeAt(int) Type      { panic("vm: Type not yet implemented") }
func (s *State) pushNil()             { panic("vm: PushNil not yet implemented") }
func (s *State) pushBoolean(bool)     { panic("vm: PushBoolean not yet implemented") }
func (s *State) pushNumber(float64)   { panic("vm: PushNumber not yet implemented") }
func (s *State) pushInteger(int64)    { panic("vm: PushInteger not yet implemented") }
func (s *State) pushString(string)    { panic("vm: PushString not yet implemented") }
func (s *State) pushVector(float32, float32, float32, float32) {
	panic("vm: PushVector not yet implemented")
}
func (s *State) pushGoFunction(GoFunction, int) { panic("vm: PushGoFunction not yet implemented") }
func (s *State) pushValue(int)                  { panic("vm: PushValue not yet implemented") }
func (s *State) toBoolean(int) bool             { panic("vm: ToBoolean not yet implemented") }
func (s *State) toNumber(int) (float64, bool)   { panic("vm: ToNumber not yet implemented") }
func (s *State) toInteger(int) (int64, bool)    { panic("vm: ToInteger not yet implemented") }
func (s *State) toString(int) (string, bool)    { panic("vm: ToString not yet implemented") }
func (s *State) toVector(int) (float32, float32, float32, float32, bool) {
	panic("vm: ToVector not yet implemented")
}
func (s *State) isString(int) bool { panic("vm: IsString not yet implemented") }
func (s *State) isNumber(int) bool { panic("vm: IsNumber not yet implemented") }
func (s *State) remove(int)        { panic("vm: Remove not yet implemented") }
func (s *State) insert(int)        { panic("vm: Insert not yet implemented") }
func (s *State) replace(int)       { panic("vm: Replace not yet implemented") }
func (s *State) newTable(int, int) { panic("vm: NewTable not yet implemented") }
func (s *State) getTable(int)      { panic("vm: GetTable not yet implemented") }
func (s *State) setTable(int)      { panic("vm: SetTable not yet implemented") }
func (s *State) getField(int, string) { panic("vm: GetField not yet implemented") }
func (s *State) setField(int, string) { panic("vm: SetField not yet implemented") }
func (s *State) rawGet(int)             { panic("vm: RawGet not yet implemented") }
func (s *State) rawSet(int)             { panic("vm: RawSet not yet implemented") }
func (s *State) rawGetI(int, int)       { panic("vm: RawGetI not yet implemented") }
func (s *State) rawSetI(int, int)       { panic("vm: RawSetI not yet implemented") }
func (s *State) next(int) bool          { panic("vm: Next not yet implemented") }
func (s *State) length(int)             { panic("vm: Length not yet implemented") }
func (s *State) rawEqual(int, int) bool { panic("vm: RawEqual not yet implemented") }
func (s *State) equal(int, int) bool    { panic("vm: Equal not yet implemented") }
func (s *State) lessThan(int, int) bool { panic("vm: LessThan not yet implemented") }
func (s *State) call(int, int)          { panic("vm: Call not yet implemented") }
func (s *State) pcall(int, int, int) Status {
	panic("vm: PCall not yet implemented")
}
func (s *State) raiseError() { panic("vm: Error not yet implemented") }
func (s *State) errorf(format string, args ...any) {
	panic(fmt.Sprintf(format, args...))
}
func (s *State) newThread() *State { panic("vm: NewThread not yet implemented") }
func (co *State) resume(*State, int) Status {
	panic("vm: Resume not yet implemented")
}
func (s *State) yield(int) int                { panic("vm: Yield not yet implemented") }
func (s *State) load(string, []byte, int) error { panic("vm: Load not yet implemented") }
func (s *State) loadModule(string, *bytecode.Module, int) error {
	panic("vm: LoadModule not yet implemented")
}
func (s *State) getGlobal(string)  { panic("vm: GetGlobal not yet implemented") }
func (s *State) setGlobal(string)  { panic("vm: SetGlobal not yet implemented") }
func (s *State) gcInfo() int       { panic("vm: GCInfo not yet implemented") }
func (s *State) collectGarbage()   { panic("vm: CollectGarbage not yet implemented") }
func (s *State) openLibs()         { panic("vm: OpenLibs not yet implemented") }
func (s *State) sandbox()          { panic("vm: Sandbox not yet implemented") }
func (s *State) sandboxThread()    { panic("vm: SandboxThread not yet implemented") }
