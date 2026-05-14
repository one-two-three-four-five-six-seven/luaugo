// Copyright (c) luaugo contributors. Licensed under the MIT License.

// Package demo contains an end-to-end demonstration: luaugo's compiler
// produces bytecode for a non-trivial Luau program, and the official
// upstream Luau VM executes that bytecode and produces the expected
// output. This is the headline "we can compile Luau and run it on the
// real VM" proof.
package demo

import (
	"strings"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/internal/upstreamvm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
)

const demoSource = `
-- A small but non-trivial Luau program touching the major language features:
-- locals, arithmetic, string concatenation, tables, closures, generic for,
-- numeric for, conditionals, methods, varargs, and a recursive function.

local function greet(name)
    return "Hello, " .. name .. "!"
end

local function factorial(n)
    if n <= 1 then return 1 end
    return n * factorial(n - 1)
end

local function sum(...)
    local total = 0
    for _, v in ipairs({...}) do
        total = total + v
    end
    return total
end

local people = {"world", "luaugo", "real luau"}
for i = 1, #people do
    print(greet(people[i]))
end

print("5! = " .. tostring(factorial(5)))
print("sum(1..10) = " .. tostring(sum(1,2,3,4,5,6,7,8,9,10)))

local counter = (function()
    local n = 0
    return function()
        n = n + 1
        return n
    end
end)()

print("counter: " .. tostring(counter()) .. " " .. tostring(counter()) .. " " .. tostring(counter()))

return "done"
`

func TestDemoCompileAndRunOnUpstreamVM(t *testing.T) {
	upstreamvm.RequireAvailable(t)

	// 1. Compile with luaugo (pure Go).
	blob, err := compiler.CompileBinary("demo.luau", []byte(demoSource), compiler.Defaults())
	if err != nil {
		t.Fatalf("luaugo compile failed: %v", err)
	}
	t.Logf("luaugo compiler produced %d bytes of bytecode", len(blob))
	if len(blob) == 0 || blob[0] == 0 {
		t.Fatalf("compile produced an error blob: %q", blob)
	}
	t.Logf("bytecode version: %d", blob[0])

	// 2. Execute on the real upstream Luau VM.
	result, err := upstreamvm.Run(blob)
	if err != nil {
		t.Fatalf("upstream VM invocation failed: %v", err)
	}

	t.Logf("upstream VM exit status: %d", result.Status)
	t.Logf("upstream VM stdout:\n%s", result.Stdout)
	if result.Stderr != "" {
		t.Logf("upstream VM stderr:\n%s", result.Stderr)
	}

	if result.Status != upstreamvm.StatusOK {
		t.Fatalf("upstream VM did not return StatusOK; got %d, stderr=%q",
			result.Status, result.Stderr)
	}

	expectations := []string{
		"Hello, world!",
		"Hello, luaugo!",
		"Hello, real luau!",
		"5! = 120",
		"sum(1..10) = 55",
		"counter: 1 2 3",
		"done",
	}
	for _, want := range expectations {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("stdout missing %q\n--- full stdout ---\n%s", want, result.Stdout)
		}
	}
}
