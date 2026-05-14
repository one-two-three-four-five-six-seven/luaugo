// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"math"
	"math/bits"
)

// table mirrors upstream LuaTable (lobject.h) and ltable.cpp. A table
// is a hybrid of an array part (dense int->value mapping for keys
// 1..n) and a hash part (open addressing with Brent's variation).
//
// The collaboration between the two parts is the same as upstream:
//   - integer keys in 1..sizearray go directly into the array slice;
//   - all other keys (or out-of-range integers) live in the hash part;
//   - on insert, if the new key extends the contiguous prefix we may
//     rehash so the array part absorbs the extension.
type table struct {
	gcHeader

	// array part. Index i holds key (i+1).
	array []value

	// hash part as a flat slice of nodes. Empty hash part is
	// represented by nil (matching upstream's dummynode).
	nodes []tNode

	// lsizenode is log2(len(nodes)). Used for fast modulo.
	lsizenode uint8

	// lastfree tracks the highest unscanned hash slot when looking for
	// a free position during insertion (upstream `lastfree`).
	lastfree int

	metatable *table

	// tmcache: a bitmask of metamethod indices known to be absent.
	tmcache uint8

	// readonly is the upstream frozen flag (table.freeze in Luau).
	readonly bool
	// safeenv mirrors upstream safeenv.
	safeenv bool

	// weakMode caches the __mode string while this table is on the GC
	// weak list. Set by the propagate phase, read by clearWeak.
	weakMode string

	// owner is the global state that allocated this table. We need it
	// to construct intern entries during get/setField.
	owner *globalState
}

// tNode mirrors upstream LuaNode: a key/value pair plus a chain link
// to the next node in the collision chain (a *signed offset* from this
// node, so 0 means "end of chain", positive means forward, negative
// backward — same encoding as upstream's bitfield).
type tNode struct {
	key  value
	val  value
	next int32 // signed offset to next node in chain (0 = none)
}

// ----------------------------------------------------------------------
// Construction / freeing
// ----------------------------------------------------------------------

func newTable(g *globalState, narray, nhash int) *table {
	t := &table{tmcache: 0xff, owner: g}
	g.gcInit(t, TTable, memSizeTableHdr)
	if narray > 0 {
		t.setArrayVector(narray)
	}
	if nhash > 0 {
		t.setNodeVector(nhash)
	}
	return t
}

func (t *table) intern(s string) *tString { return t.owner.intern(s) }

func (t *table) setArrayVector(size int) {
	if size > maxTableSize {
		panic("vm: table overflow")
	}
	old := t.array
	if cap(old) >= size {
		t.array = old[:size]
	} else {
		t.array = make([]value, size)
		copy(t.array, old)
	}
	for i := len(old); i < size; i++ {
		t.array[i] = nilValue()
	}
	t.owner.allocBytes((size - len(old)) * memSizeTValue)
}

func (t *table) setNodeVector(size int) {
	if size <= 0 {
		t.nodes = nil
		t.lsizenode = 0
		t.lastfree = 0
		return
	}
	lsize := ceilLog2(size)
	if lsize > maxTableBits {
		panic("vm: table overflow")
	}
	size = 1 << lsize
	t.nodes = make([]tNode, size)
	for i := range t.nodes {
		t.nodes[i].key = nilValue()
		t.nodes[i].val = nilValue()
		t.nodes[i].next = 0
	}
	t.lsizenode = uint8(lsize)
	t.lastfree = size
	t.owner.allocBytes(size * memSizeLuaNode)
}

const (
	maxTableBits = 26
	maxTableSize = 1 << maxTableBits
)

func ceilLog2(n int) int {
	if n <= 1 {
		return 0
	}
	return bits.Len(uint(n - 1))
}

// sizenode returns 1<<lsizenode (the length of the hash node array).
func (t *table) sizenode() int {
	if len(t.nodes) == 0 {
		return 0
	}
	return 1 << t.lsizenode
}

// ----------------------------------------------------------------------
// Hashing
// ----------------------------------------------------------------------

func (t *table) hmod(h uint32) int {
	if t.lsizenode == 0 {
		return 0
	}
	return int(h) & ((1 << t.lsizenode) - 1)
}

func hashFloat(n float64) uint32 {
	// Avoid -0 vs 0 distinction.
	if n == 0 {
		return 0
	}
	bits := math.Float64bits(n)
	return uint32(bits) ^ uint32(bits>>32)
}

func hashBool(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

func hashPointer(p any) uint32 {
	// Best-effort: use the hash from a small adapter. Go does not
	// expose a stable pointer hash so we coerce via a map probe.
	// We don't need a great hash here, only deterministic.
	return uint32(uintptrOf(p))
}

// mainPosition returns the bucket for k.
func (t *table) mainPosition(k value) int {
	switch k.tag {
	case TNumber:
		return t.hmod(hashFloat(k.num))
	case TBoolean:
		return t.hmod(hashBool(k.bool_))
	case TString:
		return t.hmod(k.gc.(*tString).hash)
	case TVector:
		i0 := math.Float32bits(k.vec[0])
		i1 := math.Float32bits(k.vec[1])
		i2 := math.Float32bits(k.vec[2])
		h := i0*73856093 ^ i1*19349663 ^ i2*83492791
		return t.hmod(h)
	case TLightUserdata:
		return t.hmod(uint32(k.ltag) ^ hashPointer(k.ptr))
	}
	if k.isCollectable() && k.gc != nil {
		return t.hmod(hashPointer(k.gc))
	}
	return 0
}

// ----------------------------------------------------------------------
// Lookups
// ----------------------------------------------------------------------

// getNum returns t[k] for integer k (1-based).
func (t *table) getNum(k int) value {
	if k >= 1 && k <= len(t.array) {
		return t.array[k-1]
	}
	if len(t.nodes) == 0 {
		return nilValue()
	}
	n := t.hmod(hashFloat(float64(k)))
	for {
		nd := &t.nodes[n]
		if nd.key.tag == TNumber && nd.key.num == float64(k) {
			return nd.val
		}
		if nd.next == 0 {
			return nilValue()
		}
		n += int(nd.next)
	}
}

func (t *table) getStr(key *tString) (value, int) {
	if len(t.nodes) == 0 || key == nil {
		return nilValue(), -1
	}
	n := t.hmod(key.hash)
	for {
		nd := &t.nodes[n]
		if nd.key.tag == TString && nd.key.gc == key {
			return nd.val, n
		}
		if nd.next == 0 {
			return nilValue(), -1
		}
		n += int(nd.next)
	}
}

func (t *table) get(k value) value {
	switch k.tag {
	case TNil:
		return nilValue()
	case TNumber:
		if i, ok := tryArrayIndex(k.num); ok {
			return t.getNum(i)
		}
		// fallthrough to hash lookup
	case TString:
		v, _ := t.getStr(k.gc.(*tString))
		return v
	}
	if len(t.nodes) == 0 {
		return nilValue()
	}
	n := t.mainPosition(k)
	for {
		nd := &t.nodes[n]
		if rawEqual(nd.key, k) {
			return nd.val
		}
		if nd.next == 0 {
			return nilValue()
		}
		n += int(nd.next)
	}
}

// tryArrayIndex returns (i, true) if n exactly represents the integer i.
func tryArrayIndex(n float64) (int, bool) {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	i := int(n)
	if float64(i) != n {
		return 0, false
	}
	return i, true
}

// ----------------------------------------------------------------------
// Inserts
// ----------------------------------------------------------------------

// set writes t[k]=v. The write barrier (if any) is invoked at the
// upper layer.
//
// Mirrors upstream Luau's `newkey` flow: when the key looks like it
// should live in the array part (k == sizearray+1) we rehash once to
// grow the array; otherwise the key goes into the hash part. We
// deliberately do NOT loop on the boundary-rehash case: rehash may
// decide that the new effective array size is still too small for k
// (e.g. when several earlier array slots are nil, the load-factor
// heuristic in computeSizes elects not to grow), in which case
// looping would rehash forever (see conformance/vararg.luau, which
// builds a table from a vararg pack containing nils).
func (t *table) set(g *globalState, k, v value) {
	if t.readonly {
		panic(luaError{message: "attempt to modify a readonly table"})
	}
	if k.tag == TNil {
		panic(luaError{message: "table index is nil"})
	}
	if k.tag == TNumber && math.IsNaN(k.num) {
		panic(luaError{message: "table index is NaN"})
	}
	// Upstream rejects vector keys with any NaN component (see
	// VM/src/ltable.cpp::luaH_setvector). conformance/vector.luau:126
	// asserts `pcall(function() t[vector.create(0/0, 2, 3)] = 1 end)
	// == false`.
	if k.tag == TVector {
		v0, v1, v2, v3 := k.vec[0], k.vec[1], k.vec[2], k.vec[3]
		if v0 != v0 || v1 != v1 || v2 != v2 || v3 != v3 {
			panic(luaError{message: "table index is NaN"})
		}
	}
	// Fast path: existing array slot.
	if k.tag == TNumber {
		if i, ok := tryArrayIndex(k.num); ok && i >= 1 && i <= len(t.array) {
			t.array[i-1] = v
			t.tmcache = 0
			if v.isCollectable() && v.gc != nil {
				g.barrierTable(t, v.gc)
			}
			return
		}
	}
	// Existing hash slot: update in place.
	if existing := t.findExistingNode(k); existing != nil {
		existing.val = v
		t.tmcache = 0
		if v.isCollectable() && v.gc != nil {
			g.barrierTable(t, v.gc)
		}
		return
	}
	// Boundary case: if k looks like it should extend the array, try
	// rehashing once to grow it. After rehash, k may end up in either
	// the array part (if growth was approved) or stay a hash key.
	if k.tag == TNumber {
		if i, ok := tryArrayIndex(k.num); ok && i == len(t.array)+1 {
			t.rehash(g, k)
			// Post-rehash: take whichever slot is appropriate now.
			if j, ok2 := tryArrayIndex(k.num); ok2 && j >= 1 && j <= len(t.array) {
				t.array[j-1] = v
				t.tmcache = 0
				if v.isCollectable() && v.gc != nil {
					g.barrierTable(t, v.gc)
				}
				return
			}
			// fall through: k still doesn't fit array, install in hash
		}
	}
	// Install in hash. tryNewKeyInHash never triggers a recursive
	// boundary rehash, so this terminates.
	t.installNewHashKey(g, k, v)
}

// installNewHashKey places a brand-new key in the hash part, growing
// the hash table via rehash if necessary. Unlike tryNewKey it never
// tries to absorb the key into the array part: the caller has already
// decided that the array path doesn't apply.
func (t *table) installNewHashKey(g *globalState, k, v value) {
	for {
		if len(t.nodes) == 0 {
			t.setNodeVector(2)
		}
		mp := t.mainPosition(k)
		if t.nodes[mp].key.tag != TNil {
			free := t.getFreePos()
			if free < 0 {
				t.rehash(g, k)
				// Rehash may have absorbed k into the array part if
				// computeSizes finally decided to grow. Re-check.
				if k.tag == TNumber {
					if i, ok := tryArrayIndex(k.num); ok && i >= 1 && i <= len(t.array) {
						t.array[i-1] = v
						t.tmcache = 0
						if v.isCollectable() && v.gc != nil {
							g.barrierTable(t, v.gc)
						}
						return
					}
				}
				continue
			}
			other := t.mainPosition(t.nodes[mp].key)
			if other != mp {
				prev := other
				for prev+int(t.nodes[prev].next) != mp {
					prev += int(t.nodes[prev].next)
				}
				t.nodes[prev].next = int32(free - prev)
				t.nodes[free] = t.nodes[mp]
				if t.nodes[mp].next != 0 {
					t.nodes[free].next += int32(mp - free)
					t.nodes[mp].next = 0
				}
				t.nodes[mp].key = nilValue()
				t.nodes[mp].val = nilValue()
			} else {
				if t.nodes[mp].next != 0 {
					t.nodes[free].next = int32(mp + int(t.nodes[mp].next) - free)
				}
				t.nodes[mp].next = int32(free - mp)
				mp = free
			}
		}
		t.nodes[mp].key = k
		t.nodes[mp].val = v
		t.tmcache = 0
		if k.isCollectable() && k.gc != nil {
			g.barrierTable(t, k.gc)
		}
		if v.isCollectable() && v.gc != nil {
			g.barrierTable(t, v.gc)
		}
		return
	}
}

// findExistingNode searches the hash chain rooted at k's main position
// and returns the node carrying k, or nil if k is not present.
func (t *table) findExistingNode(k value) *tNode {
	if len(t.nodes) == 0 {
		return nil
	}
	n := t.mainPosition(k)
	for {
		nd := &t.nodes[n]
		if nd.key.tag != TNil && rawEqual(nd.key, k) {
			return nd
		}
		if nd.next == 0 {
			return nil
		}
		n += int(nd.next)
	}
}

// tryNewKey installs a fresh hash node for k. Returns true if a rehash
// happened (and the caller should re-check the array branch); returns
// false if the node was placed directly in the current hash table.
func (t *table) tryNewKey(g *globalState, k value) bool {
	if len(t.nodes) == 0 {
		t.setNodeVector(2)
	}
	if k.tag == TNumber {
		if i, ok := tryArrayIndex(k.num); ok && i == len(t.array)+1 {
			t.rehash(g, k)
			return true
		}
	}
	mp := t.mainPosition(k)
	if t.nodes[mp].key.tag != TNil {
		free := t.getFreePos()
		if free < 0 {
			t.rehash(g, k)
			return true
		}
		other := t.mainPosition(t.nodes[mp].key)
		if other != mp {
			prev := other
			for prev+int(t.nodes[prev].next) != mp {
				prev += int(t.nodes[prev].next)
			}
			t.nodes[prev].next = int32(free - prev)
			t.nodes[free] = t.nodes[mp]
			if t.nodes[mp].next != 0 {
				t.nodes[free].next += int32(mp - free)
				t.nodes[mp].next = 0
			}
			t.nodes[mp].key = nilValue()
			t.nodes[mp].val = nilValue()
		} else {
			if t.nodes[mp].next != 0 {
				t.nodes[free].next = int32(mp + int(t.nodes[mp].next) - free)
			}
			t.nodes[mp].next = int32(free - mp)
			mp = free
		}
	}
	t.nodes[mp].key = k
	t.nodes[mp].val = nilValue()
	if k.isCollectable() && k.gc != nil {
		g.barrierTable(t, k.gc)
	}
	return false
}

// setNum is the integer-key fast path.
func (t *table) setNum(g *globalState, k int, v value) {
	if t.readonly {
		panic(luaError{message: "attempt to modify a readonly table"})
	}
	if k >= 1 && k <= len(t.array) {
		t.array[k-1] = v
		t.tmcache = 0
		if v.isCollectable() && v.gc != nil {
			g.barrierTable(t, v.gc)
		}
		return
	}
	t.set(g, numberValue(float64(k)), v)
}

// setStr is the string-key fast path.
func (t *table) setStr(g *globalState, key *tString, v value) {
	if t.readonly {
		panic(luaError{message: "attempt to modify a readonly table"})
	}
	if _, idx := t.getStr(key); idx >= 0 {
		t.nodes[idx].val = v
		t.tmcache = 0
		if v.isCollectable() && v.gc != nil {
			g.barrierTable(t, v.gc)
		}
		return
	}
	t.set(g, stringValue(key), v)
}

func (t *table) getFreePos() int {
	for t.lastfree > 0 {
		t.lastfree--
		if t.nodes[t.lastfree].key.tag == TNil {
			return t.lastfree
		}
	}
	return -1
}

// rehash grows the table to fit one additional key `ek`. The new array
// size is the largest power of two such that at least half of slots
// 1..n are populated.
func (t *table) rehash(g *globalState, ek value) {
	const maxBits = maxTableBits
	nums := make([]int, maxBits+1)
	nasize := t.numUseArray(nums)
	totaluse := nasize
	totaluse += t.numUseHash(nums, &nasize)
	if ek.tag == TNumber {
		nasize += countInt(ek.num, nums)
		totaluse++
	} else {
		totaluse++
	}
	na := computeSizes(nums, &nasize)
	nh := totaluse - na
	// Guarantee forward progress: every rehash must add capacity for
	// at least one more entry. Without this, set() can spin in a
	// rehash loop when the sizing heuristic stays at the same shape
	// after the insertion attempt (notably when ek belongs to a hash
	// slot whose mainPosition is already full).
	if nh <= len(t.nodes) && nasize <= len(t.array) {
		nh = len(t.nodes) + 1
	}
	t.resize(g, nasize, nh)
}

func (t *table) numUseArray(nums []int) int {
	lg, twotolg := 0, 1
	ause := 0
	i := 1
	for lg <= maxTableBits {
		lc := 0
		lim := twotolg
		if lim > len(t.array) {
			lim = len(t.array)
			if i > lim {
				break
			}
		}
		for ; i <= lim; i++ {
			if t.array[i-1].tag != TNil {
				lc++
			}
		}
		nums[lg] += lc
		ause += lc
		lg++
		twotolg <<= 1
	}
	return ause
}

func (t *table) numUseHash(nums []int, pnasize *int) int {
	total := 0
	ause := 0
	for i := range t.nodes {
		n := &t.nodes[i]
		if n.val.tag == TNil {
			continue
		}
		if n.key.tag == TNumber {
			ause += countInt(n.key.num, nums)
		}
		total++
	}
	*pnasize += ause
	return total
}

func countInt(n float64, nums []int) int {
	i, ok := tryArrayIndex(n)
	if ok && i >= 1 && i <= maxTableSize {
		nums[ceilLog2(i)]++
		return 1
	}
	return 0
}

func computeSizes(nums []int, narray *int) int {
	twotoi := 1
	a, na, n := 0, 0, 0
	for i := 0; twotoi/2 < *narray; i++ {
		if nums[i] > 0 {
			a += nums[i]
			if a > twotoi/2 {
				n = twotoi
				na = a
			}
		}
		if a == *narray {
			break
		}
		twotoi <<= 1
	}
	*narray = n
	return na
}

func (t *table) resize(g *globalState, nasize, nhsize int) {
	oldarr := t.array
	oldnodes := t.nodes
	// Grow array part.
	if nasize > len(oldarr) {
		t.array = make([]value, nasize)
		copy(t.array, oldarr)
		for i := len(oldarr); i < nasize; i++ {
			t.array[i] = nilValue()
		}
	}
	// Replace hash part.
	t.setNodeVector(nhsize)
	// If shrinking array, re-insert excess into hash.
	if nasize < len(oldarr) {
		excess := oldarr[nasize:]
		t.array = oldarr[:nasize]
		for i, v := range excess {
			if v.tag != TNil {
				t.set(g, numberValue(float64(nasize+i+1)), v)
			}
		}
	}
	// Re-insert hash entries from oldnodes.
	for i := range oldnodes {
		n := &oldnodes[i]
		if n.val.tag != TNil {
			t.set(g, n.key, n.val)
		}
	}
	_ = oldnodes
}

// ----------------------------------------------------------------------
// Length and iteration
// ----------------------------------------------------------------------

// rawLen returns a boundary index n such that t[n] != nil and
// t[n+1] == nil (or 0 if t[1] is nil). Mirrors upstream luaH_getn
// (VM/src/ltable.cpp:813), including its "branchless" binary search
// from Khuong & Morin 2017 which biases toward the high end of the
// array. The bias is observable to user code: with holes in the
// array part, our previous lo/hi search converged to the LEFT-most
// boundary while upstream converges to the right-most. The
// conformance fixture tables.luau:534 builds a 10-wide array with a
// hole at index 5 and asserts `#t == 9` after clearing index 10.
func (t *table) rawLen() int {
	j := len(t.array)

	if j > 0 && t.array[j-1].tag == TNil {
		// Branchless binary search. Start at offset 0, span j; at
		// each step halve the span and shift base right past any
		// non-nil midpoint. The final base+isNonNil(base[0]) is the
		// boundary.
		baseIdx := 0
		rest := j
		for {
			half := rest >> 1
			if half == 0 {
				break
			}
			if t.array[baseIdx+half].tag != TNil {
				baseIdx += half
			}
			rest -= half
		}
		boundary := baseIdx
		if t.array[baseIdx].tag != TNil {
			boundary++
		}
		return boundary
	}
	// If array is full, follow the hash part for any extension.
	if len(t.nodes) == 0 {
		return j
	}
	// Linear probe upward.
	n := j
	for {
		v := t.getNum(n + 1)
		if v.tag == TNil {
			return n
		}
		n++
	}
}

// next returns the next (key, value) pair following `key`. nil key
// starts iteration. Returns (zeroKey, zeroVal, false) when done.
func (t *table) next(key value) (value, value, bool) {
	idx := -1
	if key.tag != TNil {
		idx = t.findIndex(key)
		if idx < 0 {
			panic(luaError{message: "invalid key to 'next'"})
		}
	}
	// Array part: indices 0..len(array)-1 map to keys 1..len(array).
	for i := idx + 1; i < len(t.array); i++ {
		if t.array[i].tag != TNil {
			return numberValue(float64(i + 1)), t.array[i], true
		}
	}
	// Hash part: indices len(array)..len(array)+len(nodes)-1.
	start := idx + 1 - len(t.array)
	if start < 0 {
		start = 0
	}
	for i := start; i < len(t.nodes); i++ {
		if t.nodes[i].val.tag != TNil {
			return t.nodes[i].key, t.nodes[i].val, true
		}
	}
	return nilValue(), nilValue(), false
}

func (t *table) findIndex(k value) int {
	if k.tag == TNumber {
		if i, ok := tryArrayIndex(k.num); ok && i >= 1 && i <= len(t.array) {
			return i - 1
		}
	}
	if len(t.nodes) == 0 {
		return -1
	}
	n := t.mainPosition(k)
	for {
		nd := &t.nodes[n]
		if rawEqual(nd.key, k) {
			return len(t.array) + n
		}
		if nd.next == 0 {
			return -1
		}
		n += int(nd.next)
	}
}

// luaError is the concrete error value the table operations raise on
// frozen-table writes, nil-key inserts, etc. It is intentionally
// minimal: Tier 3 will replace this with the full error infrastructure.
type luaError struct{ message string }

func (e luaError) Error() string  { return e.message }
func (e luaError) LuaValue() any  { return e.message }
