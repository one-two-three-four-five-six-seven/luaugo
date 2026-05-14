// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib_test

import (
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// runPackCheck compiles `src` and runs it on a fresh state with
// the full standard library opened (so pack/unpack/packsize, pcall,
// string.find etc. are all available). The chunk should `return`
// its result; the returned value is the top-of-stack stringified.
// On a runtime error the second return value contains the message.
func runPackCheck(t *testing.T, src string) (string, string) {
	t.Helper()
	s := vm.NewState()
	defer s.Close()
	lib.OpenAll(s)

	blob, err := compiler.CompileBinary("=string_pack_test", []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(blob) == 0 || blob[0] == 0 {
		t.Fatalf("compile error blob: %q", blob)
	}
	if err := s.Load("=string_pack_test", blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	var rt string
	func() {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(error); ok {
					rt = e.Error()
				} else {
					rt = string([]byte{})
				}
			}
		}()
		s.Call(0, 1)
	}()
	if rt != "" {
		return "", rt
	}
	out, _ := s.ToString(-1)
	return out, ""
}

// Bug 1: unpacking a length-prefixed format ('z') consumed the value
// but never bumped the return-count, so the next read consumed the
// nil filler and the string was lost. Regression test for the tpack
// conformance fixture (line 196).
func TestStringPackZUnpackReturnsValue(t *testing.T) {
	src := `
		local s = "hello"
		local s1, b = string.unpack("zB", s .. "\0\xF9")
		return string.format("s1=%q b=%d eq=%s", s1, b, tostring(s1 == s))
	`
	got, rt := runPackCheck(t, src)
	if rt != "" {
		t.Fatalf("runtime err: %s", rt)
	}
	want := `s1="hello" b=249 eq=true`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// Bug 2: unpack of an integer wider than 8 bytes returned only the
// low 64 bits and ignored the rest of the bytes. Upstream errors
// "<size>-byte integer does not fit into Lua Integer" if the high
// bytes are not all 0 (or 0xff for sign-extended negative).
func TestStringPackUnpackIntegerOverflowDetected(t *testing.T) {
	// 9-byte unsigned: low 8 bytes zero, byte 9 = 1 → does NOT fit.
	src := `
		local s = ("\0"):rep(8) .. "\1"
		local ok, err = pcall(string.unpack, "<I9", s)
		return tostring(ok) .. "|" .. tostring(err)
	`
	got, rt := runPackCheck(t, src)
	if rt != "" {
		t.Fatalf("runtime err: %s", rt)
	}
	if !strings.Contains(got, "false") || !strings.Contains(got, "does not fit") {
		t.Fatalf("expected `does not fit` failure, got %q", got)
	}
}

// Bug 3: unpacking 9..16-byte SIGNED integers must verify the high
// bytes match the sign-extension pattern of the low 8 bytes. Negative
// values must have 0xff padding; non-negative must have 0x00.
func TestStringPackUnpackSignedOverflowDetected(t *testing.T) {
	src := `
		-- Positive low byte (0x7f...) but high byte set: violates
		-- sign-extension rules for ">i9", should "does not fit".
		local s = "\1" .. ("\xff"):rep(8)
		local ok, err = pcall(string.unpack, ">i9", s)
		return tostring(ok) .. "|" .. tostring(err)
	`
	got, rt := runPackCheck(t, src)
	if rt != "" {
		t.Fatalf("runtime err: %s", rt)
	}
	if !strings.Contains(got, "false") || !strings.Contains(got, "does not fit") {
		t.Fatalf("expected `does not fit` failure, got %q", got)
	}
}

// Bug 4: packsize result exceeding 1GB must error "format result too
// large" (conformance line 141-146). Reachable at exactly 2^30.
func TestStringPackSizeLimit(t *testing.T) {
	// 1GB is reachable.
	got, rt := runPackCheck(t, `return tostring(string.packsize("c1073741824"))`)
	if rt != "" {
		t.Fatalf("packsize(c2^30) err: %s", rt)
	}
	if got != "1073741824" {
		t.Fatalf("packsize(c2^30) got %q want 1073741824", got)
	}
	// 1GB + 1 must fail.
	got, rt = runPackCheck(t, `
		local ok, err = pcall(string.packsize, "c1073741825")
		return tostring(ok) .. "|" .. tostring(err)
	`)
	if rt != "" {
		t.Fatalf("runtime err: %s", rt)
	}
	if !strings.Contains(got, "false") || !strings.Contains(got, "too large") {
		t.Fatalf("expected `too large` failure, got %q", got)
	}
}

// Bug 5: huge size specifiers (10+ digits) must be reported as
// "too large", not "out of limits" (conformance line 323-325).
func TestStringPackHugeSizeSpecifier(t *testing.T) {
	cases := []string{
		`local ok, err = pcall(string.unpack, "i9876543210", "")
		 return tostring(ok) .. "|" .. tostring(err)`,
		`local ok, err = pcall(string.unpack, "c9876543210", "")
		 return tostring(ok) .. "|" .. tostring(err)`,
		`local ok, err = pcall(string.packsize, "c1" .. string.rep("0", 40))
		 return tostring(ok) .. "|" .. tostring(err)`,
	}
	for i, src := range cases {
		got, rt := runPackCheck(t, src)
		if rt != "" {
			t.Fatalf("case %d runtime err: %s", i, rt)
		}
		if !strings.Contains(got, "false") || !strings.Contains(got, "too large") {
			t.Fatalf("case %d: expected `too large`, got %q", i, got)
		}
	}
	// But 9-digit values still report "out of limits".
	got, rt := runPackCheck(t, `
		local ok, err = pcall(string.unpack, "i987654321", "")
		return tostring(ok) .. "|" .. tostring(err)
	`)
	if rt != "" {
		t.Fatalf("9-digit runtime err: %s", rt)
	}
	if !strings.Contains(got, "out of limits") {
		t.Fatalf("9-digit: expected `out of limits`, got %q", got)
	}
}

// Sanity: the tpack conformance script returns "OK".
func TestStringPackConformanceFixture(t *testing.T) {
	bytes, err := readTpackFixture()
	if err != nil {
		t.Skipf("fixture not found: %v", err)
		return
	}
	got, rt := runPackCheck(t, string(bytes))
	if rt != "" {
		t.Fatalf("tpack.luau runtime err: %s", rt)
	}
	if got != "OK" {
		t.Fatalf("tpack.luau returned %q, want OK", got)
	}
}
