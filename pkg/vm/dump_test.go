// Copyright (c) luaugo contributors. Licensed under the MIT License.

package vm_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
)

// TestDumpFixture compiles a file from LUAUGO_FIXTURE and prints the
// disassembly. Skipped unless env var is set.
func TestDumpFixture(t *testing.T) {
	name := os.Getenv("LUAUGO_DUMP")
	if name == "" {
		t.Skip("set LUAUGO_DUMP to enable")
	}
	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	blob, err := compiler.CompileBinary(name, src, compiler.Defaults())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	m, err := bytecode.Decode(blob)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	fmt.Println(bytecode.Disassemble(m))
	for i, p := range m.Protos {
		fmt.Printf("proto %d MaxStackSize=%d NumParams=%d IsVararg=%d NumUpvalues=%d\n",
			i, p.MaxStackSize, p.NumParams, p.IsVararg, p.NumUpvalues)
	}
}
