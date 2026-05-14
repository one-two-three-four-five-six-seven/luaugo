// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

// Command luau-ast mirrors upstream's `luau-ast` binary
// (CLI/src/Ast.cpp). It parses a single Luau source file (or stdin
// when the input is `-`) and emits the parsed AST as JSON. Upstream's
// JSON shape is documented in Analysis/src/AstJsonEncoder.cpp and is
// reproduced in pkg/ast/json.go.
//
// Exit codes: 0 on a clean parse; 1 if the source could not be read
// or if the parser produced one or more SyntaxError diagnostics. The
// JSON is still written on parse error so callers that want best-
// effort tooling can recover a partial tree.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/clitool"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/ast"
)

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr))
}

func run(argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(argv[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: %s [file]\n", argv[0])
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Parses a Lua/Luau source file and writes the AST as JSON to stdout.")
		fmt.Fprintln(stderr, "Pass `-` to read from standard input.")
	}
	help := fs.Bool("help", false, "Display this usage message")
	helpShort := fs.Bool("h", false, "alias for --help")
	if err := fs.Parse(argv[1:]); err != nil {
		return 1
	}
	if *help || *helpShort {
		fs.Usage()
		return 0
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 1
	}

	name := fs.Arg(0)
	var src []byte
	var err error
	if name == "-" {
		// Drain stdin via the injected reader so tests can pipe
		// inputs. We still apply BOM detection so a UTF-16 file
		// redirected through stdin parses cleanly.
		var raw []byte
		raw, err = io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "Couldn't read source -: %v\n", err)
			return 1
		}
		src = clitool.DecodeBOM(raw)
	} else {
		src, err = clitool.ReadSource(name)
		if err != nil {
			fmt.Fprintf(stderr, "Couldn't read source %s\n", name)
			return 1
		}
	}

	// Upstream's luau-ast enables both `captureComments` and
	// `allowDeclarationSyntax` (CLI/src/Ast.cpp:66-67). Our
	// ast.ParseOptions has the equivalent toggles; default values
	// already enable declaration syntax, and `CaptureComments` must
	// be set explicitly so `commentLocations` is populated.
	opts := ast.ParseOptions{
		CaptureComments:      true,
		AllowDeclarationSyntax: true,
	}
	result := ast.Parse(name, src, opts)

	if len(result.Errors) > 0 {
		fmt.Fprintln(stderr, "Parse errors were encountered:")
		for _, perr := range result.Errors {
			fmt.Fprintf(stderr, "  (%d,%d): %s\n",
				perr.Location.Begin.Line+1,
				perr.Location.Begin.Column+1,
				perr.Msg)
		}
		fmt.Fprintln(stderr)
	}

	// Emit JSON regardless of errors so partial trees are still
	// surfaced (matches CLI/src/Ast.cpp:81 which always calls
	// printf("%s", toJson(...)) before returning).
	if result.Program != nil {
		fmt.Fprint(stdout, ast.ToJSON(result.Program))
	}

	if len(result.Errors) > 0 {
		return 1
	}
	return 0
}

// Compile-time sanity: ensure clitool is reachable. We don't use any
// helpers from it in luau-ast (the upstream binary takes only a single
// file and does not honour --program-args / --fflags), but the
// import keeps the dependency graph consistent across the cmd tree.
var _ = clitool.SourceFiles
