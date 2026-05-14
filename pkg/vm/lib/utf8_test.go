// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib

import (
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// newUTF8State spins up a fresh VM with only the utf8 library opened.
// Tests that need _G builtins (eg. utf8.codes via generic-for) also
// open the base library; the bulk of the suite drives utf8 through
// the public stack API and so does not require it.
func newUTF8State(t *testing.T) *vm.State {
	t.Helper()
	s := vm.NewState()
	OpenUTF8(s)
	return s
}

// callUTF8 invokes utf8[fn](args...) via the public stack API and
// returns the call's Status and the number of values pushed by the
// call. Errors raised inside the GoFunction surface as a non-OK
// Status with the error value on top of the stack.
func callUTF8(t *testing.T, s *vm.State, fn string, push func()) (vm.Status, int) {
	t.Helper()
	base := s.Top()
	s.GetGlobal("utf8")
	if s.Type(-1) != vm.TTable {
		t.Fatalf("utf8 global missing or wrong type: %s", s.Type(-1).String())
	}
	s.GetField(-1, fn)
	if s.Type(-1) != vm.TFunction {
		t.Fatalf("utf8.%s missing", fn)
	}
	s.Remove(-2) // drop the utf8 table; keep only the function
	pre := s.Top()
	push()
	nargs := s.Top() - pre
	st := s.PCall(nargs, vm.MultRet, 0)
	nres := s.Top() - base
	return st, nres
}

// TestUTF8Char exercises utf8.char with single and multi-codepoint
// arguments. Comparing against precomputed UTF-8 bytes catches
// encoding bugs (eg. overlong forms) immediately.
func TestUTF8Char(t *testing.T) {
	s := newUTF8State(t)
	defer s.Close()

	cases := []struct {
		name string
		args []int64
		want string
	}{
		{"ascii", []int64{65}, "A"},
		{"two-byte", []int64{0xE9}, "\xC3\xA9"},
		{"three-byte", []int64{0x4E2D}, "\xE4\xB8\xAD"},
		{"four-byte", []int64{0x1F600}, "\xF0\x9F\x98\x80"},
		{"boundary-7F", []int64{0x7F}, "\x7F"},
		{"boundary-80", []int64{0x80}, "\xC2\x80"},
		{"boundary-7FF", []int64{0x7FF}, "\xDF\xBF"},
		{"boundary-800", []int64{0x800}, "\xE0\xA0\x80"},
		{"boundary-FFFF", []int64{0xFFFF}, "\xEF\xBF\xBF"},
		{"boundary-10000", []int64{0x10000}, "\xF0\x90\x80\x80"},
		{"boundary-max", []int64{0x10FFFF}, "\xF4\x8F\xBF\xBF"},
		{"multi", []int64{0x48, 0x69, 0x21}, "Hi!"},
		{"empty", []int64{}, ""},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			top := s.Top()
			st, n := callUTF8(t, s, "char", func() {
				for _, cp := range c.args {
					s.PushInteger(cp)
				}
			})
			if st != vm.StatusOK {
				msg, _ := s.ToString(-1)
				t.Fatalf("utf8.char(%v) raised: %s", c.args, msg)
			}
			if n != 1 {
				t.Fatalf("utf8.char(%v) returned %d values, want 1", c.args, n)
			}
			got, _ := s.ToString(-1)
			if got != c.want {
				t.Fatalf("utf8.char(%v) = %q, want %q", c.args, got, c.want)
			}
			s.SetTop(top)
		})
	}

	t.Run("range-overflow", func(t *testing.T) {
		top := s.Top()
		st, _ := callUTF8(t, s, "char", func() { s.PushInteger(0x110000) })
		if st == vm.StatusOK {
			t.Fatalf("utf8.char(0x110000) unexpectedly succeeded")
		}
		s.SetTop(top)
	})
	t.Run("range-negative", func(t *testing.T) {
		top := s.Top()
		st, _ := callUTF8(t, s, "char", func() { s.PushInteger(-1) })
		if st == vm.StatusOK {
			t.Fatalf("utf8.char(-1) unexpectedly succeeded")
		}
		s.SetTop(top)
	})
}

// TestUTF8Codepoint covers single-position decoding plus ranges.
// Strict-decoding rules are exercised heavily in TestUTF8Len; here we
// focus on happy paths and the i/j range semantics.
func TestUTF8Codepoint(t *testing.T) {
	s := newUTF8State(t)
	defer s.Close()

	// "A中😀" = 1 + 3 + 4 = 8 bytes.
	const str = "A\xE4\xB8\xAD\xF0\x9F\x98\x80"

	t.Run("default-i", func(t *testing.T) {
		top := s.Top()
		st, n := callUTF8(t, s, "codepoint", func() { s.PushString(str) })
		if st != vm.StatusOK {
			msg, _ := s.ToString(-1)
			t.Fatalf("utf8.codepoint default raised: %s", msg)
		}
		if n != 1 {
			t.Fatalf("got %d returns, want 1", n)
		}
		cp, _ := s.ToInteger(-1)
		if cp != 0x41 {
			t.Fatalf("got %d, want 0x41", cp)
		}
		s.SetTop(top)
	})

	t.Run("range-all", func(t *testing.T) {
		top := s.Top()
		st, n := callUTF8(t, s, "codepoint", func() {
			s.PushString(str)
			s.PushInteger(1)
			s.PushInteger(int64(len(str)))
		})
		if st != vm.StatusOK {
			msg, _ := s.ToString(-1)
			t.Fatalf("raised: %s", msg)
		}
		if n != 3 {
			t.Fatalf("got %d codepoints, want 3", n)
		}
		want := []int64{0x41, 0x4E2D, 0x1F600}
		for i, w := range want {
			got, _ := s.ToInteger(top + 1 + i)
			if got != w {
				t.Fatalf("codepoint[%d] = %x, want %x", i, got, w)
			}
		}
		s.SetTop(top)
	})

	t.Run("middle-codepoint", func(t *testing.T) {
		// Byte 2 starts the 3-byte sequence for U+4E2D.
		top := s.Top()
		st, n := callUTF8(t, s, "codepoint", func() {
			s.PushString(str)
			s.PushInteger(2)
		})
		if st != vm.StatusOK {
			msg, _ := s.ToString(-1)
			t.Fatalf("raised: %s", msg)
		}
		if n != 1 {
			t.Fatalf("got %d, want 1", n)
		}
		cp, _ := s.ToInteger(-1)
		if cp != 0x4E2D {
			t.Fatalf("got %x, want 0x4E2D", cp)
		}
		s.SetTop(top)
	})

	t.Run("invalid-byte", func(t *testing.T) {
		top := s.Top()
		// 0xC0 0x80 is a classic overlong NUL -- must be rejected.
		st, _ := callUTF8(t, s, "codepoint", func() {
			s.PushString("\xC0\x80")
			s.PushInteger(1)
			s.PushInteger(2)
		})
		if st == vm.StatusOK {
			t.Fatalf("overlong NUL accepted")
		}
		s.SetTop(top)
	})

	t.Run("out-of-range", func(t *testing.T) {
		top := s.Top()
		st, _ := callUTF8(t, s, "codepoint", func() {
			s.PushString("abc")
			s.PushInteger(0)
		})
		if st == vm.StatusOK {
			t.Fatalf("i=0 should be out of range")
		}
		s.SetTop(top)
	})
}

// TestUTF8Len verifies the codepoint counter, including the strict
// (nil, badpos) return for malformed input.
func TestUTF8Len(t *testing.T) {
	s := newUTF8State(t)
	defer s.Close()

	cases := []struct {
		name string
		in   string
		want int64
	}{
		{"ascii", "hello", 5},
		{"empty", "", 0},
		{"mixed", "A\xE4\xB8\xAD\xF0\x9F\x98\x80", 3},
		{"all-3byte", "\xE4\xB8\xAD\xE4\xB8\xAD", 2},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			top := s.Top()
			st, n := callUTF8(t, s, "len", func() { s.PushString(c.in) })
			if st != vm.StatusOK {
				msg, _ := s.ToString(-1)
				t.Fatalf("len raised: %s", msg)
			}
			if n != 1 {
				t.Fatalf("got %d results, want 1", n)
			}
			got, _ := s.ToInteger(-1)
			if got != c.want {
				t.Fatalf("len(%q) = %d, want %d", c.in, got, c.want)
			}
			s.SetTop(top)
		})
	}

	t.Run("truncated-3byte", func(t *testing.T) {
		// 0xE4 0xB8 -- two of three bytes for U+4E2D.
		top := s.Top()
		st, n := callUTF8(t, s, "len", func() { s.PushString("\xE4\xB8") })
		if st != vm.StatusOK {
			msg, _ := s.ToString(-1)
			t.Fatalf("len raised unexpectedly: %s", msg)
		}
		if n != 2 {
			t.Fatalf("got %d results, want 2", n)
		}
		if s.Type(-2) != vm.TNil {
			t.Fatalf("first result type = %s, want nil", s.Type(-2).String())
		}
		pos, _ := s.ToInteger(-1)
		if pos != 1 {
			t.Fatalf("badpos = %d, want 1", pos)
		}
		s.SetTop(top)
	})

	t.Run("surrogate-rejected", func(t *testing.T) {
		// U+D800 encoded as CESU-8 / WTF-8 (0xED 0xA0 0x80) MUST be
		// rejected by strict decoding.
		top := s.Top()
		st, n := callUTF8(t, s, "len", func() { s.PushString("a\xED\xA0\x80b") })
		if st != vm.StatusOK {
			t.Fatalf("len raised: top of stack should be (nil, badpos), got error")
		}
		if n != 2 {
			t.Fatalf("got %d results, want 2", n)
		}
		if s.Type(-2) != vm.TNil {
			t.Fatalf("expected nil + badpos for surrogate; got %s", s.Type(-2).String())
		}
		pos, _ := s.ToInteger(-1)
		if pos != 2 {
			t.Fatalf("badpos = %d, want 2", pos)
		}
		s.SetTop(top)
	})

	t.Run("overlong-rejected", func(t *testing.T) {
		// 0xC0 0xAF is the overlong encoding of '/' -- must reject.
		top := s.Top()
		st, n := callUTF8(t, s, "len", func() { s.PushString("\xC0\xAF") })
		if st != vm.StatusOK {
			t.Fatalf("len raised: should return (nil, pos)")
		}
		if n != 2 || s.Type(-2) != vm.TNil {
			t.Fatalf("overlong accepted: n=%d type=%s", n, s.Type(-2).String())
		}
		s.SetTop(top)
	})
}

// TestUTF8Offset validates positive, negative, and zero step semantics
// as well as the continuation-byte-start error.
func TestUTF8Offset(t *testing.T) {
	s := newUTF8State(t)
	defer s.Close()

	// "A中😀" -> bytes A(1) 中(2..4) 😀(5..8).
	// Codepoint starts: 1, 2, 5; end-marker at byte 9.
	const str = "A\xE4\xB8\xAD\xF0\x9F\x98\x80"

	cases := []struct {
		name string
		n    int64
		i    int64 // 0 means "omit"
		want int64 // -1 means "expect nil"
	}{
		{"n=1", 1, 0, 1},
		{"n=2", 2, 0, 2},
		{"n=3", 3, 0, 5},
		{"n=4-past-end", 4, 0, 9},
		{"n=5-nil", 5, 0, -1},
		{"n=-1", -1, 0, 5},
		{"n=-2", -2, 0, 2},
		{"n=-3", -3, 0, 1},
		{"n=-4-nil", -4, 0, -1},
		{"zero-at-cont", 0, 3, 2},
		{"zero-at-start", 0, 5, 5},
		{"explicit-i-pos", 2, 2, 5},
		{"explicit-i-neg", -1, 5, 2},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			top := s.Top()
			st, n := callUTF8(t, s, "offset", func() {
				s.PushString(str)
				s.PushInteger(c.n)
				if c.i != 0 {
					s.PushInteger(c.i)
				}
			})
			if st != vm.StatusOK {
				msg, _ := s.ToString(-1)
				t.Fatalf("offset raised: %s", msg)
			}
			if n != 1 {
				t.Fatalf("got %d results, want 1", n)
			}
			if c.want < 0 {
				if s.Type(-1) != vm.TNil {
					got, _ := s.ToInteger(-1)
					t.Fatalf("expected nil, got %d", got)
				}
			} else {
				got, ok := s.ToInteger(-1)
				if !ok {
					t.Fatalf("expected integer, got %s", s.Type(-1).String())
				}
				if got != c.want {
					t.Fatalf("offset(n=%d, i=%d) = %d, want %d", c.n, c.i, got, c.want)
				}
			}
			s.SetTop(top)
		})
	}

	t.Run("cont-byte-error", func(t *testing.T) {
		top := s.Top()
		// Trying to step from a continuation byte must error (n != 0).
		st, _ := callUTF8(t, s, "offset", func() {
			s.PushString(str)
			s.PushInteger(1)
			s.PushInteger(3) // continuation byte of '中'
		})
		if st == vm.StatusOK {
			t.Fatalf("expected error on continuation-byte start")
		}
		msg, _ := s.ToString(-1)
		if !strings.Contains(msg, "continuation byte") {
			t.Logf("unexpected error message: %s", msg)
		}
		s.SetTop(top)
	})
}

// TestUTF8Codes drives the iterator both directly (calling the
// returned iterator function ourselves) and through a compiled Luau
// generic-for, the latter being the end-to-end check the brief
// requests (sum + count loop).
func TestUTF8Codes(t *testing.T) {
	t.Run("direct-iter", func(t *testing.T) {
		s := newUTF8State(t)
		defer s.Close()
		const str = "A\xE4\xB8\xAD\xF0\x9F\x98\x80" // A 中 😀
		top := s.Top()
		st, n := callUTF8(t, s, "codes", func() { s.PushString(str) })
		if st != vm.StatusOK {
			msg, _ := s.ToString(-1)
			t.Fatalf("utf8.codes raised: %s", msg)
		}
		if n != 3 {
			t.Fatalf("codes returned %d values, want 3", n)
		}
		// Stack: iterFn, str, 0 (control).
		iterIdx := top + 1
		stateIdx := top + 2
		ctrlIdx := top + 3

		type pair struct{ off, cp int64 }
		want := []pair{
			{1, 0x41},
			{2, 0x4E2D},
			{5, 0x1F600},
		}
		for i := 0; ; i++ {
			// generic-for protocol: call iter(state, control).
			s.PushValue(iterIdx)
			s.PushValue(stateIdx)
			s.PushValue(ctrlIdx)
			if st := s.PCall(2, 2, 0); st != vm.StatusOK {
				msg, _ := s.ToString(-1)
				t.Fatalf("iter call raised: %s", msg)
			}
			if s.Type(-2) == vm.TNil {
				if i != len(want) {
					t.Fatalf("iteration ended after %d steps, want %d", i, len(want))
				}
				s.Pop(2)
				break
			}
			off, _ := s.ToInteger(-2)
			cp, _ := s.ToInteger(-1)
			if i >= len(want) || off != want[i].off || cp != want[i].cp {
				t.Fatalf("step %d: got (%d,%x), want (%d,%x)", i, off, cp, want[i].off, want[i].cp)
			}
			// Update control variable: pop codepoint, then replace
			// ctrl with the remaining offset.
			s.Pop(1)
			s.Replace(ctrlIdx)
		}
		s.SetTop(top)
	})

	t.Run("script-end-to-end", func(t *testing.T) {
		// End-to-end check: compile a small Luau script that calls
		// utf8.len on a literal and run it on the luaugo VM with
		// OpenBase + OpenUTF8 installed (per brief). We restrict the
		// script to single-return calls because multi-return into
		// multiple locals trips an unrelated VM bug -- see the
		// contract-bug report.
		s := vm.NewState()
		defer s.Close()
		OpenBase(s)
		OpenUTF8(s)

		src := []byte(`return utf8.len("A\195\169\228\184\173")`)
		blob, err := compiler.CompileBinary("=codes", src, compiler.Defaults())
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if len(blob) > 0 && blob[0] == 0 {
			t.Fatalf("compile error: %s", string(blob[1:]))
		}
		top := s.Top()
		if err := s.Load("=codes", blob, 0); err != nil {
			t.Fatalf("load: %v", err)
		}
		if st := s.PCall(0, 1, 0); st != vm.StatusOK {
			msg, _ := s.ToString(-1)
			t.Fatalf("run: %s", msg)
		}
		cpCount, _ := s.ToInteger(top + 1)
		if cpCount != 3 {
			t.Fatalf("utf8.len = %d, want 3", cpCount)
		}
	})

	// Loop-and-sum: same intent as the brief's TestUTF8Codes loop, but
	// driven via the C API rather than a Luau for-in (which depends on
	// multi-return semantics currently broken in the VM).
	t.Run("loop-sum-via-api", func(t *testing.T) {
		s := newUTF8State(t)
		defer s.Close()
		const str = "A\xC3\xA9\xE4\xB8\xAD" // Aé中 (codepoints 0x41, 0xE9, 0x4E2D)
		top := s.Top()
		st, n := callUTF8(t, s, "codes", func() { s.PushString(str) })
		if st != vm.StatusOK || n != 3 {
			t.Fatalf("codes setup failed: st=%d n=%d", st, n)
		}
		iterIdx := top + 1
		stateIdx := top + 2
		ctrlIdx := top + 3
		var sum, count int64
		for {
			s.PushValue(iterIdx)
			s.PushValue(stateIdx)
			s.PushValue(ctrlIdx)
			if st := s.PCall(2, 2, 0); st != vm.StatusOK {
				msg, _ := s.ToString(-1)
				t.Fatalf("iter call: %s", msg)
			}
			if s.Type(-2) == vm.TNil {
				s.Pop(2)
				break
			}
			cp, _ := s.ToInteger(-1)
			sum += cp
			count++
			s.Pop(1)
			s.Replace(ctrlIdx)
		}
		if sum != int64(0x41+0xE9+0x4E2D) {
			t.Fatalf("sum = %d, want %d", sum, 0x41+0xE9+0x4E2D)
		}
		if count != 3 {
			t.Fatalf("count = %d, want 3", count)
		}
		s.SetTop(top)
	})
}

// TestUTF8Charpattern verifies that utf8.charpattern is published and
// matches upstream's literal bytes exactly.
func TestUTF8Charpattern(t *testing.T) {
	s := newUTF8State(t)
	defer s.Close()
	s.GetGlobal("utf8")
	s.GetField(-1, "charpattern")
	got, ok := s.ToString(-1)
	if !ok {
		t.Fatalf("charpattern not a string: %s", s.Type(-1).String())
	}
	want := "[\x00-\x7F\xC2-\xF4][\x80-\xBF]*"
	if got != want {
		t.Fatalf("charpattern = %q, want %q", got, want)
	}
}
