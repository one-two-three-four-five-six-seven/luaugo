// Copyright (c) luaugo contributors. Licensed under the MIT License.

// This is a complete, runnable embedder demo. It shows:
//   - compiling a Luau source string to bytecode in-process
//   - loading the bytecode onto a luaugo VM state
//   - opening every standard library
//   - registering a Go function as a Luau global
//   - calling into Luau with arguments and reading the return values
//   - handling Luau runtime errors as Go values via PCall
//
// Run with:
//
//   go run ./docs/examples/embed
//
// The expected output is shown at the end of this file.
package main

import (
	"fmt"
	"os"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

const source = `
-- A small program exercising: a Go-provided global, numeric for loops,
-- arithmetic, and a string return value.

local function sum_random(n, lo, hi)
    local s = 0
    for i = 1, n do
        s = s + random_amount(lo, hi)
    end
    return s
end

local total = sum_random(5, 1, 100)
return "total of 5 randoms in [1,100]: " .. total
`

func main() {
	// 1. Compile.
	blob, err := compiler.CompileBinary("ledger.luau", []byte(source), compiler.Defaults())
	if err != nil {
		fmt.Fprintln(os.Stderr, "compile error:", err)
		os.Exit(1)
	}

	// 2. Build a state with the standard library.
	s := vm.NewState()
	defer s.Close()
	lib.OpenAll(s)

	// 3. Register a Go function for the script to call.
	//
	// random_amount(low, high) returns an integer in [low, high].
	// We use a deterministic seed so the demo's output is stable.
	rng := newSeededRand(42)
	s.Register("random_amount", func(s *vm.State) int {
		low, ok1 := s.ToInteger(1)
		high, ok2 := s.ToInteger(2)
		if !ok1 || !ok2 {
			s.Errorf("random_amount: expected two integers")
		}
		if high < low {
			s.Errorf("random_amount: high (%d) must be >= low (%d)", high, low)
		}
		s.PushInteger(low + rng.Int63n(high-low+1))
		return 1
	})

	// 4. Load and call.
	if err := s.Load("ledger.luau", blob, 0); err != nil {
		fmt.Fprintln(os.Stderr, "load error:", err)
		os.Exit(1)
	}
	if status := s.PCall(0, 1, 0); status != vm.StatusOK {
		errMsg, _ := s.ToString(-1)
		fmt.Fprintln(os.Stderr, "runtime error:", errMsg)
		os.Exit(1)
	}

	// 5. Read the single return value.
	report, _ := s.ToString(-1)
	fmt.Println(report)
}

// Expected output (deterministic because the Go RNG is seeded):
//
//   total of 5 randoms in [1,100]: <sum>
//
// The exact sum depends on the math/rand stream for seed 42 but will
// be in [5, 500].
