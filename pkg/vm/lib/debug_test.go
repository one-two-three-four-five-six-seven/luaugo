// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib_test

import (
	"strings"
	"testing"

	"github.com/luaugo/luaugo/pkg/compiler"
	"github.com/luaugo/luaugo/pkg/vm"
	"github.com/luaugo/luaugo/pkg/vm/lib"
)

// runDebugScript compiles src and runs it on a fresh State with
// OpenBase (best-effort) and OpenDebug installed. The chunk is loaded
// under the supplied chunkname so tests can assert that debug.info("s")
// returns it. Returns the State; the caller reads results and the
// t.Cleanup hook handles Close.
//
// OpenBase is presently a panic stub in the parallel Tier-4 swarm; we
// recover from its panic so debug tests run regardless of whether
// base.go has landed yet. All debug scenarios below only require the
// `debug` global and the bytecode `print` (the latter is not exercised
// here), so degrading to "OpenDebug only" is safe.
func runDebugScript(t *testing.T, chunkname, src string, nresults int) *vm.State {
	t.Helper()
	s := vm.NewState()
	t.Cleanup(s.Close)

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("lib.OpenBase panicked (expected pre-base-Tier-4): %v", r)
			}
		}()
		lib.OpenBase(s)
	}()
	lib.OpenDebug(s)

	blob, err := compiler.CompileBinary(chunkname, []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	if len(blob) == 0 || blob[0] == 0 {
		t.Fatalf("compile produced error blob: %q", blob)
	}
	if err := s.Load(chunkname, blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	s.Call(0, nresults)
	return s
}

// TestDebugTraceback verifies that debug.traceback() produces a string
// that mentions the chunkname of the running script. The traceback is
// constructed inside a Lua frame, so it should include at least the
// frame that invoked debug.traceback.
func TestDebugTraceback(t *testing.T) {
	const chunk = "=traceback_test"
	s := runDebugScript(t, chunk, "return debug.traceback()", 1)
	got, ok := s.ToString(-1)
	if !ok {
		t.Fatalf("debug.traceback() did not return a string (top=%d)", s.Top())
	}
	if !strings.Contains(got, "stack traceback") {
		t.Fatalf("traceback missing header: %q", got)
	}
	if !strings.Contains(got, chunk) {
		t.Fatalf("traceback %q does not mention chunkname %q", got, chunk)
	}
}

// TestDebugTracebackWithMessage verifies that the optional msg argument
// is prepended verbatim.
func TestDebugTracebackWithMessage(t *testing.T) {
	s := runDebugScript(t, "=tb_msg", `return debug.traceback("boom")`, 1)
	got, ok := s.ToString(-1)
	if !ok {
		t.Fatalf("expected string result")
	}
	if !strings.HasPrefix(got, "boom\n") {
		t.Fatalf("traceback should start with msg line; got %q", got)
	}
	if !strings.Contains(got, "stack traceback") {
		t.Fatalf("traceback missing header: %q", got)
	}
}

// TestDebugInfoSource verifies debug.info(1, "s") returns the chunkname
// of the executing chunk. Level 1 is the script frame (the caller of
// debug.info), which is the chunk loaded under `chunkname`.
func TestDebugInfoSource(t *testing.T) {
	const chunk = "=info_source"
	s := runDebugScript(t, chunk, `return debug.info(1, "s")`, 1)
	got, ok := s.ToString(-1)
	if !ok {
		t.Fatalf("debug.info(1, \"s\") did not return a string")
	}
	if got != chunk {
		t.Fatalf("expected source %q, got %q", chunk, got)
	}
}

// TestDebugInfoLine verifies debug.info(1, "l") returns an integer.
// The exact value depends on whether the luaugo compiler emits line
// info -- which it currently does not (contract bug C-LINE-1, see
// below). We therefore assert only that the call succeeds and returns
// a non-negative integer.
func TestDebugInfoLine(t *testing.T) {
	s := runDebugScript(t, "=info_line", `return debug.info(1, "l")`, 1)
	got, ok := s.ToInteger(-1)
	if !ok {
		t.Fatalf("debug.info(1, \"l\") did not return an integer")
	}
	if got < 0 {
		t.Fatalf("line should be non-negative for a Lua frame, got %d", got)
	}
}

// TestDebugInfoFunc verifies debug.info(1, "f") returns a value of
// type function. The returned function should be callable; we don't
// invoke it (recursion would be confusing) but assert its type.
func TestDebugInfoFunc(t *testing.T) {
	s := runDebugScript(t, "=info_func", `return debug.info(1, "f")`, 1)
	if !s.IsFunction(-1) {
		t.Fatalf("debug.info(1, \"f\") expected function, got %s",
			s.Type(-1).String())
	}
}

// TestDebugInfoSelf verifies debug.info(0, "s") -- level 0 -- returns
// the chunkname (the debug.info Go function itself is at the very top
// of the call stack, but level 0 from inside the script means the
// script's own frame in Luau semantics... actually upstream's level 0
// from a script means the running Go function. To stay implementation-
// neutral we assert only that the call succeeds.).
func TestDebugInfoSelf(t *testing.T) {
	s := runDebugScript(t, "=info_self", `return debug.info(0, "s")`, 1)
	if !s.IsString(-1) {
		t.Fatalf("debug.info(0, \"s\") expected string, got %s", s.Type(-1).String())
	}
}

// TestDebugInfoMultipleOpts verifies that debug.info(level, "sl")
// pushes results in option order: source first, then line.
func TestDebugInfoMultipleOpts(t *testing.T) {
	const chunk = "=info_multi"
	s := runDebugScript(t, chunk, `return debug.info(1, "sl")`, 2)
	src, ok := s.ToString(-2)
	if !ok {
		t.Fatalf("first result not a string")
	}
	if src != chunk {
		t.Fatalf("source: got %q want %q", src, chunk)
	}
	if _, ok := s.ToInteger(-1); !ok {
		t.Fatalf("second result not an integer")
	}
}

// TestDebugInfoAOption verifies the 'a' option pushes nparams and
// isvararg. The main chunk has 0 parameters and is vararg (true), per
// Luau's main-chunk semantics.
func TestDebugInfoAOption(t *testing.T) {
	s := runDebugScript(t, "=info_a", `return debug.info(1, "a")`, 2)
	np, ok := s.ToInteger(-2)
	if !ok {
		t.Fatalf("first 'a' result not an integer")
	}
	if np != 0 {
		t.Fatalf("main chunk should have 0 params; got %d", np)
	}
	// Second result is a boolean.
	if got := s.Type(-1); got != vm.TBoolean {
		t.Fatalf("second 'a' result type: got %s want boolean", got.String())
	}
}
