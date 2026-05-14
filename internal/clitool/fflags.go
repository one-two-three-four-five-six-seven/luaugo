// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package clitool

import (
	"fmt"
	"os"
	"strings"
)

// FFlags is a no-op implementation of upstream's setLuauFlags
// (CLI/src/Flags.cpp). Upstream uses a global FFlag registry to gate
// compiler/VM features for staged rollout. luaugo has no such
// registry, so we accept the syntax for CLI compatibility and report
// any unknown flag to stderr without erroring out.
//
// Supported input forms (matching the upstream parser):
//
//	--fflags=true
//	--fflags=false
//	--fflags=Name=true
//	--fflags=Name=false
//	--fflags=A=true,B=false,...
//
// The leading bare `true` / `false` (no name) sets the implicit
// global default; in upstream that maps onto FInt and FFlag defaults.
// luaugo ignores it.
//
// SetFFlags returns the number of recognized flag specifications so
// callers can decide whether to warn loudly.
func SetFFlags(spec string) int {
	parsed := 0
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parsed++
		if part == "true" || part == "false" {
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			fmt.Fprintf(os.Stderr, "luau: fflag %q ignored (luaugo has no fast-flag system)\n", part)
			continue
		}
		// Name=value form. We accept it silently; the value is
		// validated as boolean for shape only.
		val := strings.TrimSpace(part[eq+1:])
		if val != "true" && val != "false" {
			fmt.Fprintf(os.Stderr, "luau: fflag %q has non-boolean value %q (ignored)\n", part[:eq], val)
		}
	}
	return parsed
}
