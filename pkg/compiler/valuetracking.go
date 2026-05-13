// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package compiler

// valuetracking.go ports (the relevant subset of)
// Compiler/src/ValueTracking.cpp. Upstream uses a value-tracking pre-pass
// to identify locals that are never written, which lets the compiler
// substitute constants in their place and decide which locals need
// CaptureRef vs CaptureVal in closures.
//
// The current compiler always uses CaptureRef for upvalue locals (the
// safe choice). This is correct on the real Luau VM: read-only locals
// captured by reference still observe the local's initialization value.
// A future agent can layer write-detection on top to enable CaptureVal.

// localUsage records the read/write/capture state of a local.
type localUsage struct {
	written bool
	read    bool
	captured bool
}
