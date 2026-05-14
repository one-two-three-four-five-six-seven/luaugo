// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import (
	"math"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
)

// vector.go implements Luau's `vector` library, mirroring upstream
// VM/src/lveclib.cpp. Tier 2's pkg/vm/vector.go owns the in-memory
// Vector type and defines VectorComponents (locked to 3 for this
// build). This file only registers the public surface (the `vector`
// global table plus its `zero` / `one` constants).

// checkVector returns the (x,y,z) components of the vector at argn or
// raises a type-mismatch error.
func checkVector(s *vm.State, argn int) (float32, float32, float32) {
	x, y, z, _, ok := s.ToVector(argn)
	if !ok {
		s.LTypeError(argn, "vector")
	}
	return x, y, z
}

// optVector returns the (x,y,z) components of the vector at argn, or
// (0,0,0) and ok=false if the slot is none/nil. Raises if the slot is
// present but not a vector.
func optVector(s *vm.State, argn int) (float32, float32, float32, bool) {
	if s.IsNoneOrNil(argn) {
		return 0, 0, 0, false
	}
	x, y, z := checkVector(s, argn)
	return x, y, z, true
}

// pushVec3 pushes a 3-wide vector (w=0).
func pushVec3(s *vm.State, x, y, z float32) {
	s.PushVector(x, y, z, 0)
}

func vectorCreate(s *vm.State) int {
	// Mirror upstream: count check avoids accepting `nil` for required
	// args. x and y are required; z defaults to 0.
	count := s.Top()
	x := float32(s.LCheckNumber(1))
	y := float32(s.LCheckNumber(2))
	var z float32
	if count >= 3 {
		z = float32(s.LCheckNumber(3))
	}
	pushVec3(s, x, y, z)
	return 1
}

func vectorMagnitude(s *vm.State) int {
	x, y, z := checkVector(s, 1)
	// Compute in float32 to match upstream sqrtf.
	m := float32(math.Sqrt(float64(x*x + y*y + z*z)))
	s.PushNumber(float64(m))
	return 1
}

func vectorNormalize(s *vm.State) int {
	x, y, z := checkVector(s, 1)
	mag := float32(math.Sqrt(float64(x*x + y*y + z*z)))
	if mag == 0 {
		// Upstream produces inf/NaN here; the contract specifies that
		// luaugo returns the zero vector for zero-magnitude input.
		pushVec3(s, 0, 0, 0)
		return 1
	}
	inv := 1.0 / mag
	pushVec3(s, x*inv, y*inv, z*inv)
	return 1
}

func vectorCross(s *vm.State) int {
	ax, ay, az := checkVector(s, 1)
	bx, by, bz := checkVector(s, 2)
	pushVec3(s,
		ay*bz-az*by,
		az*bx-ax*bz,
		ax*by-ay*bx,
	)
	return 1
}

func vectorDot(s *vm.State) int {
	ax, ay, az := checkVector(s, 1)
	bx, by, bz := checkVector(s, 2)
	s.PushNumber(float64(ax*bx + ay*by + az*bz))
	return 1
}

func vectorAngle(s *vm.State) int {
	ax, ay, az := checkVector(s, 1)
	bx, by, bz := checkVector(s, 2)
	axisX, axisY, axisZ, hasAxis := optVector(s, 3)

	// cross(a, b)
	cx := ay*bz - az*by
	cy := az*bx - ax*bz
	cz := ax*by - ay*bx

	// Upstream promotes to double for atan2.
	sinA := math.Sqrt(float64(cx)*float64(cx) + float64(cy)*float64(cy) + float64(cz)*float64(cz))
	cosA := float64(ax)*float64(bx) + float64(ay)*float64(by) + float64(az)*float64(bz)
	angle := math.Atan2(sinA, cosA)

	if hasAxis {
		// cross . axis < 0 -> negate.
		dot := float64(cx)*float64(axisX) + float64(cy)*float64(axisY) + float64(cz)*float64(axisZ)
		if dot < 0 {
			angle = -angle
		}
	}
	s.PushNumber(angle)
	return 1
}

func vectorFloor(s *vm.State) int {
	x, y, z := checkVector(s, 1)
	pushVec3(s,
		float32(math.Floor(float64(x))),
		float32(math.Floor(float64(y))),
		float32(math.Floor(float64(z))),
	)
	return 1
}

func vectorCeil(s *vm.State) int {
	x, y, z := checkVector(s, 1)
	pushVec3(s,
		float32(math.Ceil(float64(x))),
		float32(math.Ceil(float64(y))),
		float32(math.Ceil(float64(z))),
	)
	return 1
}

func vectorAbs(s *vm.State) int {
	x, y, z := checkVector(s, 1)
	pushVec3(s,
		float32(math.Abs(float64(x))),
		float32(math.Abs(float64(y))),
		float32(math.Abs(float64(z))),
	)
	return 1
}

// signf mirrors upstream luaui_signf: returns +1 for positive, -1 for
// negative, and 0 for zero (including signed zero) and NaN.
func signf(v float32) float32 {
	if v > 0 {
		return 1
	}
	if v < 0 {
		return -1
	}
	return 0
}

func vectorSign(s *vm.State) int {
	x, y, z := checkVector(s, 1)
	pushVec3(s, signf(x), signf(y), signf(z))
	return 1
}

// clampf mirrors upstream luaui_clampf.
func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func vectorClamp(s *vm.State) int {
	x, y, z := checkVector(s, 1)
	mnx, mny, mnz := checkVector(s, 2)
	mxx, mxy, mxz := checkVector(s, 3)
	if mnx > mxx {
		s.LArgError(3, "max.x must be greater than or equal to min.x")
	}
	if mny > mxy {
		s.LArgError(3, "max.y must be greater than or equal to min.y")
	}
	if mnz > mxz {
		s.LArgError(3, "max.z must be greater than or equal to min.z")
	}
	pushVec3(s,
		clampf(x, mnx, mxx),
		clampf(y, mny, mxy),
		clampf(z, mnz, mxz),
	)
	return 1
}

func vectorMin(s *vm.State) int {
	n := s.Top()
	if n < 1 {
		s.LTypeError(1, "vector")
	}
	rx, ry, rz := checkVector(s, 1)
	for i := 2; i <= n; i++ {
		bx, by, bz := checkVector(s, i)
		if bx < rx {
			rx = bx
		}
		if by < ry {
			ry = by
		}
		if bz < rz {
			rz = bz
		}
	}
	pushVec3(s, rx, ry, rz)
	return 1
}

func vectorMax(s *vm.State) int {
	n := s.Top()
	if n < 1 {
		s.LTypeError(1, "vector")
	}
	rx, ry, rz := checkVector(s, 1)
	for i := 2; i <= n; i++ {
		bx, by, bz := checkVector(s, i)
		if bx > rx {
			rx = bx
		}
		if by > ry {
			ry = by
		}
		if bz > rz {
			rz = bz
		}
	}
	pushVec3(s, rx, ry, rz)
	return 1
}

// lerpf mirrors upstream luai_lerpf: returns b exactly when t == 1.
func lerpf(a, b, t float32) float32 {
	if t == 1 {
		return b
	}
	return a + (b-a)*t
}

func vectorLerp(s *vm.State) int {
	ax, ay, az := checkVector(s, 1)
	bx, by, bz := checkVector(s, 2)
	t := float32(s.LCheckNumber(3))
	pushVec3(s,
		lerpf(ax, bx, t),
		lerpf(ay, by, t),
		lerpf(az, bz, t),
	)
	return 1
}

// vectorLib is the ordered registry of `vector.*` functions, matching
// the layout of upstream vectorlib[] in lveclib.cpp.
var vectorLib = []vm.LFnEntry{
	{Name: "create", Fn: vectorCreate},
	{Name: "magnitude", Fn: vectorMagnitude},
	{Name: "normalize", Fn: vectorNormalize},
	{Name: "cross", Fn: vectorCross},
	{Name: "dot", Fn: vectorDot},
	{Name: "angle", Fn: vectorAngle},
	{Name: "floor", Fn: vectorFloor},
	{Name: "ceil", Fn: vectorCeil},
	{Name: "abs", Fn: vectorAbs},
	{Name: "sign", Fn: vectorSign},
	{Name: "clamp", Fn: vectorClamp},
	{Name: "max", Fn: vectorMax},
	{Name: "min", Fn: vectorMin},
	{Name: "lerp", Fn: vectorLerp},
}

// vectorMethods maps Roblox-style PascalCase method names exposed via
// vector __index and __namecall to their underlying implementations.
// These are the same Go functions the lowercase `vector.<name>`
// surface dispatches to: each implementation works equally well as a
// free function or as a bound method because the first argument is
// always the vector itself in both styles.
//
// Properties (Magnitude / Unit) are handled separately in
// vectorIndex; they return computed values rather than functions.
var vectorMethods = []vm.LFnEntry{
	{Name: "Dot", Fn: vectorDot},
	{Name: "Cross", Fn: vectorCross},
	{Name: "Angle", Fn: vectorAngle},
	{Name: "Floor", Fn: vectorFloor},
	{Name: "Ceil", Fn: vectorCeil},
	{Name: "Abs", Fn: vectorAbs},
	{Name: "Sign", Fn: vectorSign},
	{Name: "Clamp", Fn: vectorClamp},
	{Name: "Max", Fn: vectorMax},
	{Name: "Min", Fn: vectorMin},
	{Name: "Lerp", Fn: vectorLerp},
	{Name: "Normalize", Fn: vectorNormalize},
}

// vectorMethodsRegKey is the registry slot under which the
// PascalCase vector method table is parked so vectorIndex can find it
// without relying on closure upvalues (the public C-style API doesn't
// expose lua_upvalueindex).
const vectorMethodsRegKey = "luaugo.vector.methods"

// vectorIndex implements __index for vector values. Upstream Luau
// exposes Roblox-style properties (.Magnitude, .Unit) and methods
// (:Dot, :Cross, ...) on every vector. Lowercase component selectors
// (`.x`, `.y`, `.z`, `.w`) are short-circuited inside the VM by
// vectorComponent before this metamethod is consulted, so we only
// need to cover the Roblox surface here and raise on anything else.
func vectorIndex(s *vm.State) int {
	// args: 1 = vector, 2 = key
	if s.Type(2) != vm.TString {
		s.LError("attempt to index vector with a %s value", s.Type(2).String())
	}
	key, _ := s.ToString(2)
	switch key {
	case "Magnitude":
		x, y, z := checkVector(s, 1)
		m := float32(math.Sqrt(float64(x*x + y*y + z*z)))
		s.PushNumber(float64(m))
		return 1
	case "Unit":
		x, y, z := checkVector(s, 1)
		mag := float32(math.Sqrt(float64(x*x + y*y + z*z)))
		if mag == 0 {
			pushVec3(s, 0, 0, 0)
		} else {
			inv := 1.0 / mag
			pushVec3(s, x*inv, y*inv, z*inv)
		}
		return 1
	}
	// Method dispatch via the registry-cached method table.
	s.GetRegistryField(vectorMethodsRegKey)
	if s.Type(-1) == vm.TTable {
		s.PushValue(2) // key
		s.RawGet(-2)
		if !s.IsNil(-1) {
			return 1
		}
		s.Pop(2)
	} else {
		s.Pop(1)
	}
	s.LError("attempt to index vector with '%s'", key)
	return 0
}

// openVector registers the `vector` global table on s. Mirrors upstream
// luaopen_vector and additionally installs the per-type metatable on
// TVector so that scripts can use Roblox-style .Magnitude / .Unit
// property access and :Dot() / :Cross() method calls. Lowercase
// component selectors (`.x`, `.y`, `.z`, `.w`) are handled inside the
// VM's indexValue fast path and bypass this metatable.
func openVector(s *vm.State) {
	s.CreateTable(0, len(vectorLib)+2)
	s.LRegisterList(vectorLib)

	// vector.zero
	s.PushVector(0, 0, 0, 0)
	s.SetField(-2, "zero")
	// vector.one
	s.PushVector(1, 1, 1, 0)
	s.SetField(-2, "one")

	s.SetGlobal("vector")

	// Per-type metatable for vectors.
	// Build the PascalCase method table once and park it in the
	// registry so vectorIndex can resolve method lookups without
	// reconstructing closures on every access.
	s.CreateTable(0, len(vectorMethods))
	// Use the anonymous registration variant so that errors raised
	// from inside e.g. vector:Dot() omit the function-name prefix.
	// Upstream registers these per-type methods via lua_pushcfunction
	// with a NULL debug name (see VM/src/lvmtype.cpp), producing
	// "missing argument #2 (vector expected)" rather than the
	// luaL_register-style "missing argument #2 to 'Dot' (vector
	// expected)". The conformance fixture vector.luau:105 hard-codes
	// the un-prefixed form.
	s.LRegisterListAnon(vectorMethods)
	s.SetRegistryField(vectorMethodsRegKey)

	// Build the metatable: { __index = vectorIndex }.
	s.NewTable()
	s.PushGoFunction(vectorIndex, 0)
	s.SetField(-2, "__index")

	// Bind the metatable to TVector via SetMetatable on a vector value.
	s.PushVector(0, 0, 0, 0)
	s.Insert(-2)
	s.SetMetatable(-2)
	s.Pop(1) // pop the placeholder vector
}
