// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

// tString mirrors upstream TString (lobject.h). The byte payload is
// stored as an unexported Go string so it cannot be mutated after
// interning. `hash` is computed once and reused as both the string
// table lookup key and the table-key hash, matching upstream's
// `hashstr` macro which is just `hashpow2(t, str->hash)`.
type tString struct {
	gcHeader
	atom int16
	hash uint32
	data string

	// next link inside the per-bucket chain of the global intern table.
	internNext *tString
}

func (s *tString) str() string { return s.data }
func (s *tString) len() int    { return len(s.data) }

// stringTable is the global intern hash table (lstate.h struct
// stringtable). It owns no GC reference; each *tString sits on both the
// allgc list (for the collector) and one bucket chain here (for
// interning).
type stringTable struct {
	buckets []*tString
	nuse    int
}

func newStringTable() *stringTable {
	return &stringTable{buckets: make([]*tString, 32)}
}

// stringHash mirrors upstream luaS_hash (lstring.cpp).
func stringHash(s string) uint32 {
	// Same recurrence upstream uses (Lua 5.1 style): chained XOR with a
	// stride so very long strings don't pay O(len).
	h := uint32(len(s))
	step := (len(s) >> 5) + 1
	for i := len(s); i >= step; i -= step {
		h ^= (h << 5) + (h >> 2) + uint32(s[i-1])
	}
	return h
}

// intern returns the canonical *tString for the byte sequence s. The
// returned pointer is identity-stable across the lifetime of the global
// state: two calls with equal byte sequences yield the same pointer.
func (g *globalState) intern(s string) *tString {
	st := g.strt
	h := stringHash(s)
	bkt := int(h) & (len(st.buckets) - 1)
	for ts := st.buckets[bkt]; ts != nil; ts = ts.internNext {
		if ts.hash == h && ts.data == s {
			// If the GC has condemned this string, resurrect it.
			if isDead(g, ts) {
				ts.gcHead().marked = (ts.gcHead().marked &^ gcWhiteBits) | (g.currentWhite & gcWhiteBits)
			}
			return ts
		}
	}
	// Insert.
	ts := &tString{hash: h, atom: atomUndef, data: s}
	size := memSizeTStringHdr + len(s) + 1
	g.gcInit(ts, TString, size)
	ts.internNext = st.buckets[bkt]
	st.buckets[bkt] = ts
	st.nuse++
	if st.nuse > len(st.buckets)*2 {
		g.resizeStringTable(len(st.buckets) * 2)
	}
	return ts
}

func (g *globalState) resizeStringTable(newSize int) {
	st := g.strt
	if newSize < 8 {
		newSize = 8
	}
	// Round to power of two.
	p := 1
	for p < newSize {
		p <<= 1
	}
	newSize = p
	newBuckets := make([]*tString, newSize)
	for _, head := range st.buckets {
		for ts := head; ts != nil; {
			nxt := ts.internNext
			bkt := int(ts.hash) & (newSize - 1)
			ts.internNext = newBuckets[bkt]
			newBuckets[bkt] = ts
			ts = nxt
		}
	}
	st.buckets = newBuckets
}

// sweepInternTable removes dead strings from the intern table. Called
// by the sweep step *before* the strings themselves are unlinked from
// allgc, because removing from the bucket chain requires the *tString
// to still be live in Go's memory.
func (g *globalState) sweepInternTable() {
	st := g.strt
	for i := range st.buckets {
		var keep *tString
		for ts := st.buckets[i]; ts != nil; {
			nxt := ts.internNext
			if isDead(g, ts) {
				st.nuse--
			} else {
				ts.internNext = keep
				keep = ts
			}
			ts = nxt
		}
		st.buckets[i] = keep
	}
}

// atomUndef matches upstream ATOM_UNDEF: -32768 (a value outside the
// range a useratom callback can return).
const atomUndef int16 = -32768
