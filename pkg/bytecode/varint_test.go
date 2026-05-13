// Copyright (c) luaugo contributors. Licensed under the MIT License.

package bytecode

import "testing"

func TestVarintRoundTrip(t *testing.T) {
	cases := []uint32{
		0, 1, 0x7f, 0x80, 0xff, 0x100, 0x3fff, 0x4000,
		0x1fffff, 0x200000, 0xfffffff, 0x10000000, 0xffffffff,
	}
	for _, v := range cases {
		buf := VarintAppend(nil, v)
		got, n, err := VarintRead(buf, 0)
		if err != nil {
			t.Errorf("v=%d: read error %v", v, err)
			continue
		}
		if got != v {
			t.Errorf("v=%d: round-trip got %d, buf=%x", v, got, buf)
		}
		if n != len(buf) {
			t.Errorf("v=%d: consumed %d bytes, buf has %d", v, n, len(buf))
		}
	}
}

func TestVarintEncoding(t *testing.T) {
	// Spot check that the encoding matches the upstream layout: little-
	// endian, 7 data bits per byte, MSB continuation.
	want := []byte{0x80, 0x01} // 128 = 0x80 -> [0x80, 0x01]
	got := VarintAppend(nil, 128)
	if string(got) != string(want) {
		t.Errorf("128: got %x, want %x", got, want)
	}
	want = []byte{0xff, 0x7f} // 0x3fff -> [0xff, 0x7f]
	got = VarintAppend(nil, 0x3fff)
	if string(got) != string(want) {
		t.Errorf("0x3fff: got %x, want %x", got, want)
	}
}

func TestVarintTruncated(t *testing.T) {
	_, _, err := VarintRead([]byte{0x80, 0x80, 0x80}, 0)
	if err == nil {
		t.Fatal("expected truncated-varint error, got nil")
	}
}
