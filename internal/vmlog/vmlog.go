// Copyright (c) luaugo contributors. Licensed under the MIT License.

// Package vmlog provides an opt-in, env-gated diagnostic logger used
// for debugging the luaugo VM, compiler, and runtime. It is OFF by
// default; nothing is printed unless LUAUGO_DEBUG is set in the
// environment (any non-empty value enables logging; setting it to a
// comma-separated list of subsystem names enables only those).
//
// Subsystems used in the codebase: "vm", "compiler", "loader", "gc",
// "stack", "call", "fastcall", "coroutine".
//
// Typical use:
//
//	vmlog.V("vm", "dispatch op=%v a=%d b=%d c=%d", op, a, b, c)
//	vmlog.Dump("stack", L.stack[:L.top])
//
// To enable everything for a single run:
//
//	$env:LUAUGO_DEBUG="*"
//	go test ./pkg/vm/...
//
// To enable a single subsystem:
//
//	$env:LUAUGO_DEBUG="stack"
//
// The logger writes to standard error via the stdlib `log` package so
// it does not interfere with anything writing to stdout (notably
// the standard library's print, which goes through lib.Stdout).
package vmlog

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
)

var (
	enabledAll     atomic.Bool
	enabledSubs    map[string]struct{}
	enabledChecked atomic.Bool
)

func ensureInit() {
	if enabledChecked.Load() {
		return
	}
	v := os.Getenv("LUAUGO_DEBUG")
	if v != "" {
		if v == "*" || v == "all" || v == "1" || v == "true" {
			enabledAll.Store(true)
		} else {
			enabledSubs = make(map[string]struct{})
			for _, part := range strings.Split(v, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					enabledSubs[part] = struct{}{}
				}
			}
		}
		log.SetFlags(log.Lmicroseconds)
		log.SetPrefix("[luaugo] ")
	}
	enabledChecked.Store(true)
}

// Enabled reports whether the given subsystem currently has logging
// enabled. Cheap; safe to call in hot loops because the result is
// cached and most production runs short-circuit on the first atomic
// load.
func Enabled(sub string) bool {
	ensureInit()
	if enabledAll.Load() {
		return true
	}
	if enabledSubs == nil {
		return false
	}
	_, ok := enabledSubs[sub]
	return ok
}

// V logs a formatted message for subsystem sub. The message is
// suppressed if the subsystem is not enabled. Format follows
// fmt.Sprintf.
func V(sub, format string, args ...any) {
	if !Enabled(sub) {
		return
	}
	log.Output(2, sub+": "+fmt.Sprintf(format, args...))
}

// Dump logs a value with %#v formatting. Useful for slices and maps.
func Dump(sub, label string, v any) {
	if !Enabled(sub) {
		return
	}
	log.Output(2, fmt.Sprintf("%s: %s = %#v", sub, label, v))
}

// Force logs unconditionally. Use sparingly -- only for catastrophic
// invariants where we always want visibility, even with logging off.
func Force(format string, args ...any) {
	log.Output(2, fmt.Sprintf(format, args...))
}
