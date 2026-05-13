// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

import (
	"runtime"
	"testing"
)

// TestSmoke exercises the basic API required by the Tier-2 contract.
func TestSmoke(t *testing.T) {
	s := NewState()
	defer s.Close()

	s.PushNumber(3.14)
	s.PushString("hello")
	if s.Top() != 2 {
		t.Fatalf("top: got %d want 2", s.Top())
	}
	if got, ok := s.ToNumber(1); !ok || got != 3.14 {
		t.Fatalf("number: got %v ok=%v", got, ok)
	}
	if got, ok := s.ToString(2); !ok || got != "hello" {
		t.Fatalf("string: got %q ok=%v", got, ok)
	}
	s.NewTable()
	s.PushNumber(42)
	s.RawSetI(3, 1)
	s.RawGetI(3, 1)
	if got, ok := s.ToNumber(-1); !ok || got != 42 {
		t.Fatalf("table: got %v ok=%v", got, ok)
	}
}

func TestStackOps(t *testing.T) {
	s := NewState()
	defer s.Close()

	s.PushNumber(1)
	s.PushNumber(2)
	s.PushNumber(3)
	if s.Top() != 3 {
		t.Fatalf("expected top=3, got %d", s.Top())
	}
	// Remove the middle element.
	s.Remove(2)
	if got, _ := s.ToNumber(1); got != 1 {
		t.Fatalf("post-remove [1]: got %v", got)
	}
	if got, _ := s.ToNumber(2); got != 3 {
		t.Fatalf("post-remove [2]: got %v", got)
	}
	if s.Top() != 2 {
		t.Fatalf("post-remove top: %d", s.Top())
	}
	// Insert: pushing 99 then moving to position 1.
	s.PushNumber(99)
	s.Insert(1)
	if got, _ := s.ToNumber(1); got != 99 {
		t.Fatalf("post-insert [1]: got %v", got)
	}
	// Replace: top onto idx 1.
	s.PushNumber(7)
	s.Replace(1)
	if got, _ := s.ToNumber(1); got != 7 {
		t.Fatalf("post-replace [1]: got %v", got)
	}
}

func TestStringInternIdentity(t *testing.T) {
	s := NewState()
	defer s.Close()

	a := s.impl.gs.intern("hello")
	b := s.impl.gs.intern("hello")
	if a != b {
		t.Fatalf("intern: different pointers for identical strings: %p %p", a, b)
	}
	c := s.impl.gs.intern("world")
	if a == c {
		t.Fatalf("intern: collided distinct strings")
	}
	if a.str() != "hello" {
		t.Fatalf("intern: wrong data: %q", a.str())
	}
}

func TestTableArrayHashMixed(t *testing.T) {
	s := NewState()
	defer s.Close()

	g := s.impl.gs
	tbl := newTable(g, 0, 0)

	// Array part: t[1..5] = i*10.
	for i := 1; i <= 5; i++ {
		tbl.setNum(g, i, numberValue(float64(i*10)))
	}
	for i := 1; i <= 5; i++ {
		v := tbl.getNum(i)
		if v.tag != TNumber || v.num != float64(i*10) {
			t.Fatalf("array get t[%d]: got %+v", i, v)
		}
	}

	// Hash part: string keys.
	tbl.setStr(g, g.intern("foo"), numberValue(1))
	tbl.setStr(g, g.intern("bar"), numberValue(2))
	if v, _ := tbl.getStr(g.intern("foo")); v.num != 1 {
		t.Fatalf("hash get foo: %+v", v)
	}
	if v, _ := tbl.getStr(g.intern("bar")); v.num != 2 {
		t.Fatalf("hash get bar: %+v", v)
	}

	// Mixed: integer-not-in-array.
	tbl.setNum(g, 100, numberValue(999))
	if v := tbl.getNum(100); v.num != 999 {
		t.Fatalf("hash int get t[100]: %+v", v)
	}

	// rawLen returns boundary in array part.
	if n := tbl.rawLen(); n != 5 {
		t.Fatalf("rawLen: %d", n)
	}
}

func TestTableNextOrder(t *testing.T) {
	s := NewState()
	defer s.Close()

	g := s.impl.gs
	tbl := newTable(g, 3, 4)
	for i := 1; i <= 3; i++ {
		tbl.setNum(g, i, numberValue(float64(i)))
	}
	tbl.setStr(g, g.intern("x"), numberValue(100))
	tbl.setStr(g, g.intern("y"), numberValue(200))

	// Walk all entries; collect them and verify we hit both parts.
	seen := map[string]float64{}
	k := nilValue()
	for {
		nk, nv, ok := tbl.next(k)
		if !ok {
			break
		}
		var keystr string
		switch nk.tag {
		case TNumber:
			keystr = formatNumber(nk.num)
		case TString:
			keystr = nk.gc.(*tString).str()
		}
		seen[keystr] = nv.num
		k = nk
	}
	if len(seen) != 5 {
		t.Fatalf("iter saw %d entries: %v", len(seen), seen)
	}
	for i := 1; i <= 3; i++ {
		want := float64(i)
		got, ok := seen[formatNumber(float64(i))]
		if !ok || got != want {
			t.Fatalf("array key %d: ok=%v got=%v want=%v", i, ok, got, want)
		}
	}
	if seen["x"] != 100 || seen["y"] != 200 {
		t.Fatalf("hash entries missing: %v", seen)
	}
}

func TestTableNextArrayBeforeHash(t *testing.T) {
	s := NewState()
	defer s.Close()
	g := s.impl.gs
	tbl := newTable(g, 3, 2)
	tbl.setNum(g, 1, numberValue(10))
	tbl.setNum(g, 2, numberValue(20))
	tbl.setNum(g, 3, numberValue(30))
	tbl.setStr(g, g.intern("z"), numberValue(99))

	// Expect array indices in order 1, 2, 3, then hash entries.
	k := nilValue()
	var order []value
	for {
		nk, _, ok := tbl.next(k)
		if !ok {
			break
		}
		order = append(order, nk)
		k = nk
	}
	if len(order) != 4 {
		t.Fatalf("order len: %d", len(order))
	}
	for i := 0; i < 3; i++ {
		if order[i].tag != TNumber || order[i].num != float64(i+1) {
			t.Fatalf("order[%d]: %+v", i, order[i])
		}
	}
	if order[3].tag != TString || order[3].gc.(*tString).str() != "z" {
		t.Fatalf("order[3]: %+v", order[3])
	}
}

func TestGlobalSetGet(t *testing.T) {
	s := NewState()
	defer s.Close()

	s.PushString("world")
	s.SetGlobal("hello")
	s.GetGlobal("hello")
	if got, _ := s.ToString(-1); got != "world" {
		t.Fatalf("global: got %q", got)
	}
}

func TestGCMarkSweepCycle(t *testing.T) {
	s := NewState()
	defer s.Close()
	g := s.impl.gs

	// Create a cycle of two tables a <-> b.
	a := newTable(g, 0, 1)
	b := newTable(g, 0, 1)
	a.setStr(g, g.intern("ref"), tableValue(b))
	b.setStr(g, g.intern("ref"), tableValue(a))

	// Neither is rooted anywhere accessible; after full GC both should
	// be condemned. We test by counting allgc list length before and
	// after.
	before := countAllGC(g)
	s.CollectGarbage()
	// Run again because the first cycle may have promoted into the
	// "other" white; a second cycle ensures we walk past it.
	s.CollectGarbage()
	after := countAllGC(g)

	// The cycle is unreachable so allgc should shrink (we don't insist
	// on exact size because the global state itself has its own
	// roots).
	if after >= before {
		t.Fatalf("GC did not collect cycle: before=%d after=%d", before, after)
	}
	// Keep a, b alive to the end of the function so the compiler
	// doesn't reorder.
	runtime.KeepAlive(a)
	runtime.KeepAlive(b)
}

func countAllGC(g *globalState) int {
	n := 0
	for o := g.allgc; o != nil; o = o.gcHead().next {
		n++
	}
	return n
}

func TestGCRetainsRooted(t *testing.T) {
	s := NewState()
	defer s.Close()

	// Push a fresh table on the stack so it's rooted by the thread.
	s.NewTable()
	if s.Top() != 1 {
		t.Fatalf("expected top=1")
	}
	before := countAllGC(s.impl.gs)
	s.CollectGarbage()
	s.CollectGarbage()
	after := countAllGC(s.impl.gs)
	if after > before {
		t.Fatalf("GC grew allgc: %d -> %d", before, after)
	}
	// Verify the table is still reachable and usable.
	s.PushNumber(7)
	s.RawSetI(1, 1)
	s.RawGetI(1, 1)
	if got, _ := s.ToNumber(-1); got != 7 {
		t.Fatalf("post-GC table read: %v", got)
	}
}

func TestVectorBasics(t *testing.T) {
	s := NewState()
	defer s.Close()

	s.PushVector(1, 2, 3, 0)
	if s.Type(1) != TVector {
		t.Fatalf("type: %v", s.Type(1))
	}
	x, y, z, _, ok := s.ToVector(1)
	if !ok || x != 1 || y != 2 || z != 3 {
		t.Fatalf("ToVector: ok=%v x=%v y=%v z=%v", ok, x, y, z)
	}
}

func TestBufferRoundTrip(t *testing.T) {
	s := NewState()
	defer s.Close()
	g := s.impl.gs
	b := newBuffer(g, 16)
	b.WriteI32(0, -7)
	b.WriteF32(4, 3.5)
	b.WriteU16(8, 0xCAFE)
	if got := b.ReadI32(0); got != -7 {
		t.Fatalf("ReadI32: %d", got)
	}
	if got := b.ReadF32(4); got != 3.5 {
		t.Fatalf("ReadF32: %v", got)
	}
	if got := b.ReadU16(8); got != 0xCAFE {
		t.Fatalf("ReadU16: %x", got)
	}
}

func TestRawEqualSameString(t *testing.T) {
	s := NewState()
	defer s.Close()
	s.PushString("abc")
	s.PushString("abc")
	if !s.RawEqual(1, 2) {
		t.Fatalf("rawEqual: identical interned strings should compare equal")
	}
}

func TestToStringFromNumber(t *testing.T) {
	s := NewState()
	defer s.Close()
	s.PushNumber(42)
	got, ok := s.ToString(-1)
	if !ok || got != "42" {
		t.Fatalf("ToString(42): ok=%v got=%q", ok, got)
	}
	// Per Lua semantics, slot is now a string.
	if s.Type(-1) != TString {
		t.Fatalf("Type after ToString coercion: %v", s.Type(-1))
	}
}
