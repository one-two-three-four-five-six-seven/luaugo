// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package bytecode

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/luaugo/luaugo/internal/common"
)

// buildSampleModule constructs a minimal Module that exercises the
// constant kinds and instruction shapes most likely to expose
// encode/decode bugs: NEWTABLE (ABC + AUX), LOADK (AD), GETTABLEKS
// (ABC + AUX), CALL, and RETURN. The Module uses bytecode v6 (the
// default target) and typesversion 3 so the encoder must round-trip
// the userdata-name section as well.
func buildSampleModule() *Module {
	// Strings (1-based when referenced externally):
	//   1 "main"   2 "k"   3 "print"
	strings := []string{"main", "k", "print"}

	// Constants:
	//   k[0] = "print"  (string idx 3)
	//   k[1] = "k"      (string idx 2)
	//   k[2] = 42.0     (number)
	consts := []Constant{
		ConstantStringEntry{Index: 3},
		ConstantStringEntry{Index: 2},
		ConstantNumberEntry{Value: 42.0},
	}

	// Instructions (each comment shows the upstream mnemonic):
	//   NEWTABLE R0, 0, 0   AUX=0
	//   LOADK    R1, K[2]
	//   GETTABLEKS R2, R0, [cached=0]   AUX=1 (constant index of "k")
	//   GETGLOBAL R3, [cached=0]        AUX=0 (constant index of "print")
	//   CALL     R3, 1, 1
	//   RETURN   R0, 1
	code := []uint32{
		common.EncodeABC(common.OpNewTable, 0, 0, 0),
		0, // AUX for NEWTABLE
		common.EncodeAD(common.OpLoadK, 1, 2),
		common.EncodeABC(common.OpGetTableKS, 2, 0, 0),
		1, // AUX for GETTABLEKS: constant index 1 ("k")
		common.EncodeABC(common.OpGetGlobal, 3, 0, 0),
		0, // AUX for GETGLOBAL
		common.EncodeABC(common.OpCall, 3, 1, 1),
		common.EncodeABC(common.OpReturn, 0, 1, 0),
	}

	proto := &Proto{
		MaxStackSize: 4,
		NumParams:    0,
		NumUpvalues:  0,
		IsVararg:     1,
		Flags:        0,
		Code:         code,
		Constants:    consts,
		LineDefined:  1,
		DebugName:    1, // "main"
	}

	return &Module{
		Version:     6,
		TypeVersion: common.TypeVersionTarget, // 3
		Strings:     strings,
		Protos:      []*Proto{proto},
		MainProto:   0,
	}
}

// TestRoundTripSyntheticModule asserts that Encode is deterministic and
// stable across an Encode/Decode/Encode cycle. The first Encode produces
// the canonical blob; Decode reconstructs the in-memory form; the second
// Encode must produce the exact same bytes.
func TestRoundTripSyntheticModule(t *testing.T) {
	m := buildSampleModule()

	blob1, err := Encode(m, EncodeOptions{})
	if err != nil {
		t.Fatalf("first Encode: %v", err)
	}

	m2, err := Decode(blob1)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	blob2, err := Encode(m2, EncodeOptions{})
	if err != nil {
		t.Fatalf("second Encode: %v", err)
	}

	if !bytes.Equal(blob1, blob2) {
		t.Fatalf("round trip not byte-identical\nfirst:  %x\nsecond: %x", blob1, blob2)
	}
}

// TestRoundTripAllConstantKinds covers every Constant variant in the
// contract, including v7+ TABLE_WITH_CONSTANTS, v8+ INTEGER (both
// positive and negative), and v5+ VECTOR. Uses bytecode v9 to enable
// every constant kind simultaneously.
func TestRoundTripAllConstantKinds(t *testing.T) {
	m := &Module{
		Version:     9,
		TypeVersion: 3,
		Strings:     []string{"a", "b"},
		Protos: []*Proto{
			{
				MaxStackSize: 1,
				IsVararg:     0,
				Code:         []uint32{common.EncodeABC(common.OpReturn, 0, 1, 0)},
				Constants: []Constant{
					ConstantNilEntry{},
					ConstantBooleanEntry{Value: true},
					ConstantBooleanEntry{Value: false},
					ConstantNumberEntry{Value: 3.14},
					ConstantStringEntry{Index: 1},
					ConstantImportEntry{Packed: 0xdeadbeef},
					ConstantTableEntry{Keys: []uint32{0, 1, 2}},
					ConstantClosureEntry{ProtoIndex: 0},
					ConstantVectorEntry{X: 1, Y: 2, Z: 3, W: 0},
					ConstantTableWithConstantsEntry{Pairs: []ConstantTablePair{{Key: 4, Value: 3}}},
					ConstantIntegerEntry{Value: 9223372036854775807},
					ConstantIntegerEntry{Value: -9223372036854775808},
					ConstantIntegerEntry{Value: 0},
					ConstantIntegerEntry{Value: -1},
				},
				LineDefined: 1,
				DebugName:   1,
			},
		},
		MainProto: 0,
	}

	blob1, err := Encode(m, EncodeOptions{})
	if err != nil {
		t.Fatalf("first Encode: %v", err)
	}
	m2, err := Decode(blob1)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	blob2, err := Encode(m2, EncodeOptions{})
	if err != nil {
		t.Fatalf("second Encode: %v", err)
	}
	if !bytes.Equal(blob1, blob2) {
		t.Fatalf("round trip not byte-identical\nfirst:  %x\nsecond: %x", blob1, blob2)
	}
}

// TestRoundTripWithLineAndDebugInfo verifies that the line-info and
// debug-info sections survive a full round trip.
func TestRoundTripWithLineAndDebugInfo(t *testing.T) {
	m := &Module{
		Version:     6,
		TypeVersion: 3,
		Strings:     []string{"f", "x"},
		Protos: []*Proto{
			{
				MaxStackSize: 2,
				NumParams:    1,
				IsVararg:     0,
				Code: []uint32{
					common.EncodeABC(common.OpMove, 1, 0, 0),
					common.EncodeABC(common.OpReturn, 1, 2, 0),
				},
				LineDefined: 7,
				DebugName:   1,
				LineInfo: &LineInfo{
					LineGapLog2: 24,           // intervals = ((2-1)>>24)+1 = 1
					LineInfo:    []int8{0, 5}, // per-instruction deltas
					AbsLineInfo: []int32{7},
				},
				DebugInfo: &DebugInfo{
					Locals: []DebugLocal{
						{Name: 2, StartPC: 0, EndPC: 2, Reg: 0},
					},
				},
			},
		},
		MainProto: 0,
	}

	blob1, err := Encode(m, EncodeOptions{})
	if err != nil {
		t.Fatalf("first Encode: %v", err)
	}
	m2, err := Decode(blob1)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	blob2, err := Encode(m2, EncodeOptions{})
	if err != nil {
		t.Fatalf("second Encode: %v", err)
	}
	if !bytes.Equal(blob1, blob2) {
		t.Fatalf("round trip not byte-identical\nfirst:  %x\nsecond: %x", blob1, blob2)
	}
}

// TestDecodeErrorPrefix verifies the convention that a leading zero byte
// signals a compile-time error, with the remainder of the blob being the
// error message returned via *DecodeError.
func TestDecodeErrorPrefix(t *testing.T) {
	msg := "syntax error near 'end'"
	blob := append([]byte{0}, []byte(msg)...)

	_, err := Decode(blob)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var de *DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DecodeError, got %T (%v)", err, err)
	}
	if de.Msg != msg {
		t.Errorf("DecodeError.Msg = %q, want %q", de.Msg, msg)
	}
	if de.Offset != 0 {
		t.Errorf("DecodeError.Offset = %d, want 0", de.Offset)
	}
}

// TestGoldenRoundTrip walks tests/golden/*.luac (relative to the
// workspace root) and round-trips each file. Skipped cleanly if no
// golden files are present, so the suite stays green on fresh
// checkouts.
func TestGoldenRoundTrip(t *testing.T) {
	roots := []string{
		filepath.Join("..", "..", "tests", "golden"),
		filepath.Join("..", "..", "..", "tests", "golden"),
	}
	var matches []string
	for _, root := range roots {
		entries, err := filepath.Glob(filepath.Join(root, "*.luac"))
		if err == nil && len(entries) > 0 {
			matches = entries
			break
		}
	}
	if len(matches) == 0 {
		t.Skip("no golden *.luac files found; skipping")
	}

	for _, path := range matches {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			blob, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			m, err := Decode(blob)
			if err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			re, err := Encode(m, EncodeOptions{})
			if err != nil {
				t.Fatalf("re-encode %s: %v", path, err)
			}
			if !bytes.Equal(blob, re) {
				t.Fatalf("re-encoded %s not byte-identical to original\noriginal: %x\nreencoded: %x",
					path, blob, re)
			}
		})
	}
}
