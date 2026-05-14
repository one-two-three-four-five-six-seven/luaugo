// Copyright (c) luaugo contributors. Licensed under the MIT License.

package compiler

import (
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/upstreamvm"
)

// TestNumericForUserVarAssignmentBasicLine188 is a regression test for
// tests/conformance/basic.luau:188:
//
//	assert((function() local a = 1 for b=9,1,-2 do a = a * 2 b = nil end return a end)() == 32)
//
// The fixture deliberately assigns to the numeric-for loop variable
// inside the body. Upstream Luau's numeric-for layout reserves four
// registers per loop -- limit, step, internal-index, user-visible-var
// -- so the body's writes to `b` (the user-visible variable) cannot
// reach the VM's private index slot. luaugo's compiler previously
// emitted only three registers and shared the index slot with the
// loop variable, so `b = nil` clobbered the index and the loop
// terminated after one iteration (a == 2 instead of a == 32).
//
// Worse: the resulting bytecode was structurally inconsistent with
// what the upstream VM expects (upstream's FORNPREP/FORNLOOP assume
// the 4-register layout), so running our bytecode under the upstream
// VM produced STATUS_ILLEGAL_INSTRUCTION on this snippet.
//
// We exercise the upstream VM here for two reasons:
//
//  1. It validates both bugs in one shot: the upstream VM rejects
//     malformed bytecode AND, for well-formed bytecode, checks
//     semantic correctness via the embedded `assert`.
//  2. pkg/vm imports pkg/compiler (see pkg/vm/lib/base.go), so a
//     compiler-package test cannot import pkg/vm without an import
//     cycle. The upstream VM is reachable via internal/upstreamvm
//     instead.
func TestNumericForUserVarAssignmentBasicLine188(t *testing.T) {
	upstreamvm.RequireAvailable(t)
	src := `
local function f()
  local a = 1
  for b = 9, 1, -2 do
    a = a * 2
    b = nil
  end
  return a
end
local r = f()
if r ~= 32 then error("expected 32, got " .. tostring(r)) end
print(r)
`
	res := runOnUpstream(t, src)
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q stdout=%q", res.Status, res.Stderr, res.Stdout)
	}
	if !strings.Contains(res.Stdout, "32") {
		t.Fatalf("stdout=%q want contains 32", res.Stdout)
	}
}

// TestNumericForUserVarAssignmentAscending covers the same shape but
// with a positive step, to guard against any sign-of-step asymmetry in
// the FORNPREP/FORNLOOP emission. Loop iterates 5 times (1,3,5,7,9),
// doubling `a` each time: 1 -> 32.
func TestNumericForUserVarAssignmentAscending(t *testing.T) {
	upstreamvm.RequireAvailable(t)
	src := `
local function f()
  local a = 1
  for b = 1, 9, 2 do
    a = a * 2
    b = "clobbered"
  end
  return a
end
local r = f()
if r ~= 32 then error("expected 32, got " .. tostring(r)) end
print(r)
`
	res := runOnUpstream(t, src)
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q stdout=%q", res.Status, res.Stderr, res.Stdout)
	}
	if !strings.Contains(res.Stdout, "32") {
		t.Fatalf("stdout=%q want contains 32", res.Stdout)
	}
}

// TestNumericForLoopVarVisibleOnFirstIteration guards the body-prelude
// MOVE that seeds R(A+3) from R(A+2) before the first iteration runs.
// Without that MOVE, the body would see a stale slot on iteration 1
// (FORNPREP itself does not seed R(A+3); only FORNLOOP does, and only
// for subsequent iterations).
func TestNumericForLoopVarVisibleOnFirstIteration(t *testing.T) {
	upstreamvm.RequireAvailable(t)
	src := `
local function f()
  local first
  for b = 7, 7 do
    first = b
  end
  return first
end
local r = f()
if r ~= 7 then error("expected 7, got " .. tostring(r)) end
print(r)
`
	res := runOnUpstream(t, src)
	if res.Status != upstreamvm.StatusOK {
		t.Fatalf("status=%v stderr=%q stdout=%q", res.Status, res.Stderr, res.Stdout)
	}
	if !strings.Contains(res.Stdout, "7") {
		t.Fatalf("stdout=%q want contains 7", res.Stdout)
	}
}
