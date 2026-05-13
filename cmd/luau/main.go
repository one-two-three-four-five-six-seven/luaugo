// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

// Command luau is a REPL and script runner for luaugo. Tier-1
// implementation is a stub: it parses flags but does not execute
// scripts. Tier 5 (Integration) replaces this with a full implementation
// matching the upstream `luau` binary's CLI surface.
package main

import (
	"flag"
	"fmt"
	"os"
)

const versionString = "luaugo (Luau port) - development build"

func main() {
	var (
		compile  = flag.String("compile", "", "compile and print output; mode is one of 'text', 'binary', 'remarks'")
		version  = flag.Bool("version", false, "print version and exit")
		optLevel = flag.Int("O", 1, "optimization level (0..2)")
		dbgLevel = flag.Int("g", 1, "debug-info level (0..2)")
	)
	flag.Parse()

	if *version {
		fmt.Println(versionString)
		return
	}

	if *compile != "" {
		fmt.Fprintf(os.Stderr, "luau: --compile=%s not yet implemented (Tier 5)\n", *compile)
		os.Exit(2)
	}

	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "luau: REPL not yet implemented (Tier 5)")
		os.Exit(2)
	}

	// Scripts execution path: not yet wired.
	fmt.Fprintf(os.Stderr, "luau: script execution not yet implemented (Tier 2/3/5)\n")
	fmt.Fprintf(os.Stderr, "luau: would run %s with -O%d -g%d\n", flag.Arg(0), *optLevel, *dbgLevel)
	os.Exit(2)
}
