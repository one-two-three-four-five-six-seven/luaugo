// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib

import "github.com/luaugo/luaugo/pkg/vm"

// stubs.go: declarations for Open* helpers whose dedicated file has
// not yet landed in the parallel Tier-4 stdlib swarm. Each landing
// agent removes its own entry from this file or replaces the panic
// with a delegation to its _Impl counterpart.

func openBuffer(s *vm.State)    { openBufferImpl(s) }
func openCoroutine(s *vm.State) { openCoroutineImpl(s) }
func openBit32(s *vm.State)     { openBit32Impl(s) }
func openOS(s *vm.State)        { openOSImpl(s) }
