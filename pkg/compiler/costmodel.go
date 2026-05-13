// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.

package compiler

// costmodel.go is the placeholder for a port of upstream
// Compiler/src/CostModel.cpp. Upstream uses this to decide when a
// function is small enough to inline at a call site.
//
// The current compiler does NOT perform call-site inlining, so the
// cost model is unused. costEstimate returns a sentinel "expensive"
// score that disables inlining unconditionally.

const inlineCostUnknown = 1 << 16

// estimateCost returns a fake high cost so the (absent) inliner never
// fires.
func estimateCost() int { return inlineCostUnknown }
