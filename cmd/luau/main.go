// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

// Command luau is the luaugo script runner and REPL. The argument
// grammar and runtime behaviour mirror upstream's `luau` binary
// (CLI/src/Repl.cpp). The help text is rendered via Go's flag
// package, with one flag per upstream option; native-codegen-only
// flags are accepted-with-warning so existing build scripts keep
// working.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/clitool"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

const versionString = "luaugo (Luau port) - development build"

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr))
}

// run is main split out for testability. argv[0] is the program name;
// stdin/stdout/stderr are injected so tests can capture them.
func run(argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opts, code := parseArgs(argv, stdout, stderr)
	if code >= 0 {
		return code
	}

	// Files (everything before `-a`) and program args (everything
	// after) are extracted from argv directly because upstream's
	// argument grammar mixes positional and dashed args freely. See
	// clitool.SourceFiles / clitool.ProgramArgs.
	// Several flags accept `--name value` form via the Go flag package
	// (e.g. `--profile 1000`); tell the file walker to skip the value
	// so it isn't picked up as a script path.
	files := clitool.SourceFilesSkippingFlags(argv, map[string]bool{
		"O":       true,
		"g":       true,
		"profile": true,
		"fflags":  true,
	})
	progArgs := clitool.ProgramArgs(argv)

	// Execute scripts in order. Upstream creates a single global
	// state and runs each file on a fresh sandboxed thread off it
	// (CLI/src/Repl.cpp:582-585). The thread approach is what lets
	// `-i` keep the last script's environment for the REPL.
	s := vm.NewState()
	defer s.Close()
	lib.OpenAll(s)
	// We never write upstream's "luau> " banner on success; only
	// when entering the REPL.
	prevStdout := lib.Stdout
	lib.Stdout = stdout
	defer func() { lib.Stdout = prevStdout }()

	exitCode := 0
	for i, file := range files {
		isLast := i == len(files)-1
		if !runFile(s, file, progArgs, opts, isLast && opts.interactive, stderr) {
			exitCode = 1
		}
	}

	// Upstream behaviour: REPL starts when --interactive is set, OR
	// when no files were given.
	if opts.interactive || len(files) == 0 {
		runRepl(s, opts, stdin, stdout, stderr)
	}
	return exitCode
}

// ---------------------------------------------------------------------------
// Argument parsing
// ---------------------------------------------------------------------------

type cliOptions struct {
	interactive bool
	coverage    bool
	codegen     bool
	codegenCold bool
	optLevel    int
	dbgLevel    int
}

// parseArgs uses Go's flag package to handle the help/version surface
// and option validation, then walks argv a second time for the
// positional grammar (files + `-a`-separated program args) which
// `flag` cannot express on its own.
//
// Returns (opts, -1) on success, or (opts, code) when the caller
// should exit with `code` (0 for help/version, non-zero for parse
// errors).
func parseArgs(argv []string, stdout, stderr io.Writer) (cliOptions, int) {
	fs := flag.NewFlagSet(argv[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: %s [options] [file list] [-a] [arg list]\n", argv[0])
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "When file list is omitted, an interactive REPL is started instead.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Options:")
		fs.PrintDefaults()
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Compatibility flags accepted but not implemented in luaugo's pure VM:")
		fmt.Fprintln(stderr, "  --codegen, --codegen-cold, --codegen-perf, --counters, --profile,")
		fmt.Fprintln(stderr, "  --timetrace, --fflags=...")
	}

	var (
		help        = fs.Bool("help", false, "Display this usage message")
		helpShort   = fs.Bool("h", false, "alias for --help")
		version     = fs.Bool("version", false, "Print luaugo build identifier and exit")
		interactive = fs.Bool("interactive", false, "Run an interactive REPL after executing the last script")
		intShort    = fs.Bool("i", false, "alias for --interactive")
		coverage    = fs.Bool("coverage", false, "Collect code coverage and write coverage.out")
		optLevel    = fs.Int("O", 1, "Optimization level (0..2)")
		dbgLevel    = fs.Int("g", 1, "Debug-info level (0..2)")
		// Compatibility flags. These are accepted with a stderr
		// warning so upstream build scripts keep working.
		codegen     = fs.Bool("codegen", false, "(unsupported) execute code using native code generation")
		codegenCold = fs.Bool("codegen-cold", false, "(unsupported) native codegen including cold functions")
		codegenPerf = fs.Bool("codegen-perf", false, "(unsupported) native codegen + perf profiling")
		counters    = fs.Bool("counters", false, "(unsupported) collect native counters")
		profile     = fs.String("profile", "", "(unsupported) sampling profiler frequency in Hz")
		timetrace   = fs.Bool("timetrace", false, "(unsupported) record compiler time trace")
		fflags      = fs.String("fflags", "", "Accepted for compatibility; luaugo has no fast-flag system")
	)
	_ = codegenPerf
	_ = profile

	// We strip everything from `-a` / `--program-args` onward before
	// handing the remainder to flag.Parse, otherwise flag would
	// reject the trailing values (which can be arbitrary -- e.g.
	// `-a --foo`).
	args := argv[1:]
	for i, a := range args {
		if a == "-a" || a == "--program-args" {
			args = args[:i]
			break
		}
	}
	if err := fs.Parse(args); err != nil {
		// flag already printed the error.
		return cliOptions{}, 2
	}

	if *help || *helpShort {
		fs.Usage()
		return cliOptions{}, 0
	}
	if *version {
		fmt.Fprintln(stdout, versionString)
		return cliOptions{}, 0
	}
	if *optLevel < 0 || *optLevel > 2 {
		fmt.Fprintf(stderr, "luau: Error: Optimization level must be between 0 and 2 inclusive.\n")
		return cliOptions{}, 1
	}
	if *dbgLevel < 0 || *dbgLevel > 2 {
		fmt.Fprintf(stderr, "luau: Error: Debug level must be between 0 and 2 inclusive.\n")
		return cliOptions{}, 1
	}
	if *codegen || *codegenCold {
		fmt.Fprintf(stderr, "luau: warning: --codegen* requested; luaugo is interpreter-only, ignoring\n")
	}
	if *counters {
		fmt.Fprintf(stderr, "luau: warning: --counters ignored (not implemented)\n")
	}
	if *timetrace {
		fmt.Fprintf(stderr, "luau: warning: --timetrace ignored (not implemented)\n")
	}
	if *fflags != "" {
		clitool.SetFFlags(*fflags)
	}

	opts := cliOptions{
		interactive: *interactive || *intShort,
		coverage:    *coverage,
		codegen:     *codegen,
		codegenCold: *codegenCold,
		optLevel:    *optLevel,
		dbgLevel:    *dbgLevel,
	}
	return opts, -1
}

// ---------------------------------------------------------------------------
// Run mode
// ---------------------------------------------------------------------------

// runFile is the per-file execution path. Mirrors upstream's runFile
// in CLI/src/Repl.cpp:572-647: read source, sandbox a new thread,
// compile, load, resume with program args as varargs, optionally
// drop into the REPL on the resulting thread.
func runFile(parent *vm.State, file string, progArgs []string, opts cliOptions, dropToRepl bool, stderr io.Writer) bool {
	src, err := clitool.ReadSource(file)
	if err != nil {
		fmt.Fprintf(stderr, "luau: Error opening %s: %v\n", file, err)
		return false
	}
	chunkname := "@" + normalizePath(file)

	co := parent.NewThread()
	co.SandboxThread()

	blob, err := compiler.CompileBinary(chunkname, src, compilerOpts(opts))
	if err != nil {
		fmt.Fprintf(stderr, "luau: %s: %v\n", file, err)
		// Pop the thread from parent's stack to keep things tidy.
		parent.Pop(1)
		return false
	}
	if len(blob) > 0 && blob[0] == 0 {
		fmt.Fprintf(stderr, "luau: %s: %s\n", file, string(blob[1:]))
		parent.Pop(1)
		return false
	}
	if err := co.Load(chunkname, blob, 0); err != nil {
		fmt.Fprintf(stderr, "luau: %s: %v\n", file, err)
		parent.Pop(1)
		return false
	}

	// Push program args on the *resumer* (parent) stack. luaugo's
	// Resume moves nargs from `from`'s top onto the coroutine, where
	// they become the chunk's `...` varargs. (Upstream pushes
	// directly on the coroutine because lua_resume there takes from
	// the same stack; the luaugo API splits those.)
	parent.CheckStack(len(progArgs))
	for _, a := range progArgs {
		parent.PushString(a)
	}
	st := co.Resume(parent, len(progArgs))
	ok := st == vm.StatusOK
	if !ok {
		var msg string
		if st == vm.StatusYield {
			msg = "thread yielded unexpectedly"
		} else if m, hadMsg := co.ToString(-1); hadMsg {
			msg = m
		}
		fmt.Fprintf(stderr, "%s\nstacktrace:\n%s", msg, debugTrace(co))
	}

	if dropToRepl {
		// Upstream invokes runReplImpl on the per-script thread so
		// the user can poke at the post-script state. Mirror that.
		runReplImpl(co, opts, os.Stdin, os.Stdout, stderr)
	}

	// Drop the thread from parent's stack (upstream calls
	// lua_pop(GL, 1) at Repl.cpp:645).
	parent.Pop(1)
	return ok
}

// normalizePath turns OS-specific separators into forward slashes to
// match upstream's normalizePath in CLI/src/FileUtils.cpp -- the
// chunkname is embedded into debug info so the output is identical
// across platforms.
func normalizePath(p string) string {
	return strings.ReplaceAll(filepath.ToSlash(p), `\`, "/")
}

func compilerOpts(opts cliOptions) compiler.Options {
	co := compiler.Defaults()
	co.OptimizationLevel = compiler.OptimizationLevel(opts.optLevel)
	co.DebugLevel = compiler.DebugLevel(opts.dbgLevel)
	if opts.coverage {
		co.CoverageLevel = compiler.CoverageStatement
	}
	return co
}

// debugTrace mirrors upstream's lua_debugtrace by walking the active
// call frames of the failing coroutine. luaugo doesn't yet expose a
// frame walker through pkg/vm/contract.go, so for now we print an
// empty trace placeholder -- the error message itself already has the
// most-recent location prefix.
func debugTrace(co *vm.State) string {
	_ = co
	return "  [stack unwind details not yet exposed by the luaugo VM]\n"
}

// ---------------------------------------------------------------------------
// REPL
// ---------------------------------------------------------------------------

// runRepl starts a stand-alone REPL with a freshly sandboxed thread.
// This is the entry point taken when the user invokes `luau` with no
// files at all; the per-file `-i` path uses runReplImpl directly so
// the script's own state survives into the REPL.
func runRepl(parent *vm.State, opts cliOptions, stdin io.Reader, stdout, stderr io.Writer) {
	co := parent.NewThread()
	co.SandboxThread()
	runReplImpl(co, opts, stdin, stdout, stderr)
	parent.Pop(1)
}

// runReplImpl is the REPL inner loop. It mirrors upstream's
// runReplImpl in CLI/src/Repl.cpp:500-549. We use bufio.Scanner with
// the canonical 64 KiB buffer; readline-style editing is not provided
// (upstream uses the isocline vendor library, which is out of scope
// for a pure-Go port).
//
// Behaviour:
//
//   - First line: try as expression by prefixing `return `. If that
//     compiles and runs cleanly any returned values are pretty-printed
//     via the global `print` (or `_PRETTYPRINT` if defined).
//   - On failure (or on continuation lines), buffer until the input
//     parses as a statement. Errors ending in `<eof>` mean the parser
//     wants more input, so we read another line.
//   - Empty buffer + EOF exits.
func runReplImpl(co *vm.State, opts cliOptions, stdin io.Reader, stdout, stderr io.Writer) {
	fmt.Fprintln(stdout, versionString)
	fmt.Fprintln(stdout, "Type expressions to evaluate, statements to execute. EOF (Ctrl-D / Ctrl-Z) exits.")

	br := bufio.NewReader(stdin)
	var buffer strings.Builder
	for {
		prompt := "> "
		if buffer.Len() > 0 {
			prompt = ">> "
		}
		fmt.Fprint(stdout, prompt)
		line, err := br.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")

		// EOF on an empty buffer exits cleanly.
		if err == io.EOF && line == "" && buffer.Len() == 0 {
			fmt.Fprintln(stdout)
			return
		}

		// Upstream's flow: if the buffer is empty, first try `return <line>`.
		// If that produces no error we accept; otherwise we fall through to
		// the statement path.
		if buffer.Len() == 0 {
			if errStr := evalReplLine(co, "return "+line, opts, stdout); errStr == "" {
				if err == io.EOF {
					return
				}
				continue
			}
		}

		if buffer.Len() > 0 {
			buffer.WriteByte('\n')
		}
		buffer.WriteString(line)

		errStr := evalReplLine(co, buffer.String(), opts, stdout)

		// Upstream detects `<eof>` to know the parser wants more
		// input: see CLI/src/Repl.cpp:537. Same logic here.
		if strings.HasSuffix(errStr, "<eof>") {
			if err == io.EOF {
				// User hit EOF mid-statement; abandon buffer.
				fmt.Fprintln(stderr, "luau: unexpected EOF in incomplete statement")
				return
			}
			continue
		}
		if errStr != "" {
			fmt.Fprintln(stdout, errStr)
		}
		buffer.Reset()
		if err == io.EOF {
			return
		}
	}
}

// evalReplLine compiles and runs a single REPL chunk. Returns the
// error message (without trailing newline) or empty on success.
// Successful chunks with return values are pretty-printed via the
// global `print` to mimic upstream's `_PRETTYPRINT` lookup.
func evalReplLine(co *vm.State, src string, opts cliOptions, stdout io.Writer) string {
	blob, err := compiler.CompileBinary("=stdin", []byte(src), compilerOpts(opts))
	if err != nil {
		return err.Error()
	}
	if len(blob) > 0 && blob[0] == 0 {
		return string(blob[1:])
	}
	if err := co.Load("=stdin", blob, 0); err != nil {
		return err.Error()
	}
	base := co.Top() - 1
	if st := co.PCall(0, -1, 0); st != vm.StatusOK {
		msg, _ := co.ToString(-1)
		co.SetTop(base)
		return msg
	}
	nres := co.Top() - base
	if nres > 0 {
		// Pretty-print: upstream looks up `_PRETTYPRINT`, falling
		// back to the global `print` (CLI/src/Repl.cpp:268-273).
		// `GetGlobal` is a *raw* lookup on the per-thread globals
		// table, which under SandboxThread is empty (read-through
		// to the parent happens via __index, not raw access). So we
		// push the globals table and use GetField, which routes
		// through indexValue and respects the sandbox metatable.
		co.PushGlobalsTable()
		co.PushString("_PRETTYPRINT")
		co.GetTable(-2)
		if co.IsNil(-1) {
			co.Pop(1)
			co.PushString("print")
			co.GetTable(-2)
		}
		// Stack now: [..., values..., globals, printer]. Remove
		// globals so [..., values..., printer] then Insert printer
		// below the values.
		co.Remove(-2)
		co.Insert(base + 1)
		if st := co.PCall(nres, 0, 0); st != vm.StatusOK {
			msg, _ := co.ToString(-1)
			co.SetTop(base)
			return msg
		}
	}
	co.SetTop(base)
	_ = stdout
	return ""
}

