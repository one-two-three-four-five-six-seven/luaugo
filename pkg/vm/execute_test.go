// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"bytes"
	"testing"

	"github.com/luaugo/luaugo/internal/common"
	"github.com/luaugo/luaugo/pkg/bytecode"
)

// helper to build a single-proto module with the given code +
// constants. The proto is set as MainProto.
func buildModule(code []uint32, constants []bytecode.Constant, strings []string, maxStack uint8) *bytecode.Module {
	p := &bytecode.Proto{
		MaxStackSize: maxStack,
		Code:         code,
		Constants:    constants,
	}
	return &bytecode.Module{
		Version:   common.BytecodeVersionTarget,
		Strings:   strings,
		Protos:    []*bytecode.Proto{p},
		MainProto: 0,
	}
}

// TestLoadEmptyChunk hand-builds a module with a single empty proto.
func TestLoadEmptyChunk(t *testing.T) {
	s := NewState()
	defer s.Close()
	// Empty bodies must still RETURN to terminate. Minimal proto:
	//   RETURN 0 1   (no return values)
	code := []uint32{common.EncodeABC(common.OpReturn, 0, 1, 0)}
	m := buildModule(code, nil, nil, 2)
	if err := s.LoadModule("=empty", m, 0); err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	s.Call(0, 0)
	if s.Top() != 0 {
		t.Fatalf("expected empty stack after empty chunk, top=%d", s.Top())
	}
}

// TestExecuteReturnNumber: LOADN 0,42 / RETURN 0,2 -> returns 42.
func TestExecuteReturnNumber(t *testing.T) {
	s := NewState()
	defer s.Close()
	code := []uint32{
		common.EncodeAD(common.OpLoadN, 0, 42),
		common.EncodeABC(common.OpReturn, 0, 2, 0),
	}
	m := buildModule(code, nil, nil, 2)
	if err := s.LoadModule("=test", m, 0); err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	s.Call(0, 1)
	if got, ok := s.ToNumber(-1); !ok || got != 42 {
		t.Fatalf("expected 42, got %v ok=%v top=%d", got, ok, s.Top())
	}
}

// TestExecuteArith: LOADN 0,2 / LOADN 1,3 / ADD 2,0,1 / RETURN 2,2 -> 5
func TestExecuteArith(t *testing.T) {
	s := NewState()
	defer s.Close()
	code := []uint32{
		common.EncodeAD(common.OpLoadN, 0, 2),
		common.EncodeAD(common.OpLoadN, 1, 3),
		common.EncodeABC(common.OpAdd, 2, 0, 1),
		common.EncodeABC(common.OpReturn, 2, 2, 0),
	}
	m := buildModule(code, nil, nil, 3)
	if err := s.LoadModule("=arith", m, 0); err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	s.Call(0, 1)
	if got, ok := s.ToNumber(-1); !ok || got != 5 {
		t.Fatalf("expected 5, got %v ok=%v", got, ok)
	}
}

// TestExecuteCallPrint: builds GETIMPORT 0 (->_G.print) / LOADK 1 ("hi")
// / CALL 0,2,1 / RETURN 0,1 — with print captured into a buffer.
func TestExecuteCallPrint(t *testing.T) {
	s := NewState()
	defer s.Close()
	var buf bytes.Buffer
	s.Register("print", func(s *State) int {
		for i := 1; i <= s.Top(); i++ {
			if i > 1 {
				buf.WriteByte('\t')
			}
			v, _ := s.ToString(i)
			buf.WriteString(v)
		}
		return 0
	})

	// Constants:
	//   0: "print" (string)
	//   1: import (packed: count=1, id0=0)
	//   2: "hi" (string)
	packed := uint32(1) << 30  // count=1
	packed |= uint32(0) << 20  // id0=0
	consts := []bytecode.Constant{
		bytecode.ConstantStringEntry{Index: 1},
		bytecode.ConstantImportEntry{Packed: packed},
		bytecode.ConstantStringEntry{Index: 2},
	}
	strs := []string{"print", "hi"}

	code := []uint32{
		common.EncodeAD(common.OpGetImport, 0, 1), // R(0) = _G.print
		uint32(packed),                            // AUX
		common.EncodeAD(common.OpLoadK, 1, 2),     // R(1) = "hi"
		common.EncodeABC(common.OpCall, 0, 2, 1),  // CALL R(0)(R(1)); 0 results
		common.EncodeABC(common.OpReturn, 0, 1, 0),
	}
	m := buildModule(code, consts, strs, 3)
	if err := s.LoadModule("=callprint", m, 0); err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	s.Call(0, 0)
	if buf.String() != "hi" {
		t.Fatalf("print buffer: got %q want %q", buf.String(), "hi")
	}
}

// TestExecuteTable: NEWTABLE / SETTABLEN / SETTABLEN / GETTABLEN
func TestExecuteTable(t *testing.T) {
	s := NewState()
	defer s.Close()
	code := []uint32{
		common.EncodeABC(common.OpNewTable, 0, 0, 0), // R(0)=new table
		0,                                            // AUX (array hint 0)
		common.EncodeAD(common.OpLoadN, 1, 100),      // R(1)=100
		common.EncodeABC(common.OpSetTableN, 1, 0, 0), // t[1]=R(1) (C=0 means key=1)
		common.EncodeAD(common.OpLoadN, 2, 200),      // R(2)=200
		common.EncodeABC(common.OpSetTableN, 2, 0, 1), // t[2]=R(2)
		common.EncodeABC(common.OpGetTableN, 3, 0, 0), // R(3)=t[1] -> 100
		common.EncodeABC(common.OpReturn, 3, 2, 0),
	}
	m := buildModule(code, nil, nil, 5)
	if err := s.LoadModule("=table", m, 0); err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	s.Call(0, 1)
	if got, ok := s.ToNumber(-1); !ok || got != 100 {
		t.Fatalf("expected 100, got %v ok=%v", got, ok)
	}
}

// TestPCallCatch registers a Go function that calls s.LError, wraps the
// call in PCall, and verifies the error reaches the API caller.
func TestPCallCatch(t *testing.T) {
	s := NewState()
	defer s.Close()
	s.Register("boom", func(s *State) int {
		s.LError("kaboom")
		return 0
	})
	s.GetGlobal("boom")
	st := s.PCall(0, 0, 0)
	if st == StatusOK {
		t.Fatalf("expected non-OK status, got OK")
	}
	if s.Top() < 1 {
		t.Fatalf("expected error value on stack, top=%d", s.Top())
	}
	msg, _ := s.ToString(-1)
	if msg == "" {
		t.Fatalf("error message empty: %q", msg)
	}
}

// TestCoroutineSimple creates a coroutine that yields 1, yields 2,
// returns 3.
func TestCoroutineSimple(t *testing.T) {
	s := NewState()
	defer s.Close()

	// Use a Go function as the coroutine body.
	body := func(co *State) int {
		co.PushNumber(1)
		co.Yield(1)
		co.PushNumber(2)
		co.Yield(1)
		co.PushNumber(3)
		return 1
	}

	co := s.NewThread()
	// Push the body function onto the coroutine's stack.
	co.PushGoFunction(body, 0)

	// Resume #1: should yield 1.
	st := co.Resume(s, 0)
	if st != StatusYield {
		t.Fatalf("resume1: expected Yield, got %v", st)
	}
	if got, _ := s.ToNumber(-1); got != 1 {
		t.Fatalf("resume1: got %v want 1", got)
	}
	s.Pop(1)

	// Resume #2: should yield 2.
	st = co.Resume(s, 0)
	if st != StatusYield {
		t.Fatalf("resume2: expected Yield, got %v", st)
	}
	if got, _ := s.ToNumber(-1); got != 2 {
		t.Fatalf("resume2: got %v want 2", got)
	}
	s.Pop(1)

	// Resume #3: should return 3.
	st = co.Resume(s, 0)
	if st != StatusOK {
		t.Fatalf("resume3: expected OK, got %v", st)
	}
	if got, _ := s.ToNumber(-1); got != 3 {
		t.Fatalf("resume3: got %v want 3", got)
	}
}
