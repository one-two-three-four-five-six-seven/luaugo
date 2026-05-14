// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib

import (
	"strings"
	"testing"
	"time"

	"github.com/luaugo/luaugo/pkg/compiler"
	"github.com/luaugo/luaugo/pkg/vm"
)

// osRunLua compiles and runs `source` against s, expecting `nresults`
// return values left on the stack. Only the os library is opened in
// these tests; OpenBase is still a Tier-4 stub at the time of writing.
func osRunLua(t *testing.T, s *vm.State, source string, nresults int) {
	t.Helper()
	m, err := compiler.CompileSource("=test", []byte(source), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := s.LoadModule("=test", m, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	s.Call(0, nresults)
}

// TestOSClock asserts that os.clock is monotonically nondecreasing
// across two sufficiently-spaced calls.
func TestOSClock(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenOS(s)

	s.GetGlobal("os")
	s.GetField(-1, "clock")
	s.Call(0, 1)
	first, ok := s.ToNumber(-1)
	if !ok {
		t.Fatalf("os.clock #1: not a number")
	}
	s.Pop(1)

	// Burn a small amount of wall-clock time so the second reading
	// should be strictly greater (clock resolution permitting).
	time.Sleep(2 * time.Millisecond)

	s.GetField(-1, "clock")
	s.Call(0, 1)
	second, ok := s.ToNumber(-1)
	if !ok {
		t.Fatalf("os.clock #2: not a number")
	}
	s.Pop(1)

	if !(second >= first) {
		t.Fatalf("os.clock not monotonically nondecreasing: first=%v second=%v", first, second)
	}
	if second == first {
		t.Logf("warning: os.clock returned identical values across a 2ms sleep (first=%v)", first)
	}
}

// TestOSDifftime exercises os.difftime(a, b) and the b=0 default.
func TestOSDifftime(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenOS(s)

	cases := []struct {
		a, b, want float64
	}{
		{100, 25, 75},
		{0, 0, 0},
		{50, 100, -50},
	}
	for _, c := range cases {
		s.GetGlobal("os")
		s.GetField(-1, "difftime")
		s.PushNumber(c.a)
		s.PushNumber(c.b)
		s.Call(2, 1)
		got, ok := s.ToNumber(-1)
		if !ok || got != c.want {
			t.Fatalf("difftime(%v,%v) = %v ok=%v want %v", c.a, c.b, got, ok, c.want)
		}
		s.Pop(2)
	}

	// Single-argument form (b defaults to 0).
	s.GetGlobal("os")
	s.GetField(-1, "difftime")
	s.PushNumber(42)
	s.Call(1, 1)
	got, ok := s.ToNumber(-1)
	if !ok || got != 42 {
		t.Fatalf("difftime(42) = %v ok=%v want 42", got, ok)
	}
	s.Pop(2)
}

// TestOSTimeAndDate composes a Unix timestamp from a known calendar
// table via os.time, then decomposes it back via os.date("!*t") and
// asserts the round-trip preserves every interesting field.
func TestOSTimeAndDate(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenOS(s)

	// 2024-05-14 13:45:30 UTC -> Unix 1715694330.
	const wantUnix = int64(1715694330)

	s.GetGlobal("os")
	s.GetField(-1, "time")
	s.CreateTable(0, 6)
	s.PushInteger(2024)
	s.SetField(-2, "year")
	s.PushInteger(5)
	s.SetField(-2, "month")
	s.PushInteger(14)
	s.SetField(-2, "day")
	s.PushInteger(13)
	s.SetField(-2, "hour")
	s.PushInteger(45)
	s.SetField(-2, "min")
	s.PushInteger(30)
	s.SetField(-2, "sec")
	s.Call(1, 1)

	gotUnix, ok := s.ToInteger(-1)
	if !ok {
		// Upstream returns time as a number; try number fallback.
		f, fok := s.ToNumber(-1)
		if !fok {
			t.Fatalf("os.time(table): not numeric")
		}
		gotUnix = int64(f)
	}
	if gotUnix != wantUnix {
		t.Fatalf("os.time(table) = %d, want %d", gotUnix, wantUnix)
	}
	s.Pop(2)

	s.GetGlobal("os")
	s.GetField(-1, "date")
	s.PushString("!*t")
	s.PushNumber(float64(gotUnix))
	s.Call(2, 1)

	check := func(field string, want int64) {
		t.Helper()
		s.GetField(-1, field)
		v, ok := s.ToInteger(-1)
		if !ok {
			f, fok := s.ToNumber(-1)
			if !fok {
				t.Fatalf("os.date *t field %q: not numeric", field)
			}
			v = int64(f)
		}
		if v != want {
			t.Fatalf("os.date *t field %q = %d, want %d", field, v, want)
		}
		s.Pop(1)
	}
	check("year", 2024)
	check("month", 5)
	check("day", 14)
	check("hour", 13)
	check("min", 45)
	check("sec", 30)
	// 2024-05-14 was a Tuesday -> Go Weekday=2, Lua wday=3.
	check("wday", 3)
	// Day of year: Jan(31)+Feb(29)+Mar(31)+Apr(30)+14 = 135.
	check("yday", 135)
	s.Pop(2)
}

// TestOSDateFormat exercises a handful of strftime specifiers and the
// '!' UTC prefix.
func TestOSDateFormat(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenOS(s)

	const epoch = int64(1715694330) // 2024-05-14 13:45:30 UTC, Tuesday

	call := func(fmtStr string) string {
		t.Helper()
		s.GetGlobal("os")
		s.GetField(-1, "date")
		s.PushString(fmtStr)
		s.PushNumber(float64(epoch))
		s.Call(2, 1)
		got, ok := s.ToString(-1)
		if !ok {
			t.Fatalf("os.date(%q): not a string", fmtStr)
		}
		s.Pop(2)
		return got
	}

	if got := call("!%Y-%m-%d"); got != "2024-05-14" {
		t.Fatalf("!%%Y-%%m-%%d = %q, want 2024-05-14", got)
	}
	if got := call("!%H:%M:%S"); got != "13:45:30" {
		t.Fatalf("!%%H:%%M:%%S = %q, want 13:45:30", got)
	}
	if got := call("!%A"); got != "Tuesday" {
		t.Fatalf("!%%A = %q, want Tuesday", got)
	}
	if got := call("!%B"); got != "May" {
		t.Fatalf("!%%B = %q, want May", got)
	}
	if got := call("!%j"); got != "135" {
		t.Fatalf("!%%j = %q, want 135", got)
	}
	if got := call("!100%%"); got != "100%" {
		t.Fatalf("!100%%%% = %q, want 100%%", got)
	}
	c := call("!%c")
	if !strings.Contains(c, "2024") || !strings.Contains(c, "13:45:30") {
		t.Fatalf("!%%c = %q, expected to contain 2024 and 13:45:30", c)
	}
}

// TestOSEndToEnd compiles a tiny Lua chunk that exercises os.* via the
// VM. The script is deliberately minimal because OpenBase is still a
// Tier-4 stub (no print, no tostring) and the C-API surface already
// covers per-function behaviour in the tests above.
func TestOSEndToEnd(t *testing.T) {
	s := vm.NewState()
	defer s.Close()
	OpenOS(s)

	src := `return os.difftime(100, 25)`
	osRunLua(t, s, src, 1)
	got, ok := s.ToNumber(-1)
	if !ok || got != 75 {
		t.Fatalf("os.difftime via Lua: got %v ok=%v want 75", got, ok)
	}
}
