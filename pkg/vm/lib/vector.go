// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import (
	"math"

	"github.com/luaugo/luaugo/pkg/vm"
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

// openVector registers the `vector` global table on s. Mirrors upstream
// luaopen_vector. The vector metatable (`__index` for component access)
// is owned by the VM and is not touched here.
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
}
