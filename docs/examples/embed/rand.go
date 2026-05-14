// Copyright (c) luaugo contributors. Licensed under the MIT License.

package main

import "math/rand"

// newSeededRand returns a *rand.Rand seeded with the given value so the
// embedder demo produces stable output.
func newSeededRand(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}
