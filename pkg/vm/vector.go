// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package vm

// VectorComponents is the number of float32 components carried by
// `vector` values. Upstream Luau is built with either 3 (the default)
// or 4 components; we lock to 3 for v0.720 conformance but the rest of
// the VM consults this constant so a 4-wide rebuild only changes a
// single line.
const VectorComponents = 3

// Vector is the in-memory layout of a Lua vector value. We expose it as
// an exported struct so library code (`pkg/vm/lib/vector`) can pass it
// across the package boundary without going through the value tag
// union.
type Vector struct {
	X, Y, Z, W float32
}

// vectorFromValue extracts the components of a TVector value as a
// Vector struct. Caller is responsible for checking the tag.
func vectorFromValue(v value) Vector {
	return Vector{X: v.vec[0], Y: v.vec[1], Z: v.vec[2], W: v.vec[3]}
}

// valueFromVector reverses the above.
func valueFromVector(v Vector) value {
	return vectorValue(v.X, v.Y, v.Z, v.W)
}

// Add returns a+b component-wise.
func (a Vector) Add(b Vector) Vector { return Vector{a.X + b.X, a.Y + b.Y, a.Z + b.Z, a.W + b.W} }

// Sub returns a-b component-wise.
func (a Vector) Sub(b Vector) Vector { return Vector{a.X - b.X, a.Y - b.Y, a.Z - b.Z, a.W - b.W} }

// Mul returns a*b component-wise (Hadamard product, matching upstream
// vector arithmetic).
func (a Vector) Mul(b Vector) Vector { return Vector{a.X * b.X, a.Y * b.Y, a.Z * b.Z, a.W * b.W} }

// Div returns a/b component-wise.
func (a Vector) Div(b Vector) Vector { return Vector{a.X / b.X, a.Y / b.Y, a.Z / b.Z, a.W / b.W} }

// Neg returns -a.
func (a Vector) Neg() Vector { return Vector{-a.X, -a.Y, -a.Z, -a.W} }

// Scale multiplies every component by s.
func (a Vector) Scale(s float32) Vector { return Vector{a.X * s, a.Y * s, a.Z * s, a.W * s} }

// Eq reports whether a and b are bitwise-equal across all active
// components. The W component is compared only if the VM is 4-wide.
func (a Vector) Eq(b Vector) bool {
	if VectorComponents == 4 {
		return a == b
	}
	return a.X == b.X && a.Y == b.Y && a.Z == b.Z
}

// Dot returns the dot product across active components.
func (a Vector) Dot(b Vector) float32 {
	if VectorComponents == 4 {
		return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W
	}
	return a.X*b.X + a.Y*b.Y + a.Z*b.Z
}
