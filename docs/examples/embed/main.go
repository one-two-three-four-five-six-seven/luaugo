// Copyright (c) luaugo contributors. Licensed under the MIT License.

// Command embed is a self-contained worked example of embedding the
// luaugo VM into a Go program. It demonstrates the surface a host
// application typically uses:
//
//   - building a State and opening the standard library
//   - compiling source into a bytecode blob
//   - loading the blob and running it under PCall
//   - registering Go functions callable from Lua
//   - reading and writing globals
//   - calling a Lua function from Go with arguments and return values
//   - reporting errors with their Lua-side traceback
//
// Run with:
//
//	go run ./docs/examples/embed
//
// or build a standalone binary:
//
//	go build -o embed ./docs/examples/embed
//	./embed
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// The Lua source we'll execute. It exercises:
//   - a global function the host registered (greet)
//   - a global value the host set (appName)
//   - the standard library (table.concat, math.floor)
//   - returning multiple values to Go
//
// Real programs will usually read this from a file via os.ReadFile.
const script = `
print("script: appName =", appName)

local words = {"hello", "from", "luau"}
greet(table.concat(words, " "))

-- Return three values; the host will harvest them via state.Top()
-- and the typed To* accessors.
local pi = math.floor(math.pi * 100) / 100
return pi, #words, "done"
`

func main() {
	// 1. Create a fresh VM state. Always defer Close so any
	//    coroutines parked on the state can shut down cleanly.
	state := vm.NewState()
	defer state.Close()

	// 2. Open the standard library (print, math.*, table.*, string.*,
	//    coroutine.*, os.*, etc.). Hosts that want to deny specific
	//    libraries can skip this and call the individual lib.OpenX
	//    functions instead.
	lib.OpenAll(state)

	// 3. Register a Go function as a Lua global. The callback receives
	//    the State and reads its arguments through the typed To*
	//    accessors; it returns the number of values it pushed.
	state.Register("greet", func(s *vm.State) int {
		who, _ := s.ToString(1)
		fmt.Fprintf(os.Stdout, "greet: hello, %s!\n", who)
		// We're returning zero values to the script. If we wanted to
		// return e.g. a number, we'd PushNumber(...) and return 1.
		return 0
	})

	// 4. Set a global from Go. The general pattern is "push value,
	//    SetGlobal name"; convenience helpers exist for common types.
	state.PushString("luaugo-demo")
	state.SetGlobal("appName")

	// 5. Compile the script. CompileBinary returns the same bytecode
	//    blob that `luau-compile --binary` would emit, so you can
	//    pre-compile scripts at build time and ship the blob.
	blob, err := compiler.CompileBinary("=demo", []byte(script), compiler.Defaults())
	if err != nil {
		log.Fatalf("compile: %v", err)
	}
	if len(blob) > 0 && blob[0] == 0 {
		// Compile-error blobs start with a zero byte; the rest is
		// the human-readable error message.
		log.Fatalf("compile: %s", blob[1:])
	}

	// 6. Load the blob into the VM. The chunkname is what shows up
	//    in stack traces (`@demo:5: error`); prefix it with '@' for
	//    a file-style chunk or '=' for an embedded one.
	if err := state.Load("=demo", blob, 0); err != nil {
		log.Fatalf("load: %v", err)
	}

	// 7. Run the chunk under PCall so a Lua-side error becomes a
	//    Status return value rather than a Go panic. The chunk takes
	//    no arguments and we want all its return values, so pass
	//    nargs=0 and nresults=-1.
	//
	//    The closure currently lives at the top of the stack (Load
	//    pushed it). After PCall consumes it, any return values land
	//    starting at the index the closure occupied. We capture that
	//    base index BEFORE the call so we can slice the results out
	//    of the stack afterwards.
	base := state.Top() - 1
	if status := state.PCall(0, -1, 0); status != vm.StatusOK {
		// On error the message is on top of the stack.
		msg, _ := state.ToString(-1)
		log.Fatalf("runtime: %s", msg)
	}

	// 8. Harvest the chunk's return values. The chunk returned three
	//    values; they're now on the stack at indices base+1..Top().
	nres := state.Top() - base
	fmt.Printf("script returned %d values:\n", nres)
	for i := 1; i <= nres; i++ {
		idx := base + i
		switch state.Type(idx) {
		case vm.TNumber:
			n, _ := state.ToNumber(idx)
			fmt.Printf("  [%d] number: %v\n", i, n)
		case vm.TString:
			s, _ := state.ToString(idx)
			fmt.Printf("  [%d] string: %q\n", i, s)
		case vm.TBoolean:
			fmt.Printf("  [%d] boolean: %v\n", i, state.ToBoolean(idx))
		case vm.TNil:
			fmt.Printf("  [%d] nil\n", i)
		default:
			fmt.Printf("  [%d] (other: %v)\n", i, state.Type(idx))
		}
	}
	state.SetTop(base) // pop the results when we're done with them

	// 9. Call a function defined in the script from Go. We installed
	//    the chunk's environment via load+PCall above, so the
	//    standard library is reachable. Push the function, push the
	//    args, then PCall.
	state.GetGlobal("string")
	state.GetField(-1, "upper") // pushes the upper function
	state.Remove(-2)            // drop the `string` table we kept on the stack
	state.PushString("luaugo")
	if status := state.PCall(1, 1, 0); status != vm.StatusOK {
		msg, _ := state.ToString(-1)
		log.Fatalf("string.upper: %s", msg)
	}
	upper, _ := state.ToString(-1)
	state.Pop(1)
	fmt.Printf("string.upper(\"luaugo\") = %q\n", upper)

	// 10. Show what an in-Lua error looks like. PCall returns
	//     StatusErrRun and leaves the error value on the stack.
	state.GetGlobal("error")
	state.PushString("something exploded")
	status := state.PCall(1, 0, 0)
	msg, _ := state.ToString(-1)
	state.Pop(1)
	fmt.Printf("error() under PCall: status=%v, msg=%q\n", status, msg)
}
