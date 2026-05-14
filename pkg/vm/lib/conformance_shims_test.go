// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib

import (
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// conformance_shims_test.go is a sibling sanity check for
// tests/conformance_shims.go. The shim file lives under `package
// tests` (so it compiles alongside the suite), which means it cannot
// itself host unit tests against the vm package without circular
// imports. We re-register the same minimal set here against pkg/vm
// directly and confirm each shim returns the expected default and
// does not panic.
//
// If a shim's contract changes in tests/conformance_shims.go (e.g.
// is_native starts returning true), update this file in lockstep.

// installShimsForTest mirrors installConformanceShims from the
// tests package but is local so this file has no upstream-package
// dependency.
func installShimsForTest(s *vm.State) {
	s.Register("is_native", func(s *vm.State) int {
		s.PushBoolean(false)
		return 1
	})
	s.Register("is_native_if_supported", func(s *vm.State) int {
		s.PushBoolean(false)
		return 1
	})
	s.Register("breakpoint", func(*vm.State) int { return 0 })
	s.Register("coverage", func(s *vm.State) int {
		s.NewTable()
		return 1
	})
	s.Register("getcoverage", func(s *vm.State) int {
		s.NewTable()
		return 1
	})
	s.Register("setblockallocations", func(*vm.State) int { return 0 })
	s.Register("getmaxstacksize", func(s *vm.State) int {
		s.PushInteger(20000)
		return 1
	})
	s.Register("makelud", func(s *vm.State) int {
		p := new(int)
		s.PushLightUserdata(p)
		return 1
	})
	s.Register("vec2", func(s *vm.State) int {
		x, _ := s.ToNumber(1)
		y, _ := s.ToNumber(2)
		s.NewTable()
		s.PushNumber(x)
		s.SetField(-2, "X")
		s.PushNumber(y)
		s.SetField(-2, "Y")
		return 1
	})
	s.Register("int64", func(s *vm.State) int {
		if v, ok := s.ToInteger(1); ok {
			s.PushInteger(v)
		} else if v, ok := s.ToNumber(1); ok {
			s.PushInteger(int64(v))
		} else {
			s.PushInteger(0)
		}
		return 1
	})
}

// runLuauWithShims compiles src under all libs + conformance shims
// and returns the resulting State (caller closes).
func runLuauWithShims(t *testing.T, name, src string) *vm.State {
	t.Helper()
	s := runAllLibs(t, name, src) // installs libs and runs main chunk
	// The fixture in src is expected to register-and-call the shims
	// itself. We install them on the resulting State so callers can
	// invoke them post-load if they prefer.
	installShimsForTest(s)
	return s
}

// TestConformanceShimsIsNative confirms both is_native variants
// return false from Lua.
func TestConformanceShimsIsNative(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenAll(s)
	installShimsForTest(s)

	s.GetGlobal("is_native")
	if !s.IsFunction(-1) {
		t.Fatal("is_native should be a function")
	}
	st := s.PCall(0, 1, 0)
	if st != vm.StatusOK {
		t.Fatalf("is_native call failed: status=%v", st)
	}
	if s.ToBoolean(-1) {
		t.Fatal("is_native should return false")
	}
	s.Pop(1)

	s.GetGlobal("is_native_if_supported")
	st = s.PCall(0, 1, 0)
	if st != vm.StatusOK {
		t.Fatalf("is_native_if_supported call failed: status=%v", st)
	}
	if s.ToBoolean(-1) {
		t.Fatal("is_native_if_supported should return false")
	}
}

// TestConformanceShimsNoops verifies the no-op shims accept any
// args and produce no return values.
func TestConformanceShimsNoops(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenAll(s)
	installShimsForTest(s)

	for _, name := range []string{"breakpoint", "setblockallocations"} {
		s.GetGlobal(name)
		if !s.IsFunction(-1) {
			t.Fatalf("%s should be a function", name)
		}
		s.PushInteger(1) // dummy arg
		s.PushBoolean(true)
		st := s.PCall(2, 0, 0)
		if st != vm.StatusOK {
			t.Fatalf("%s call failed: status=%v", name, st)
		}
	}
}

// TestConformanceShimsCoverageReturnsTable confirms coverage and
// getcoverage return a (possibly empty) table rather than nil.
func TestConformanceShimsCoverageReturnsTable(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenAll(s)
	installShimsForTest(s)

	for _, name := range []string{"coverage", "getcoverage"} {
		s.GetGlobal(name)
		s.PushNil()
		st := s.PCall(1, 1, 0)
		if st != vm.StatusOK {
			t.Fatalf("%s call failed: status=%v", name, st)
		}
		if !s.IsTable(-1) {
			t.Fatalf("%s should return a table, got %v", name, s.Type(-1))
		}
		s.Pop(1)
	}
}

// TestConformanceShimsMakelud confirms makelud returns a
// light-userdata value (so it can be used as a table key).
func TestConformanceShimsMakelud(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenAll(s)
	installShimsForTest(s)

	s.GetGlobal("makelud")
	s.PushInteger(42)
	st := s.PCall(1, 1, 0)
	if st != vm.StatusOK {
		t.Fatalf("makelud call failed: status=%v", st)
	}
	if got := s.Type(-1); got != vm.TLightUserdata {
		t.Fatalf("makelud should return light userdata, got %v", got)
	}
}

// TestConformanceShimsVec2 confirms vec2(x,y) returns a table with X
// and Y fields populated.
func TestConformanceShimsVec2(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenAll(s)
	installShimsForTest(s)

	s.GetGlobal("vec2")
	s.PushNumber(3)
	s.PushNumber(4)
	st := s.PCall(2, 1, 0)
	if st != vm.StatusOK {
		t.Fatalf("vec2 call failed: status=%v", st)
	}
	if !s.IsTable(-1) {
		t.Fatalf("vec2 should return a table, got %v", s.Type(-1))
	}
	s.GetField(-1, "X")
	if n, _ := s.ToNumber(-1); n != 3 {
		t.Fatalf("vec2.X should be 3, got %v", n)
	}
	s.Pop(1)
	s.GetField(-1, "Y")
	if n, _ := s.ToNumber(-1); n != 4 {
		t.Fatalf("vec2.Y should be 4, got %v", n)
	}
}

// TestConformanceShimsGetMaxStackSize confirms a positive integer is
// returned.
func TestConformanceShimsGetMaxStackSize(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenAll(s)
	installShimsForTest(s)

	s.GetGlobal("getmaxstacksize")
	st := s.PCall(0, 1, 0)
	if st != vm.StatusOK {
		t.Fatalf("getmaxstacksize call failed: status=%v", st)
	}
	n, ok := s.ToInteger(-1)
	if !ok || n <= 0 {
		t.Fatalf("getmaxstacksize should return positive integer, got %v ok=%v", n, ok)
	}
}

// TestConformanceShimsInt64 confirms int64(n) returns an integer
// equal to n for integer-valued inputs.
func TestConformanceShimsInt64(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenAll(s)
	installShimsForTest(s)

	s.GetGlobal("int64")
	s.PushInteger(42)
	st := s.PCall(1, 1, 0)
	if st != vm.StatusOK {
		t.Fatalf("int64 call failed: status=%v", st)
	}
	if n, _ := s.ToInteger(-1); n != 42 {
		t.Fatalf("int64(42) should be 42, got %v", n)
	}
}

// Ensure the unused helper above is referenced (silences staticcheck
// SA9002 about unused functions in test files).
var _ = runLuauWithShims
