// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

// Memory accounting hooks. The Go runtime owns actual allocation; we
// just track the *logical* number of bytes held by GC-managed Lua
// objects so that GCInfo() (gcinfo() in upstream) returns a meaningful
// value and so the incremental collector can decide when to step.
//
// The values produced here do NOT correspond to actual Go heap usage;
// they mirror the upstream `totalbytes` counter computed from the sizes
// of upstream C structs. Sizes therefore use the upstream convention
// (e.g. a TString costs `sizestring(len)` bytes) so behaviour is
// comparable.

// Approximate sizes (in bytes) of the upstream C structs we mirror.
// These constants are not load-bearing; they exist purely so GCInfo()
// returns a number that grows and shrinks proportionally to the live
// heap, just like upstream `gcinfo()`.
const (
	memSizeTValue      = 16
	memSizeTStringHdr  = 24
	memSizeTableHdr    = 56
	memSizeLuaNode     = 32 // TValue + TKey, packed
	memSizeClosureHdr  = 32
	memSizeUpVal       = 32
	memSizeUdataHdr    = 24
	memSizeBufferHdr   = 16
	memSizeProtoHdr    = 128
	memSizeThreadHdr   = 256
)

// allocBytes records that n bytes have been allocated to GC-managed
// state. Safe to call before the global state is finalised.
func (g *globalState) allocBytes(n int) {
	if n <= 0 {
		return
	}
	g.totalBytes += uint64(n)
}

// freeBytes records that n bytes have been freed. n must match a
// previous allocBytes for correctness; the only consumer is GCInfo().
func (g *globalState) freeBytes(n int) {
	if n <= 0 {
		return
	}
	if uint64(n) >= g.totalBytes {
		g.totalBytes = 0
	} else {
		g.totalBytes -= uint64(n)
	}
}
