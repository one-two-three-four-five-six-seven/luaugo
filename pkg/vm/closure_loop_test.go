// Copyright (c) luaugo contributors. Licensed under the MIT License.

// closure_loop_test.go documents the infinite-loop / timeout failure
// observed on tests/conformance/closure.luau. The fixture exercises a
// Eratosthenes-style sieve built out of CHAINED coroutines (closure.luau
// lines 250-276), where each `filter(p, g)` captures the previous
// generator `g` as an upvalue and is itself wrapped in a new
// coroutine via coroutine.wrap. Inside the chained body the outer
// coroutine calls `g()` -- which re-enters Resume on an inner
// coroutine.
//
// Root cause (out of this agent's edit scope): pkg/vm/thread.go uses a
// per-VM serialising mutex (globalMutexes / getVMMutex). Each coroutine
// goroutine acquires that mutex at thread.go:123 before running and
// releases it ONLY in yieldImpl (thread.go:227) and around its normal
// exit. resumeImpl (thread.go:166-173) sends args and blocks on
// yieldCh WITHOUT releasing the mutex, so when an outer coroutine's
// goroutine -- which already holds the mutex -- tries to Resume an
// inner coroutine, the inner goroutine deadlocks at the mu.Lock() at
// thread.go:123, and the outer deadlocks at the <-yieldCh at
// thread.go:173.
//
// Verified empirically: the goroutine dump in
// TestNestedCoroutineResumeDumpGoroutines shows exactly this state.
//
// The fix belongs in thread.go::resumeImpl: release the mutex around
// the c.resumeCh <- args / <-c.yieldCh rendezvous (mirroring what
// yieldImpl already does). That file is owned by a different agent in
// the swarm (or in master), so this agent leaves the failing tests
// behind as living documentation of the bug.

package vm_test

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// runScriptCL compiles a snippet of Lua and runs it with a deadline.
// Returns (out, status, errMsg). On deadline the test fails.
func runScriptCL(t *testing.T, src string, deadline time.Duration) (string, vm.Status, string) {
	t.Helper()
	blob, err := compiler.CompileBinary("=closure_loop_test", []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(blob) > 0 && blob[0] == 0 {
		t.Fatalf("compile error blob: %s", string(blob[1:]))
	}
	s := vm.NewState()
	defer s.Close()
	prev := lib.Stdout
	defer func() { lib.Stdout = prev }()
	lib.Stdout = io.Discard
	lib.OpenAll(s)
	if err := s.Load("=closure_loop_test", blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	done := make(chan struct{})
	var st vm.Status
	var out, msg string
	go func() {
		defer close(done)
		st = s.PCall(0, 1, 0)
		if st != vm.StatusOK {
			msg, _ = s.ToString(-1)
			return
		}
		out, _ = s.ToString(-1)
	}()
	select {
	case <-done:
	case <-time.After(deadline):
		t.Fatalf("script exceeded %v deadline; suspected infinite loop", deadline)
	}
	return out, st, msg
}

// TestClosureLargeArrayOfClosures reproduces the start of
// tests/conformance/closure.luau:8-24 -- a function that builds 1000
// closures, each capturing a loop-local `y`. The pattern hammers
// findOrCreateUpval / closeUpvalsTo every iteration. This block
// passes on its own, which rules out the upvalue-list traversal in
// pkg/vm/closure.go and pkg/vm/do.go::closeUpvalsTo as the timeout
// trigger.
func TestClosureLargeArrayOfClosures(t *testing.T) {
	src := `local A = 0
local B = {g=10}
local function f(x)
  local a = {}
  for i=1,1000 do
    local y = 0
    do
      a[i] = function () B.g = B.g + 1; y = y + x; return y + A end
    end
  end
  return a
end
local a = f(10)
return tostring(a[1]() + a[3]())`
	out, st, msg := runScriptCL(t, src, 2*time.Second)
	if st != vm.StatusOK {
		t.Fatalf("run: %v %q", st, msg)
	}
	if out != "20" {
		t.Fatalf("got %q want 20", out)
	}
}

// TestClosureForLoopControlVar reproduces closure.luau:46-56: capturing
// the for-loop control variable. Each iteration must produce a FRESH
// upvalue (Luau semantics), and closeUpvalsTo runs at every loop
// boundary. Also passes on its own -- closeUpvalsTo's stop condition
// is correct.
func TestClosureForLoopControlVar(t *testing.T) {
	src := `local a = {}
for i=1,10 do
  a[i] = {set = function(x) i = x end, get = function() return i end}
  if i == 3 then break end
end
a[1].set(10)
local r2 = a[2].get()
a[2].set('a')
local r3 = a[3].get()
local r2b = a[2].get()
return tostring(r2) .. "/" .. tostring(r3) .. "/" .. tostring(r2b)`
	out, st, msg := runScriptCL(t, src, 2*time.Second)
	if st != vm.StatusOK {
		t.Fatalf("run: %v %q", st, msg)
	}
	if out != "2/3/a" {
		t.Fatalf("got %q want 2/3/a", out)
	}
}

// TestClosureSieveJustGen drains a single coroutine via
// coroutine.wrap. Passes. Establishes that one-level coroutine
// iteration works fine.
func TestClosureSieveJustGen(t *testing.T) {
	src := `local function gen(n)
  return coroutine.wrap(function ()
    for i=2,n do coroutine.yield(i) end
  end)
end
local x = gen(5)
local s = ""
while 1 do
  local n = x()
  if n == nil then break end
  s = s .. tostring(n) .. ","
end
return s`
	out, st, msg := runScriptCL(t, src, 3*time.Second)
	if st != vm.StatusOK {
		t.Fatalf("run: %v %q", st, msg)
	}
	if out != "2,3,4,5," {
		t.Fatalf("got %q want 2,3,4,5,", out)
	}
}

// TestNestedCoroutineResume is the MINIMAL REPRO of the closure.luau
// timeout. It contains no upvalue capture and no closure trickery; the
// hang is purely a coroutine.resume-from-coroutine deadlock.
//
// EXPECTED to fail until thread.go::resumeImpl releases the VM mutex
// across the resumeCh/yieldCh rendezvous. Disabled by default so the
// suite does not red-light; enable with LUAUGO_NESTED_CORO=1 to verify
// the bug is still present (or, once fixed, to gate the regression).
func TestNestedCoroutineResume(t *testing.T) {
	if os.Getenv("LUAUGO_NESTED_CORO") == "" {
		t.Skip("nested coroutine.resume deadlocks in thread.go; " +
			"set LUAUGO_NESTED_CORO=1 to demonstrate")
	}
	src := `local inner = coroutine.create(function ()
  coroutine.yield(42)
end)
local outer = coroutine.create(function ()
  local ok, v = coroutine.resume(inner)
  coroutine.yield(v)
end)
local ok, v = coroutine.resume(outer)
return tostring(v)`
	out, st, msg := runScriptCL(t, src, 3*time.Second)
	if st != vm.StatusOK {
		t.Fatalf("run: %v %q", st, msg)
	}
	if out != "42" {
		t.Fatalf("got %q want 42", out)
	}
}

// TestNestedCoroutineResumeDumpGoroutines is the diagnostic flavour of
// the minimal repro: on timeout it dumps all goroutines so the deadlock
// pair (outer holding the mutex at thread.go:123, inner blocked on
// mu.Lock; outer blocked on <-c.yieldCh at thread.go:173) is visible.
// Disabled by default; enable with LUAUGO_DUMP_GOROUTINES=1.
func TestNestedCoroutineResumeDumpGoroutines(t *testing.T) {
	if os.Getenv("LUAUGO_DUMP_GOROUTINES") == "" {
		t.Skip("set LUAUGO_DUMP_GOROUTINES=1 to dump")
	}
	src := `local inner = coroutine.create(function ()
  coroutine.yield(42)
end)
local outer = coroutine.create(function ()
  local ok, v = coroutine.resume(inner)
  coroutine.yield(v)
end)
local ok, v = coroutine.resume(outer)
return tostring(v)`
	blob, err := compiler.CompileBinary("=dump", []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	s := vm.NewState()
	defer s.Close()
	lib.OpenAll(s)
	if err := s.Load("=dump", blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.PCall(0, 1, 0)
	}()
	select {
	case <-done:
		t.Logf("script returned normally")
	case <-time.After(2 * time.Second):
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		t.Logf("stack dump:\n%s", buf[:n])
		t.Fatal("timed out")
	}
}

// TestBisectClosureLuau is a diagnostic helper that runs increasing
// prefixes of the upstream closure.luau fixture and reports the
// smallest prefix that hangs. Enable with LUAUGO_BISECT_CLOSURE=1.
//
// Bisect result: timeout first appears around line 275, i.e. inside
// the sieve at closure.luau:267-274. The sieve chains coroutines via
// `filter(p, x)` so each iteration adds another coroutine layer that
// resumes the previous one -- exactly the nested-resume pattern
// exercised by TestNestedCoroutineResume.
func TestBisectClosureLuau(t *testing.T) {
	if os.Getenv("LUAUGO_BISECT_CLOSURE") == "" {
		t.Skip("set LUAUGO_BISECT_CLOSURE=1 to run")
	}
	srcBytes, err := os.ReadFile("../../tests/conformance/closure.luau")
	if err != nil {
		t.Skipf("read closure.luau: %v", err)
	}
	lines := strings.Split(string(srcBytes), "\n")
	tries := []int{248, 254, 260, 265, 270, 275, 280, 290, 300, 310, 320}
	for _, n := range tries {
		if n > len(lines) {
			n = len(lines)
		}
		prefix := strings.Join(lines[:n], "\n")
		st, det := runPrefix(prefix)
		t.Logf("prefix=%d status=%s detail=%s", n, st, det)
		if st == "TIMEOUT" {
			break
		}
	}
}

func runPrefix(src string) (status, detail string) {
	defer func() {
		if r := recover(); r != nil {
			status = "PANIC"
			detail = fmt.Sprint(r)
		}
	}()
	blob, err := compiler.CompileBinary("closure.luau", []byte(src), compiler.Defaults())
	if err != nil {
		return "COMPILE_ERROR", err.Error()
	}
	if len(blob) > 0 && blob[0] == 0 {
		return "COMPILE_ERROR", string(blob[1:])
	}
	s := vm.NewState()
	defer s.Close()
	prev := lib.Stdout
	defer func() { lib.Stdout = prev }()
	lib.Stdout = io.Discard
	lib.OpenAll(s)
	if err := s.Load("closure.luau", blob, 0); err != nil {
		return "LOAD_ERROR", err.Error()
	}
	done := make(chan struct{})
	var st vm.Status
	var msg string
	go func() {
		defer func() { recover(); close(done) }()
		st = s.PCall(0, 0, 0)
		if st != vm.StatusOK {
			msg, _ = s.ToString(-1)
		}
	}()
	select {
	case <-done:
		if st == vm.StatusOK {
			return "OK", ""
		}
		return "RUNTIME_ERR", msg
	case <-time.After(3 * time.Second):
		return "TIMEOUT", ">3s"
	}
}
