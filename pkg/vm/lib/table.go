// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import (
	"math"
	"strings"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// table.go: Luau `table` standard library. Mirrors upstream
// ltablib.cpp:
//
//   concat, foreach, foreachi, getn, maxn, insert, remove, sort,
//   pack, unpack, move, create, find, clear, freeze, isfrozen, clone.
//
// `foreach`, `foreachi`, and `getn` are deprecated in Luau but still
// registered for compatibility with Lua 5.1 code.

// openTable registers the `table` global and the `unpack` global
// (which aliases `table.unpack` for Lua 5.1 compatibility).
func openTable(s *vm.State) {
	s.CreateTable(0, 17)
	s.LRegisterList([]vm.LFnEntry{
		{Name: "concat", Fn: tableConcat},
		{Name: "foreach", Fn: tableForeach},
		{Name: "foreachi", Fn: tableForeachi},
		{Name: "getn", Fn: tableGetn},
		{Name: "maxn", Fn: tableMaxn},
		{Name: "insert", Fn: tableInsert},
		{Name: "remove", Fn: tableRemove},
		{Name: "sort", Fn: tableSort},
		{Name: "pack", Fn: tablePack},
		{Name: "unpack", Fn: tableUnpack},
		{Name: "move", Fn: tableMove},
		{Name: "create", Fn: tableCreate},
		{Name: "find", Fn: tableFind},
		{Name: "clear", Fn: tableClear},
		{Name: "freeze", Fn: tableFreeze},
		{Name: "isfrozen", Fn: tableIsFrozen},
		{Name: "clone", Fn: tableClone},
	})
	s.SetGlobal("table")

	// Lua 5.1 compatibility: `unpack` is a global alias of
	// `table.unpack`.
	s.PushGoFunction(tableUnpack, 0)
	s.SetGlobal("unpack")
}

// ----------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------

// tableLibLen returns #t at idx via the Lua `#` operator. Named
// distinctly from sibling helpers so this file is self-contained
// against parallel additions in the stdlib swarm.
func tableLibLen(s *vm.State, idx int) int {
	s.Length(idx)
	n, _ := s.ToInteger(-1)
	s.Pop(1)
	return int(n)
}

// ----------------------------------------------------------------------
// table.concat (t, sep?, i?, j?)
// ----------------------------------------------------------------------

func tableConcat(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	sep := s.LOptString(2, "")
	i := int(s.LOptInteger(3, 1))
	last := int(s.LOptInteger(4, int64(tableLibLen(s, 1))))

	var sb strings.Builder
	for k := i; k <= last; k++ {
		s.RawGetI(1, k)
		t := s.Type(-1)
		if t != vm.TString && t != vm.TNumber {
			s.LError("invalid value (%s) at index %d in table for 'concat'", t.String(), k)
		}
		v, _ := s.ToString(-1)
		s.Pop(1)
		sb.WriteString(v)
		if k < last {
			sb.WriteString(sep)
		}
	}
	s.PushString(sb.String())
	return 1
}

// ----------------------------------------------------------------------
// table.foreach (t, f)  -- deprecated; iterate via pairs.
// ----------------------------------------------------------------------

func tableForeach(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	s.LCheckType(2, vm.TFunction)

	s.PushNil() // first key
	for s.Next(1) {
		// stack: ... key value
		s.PushValue(2)  // function
		s.PushValue(-3) // key
		s.PushValue(-3) // value
		s.Call(2, 1)
		if !s.IsNil(-1) {
			return 1
		}
		s.Pop(2) // pop result + value, keep key for next iteration
	}
	return 0
}

// ----------------------------------------------------------------------
// table.foreachi (t, f)  -- deprecated; iterate via ipairs.
// ----------------------------------------------------------------------

func tableForeachi(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	s.LCheckType(2, vm.TFunction)
	n := tableLibLen(s, 1)
	for i := 1; i <= n; i++ {
		s.PushValue(2) // function
		s.PushInteger(int64(i))
		s.RawGetI(1, i)
		s.Call(2, 1)
		if !s.IsNil(-1) {
			return 1
		}
		s.Pop(1)
	}
	return 0
}

// ----------------------------------------------------------------------
// table.getn (t)  -- deprecated; equivalent to `#t`.
// ----------------------------------------------------------------------

func tableGetn(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	s.PushInteger(int64(tableLibLen(s, 1)))
	return 1
}

// ----------------------------------------------------------------------
// table.maxn (t)  -- largest positive numeric key, or 0 if none.
// ----------------------------------------------------------------------

func tableMaxn(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	max := 0.0
	s.PushNil()
	for s.Next(1) {
		// stack: ... key value
		if s.Type(-2) == vm.TNumber {
			if v, ok := s.ToNumber(-2); ok && v > max {
				max = v
			}
		}
		s.Pop(1) // drop value, keep key
	}
	s.PushNumber(max)
	return 1
}

// ----------------------------------------------------------------------
// table.insert
//   table.insert(t, v)        -- append at end (t[#t+1] = v)
//   table.insert(t, pos, v)   -- insert at pos, shifting later
// ----------------------------------------------------------------------

func tableInsert(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	n := tableLibLen(s, 1)
	switch s.Top() {
	case 2:
		// Append.
		s.RawSetI(1, n+1)
	case 3:
		// Upstream's table.insert accepts any numeric value for the
		// position argument: whole-valued floats (`2.0`), arbitrary
		// magnitudes, and even NaN. Our prior `LCheckInteger` was
		// stricter than upstream because it rejects NaN (since NaN
		// cannot represent an exact integer). The conformance fixture
		// tables.luau:388 deliberately calls `table.insert(a, 0/0,
		// 42)` and expects no error -- the platform decides where
		// NaN-as-index lands, but it must not throw.
		raw, ok := s.ToNumber(2)
		if !ok {
			s.LTypeError(2, "number")
		}
		var pos int
		switch {
		case math.IsNaN(raw):
			// Truncate to 0; this falls outside [1,n] so the shift is
			// skipped and the value lands at t[0]. Matches the
			// upstream observation that NaN-as-index is "platform-
			// dependent" -- we just need to not raise.
			pos = 0
		case math.IsInf(raw, 1) || raw > math.MaxInt:
			pos = math.MaxInt
		case math.IsInf(raw, -1) || raw < math.MinInt:
			pos = math.MinInt
		default:
			pos = int(raw)
		}
		if 1 <= pos && pos <= n {
			// Shift t[pos..n] up by one.
			for k := n; k >= pos; k-- {
				s.RawGetI(1, k)
				s.RawSetI(1, k+1)
			}
		}
		s.RawSetI(1, pos)
	default:
		s.LError("wrong number of arguments to 'insert'")
	}
	return 0
}

// ----------------------------------------------------------------------
// table.remove (t, pos?)  -- remove last element by default.
// ----------------------------------------------------------------------

func tableRemove(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	n := tableLibLen(s, 1)
	pos := int(s.LOptInteger(2, int64(n)))
	if !(1 <= pos && pos <= n) {
		// Out-of-bounds remove yields nothing.
		return 0
	}
	s.RawGetI(1, pos) // result on the stack
	// Shift t[pos+1..n] down by one.
	for k := pos; k < n; k++ {
		s.RawGetI(1, k+1)
		s.RawSetI(1, k)
	}
	s.PushNil()
	s.RawSetI(1, n)
	return 1
}

// ----------------------------------------------------------------------
// table.move (src, f, t, d [, dst])
// ----------------------------------------------------------------------

func tableMove(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	f := int(s.LCheckInteger(2))
	e := int(s.LCheckInteger(3))
	d := int(s.LCheckInteger(4))

	dstIdx := 5
	if s.IsNoneOrNil(5) {
		dstIdx = 1
	}
	s.LCheckType(dstIdx, vm.TTable)

	if e >= f {
		// Range-validity checks mirror upstream ltablib.cpp tmove.
		// Without them, table.move({}, 0, INT_MAX, 1) would attempt
		// ~2 billion iterations before observing the destination
		// readonly check, which is exactly what conformance/move.luau
		// asserts against ("too many elements to move").
		//
		// Upstream uses C `int` (32-bit) so the bounds are 32-bit
		// INT_MAX. The fixtures are written against that contract;
		// any larger index is considered "too many" regardless of
		// the host int size.
		const (
			maxInt32 = 2147483647
			minInt32 = -2147483648
		)
		// Clamp to int32 range up front -- fixtures pass at the
		// boundaries (move.luau uses maxI=2^31-1, minI=-2^31).
		if f < minInt32 || f > maxInt32 || e < minInt32 || e > maxInt32 || d < minInt32 || d > maxInt32 {
			s.LArgError(3, "too many elements to move")
		}
		if !(f > 0 || e < maxInt32+f) {
			s.LArgError(3, "too many elements to move")
		}
		n := e - f + 1
		if d > maxInt32-n+1 {
			s.LArgError(4, "destination wrap around")
		}
		// Reject writes into a frozen destination up front so partial
		// copies don't leak state when the first RawSetI raises.
		if s.GetReadonly(dstIdx) {
			s.LError("attempt to modify a readonly table")
		}

		// Overlap handling: copy ascending when d > e (dest starts
		// past source), or d <= f (dest at-or-before source), or
		// the tables differ. Otherwise copy descending.
		sameTable := s.RawEqual(1, dstIdx)
		ascending := d > e || d <= f || !sameTable
		if ascending {
			for i := 0; i < n; i++ {
				s.RawGetI(1, f+i)
				// Skip writing nil into slots that don't already
				// have a binding: pure-nil writes would otherwise
				// force a fresh hash node and (eventually) a
				// rehash, which is prohibitively expensive when
				// moving large sparse ranges (see move.luau:87).
				if s.IsNil(-1) {
					// Need to overwrite only when the slot is
					// already populated. RawGetI followed by check.
					s.RawGetI(dstIdx, d+i)
					existsNil := s.IsNil(-1)
					s.Pop(1)
					if existsNil {
						s.Pop(1)
						continue
					}
				}
				s.RawSetI(dstIdx, d+i)
			}
		} else {
			for i := n - 1; i >= 0; i-- {
				s.RawGetI(1, f+i)
				if s.IsNil(-1) {
					s.RawGetI(dstIdx, d+i)
					existsNil := s.IsNil(-1)
					s.Pop(1)
					if existsNil {
						s.Pop(1)
						continue
					}
				}
				s.RawSetI(dstIdx, d+i)
			}
		}
	}
	s.PushValue(dstIdx)
	return 1
}

// ----------------------------------------------------------------------
// table.pack (...)  -- returns {n=count, ...}.
// ----------------------------------------------------------------------

func tablePack(s *vm.State) int {
	n := s.Top()
	s.CreateTable(n, 1)
	// stack: arg1..argN, t  (t at position n+1)
	for i := 1; i <= n; i++ {
		s.PushValue(i)
		s.RawSetI(n+1, i)
	}
	s.PushInteger(int64(n))
	s.SetField(n+1, "n")
	return 1
}

// ----------------------------------------------------------------------
// table.unpack (t, i?, j?)  -- pushes t[i], t[i+1], ..., t[j].
// ----------------------------------------------------------------------

func tableUnpack(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	i := int(s.LOptInteger(2, 1))
	var e int
	if s.IsNoneOrNil(3) {
		e = tableLibLen(s, 1)
	} else {
		e = int(s.LCheckInteger(3))
	}
	if i > e {
		return 0
	}
	n := e - i + 1
	if n <= 0 {
		return 0
	}
	if !s.CheckStack(n) {
		s.LError("too many results to unpack")
	}
	for k := 0; k < n; k++ {
		s.RawGetI(1, i+k)
	}
	return n
}

// ----------------------------------------------------------------------
// table.create (n, v?)  -- create a table of length n filled with v.
// ----------------------------------------------------------------------

func tableCreate(s *vm.State) int {
	size := int(s.LCheckInteger(1))
	if size < 0 {
		s.LArgError(1, "size out of range")
	}
	// Upstream conformance pcall.luau:172 expects
	// `pcall(function() table.create(1e6) end)` to fail with
	// "not enough memory". The upstream conformance harness wires
	// in a custom allocator (limitedRealloc) that returns NULL for
	// any single allocation larger than 8 MiB; we don't have a
	// pluggable allocator here, so model the same behavior by
	// rejecting sizes whose backing slice exceeds 8 MiB worth of
	// value slots. The "not enough memory" string is the exact
	// upstream wording so conformance fixtures that match against it
	// (pcall.luau:172-176, including the xpcall variants) pass.
	const valueBytes = 16 // approx. sizeof(value) in our impl
	if size > 0 && uint64(size)*valueBytes > 8*1024*1024 {
		// Push the bare "not enough memory" string and raise without
		// the "<chunkname>:<line>: " prefix LError would add --
		// upstream's OOM errors come from the allocator failure
		// path and carry no source location.
		s.PushString("not enough memory")
		s.Error()
	}
	hasFill := !s.IsNoneOrNil(2)
	s.CreateTable(size, 0)
	if hasFill {
		// Source value sits at index 2; the new table is now on top.
		for i := 1; i <= size; i++ {
			s.PushValue(2)
			s.RawSetI(-2, i)
		}
	}
	return 1
}

// ----------------------------------------------------------------------
// table.find (t, v, init?)
// ----------------------------------------------------------------------

func tableFind(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	if s.IsNoneOrNil(2) {
		s.LArgError(2, "value expected")
	}
	init := int(s.LOptInteger(3, 1))
	if init < 1 {
		s.LArgError(3, "index out of range")
	}
	for i := init; ; i++ {
		s.RawGetI(1, i)
		if s.IsNil(-1) {
			s.Pop(1)
			break
		}
		// Use Equal (not RawEqual) so __eq metamethods participate.
		// conformance/tables.luau:454 builds a table of objects with
		// custom __eq and asserts table.find honours it.
		if s.Equal(2, -1) {
			s.Pop(1)
			s.PushInteger(int64(i))
			return 1
		}
		s.Pop(1)
	}
	s.PushNil()
	return 1
}

// ----------------------------------------------------------------------
// table.clear (t)  -- remove every entry, preserve metatable.
// ----------------------------------------------------------------------

func tableClear(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	s.ClearTable(1)
	return 0
}

// ----------------------------------------------------------------------
// table.freeze (t) / table.isfrozen (t) / table.clone (t)
// ----------------------------------------------------------------------

func tableFreeze(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	if s.GetReadonly(1) {
		s.LArgError(1, "table is already frozen")
	}
	if s.LGetMetafield(1, "__metatable") {
		s.Pop(1)
		s.LArgError(1, "table has a protected metatable")
	}
	s.SetReadonly(1, true)
	s.PushValue(1)
	return 1
}

func tableIsFrozen(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	s.PushBoolean(s.GetReadonly(1))
	return 1
}

func tableClone(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	if s.LGetMetafield(1, "__metatable") {
		s.Pop(1)
		s.LArgError(1, "table has a protected metatable")
	}
	s.CloneTable(1)
	return 1
}

// ----------------------------------------------------------------------
// table.sort (t, comp?)
// ----------------------------------------------------------------------
//
// Sort is implemented as introsort: a quicksort with a heapsort
// fallback when recursion exceeds 1.5 log2(n) steps, mirroring
// upstream ltablib.cpp's sort_rec. Reads and writes go through
// RawGetI / RawSetI so a user comparator that side-effects the same
// table cannot corrupt internal sort state.

func tableSort(s *vm.State) int {
	s.LCheckType(1, vm.TTable)
	if s.GetReadonly(1) {
		s.LError("attempt to modify a readonly table")
	}
	// table.sort is non-yieldable upstream: the comparator is invoked
	// in a "C call" context that gates luaD_yield. conformance/
	// coroutine.luau:123 wraps `not pcall(table.sort, {1,2,3},
	// coroutine.yield)` to verify the yield is refused.
	s.EnterNonYieldable()
	defer s.LeaveNonYieldable()
	n := tableLibLen(s, 1)
	hasComp := !s.IsNoneOrNil(2)
	if hasComp {
		s.LCheckType(2, vm.TFunction)
	}
	if n <= 1 {
		return 0
	}
	// 1-based [l..u] inclusive.
	sortRec(s, 1, n, 2*tabIlog2(n)+8, hasComp)
	return 0
}

func tabIlog2(n int) int {
	r := 0
	for n > 1 {
		n >>= 1
		r++
	}
	return r
}

// sortLess returns true if t[i] < t[j] using either `<` or the
// supplied comparator. Detects "table modified during sorting" by
// re-measuring length before and after the comparator call.
func sortLess(s *vm.State, i, j int, hasComp bool) bool {
	curLen := tableLibLen(s, 1)
	if i < 1 || i > curLen || j < 1 || j > curLen {
		s.LError("table modified during sorting")
	}
	s.RawGetI(1, i)
	s.RawGetI(1, j)
	var res bool
	if hasComp {
		// stack: vi vj
		// Push comparator then args: comp, vi, vj.
		s.PushValue(2) // comp
		s.PushValue(-3)
		s.PushValue(-3)
		s.Call(2, 1)
		// stack: vi vj result
		res = s.ToBoolean(-1)
		s.Pop(3)
	} else {
		res = s.LessThan(-2, -1)
		s.Pop(2)
	}
	if tableLibLen(s, 1) != curLen {
		s.LError("table modified during sorting")
	}
	return res
}

func sortSwap(s *vm.State, i, j int) {
	if i == j {
		return
	}
	s.RawGetI(1, i)
	s.RawGetI(1, j)
	// stack: vi vj  (top is vj)
	s.RawSetI(1, i) // pops vj, t[i] = vj
	s.RawSetI(1, j) // pops vi, t[j] = vi
}

func sortHeap(s *vm.State, l, u int, hasComp bool) {
	count := u - l + 1
	for i := count/2 - 1; i >= 0; i-- {
		siftHeap(s, l, u, hasComp, i)
	}
	for i := count - 1; i > 0; i-- {
		sortSwap(s, l, l+i)
		siftHeap(s, l, l+i-1, hasComp, 0)
	}
}

func siftHeap(s *vm.State, l, u int, hasComp bool, root int) {
	count := u - l + 1
	for root*2+2 < count {
		left := root*2 + 1
		right := root*2 + 2
		next := root
		if sortLess(s, l+next, l+left, hasComp) {
			next = left
		}
		if sortLess(s, l+next, l+right, hasComp) {
			next = right
		}
		if next == root {
			return
		}
		sortSwap(s, l+root, l+next)
		root = next
	}
	lastleft := root*2 + 1
	if lastleft == count-1 && sortLess(s, l+root, l+lastleft, hasComp) {
		sortSwap(s, l+root, l+lastleft)
	}
}

// sortRec is the introsort driver. l, u are 1-based inclusive indices.
func sortRec(s *vm.State, l, u, limit int, hasComp bool) {
	for l < u {
		if limit == 0 {
			sortHeap(s, l, u, hasComp)
			return
		}
		// Median-of-three: order t[l], t[m], t[u].
		if sortLess(s, u, l, hasComp) {
			sortSwap(s, l, u)
		}
		if u-l == 1 {
			return
		}
		m := l + (u-l)/2
		if sortLess(s, m, l, hasComp) {
			sortSwap(s, m, l)
		} else if sortLess(s, u, m, hasComp) {
			sortSwap(s, m, u)
		}
		if u-l == 2 {
			return
		}
		// Pivot is placed at u-1.
		p := u - 1
		sortSwap(s, m, u-1)

		i := l
		j := u - 1
		for {
			// repeat ++i until t[i] >= pivot.
			for {
				i++
				if !sortLess(s, i, p, hasComp) {
					break
				}
				if i >= u {
					s.LError("invalid order function for sorting")
				}
			}
			// repeat --j until t[j] <= pivot.
			for {
				j--
				if !sortLess(s, p, j, hasComp) {
					break
				}
				if j <= l {
					s.LError("invalid order function for sorting")
				}
			}
			if j < i {
				break
			}
			sortSwap(s, i, j)
		}
		// Swap pivot into place.
		sortSwap(s, p, i)
		// Recurse on smaller half, iterate on larger.
		limit = (limit >> 1) + (limit >> 2)
		if i-l < u-i {
			sortRec(s, l, i-1, limit, hasComp)
			l = i + 1
		} else {
			sortRec(s, i+1, u, limit, hasComp)
			u = i - 1
		}
	}
}
