// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

// Command luau-compile mirrors upstream's `luau-compile` binary
// (CLI/src/Compile.cpp). It compiles one or more Lua/Luau source
// files and emits the result in a chosen format: human-readable
// disassembly (`--text`, the default), a raw bytecode blob
// (`--binary`), source-attached optimizer remarks (`--remarks`), or
// nothing at all (`--null`, useful for timing pure compile cost).
//
// Native-codegen modes (`--codegen`, `--codegenir`, `--codegenasm`,
// `--codegenverbose`, `--codegennull`) are accepted-with-warning so
// existing build scripts keep working but produce only the standard
// bytecode disassembly; luaugo has no native code generator.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/clitool"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
)

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

// compileFormat is the parsed `--<mode>` selector. Names mirror
// upstream's CompileFormat enum (CLI/src/Compile.cpp:24-35) and the
// strings recognized by getCompileFormat (line 70).
type compileFormat int

const (
	formatText           compileFormat = iota // --text (default)
	formatBinary                              // --binary
	formatRemarks                             // --remarks
	formatCodegen                             // --codegen
	formatCodegenAsm                          // --codegenasm
	formatCodegenIr                           // --codegenir
	formatCodegenVerbose                      // --codegenverbose
	formatCodegenNull                         // --codegennull
	formatNull                                // --null
)

// run is the testable inner main. argv[0] is the program name; the
// returned int is the exit code.
func run(argv []string, stdout, stderr io.Writer) int {
	opts, code := parseArgs(argv, stderr)
	if code >= 0 {
		return code
	}

	// Many flags accept `--name value` form via the Go flag package;
	// tell the file walker to skip those values so a value like
	// `stats.json` isn't picked up as a positional file argument.
	files := clitool.SourceFilesSkippingFlags(argv, map[string]bool{
		"O":             true,
		"g":             true,
		"t":             true,
		"vector-lib":    true,
		"vector-ctor":   true,
		"vector-type":   true,
		"target":        true,
		"record-stats":  true,
		"stats-file":    true,
		"fflags":        true,
	})
	if len(files) == 0 {
		fmt.Fprintln(stderr, "luau-compile: no input files")
		return 1
	}

	// Stats aggregation. Upstream's --record-stats=total/file/function
	// emits a stats.json sidecar; luaugo currently honours --null and
	// --record-stats=total by tracking aggregate timings here.
	var agg compileStats
	failed := 0
	for _, file := range files {
		fs, ok := compileFile(file, opts, stdout, stderr)
		if !ok {
			failed++
			continue
		}
		agg.add(fs)
	}

	// Match upstream's summary printout for --null mode
	// (CLI/src/Compile.cpp:650-659). The codegen-null branch also
	// prints native-code size but we have no codegen so the codegen
	// counters are always zero.
	if opts.format == formatNull {
		fmt.Fprintf(stdout, "Compiled %d KLOC into %d KB bytecode (read %.2fs, parse %.2fs, compile %.2fs)\n",
			agg.lines/1000,
			agg.bytecode/1024,
			agg.readTime.Seconds(),
			agg.parseTime.Seconds(),
			agg.compileTime.Seconds(),
		)
	} else if opts.format == formatCodegenNull {
		fmt.Fprintf(stdout, "Compiled %d KLOC into %d KB bytecode => %d KB native code (%.2fx) (read %.2fs, parse %.2fs, compile %.2fs, codegen %.2fs)\n",
			agg.lines/1000,
			agg.bytecode/1024,
			0,
			0.0,
			agg.readTime.Seconds(),
			agg.parseTime.Seconds(),
			agg.compileTime.Seconds(),
			0.0,
		)
		// Upstream also prints "Lowering: regalloc failed: ..." here;
		// there is no equivalent in a pure interpreter.
	}

	if failed > 0 {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Argument parsing
// ---------------------------------------------------------------------------

type compileOptions struct {
	format       compileFormat
	optLevel     int
	dbgLevel     int
	typeInfo     int
	vectorLib    string
	vectorCtor   string
	vectorType   string
	dumpConsts   bool
	target       string // accepted-with-warning; codegen-only
	recordStats  string // "", "total", "file", "function"
	statsFile    string
	bcSummary    bool
}

func parseArgs(argv []string, stderr io.Writer) (compileOptions, int) {
	fs := flag.NewFlagSet(argv[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: %s [--mode] [options] [file list]\n", argv[0])
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Available modes (pick one; defaults to --text):")
		fmt.Fprintln(stderr, "  --text       human-readable bytecode disassembly")
		fmt.Fprintln(stderr, "  --binary     raw bytecode blob written to stdout")
		fmt.Fprintln(stderr, "  --remarks    source listing with optimizer remarks")
		fmt.Fprintln(stderr, "  --null       compile only, report timing on stdout")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Codegen modes (accepted for upstream compatibility, fall back to --text):")
		fmt.Fprintln(stderr, "  --codegen --codegenasm --codegenir --codegenverbose --codegennull")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Options:")
		fs.PrintDefaults()
	}

	// Mode flags are mutually exclusive; we track them via callbacks
	// onto a single `mode` var. Go's flag package doesn't have a
	// natural one-of selector so we let any flag set mode and warn
	// if more than one was passed.
	var (
		mode = "text"
		help bool
	)
	setMode := func(name string) func(string) error {
		return func(string) error {
			if mode != "text" && mode != name {
				fmt.Fprintf(stderr, "luau-compile: warning: multiple modes set (%s overrides %s)\n", name, mode)
			}
			mode = name
			return nil
		}
	}
	fs.BoolFunc("text", "human-readable bytecode disassembly (default)", setMode("text"))
	fs.BoolFunc("binary", "raw bytecode blob on stdout", setMode("binary"))
	fs.BoolFunc("remarks", "source listing with optimizer remarks", setMode("remarks"))
	fs.BoolFunc("null", "compile only, report timing to stdout", setMode("null"))
	fs.BoolFunc("codegen", "(unsupported) native codegen + bytecode", setMode("codegen"))
	fs.BoolFunc("codegenasm", "(unsupported) native assembly only", setMode("codegenasm"))
	fs.BoolFunc("codegenir", "(unsupported) native IR only", setMode("codegenir"))
	fs.BoolFunc("codegenverbose", "(unsupported) native IR + asm + outlined", setMode("codegenverbose"))
	fs.BoolFunc("codegennull", "(unsupported) native codegen, no output", setMode("codegennull"))

	fs.BoolVar(&help, "h", false, "alias for --help")
	fs.BoolVar(&help, "help", false, "Display this usage message")

	var opts compileOptions
	opts.statsFile = "stats.json"
	fs.IntVar(&opts.optLevel, "O", 1, "Optimization level (0..2)")
	fs.IntVar(&opts.dbgLevel, "g", 1, "Debug-info level (0..2)")
	fs.IntVar(&opts.typeInfo, "t", 0, "Type-info level (0..1)")
	fs.StringVar(&opts.vectorLib, "vector-lib", "", "Library providing the vector type")
	fs.StringVar(&opts.vectorCtor, "vector-ctor", "", "Function constructing a vector value")
	fs.StringVar(&opts.vectorType, "vector-type", "", "Name of the vector type")
	fs.StringVar(&opts.target, "target", "", "(unsupported) codegen target (a64, x64, a64_nf, x64_ms)")
	fs.StringVar(&opts.recordStats, "record-stats", "", "Record compilation stats (total|file|function)")
	fs.StringVar(&opts.statsFile, "stats-file", opts.statsFile, "File in which compilation stats are recorded")
	fs.BoolVar(&opts.bcSummary, "bytecode-summary", false, "Compute bytecode operation distribution (requires --record-stats=function)")
	fs.BoolVar(&opts.dumpConsts, "dump-constants", false, "Dump constant tables (text mode only)")
	fflags := fs.String("fflags", "", "Accepted for compatibility; luaugo has no fast-flag system")

	if err := fs.Parse(argv[1:]); err != nil {
		return opts, 2
	}
	if help {
		fs.Usage()
		return opts, 0
	}
	if opts.optLevel < 0 || opts.optLevel > 2 {
		fmt.Fprintf(stderr, "Error: Optimization level must be between 0 and 2 inclusive.\n")
		return opts, 1
	}
	if opts.dbgLevel < 0 || opts.dbgLevel > 2 {
		fmt.Fprintf(stderr, "Error: Debug level must be between 0 and 2 inclusive.\n")
		return opts, 1
	}
	if opts.typeInfo < 0 || opts.typeInfo > 1 {
		fmt.Fprintf(stderr, "Error: Type info level must be between 0 and 1 inclusive.\n")
		return opts, 1
	}
	if opts.bcSummary && opts.recordStats != "function" {
		fmt.Fprintf(stderr, "Error: Required '--record-stats=function' for '--bytecode-summary'.\n")
		return opts, 1
	}
	if opts.target != "" {
		fmt.Fprintf(stderr, "luau-compile: warning: --target=%s ignored (luaugo has no native codegen)\n", opts.target)
	}
	if *fflags != "" {
		clitool.SetFFlags(*fflags)
	}

	// Map mode string to enum.
	switch mode {
	case "text":
		opts.format = formatText
	case "binary":
		opts.format = formatBinary
	case "remarks":
		opts.format = formatRemarks
	case "null":
		opts.format = formatNull
	case "codegen":
		opts.format = formatCodegen
		fmt.Fprintln(stderr, "luau-compile: warning: --codegen falls back to --text (no native codegen)")
	case "codegenasm":
		opts.format = formatCodegenAsm
		fmt.Fprintln(stderr, "luau-compile: warning: --codegenasm falls back to --text (no native codegen)")
	case "codegenir":
		opts.format = formatCodegenIr
		fmt.Fprintln(stderr, "luau-compile: warning: --codegenir falls back to --text (no native codegen)")
	case "codegenverbose":
		opts.format = formatCodegenVerbose
		fmt.Fprintln(stderr, "luau-compile: warning: --codegenverbose falls back to --text (no native codegen)")
	case "codegennull":
		opts.format = formatCodegenNull
		fmt.Fprintln(stderr, "luau-compile: warning: --codegennull falls back to --null (no native codegen)")
	}
	return opts, -1
}

// ---------------------------------------------------------------------------
// Compile
// ---------------------------------------------------------------------------

// compileStats mirrors upstream's CompileStats struct (lines 135-172).
// We only track the fields that have an analogue in a pure-VM
// implementation; codegen fields stay zero.
type compileStats struct {
	lines                    int
	bytecode                 int
	bytecodeInstructionCount int
	codegen                  int

	readTime    time.Duration
	parseTime   time.Duration
	compileTime time.Duration
}

func (s *compileStats) add(o compileStats) {
	s.lines += o.lines
	s.bytecode += o.bytecode
	s.bytecodeInstructionCount += o.bytecodeInstructionCount
	s.codegen += o.codegen
	s.readTime += o.readTime
	s.parseTime += o.parseTime
	s.compileTime += o.compileTime
}

// compileFile compiles one input file and emits its output, returning
// per-file stats for aggregation. Mirrors upstream's compileFile
// (CLI/src/Compile.cpp:297-420).
func compileFile(path string, opts compileOptions, stdout, stderr io.Writer) (compileStats, bool) {
	var stats compileStats

	t0 := time.Now()
	src, err := clitool.ReadSource(path)
	if err != nil {
		fmt.Fprintf(stderr, "Error opening %s: %v\n", path, err)
		return stats, false
	}
	stats.readTime = time.Since(t0)
	stats.lines = countLines(src)

	chunkname := filepath.Base(path)
	cOpts := compilerOpts(opts)

	t1 := time.Now()
	switch opts.format {
	case formatBinary:
		blob, err := compiler.CompileBinary(chunkname, src, cOpts)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return stats, false
		}
		if len(blob) > 0 && blob[0] == 0 {
			fmt.Fprintf(stderr, "%s: %s\n", path, string(blob[1:]))
			return stats, false
		}
		stats.compileTime = time.Since(t1)
		stats.bytecode = len(blob)
		if _, err := stdout.Write(blob); err != nil {
			fmt.Fprintf(stderr, "write stdout: %v\n", err)
			return stats, false
		}

	case formatText, formatCodegen, formatCodegenAsm, formatCodegenIr, formatCodegenVerbose:
		mod, err := compiler.CompileSource(chunkname, src, cOpts)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return stats, false
		}
		stats.compileTime = time.Since(t1)
		// We don't track per-proto instruction count separately; the
		// total is the sum of len(p.Code) across protos.
		for _, p := range mod.Protos {
			stats.bytecodeInstructionCount += len(p.Code)
		}
		// stats.bytecode is "encoded size"; recompute by encoding.
		if enc, err := compiler.CompileBinary(chunkname, src, cOpts); err == nil {
			stats.bytecode = len(enc)
		}
		fmt.Fprint(stdout, bytecode.Disassemble(mod))

	case formatRemarks:
		// Remarks-only output: upstream emits the source listing
		// with `--!` style comments where the optimizer applied a
		// transformation. luaugo doesn't surface remarks separately,
		// so we just print the source verbatim with a leading
		// comment that documents the gap.
		if _, err := compiler.CompileSource(chunkname, src, cOpts); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return stats, false
		}
		stats.compileTime = time.Since(t1)
		fmt.Fprintf(stdout, "-- remarks for %s (luaugo does not emit optimizer remarks yet)\n", path)
		stdout.Write(src)
		if len(src) > 0 && src[len(src)-1] != '\n' {
			fmt.Fprintln(stdout)
		}

	case formatNull, formatCodegenNull:
		// Compile and discard; only timing counters matter.
		blob, err := compiler.CompileBinary(chunkname, src, cOpts)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return stats, false
		}
		if len(blob) > 0 && blob[0] == 0 {
			fmt.Fprintf(stderr, "%s: %s\n", path, string(blob[1:]))
			return stats, false
		}
		stats.compileTime = time.Since(t1)
		stats.bytecode = len(blob)
	}

	// Stats sidecar. We only implement the "total" record level
	// fully; "file" and "function" granularities require per-proto
	// metric collection that the compiler doesn't currently expose.
	if opts.recordStats == "total" || opts.recordStats == "file" || opts.recordStats == "function" {
		if err := writeStats(opts.statsFile, path, stats, opts); err != nil {
			fmt.Fprintf(stderr, "stats: %v\n", err)
		}
	}
	return stats, true
}

func compilerOpts(opts compileOptions) compiler.Options {
	co := compiler.Defaults()
	co.OptimizationLevel = compiler.OptimizationLevel(opts.optLevel)
	co.DebugLevel = compiler.DebugLevel(opts.dbgLevel)
	co.TypeInfoLevel = compiler.TypeInfoLevel(opts.typeInfo)
	co.VectorLib = opts.vectorLib
	co.VectorCtor = opts.vectorCtor
	co.VectorType = opts.vectorType
	return co
}

func countLines(src []byte) int {
	if len(src) == 0 {
		return 0
	}
	n := 0
	for _, b := range src {
		if b == '\n' {
			n++
		}
	}
	if src[len(src)-1] != '\n' {
		n++
	}
	return n
}

// writeStats emits the stats.json sidecar in a layout compatible with
// upstream's serializeCompileStats (CLI/src/Compile.cpp:265-283). We
// only ever emit the "total" shape for now; "file" and "function"
// granularities fall back to the same totals.
func writeStats(path, source string, s compileStats, opts compileOptions) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// Single-file shape is just the totals object; multi-file would
	// wrap them in a `{"file": ...}` map but we record one file at a
	// time so the wrapping happens elsewhere if needed.
	fmt.Fprintf(f, `{
    "source": "%s",
    "lines": %d,
    "bytecode": %d,
    "bytecodeInstructionCount": %d,
    "codegen": %d,
    "readTime": %f,
    "miscTime": 0.0,
    "parseTime": %f,
    "compileTime": %f,
    "codegenTime": 0.0
}
`,
		escapeJSON(source),
		s.lines,
		s.bytecode,
		s.bytecodeInstructionCount,
		s.codegen,
		s.readTime.Seconds(),
		s.parseTime.Seconds(),
		s.compileTime.Seconds(),
	)
	_ = opts
	return nil
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, "/")
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
