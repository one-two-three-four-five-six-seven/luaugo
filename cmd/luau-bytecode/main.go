// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

// Command luau-bytecode mirrors upstream's `luau-bytecode` binary
// (CLI/src/Bytecode.cpp). For each input source file it compiles to
// bytecode and emits a per-function summary of opcode frequencies
// into a JSON file. The default output file is `bytecode-summary.json`.
//
// Upstream's summarizer recurses through the function nest tree with a
// configurable depth limit; the JSON wrapper records counts at each
// nesting level. luaugo doesn't have a nesting parameter exposed on
// the command line (upstream's binary doesn't either; the limit is
// hard-coded to 0), so we emit a single counts bucket per function.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/clitool"
	"github.com/one-two-three-four-five-six-seven/luaugo/internal/common"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
)

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

func run(argv []string, stdout, stderr io.Writer) int {
	opts, code := parseArgs(argv, stderr)
	if code >= 0 {
		return code
	}

	// summary-file, fflags and O/g accept `--name value` form via the
	// Go flag package; tell the file walker to skip those values.
	files := clitool.SourceFilesSkippingFlags(argv, map[string]bool{
		"summary-file": true,
		"fflags":       true,
		"O":            true,
		"g":            true,
	})
	if len(files) == 0 {
		fmt.Fprintln(stderr, "luau-bytecode: no input files")
		return 1
	}

	summaries := make([]fileSummary, 0, len(files))

	for _, file := range files {
		src, err := clitool.ReadSource(file)
		if err != nil {
			fmt.Fprintf(stderr, "Error opening %s: %v\n", file, err)
			return 1
		}
		chunkname := filepath.Base(file)
		mod, err := compiler.CompileSource(chunkname, src, compilerOpts(opts))
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", file, err)
			return 1
		}
		summaries = append(summaries, fileSummary{
			path: file,
			fns:  summarizeModule(mod),
		})
	}

	// Write JSON. The layout matches upstream's
	// serializeSummaries / serializeScriptSummary / serializeFunctionSummary
	// in CLI/src/Bytecode.cpp:185-261 -- a top-level object keyed by
	// file path, mapping to an array of per-function records.
	f, err := os.Create(opts.summaryFile)
	if err != nil {
		fmt.Fprintf(stderr, "Unable to open '%s': %v\n", opts.summaryFile, err)
		return 1
	}
	defer f.Close()
	if err := writeSummary(f, summaries); err != nil {
		fmt.Fprintf(stderr, "write summary: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Bytecode summary written to '%s'\n", opts.summaryFile)
	return 0
}

// ---------------------------------------------------------------------------
// CLI options
// ---------------------------------------------------------------------------

type bytecodeOptions struct {
	optLevel     int
	dbgLevel     int
	summaryFile  string
}

func parseArgs(argv []string, stderr io.Writer) (bytecodeOptions, int) {
	fs := flag.NewFlagSet(argv[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: %s [options] [file list]\n", argv[0])
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Computes a per-function opcode frequency summary and writes")
		fmt.Fprintln(stderr, "it as JSON. One entry per input source file.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Options:")
		fs.PrintDefaults()
	}

	var (
		help bool
		opts bytecodeOptions
	)
	fs.BoolVar(&help, "h", false, "alias for --help")
	fs.BoolVar(&help, "help", false, "Display this usage message")
	fs.IntVar(&opts.optLevel, "O", 1, "Optimization level (0..2)")
	fs.IntVar(&opts.dbgLevel, "g", 1, "Debug-info level (0..2)")
	fs.StringVar(&opts.summaryFile, "summary-file", "bytecode-summary.json",
		"File in which the bytecode summary will be written")
	fflags := fs.String("fflags", "", "Accepted for compatibility; luaugo has no fast-flag system")

	if err := fs.Parse(argv[1:]); err != nil {
		return opts, 2
	}
	if help {
		fs.Usage()
		return opts, 0
	}
	if opts.optLevel < 0 || opts.optLevel > 2 {
		fmt.Fprintln(stderr, "Error: Optimization level must be between 0 and 2 inclusive.")
		return opts, 1
	}
	if opts.dbgLevel < 0 || opts.dbgLevel > 2 {
		fmt.Fprintln(stderr, "Error: Debug level must be between 0 and 2 inclusive.")
		return opts, 1
	}
	if opts.summaryFile == "" {
		fmt.Fprintln(stderr, "Error: filename missing for '--summary-file'.")
		return opts, 1
	}
	if *fflags != "" {
		clitool.SetFFlags(*fflags)
	}
	return opts, -1
}

func compilerOpts(opts bytecodeOptions) compiler.Options {
	co := compiler.Defaults()
	co.OptimizationLevel = compiler.OptimizationLevel(opts.optLevel)
	co.DebugLevel = compiler.DebugLevel(opts.dbgLevel)
	// Upstream hard-wires typeInfoLevel = 1 in copts() so the
	// downstream summarizer sees TypeInfo entries
	// (CLI/src/Bytecode.cpp:28). Mirror that.
	co.TypeInfoLevel = compiler.TypeInfoFunc
	return co
}

// ---------------------------------------------------------------------------
// Summarization
// ---------------------------------------------------------------------------

// fileSummary holds the per-input-file aggregation -- the input path
// plus the per-function records harvested from its compiled module.
type fileSummary struct {
	path string
	fns  []functionSummary
}

// functionSummary captures the per-proto data the JSON output needs:
// the proto's debug name + line, plus an opcode histogram. Upstream
// records a 2-D `counts[nesting][op]` array; we fix nesting=0 so the
// array always has length 1.
type functionSummary struct {
	source string
	name   string
	line   int
	counts [common.OpCount]uint32
}

func summarizeModule(m *bytecode.Module) []functionSummary {
	out := make([]functionSummary, 0, len(m.Protos))
	// Resolve a string from the constant pool by 1-based index, with
	// 0 meaning "no name".
	resolveStr := func(idx uint32) string {
		if idx == 0 || int(idx) > len(m.Strings) {
			return ""
		}
		return m.Strings[idx-1]
	}
	for i, p := range m.Protos {
		name := resolveStr(p.DebugName)
		if name == "" && uint32(i) == m.MainProto {
			name = "main"
		}
		fs := functionSummary{
			source: "", // Source file is set per-script in writeSummary.
			name:   name,
			line:   int(p.LineDefined),
		}
		summarizeProto(p, &fs)
		out = append(out, fs)
	}
	return out
}

// summarizeProto walks p.Code, incrementing fs.counts[op] for each
// instruction. Instructions that carry an AUX word advance two slots
// per logical op (mirrors upstream's instruction iterator in
// CLI/src/Bytecode.cpp:summarizeBytecode).
func summarizeProto(p *bytecode.Proto, fs *functionSummary) {
	code := p.Code
	for pc := 0; pc < len(code); {
		insn := code[pc]
		op := common.InsnOp(insn)
		if int(op) < int(common.OpCount) {
			fs.counts[op]++
		}
		pc++
		if op.HasAux() && pc < len(code) {
			pc++ // skip the aux word
		}
		// OpNewClosure is followed by one OpCapture per upvalue. The
		// captures are themselves valid opcodes with their own
		// histogram entry, so just letting the loop fall through to
		// the next iteration is correct.
	}
}

// ---------------------------------------------------------------------------
// JSON output
// ---------------------------------------------------------------------------

// writeSummary emits the {file: [...functions...]} JSON document.
// Function records carry source path, function name, defining line,
// and a single-level counts array (nesting limit 0). This matches the
// layout of upstream's serializeFunctionSummary
// (CLI/src/Bytecode.cpp:185-215) with nestingLimit=0.
func writeSummary(w io.Writer, summaries []fileSummary) error {
	bw := newJSONWriter(w)
	bw.write("{\n")
	for i, fileSum := range summaries {
		escaped := escapeFilenameForJSON(fileSum.path)
		bw.writef("    %q: [\n", escaped)
		for j, fn := range fileSum.fns {
			bw.write("        {\n")
			bw.writef("            \"source\": %q,\n", escaped)
			bw.writef("            \"name\": %q,\n", fn.name)
			bw.writef("            \"line\": %d,\n", fn.line)
			bw.write("            \"nestingLimit\": 0,\n")
			bw.write("            \"counts\": [\n                [")
			// We emit all opcodes (including unused tail entries) so
			// downstream tooling sees a stable column count. Sort keys
			// by numeric order for determinism.
			opIdx := make([]int, 0, int(common.OpCount))
			for k := 0; k < int(common.OpCount); k++ {
				opIdx = append(opIdx, k)
			}
			sort.Ints(opIdx)
			for k, op := range opIdx {
				bw.writef("%d", fn.counts[op])
				if k < len(opIdx)-1 {
					bw.write(", ")
				}
			}
			bw.write("]\n            ]\n")
			if j < len(fileSum.fns)-1 {
				bw.write("        },\n")
			} else {
				bw.write("        }\n")
			}
		}
		if i < len(summaries)-1 {
			bw.write("    ],\n")
		} else {
			bw.write("    ]\n")
		}
	}
	bw.write("}\n")
	return bw.err
}

// escapeFilenameForJSON matches upstream's escapeFilename
// (CLI/src/Bytecode.cpp:161-183): backslashes -> forward slashes and
// quotes are escaped. Used so paths on Windows look identical to ones
// on POSIX.
func escapeFilenameForJSON(path string) string {
	path = strings.ReplaceAll(path, `\`, "/")
	path = strings.ReplaceAll(path, `"`, `\"`)
	return path
}

// jsonWriter is a tiny error-deferring writer wrapper. We don't pull
// in encoding/json for this output because upstream emits a specific
// indentation style and key order that we want to reproduce verbatim.
type jsonWriter struct {
	w   io.Writer
	err error
}

func newJSONWriter(w io.Writer) *jsonWriter { return &jsonWriter{w: w} }

func (j *jsonWriter) write(s string) {
	if j.err != nil {
		return
	}
	_, j.err = io.WriteString(j.w, s)
}

func (j *jsonWriter) writef(format string, args ...any) {
	if j.err != nil {
		return
	}
	_, j.err = fmt.Fprintf(j.w, format, args...)
}
