// Copyright (c) luaugo contributors. Licensed under the MIT License.

package clitool

import (
	"bytes"
	"testing"
)

func TestDecodeBOM_PlainUTF8_PassesThrough(t *testing.T) {
	in := []byte("local x = 1\n")
	out := DecodeBOM(in)
	if !bytes.Equal(in, out) {
		t.Errorf("UTF-8 without BOM was modified:\n in:  %q\n out: %q", in, out)
	}
}

func TestDecodeBOM_StripsUTF8BOM(t *testing.T) {
	in := append([]byte{0xEF, 0xBB, 0xBF}, []byte("local x = 1\n")...)
	out := DecodeBOM(in)
	want := []byte("local x = 1\n")
	if !bytes.Equal(out, want) {
		t.Errorf("UTF-8 BOM not stripped:\n got: %q\n want: %q", out, want)
	}
}

func TestDecodeBOM_TranscodesUTF16LE(t *testing.T) {
	// Synthesize the exact byte stream PowerShell `Set-Content
	// -Encoding Unicode` produces for "ab" + LF.
	in := []byte{0xFF, 0xFE, 'a', 0x00, 'b', 0x00, 0x0A, 0x00}
	out := DecodeBOM(in)
	if string(out) != "ab\n" {
		t.Errorf("UTF-16 LE decode wrong:\n got: %q\n want: %q", out, "ab\n")
	}
}

func TestDecodeBOM_TranscodesUTF16BE(t *testing.T) {
	in := []byte{0xFE, 0xFF, 0x00, 'a', 0x00, 'b', 0x00, 0x0A}
	out := DecodeBOM(in)
	if string(out) != "ab\n" {
		t.Errorf("UTF-16 BE decode wrong:\n got: %q\n want: %q", out, "ab\n")
	}
}

func TestDecodeBOM_NonBMPSurrogatePair(t *testing.T) {
	// U+1F600 GRINNING FACE = surrogate pair D83D DE00 in UTF-16.
	in := []byte{0xFF, 0xFE, 0x3D, 0xD8, 0x00, 0xDE}
	out := DecodeBOM(in)
	want := []byte{0xF0, 0x9F, 0x98, 0x80} // UTF-8 encoding of U+1F600
	if !bytes.Equal(out, want) {
		t.Errorf("surrogate pair decode wrong:\n got: % x\n want: % x", out, want)
	}
}

func TestDecodeBOM_LoneHighSurrogate_BecomesReplacement(t *testing.T) {
	// A high surrogate with no low partner should be replaced with
	// U+FFFD (EF BF BD) so the rest of the file still parses.
	in := []byte{0xFF, 0xFE, 0x3D, 0xD8}
	out := DecodeBOM(in)
	if !bytes.Equal(out, []byte{0xEF, 0xBF, 0xBD}) {
		t.Errorf("lone surrogate not replaced: % x", out)
	}
}

func TestDecodeBOM_EmptyInput(t *testing.T) {
	if got := DecodeBOM(nil); got != nil && len(got) != 0 {
		t.Errorf("nil input should stay nil/empty, got %q", got)
	}
}
