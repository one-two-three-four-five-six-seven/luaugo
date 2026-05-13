// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib

import "github.com/luaugo/luaugo/pkg/vm"

// Tier-1 placeholders for Tier 4's stdlib swarm. Each Open* call below
// will be replaced in Tier 4 with a real implementation in its
// dedicated file (base.go, math.go, ...).

func openBase(s *vm.State)      { panic("lib: base not yet implemented (Tier 4)") }
func openMath(s *vm.State)      { panic("lib: math not yet implemented (Tier 4)") }
func openString(s *vm.State)    { panic("lib: string not yet implemented (Tier 4)") }
func openTable(s *vm.State)     { panic("lib: table not yet implemented (Tier 4)") }
func openCoroutine(s *vm.State) { panic("lib: coroutine not yet implemented (Tier 4)") }
func openBit32(s *vm.State)     { panic("lib: bit32 not yet implemented (Tier 4)") }
func openUTF8(s *vm.State)      { panic("lib: utf8 not yet implemented (Tier 4)") }
func openOS(s *vm.State)        { panic("lib: os not yet implemented (Tier 4)") }
func openDebug(s *vm.State)     { panic("lib: debug not yet implemented (Tier 4)") }
func openBuffer(s *vm.State)    { panic("lib: buffer not yet implemented (Tier 4)") }
func openVector(s *vm.State)    { panic("lib: vector not yet implemented (Tier 4)") }
