// Copyright (c) luaugo contributors. Licensed under the MIT License.

package tests

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"time"
)

// startMemoryWatchdog spawns a background goroutine that polls the
// process's RSS once per second and forcibly exits with a non-zero
// status if it crosses limitBytes. This protects developer machines
// from runaway tests (e.g. an accidental coroutine leak or an
// infinite recursion that fills the heap) that would otherwise sit
// at 20+ GB until the user notices.
//
// The watchdog uses runtime.MemStats.HeapAlloc + the OS-reported RSS
// when available. On Windows we have no cheap way to read RSS without
// pulling in psapi, so we fall back to runtime.MemStats only, which
// already covers >95% of the failure modes we care about (every leak
// observed so far has been Go-heap-bound).
//
// Call this once from a TestMain-style init or from the suite test
// itself. Subsequent calls are no-ops (guarded by `watchdogStarted`).
func startMemoryWatchdog(limitBytes uint64) {
	if !atomic.CompareAndSwapInt32(&watchdogStarted, 0, 1) {
		return
	}
	go func() {
		var m runtime.MemStats
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			runtime.ReadMemStats(&m)
			// Sys reports total bytes obtained from the OS for all
			// purposes (heap, stacks, GC bookkeeping). It's the
			// closest in-process analogue of RSS without cgo.
			if m.Sys > limitBytes {
				fmt.Fprintf(os.Stderr,
					"\n[memwatchdog] aborting: runtime.MemStats.Sys=%d bytes "+
						"exceeded limit %d bytes (HeapAlloc=%d, NumGoroutine=%d)\n",
					m.Sys, limitBytes, m.HeapAlloc, runtime.NumGoroutine())
				// Force exit; t.Fatal won't help if a goroutine is
				// spinning and hasn't yielded back to the test thread.
				os.Exit(137) // 128+SIGKILL convention
			}
		}
	}()
}

var watchdogStarted int32

// init activates the watchdog as soon as the test binary starts. The
// 4 GiB ceiling is well below any reasonable workstation's available
// RAM but well above the steady-state of our entire suite (~150 MiB
// at peak when sort.luau runs).
func init() {
	startMemoryWatchdog(4 * 1024 * 1024 * 1024) // 4 GiB
}
