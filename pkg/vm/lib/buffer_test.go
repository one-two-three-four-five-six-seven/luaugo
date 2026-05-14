// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package lib_test

import (
	"math"
	"strings"
	"testing"

	"github.com/luaugo/luaugo/pkg/vm"
	"github.com/luaugo/luaugo/pkg/vm/lib"
)

// These tests drive the buffer standard library end-to-end through the
// public vm.State API: OpenBase + OpenBuffer install the globals,
// callBuf invokes `buffer.<name>(args...)` via GetGlobal + GetField +
// Call, just as a real interpreter would after evaluating an
// expression like `buffer.create(16)`. This deliberately avoids
// authoring multi-statement Luau chunks so the tests stay decoupled
// from a parallel-tier interpreter bug in vm.callGo that mis-truncates
// the caller's stack after a Go function returns (see "contract
// bugs"). The library's correctness still flows through Lua-style
// argument coercion, the GoFunction call path, panic-as-error
// translation, and the buffer GC type.

// newBufState builds a fresh State with OpenBase and OpenBuffer. The
// OpenBase call is wrapped in a recover so missing Tier-4 base
// implementations don't fail the buffer tests.
func newBufState(t *testing.T) *vm.State {
	t.Helper()
	s := vm.NewState()
	t.Cleanup(s.Close)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("lib.OpenBase panicked (pre-Tier 4 expected): %v", r)
			}
		}()
		lib.OpenBase(s)
	}()
	lib.OpenBuffer(s)
	return s
}

// callBuf invokes buffer.<name>(args...) and leaves nresults on the
// stack. Each arg is pushed via pushAny. The function is fetched via
// GetGlobal("buffer") + GetField(name). This is exactly the call
// sequence a Lua script's GETIMPORT for "buffer.foo" decodes to.
func callBuf(t *testing.T, s *vm.State, name string, nresults int, args ...any) {
	t.Helper()
	s.GetGlobal("buffer")
	if !s.IsTable(-1) {
		t.Fatalf("buffer global is %s, not table", s.Type(-1).String())
	}
	s.GetField(-1, name)
	if !s.IsFunction(-1) {
		t.Fatalf("buffer.%s is %s, not function", name, s.Type(-1).String())
	}
	// Reorder: stack has [buffer, fn]. We want [fn, args...]; drop the
	// `buffer` table.
	s.Remove(-2)
	for _, a := range args {
		pushAny(t, s, a)
	}
	s.Call(len(args), nresults)
}

// pushAny pushes a Go value as the matching Lua value. The buffer
// tests only need numbers, strings, and previously-pushed buffer
// slots (referenced by *vm.State stack index).
func pushAny(t *testing.T, s *vm.State, v any) {
	t.Helper()
	switch x := v.(type) {
	case int:
		s.PushNumber(float64(x))
	case int64:
		s.PushNumber(float64(x))
	case uint32:
		s.PushNumber(float64(x))
	case float64:
		s.PushNumber(x)
	case string:
		s.PushString(x)
	case bufferRef:
		s.PushValue(x.idx)
	default:
		t.Fatalf("pushAny: unsupported type %T", v)
	}
}

// bufferRef tags an existing stack slot that holds a buffer.
type bufferRef struct{ idx int }

// expectNumber asserts the value at idx is a number equal to want.
func expectNumber(t *testing.T, s *vm.State, idx int, want float64) {
	t.Helper()
	got, ok := s.ToNumber(idx)
	if !ok {
		t.Fatalf("idx=%d: expected number, got %s", idx, s.Type(idx).String())
	}
	if got != want {
		t.Fatalf("idx=%d: got %v want %v", idx, got, want)
	}
}

// expectString asserts the value at idx is a string equal to want.
func expectString(t *testing.T, s *vm.State, idx int, want string) {
	t.Helper()
	got, ok := s.ToString(idx)
	if !ok {
		t.Fatalf("idx=%d: expected string, got %s", idx, s.Type(idx).String())
	}
	if got != want {
		t.Fatalf("idx=%d: got %q want %q", idx, got, want)
	}
}

// ---------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------

// TestBufferCreate exercises buffer.create / buffer.len and confirms
// zero-initialisation.
func TestBufferCreate(t *testing.T) {
	s := newBufState(t)
	callBuf(t, s, "create", 1, 16) // [buf]
	callBuf(t, s, "len", 1, bufferRef{1})
	expectNumber(t, s, -1, 16)
	s.Pop(1)

	callBuf(t, s, "readu8", 1, bufferRef{1}, 0)
	expectNumber(t, s, -1, 0)
	s.Pop(1)

	callBuf(t, s, "readu8", 1, bufferRef{1}, 15)
	expectNumber(t, s, -1, 0)
}

// TestBufferFromString round-trips a string through fromstring +
// tostring and checks the resulting buffer's length matches.
func TestBufferFromString(t *testing.T) {
	s := newBufState(t)
	const payload = "hello, world!"
	callBuf(t, s, "fromstring", 1, payload) // [buf]

	callBuf(t, s, "tostring", 1, bufferRef{1})
	expectString(t, s, -1, payload)
	s.Pop(1)

	callBuf(t, s, "len", 1, bufferRef{1})
	expectNumber(t, s, -1, float64(len(payload)))
}

// TestBufferReadWrite covers every integer width with positive,
// negative, and wrapping payloads.
func TestBufferReadWrite(t *testing.T) {
	s := newBufState(t)
	callBuf(t, s, "create", 1, 32) // buf at idx 1

	type wcase struct {
		name string
		fn   string
		off  int
		val  any
	}
	type rcase struct {
		name string
		fn   string
		off  int
		want float64
	}

	writes := []wcase{
		{"writeu8_max", "writeu8", 0, 255},
		{"writei8_neg1", "writei8", 1, -1},
		{"writeu16", "writeu16", 2, 0xBEEF},
		{"writei16_min", "writei16", 4, -32768},
		{"writeu32", "writeu32", 6, uint32(0xDEADBEEF)},
		{"writei32_neg1", "writei32", 10, -1},
		// Truncation: bit32 wrap to low 8/16 bits.
		{"writei8_wrap", "writei8", 14, 0x1FF},
		{"writei16_wrap", "writei16", 15, 0x10001},
	}
	for _, w := range writes {
		t.Run(w.name, func(t *testing.T) {
			callBuf(t, s, w.fn, 0, bufferRef{1}, w.off, w.val)
		})
	}

	reads := []rcase{
		{"readu8", "readu8", 0, 255},
		{"readi8", "readi8", 1, -1},
		{"readu16", "readu16", 2, 0xBEEF},
		{"readi16", "readi16", 4, -32768},
		{"readu32", "readu32", 6, 0xDEADBEEF},
		{"readi32", "readi32", 10, -1},
		{"readi8_wrap", "readi8", 14, -1},
		{"readi16_wrap", "readi16", 15, 1},
	}
	for _, r := range reads {
		t.Run(r.name, func(t *testing.T) {
			callBuf(t, s, r.fn, 1, bufferRef{1}, r.off)
			expectNumber(t, s, -1, r.want)
			s.Pop(1)
		})
	}
}

// TestBufferReadWriteFloat verifies f32 and f64 round-trip and that
// the storage occupies the correct number of bytes.
func TestBufferReadWriteFloat(t *testing.T) {
	s := newBufState(t)
	callBuf(t, s, "create", 1, 16) // buf at idx 1

	callBuf(t, s, "writef32", 0, bufferRef{1}, 0, 1.5)
	callBuf(t, s, "writef64", 0, bufferRef{1}, 4, math.Pi)

	callBuf(t, s, "readf32", 1, bufferRef{1}, 0)
	expectNumber(t, s, -1, 1.5)
	s.Pop(1)

	callBuf(t, s, "readf64", 1, bufferRef{1}, 4)
	got, _ := s.ToNumber(-1)
	if got != math.Pi {
		t.Fatalf("f64 readback: got %v want %v", got, math.Pi)
	}
	s.Pop(1)

	// Ensure f32 truncation matches IEEE-754 round-to-nearest-even.
	v := 0.1 // not exactly representable in f32
	callBuf(t, s, "writef32", 0, bufferRef{1}, 8, v)
	callBuf(t, s, "readf32", 1, bufferRef{1}, 8)
	got, _ = s.ToNumber(-1)
	if got != float64(float32(v)) {
		t.Fatalf("f32 round-trip: got %v want %v", got, float64(float32(v)))
	}
}

// TestBufferReadString exercises readstring and writestring with the
// optional count argument.
func TestBufferReadString(t *testing.T) {
	s := newBufState(t)
	callBuf(t, s, "create", 1, 16) // buf at idx 1

	callBuf(t, s, "writestring", 0, bufferRef{1}, 0, "abcdef")
	// count argument: writes only the first 2 bytes of "XYZ".
	callBuf(t, s, "writestring", 0, bufferRef{1}, 6, "XYZ", 2)

	callBuf(t, s, "readstring", 1, bufferRef{1}, 0, 8)
	expectString(t, s, -1, "abcdefXY")
	s.Pop(1)

	callBuf(t, s, "readstring", 1, bufferRef{1}, 1, 3)
	expectString(t, s, -1, "bcd")
}

// TestBufferCopy verifies that copy works between buffers and is
// overlap-safe within a single buffer (memmove semantics).
func TestBufferCopy(t *testing.T) {
	s := newBufState(t)

	// Cross-buffer full copy.
	callBuf(t, s, "fromstring", 1, "0123456789") // src at idx 1
	callBuf(t, s, "create", 1, 10)               // dst at idx 2
	callBuf(t, s, "copy", 0, bufferRef{2}, 0, bufferRef{1})
	callBuf(t, s, "tostring", 1, bufferRef{2})
	expectString(t, s, -1, "0123456789")
	s.Pop(1)

	// Partial copy with explicit count.
	callBuf(t, s, "fill", 0, bufferRef{2}, 0, 0)
	callBuf(t, s, "copy", 0, bufferRef{2}, 2, bufferRef{1}, 4, 3)
	callBuf(t, s, "tostring", 1, bufferRef{2})
	got, _ := s.ToString(-1)
	want := "\x00\x00456\x00\x00\x00\x00\x00"
	if got != want {
		t.Fatalf("partial: got %q want %q", got, want)
	}
	s.Pop(1)

	// Forward-overlapping shift: buf[1..9] = buf[0..8].
	callBuf(t, s, "fromstring", 1, "0123456789") // ov1 at idx 3
	callBuf(t, s, "copy", 0, bufferRef{3}, 1, bufferRef{3}, 0, 9)
	callBuf(t, s, "tostring", 1, bufferRef{3})
	expectString(t, s, -1, "0012345678")
	s.Pop(1)

	// Backward-overlapping shift: buf[0..8] = buf[1..9].
	callBuf(t, s, "fromstring", 1, "0123456789") // ov2 at idx 4
	callBuf(t, s, "copy", 0, bufferRef{4}, 0, bufferRef{4}, 1, 9)
	callBuf(t, s, "tostring", 1, bufferRef{4})
	expectString(t, s, -1, "1234567899")
}

// TestBufferFill exercises the fill primitive: full and partial fills,
// and the truncation of values > 0xff to a single byte.
func TestBufferFill(t *testing.T) {
	s := newBufState(t)
	callBuf(t, s, "create", 1, 8) // buf at idx 1

	// Default count fills to end.
	callBuf(t, s, "fill", 0, bufferRef{1}, 0, 0xAA)
	callBuf(t, s, "tostring", 1, bufferRef{1})
	expectString(t, s, -1, strings.Repeat("\xAA", 8))
	s.Pop(1)

	// Partial: overwrite bytes [2..5].
	callBuf(t, s, "fill", 0, bufferRef{1}, 2, 0x55, 4)
	callBuf(t, s, "tostring", 1, bufferRef{1})
	got, _ := s.ToString(-1)
	want := "\xAA\xAA\x55\x55\x55\x55\xAA\xAA"
	if got != want {
		t.Fatalf("partial fill: got %x want %x", got, want)
	}
	s.Pop(1)

	// Truncation: 0x1FF -> 0xFF.
	callBuf(t, s, "create", 1, 4) // c at idx 2
	callBuf(t, s, "fill", 0, bufferRef{2}, 0, 0x1FF)
	callBuf(t, s, "tostring", 1, bufferRef{2})
	expectString(t, s, -1, strings.Repeat("\xFF", 4))
}

// TestBufferBits exercises readbits/writebits across byte boundaries
// and at the maximum bit count (32).
func TestBufferBits(t *testing.T) {
	s := newBufState(t)
	callBuf(t, s, "create", 1, 8) // buf at idx 1

	// Write a 32-bit pattern starting at bit offset 3 (crosses 5 bytes).
	callBuf(t, s, "writebits", 0, bufferRef{1}, 3, 32, uint32(0xDEADBEEF))
	callBuf(t, s, "readbits", 1, bufferRef{1}, 3, 32)
	expectNumber(t, s, -1, 0xDEADBEEF)
	s.Pop(1)

	// Overwrite a 4-bit nibble at bit offset 12 (i.e. relative bit 9 of
	// the 32-bit field) and verify the field reads back accordingly.
	callBuf(t, s, "writebits", 0, bufferRef{1}, 12, 4, uint32(0xA))
	callBuf(t, s, "readbits", 1, bufferRef{1}, 12, 4)
	expectNumber(t, s, -1, 0xA)
	s.Pop(1)

	// A zero-bit read returns 0 without touching the buffer.
	callBuf(t, s, "readbits", 1, bufferRef{1}, 35, 0)
	expectNumber(t, s, -1, 0)
	s.Pop(1)

	// Confirm only bits [9..12] of the field were changed.
	callBuf(t, s, "readbits", 1, bufferRef{1}, 3, 32)
	orig := uint32(0xDEADBEEF)
	mask := uint32(0xF) << 9
	expected := (orig &^ mask) | (uint32(0xA) << 9)
	expectNumber(t, s, -1, float64(expected))
}

// TestBufferBoundsError verifies an out-of-bounds access raises a Lua
// runtime error rather than producing garbage. The error propagates
// through PCall as a string containing "out of bounds".
func TestBufferBoundsError(t *testing.T) {
	s := newBufState(t)
	callBuf(t, s, "create", 1, 4) // buf at idx 1

	// Now run buffer.readi32(buf, 1) under PCall; should fail because
	// the read extends past the end (1+4 > 4).
	s.GetGlobal("buffer")
	s.GetField(-1, "readi32")
	s.Remove(-2)
	s.PushValue(1) // buf
	s.PushNumber(1)
	st := s.PCall(2, 0, 0)
	if st == vm.StatusOK {
		t.Fatalf("expected non-OK status for OOB read, got OK")
	}
	msg, _ := s.ToString(-1)
	if !strings.Contains(msg, "out of bounds") {
		t.Fatalf("error message: got %q, want substring 'out of bounds'", msg)
	}
}
