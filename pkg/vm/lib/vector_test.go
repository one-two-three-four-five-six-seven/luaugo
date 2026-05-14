// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import (
	"math"
	"testing"

	"github.com/luaugo/luaugo/pkg/vm"
)

// newVecState returns a fresh VM with the vector library opened.
//
// The C1 brief asks for end-to-end exercise via OpenBase + OpenVector;
// OpenBase is owned by another Tier-4 worker and is still a panicking
// stub, so these tests drive the vector library through the public
// Go API instead. Function dispatch still goes through the same
// CALL/PCall plumbing that a Lua script would use.
func newVecState(t *testing.T) *vm.State {
	t.Helper()
	s := vm.NewState()
	t.Cleanup(s.Close)
	OpenVector(s)
	// Sanity-check: `vector` is a global table.
	s.GetGlobal("vector")
	if s.Type(-1) != vm.TTable {
		t.Fatalf("vector global: expected table, got %v", s.Type(-1))
	}
	s.Pop(1)
	return s
}

// callVecFn pushes `vector.<name>` and the given args, then invokes the
// function and leaves exactly one return value on the stack.
func callVecFn(t *testing.T, s *vm.State, name string, args ...any) {
	t.Helper()
	s.GetGlobal("vector")
	s.GetField(-1, name)
	s.Remove(-2) // drop the `vector` table; leave the function on top
	for _, a := range args {
		pushArg(t, s, a)
	}
	if st := s.PCall(len(args), 1, 0); st != vm.StatusOK {
		msg, _ := s.ToString(-1)
		t.Fatalf("vector.%s call failed: %s", name, msg)
	}
}

// vec3 is a tiny convenience type for pushing 3-component vector args.
type vec3 struct{ x, y, z float32 }

func pushArg(t *testing.T, s *vm.State, a any) {
	t.Helper()
	switch v := a.(type) {
	case vec3:
		s.PushVector(v.x, v.y, v.z, 0)
	case float32:
		s.PushNumber(float64(v))
	case float64:
		s.PushNumber(v)
	case int:
		s.PushNumber(float64(v))
	default:
		t.Fatalf("pushArg: unsupported type %T", a)
	}
}

// topVec pops the top stack value and returns its (x,y,z).
func topVec(t *testing.T, s *vm.State) (float32, float32, float32) {
	t.Helper()
	if s.Type(-1) != vm.TVector {
		t.Fatalf("expected vector on stack, got %v", s.Type(-1))
	}
	x, y, z, _, ok := s.ToVector(-1)
	if !ok {
		t.Fatalf("ToVector failed")
	}
	s.Pop(1)
	return x, y, z
}

// topNum pops the top stack value and returns it as float64.
func topNum(t *testing.T, s *vm.State) float64 {
	t.Helper()
	v, ok := s.ToNumber(-1)
	if !ok {
		t.Fatalf("expected number on stack, got %v", s.Type(-1))
	}
	s.Pop(1)
	return v
}

func approxEqF32(a, b, eps float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= eps
}

func approxEqF64(a, b, eps float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= eps
}

func TestVectorCreate(t *testing.T) {
	s := newVecState(t)

	// 3-arg create.
	callVecFn(t, s, "create", float64(1), float64(2), float64(3))
	x, y, z := topVec(t, s)
	if x != 1 || y != 2 || z != 3 {
		t.Fatalf("create(1,2,3): got (%v,%v,%v)", x, y, z)
	}

	// 2-arg create -> z defaults to 0.
	callVecFn(t, s, "create", float64(4), float64(5))
	x, y, z = topVec(t, s)
	if x != 4 || y != 5 || z != 0 {
		t.Fatalf("create(4,5): got (%v,%v,%v)", x, y, z)
	}

	// vector.zero / vector.one constants.
	s.GetGlobal("vector")
	s.GetField(-1, "zero")
	x, y, z, _, ok := s.ToVector(-1)
	if !ok || x != 0 || y != 0 || z != 0 {
		t.Fatalf("vector.zero: ok=%v got (%v,%v,%v)", ok, x, y, z)
	}
	s.Pop(1)
	s.GetField(-1, "one")
	x, y, z, _, ok = s.ToVector(-1)
	if !ok || x != 1 || y != 1 || z != 1 {
		t.Fatalf("vector.one: ok=%v got (%v,%v,%v)", ok, x, y, z)
	}
	s.Pop(2)
}

func TestVectorMagnitudeNormalize(t *testing.T) {
	s := newVecState(t)

	// magnitude(3,4,0) = 5
	callVecFn(t, s, "magnitude", vec3{3, 4, 0})
	if got := topNum(t, s); !approxEqF64(got, 5, 1e-6) {
		t.Fatalf("magnitude(3,4,0): got %v want 5", got)
	}

	// magnitude(0,0,0) = 0
	callVecFn(t, s, "magnitude", vec3{0, 0, 0})
	if got := topNum(t, s); got != 0 {
		t.Fatalf("magnitude(0): got %v", got)
	}

	// normalize(0,3,0) = (0,1,0)
	callVecFn(t, s, "normalize", vec3{0, 3, 0})
	x, y, z := topVec(t, s)
	if !approxEqF32(x, 0, 1e-6) || !approxEqF32(y, 1, 1e-6) || !approxEqF32(z, 0, 1e-6) {
		t.Fatalf("normalize(0,3,0): got (%v,%v,%v)", x, y, z)
	}

	// normalize of (1,2,2) -> 1/3, 2/3, 2/3 (magnitude = 3)
	callVecFn(t, s, "normalize", vec3{1, 2, 2})
	x, y, z = topVec(t, s)
	if !approxEqF32(x, 1.0/3, 1e-6) || !approxEqF32(y, 2.0/3, 1e-6) || !approxEqF32(z, 2.0/3, 1e-6) {
		t.Fatalf("normalize(1,2,2): got (%v,%v,%v)", x, y, z)
	}

	// normalize of zero -> zero (luaugo contract).
	callVecFn(t, s, "normalize", vec3{0, 0, 0})
	x, y, z = topVec(t, s)
	if x != 0 || y != 0 || z != 0 {
		t.Fatalf("normalize(0,0,0): got (%v,%v,%v)", x, y, z)
	}
}

func TestVectorCrossDot(t *testing.T) {
	s := newVecState(t)

	// cross(x_hat, y_hat) = z_hat
	callVecFn(t, s, "cross", vec3{1, 0, 0}, vec3{0, 1, 0})
	x, y, z := topVec(t, s)
	if x != 0 || y != 0 || z != 1 {
		t.Fatalf("cross(x,y): got (%v,%v,%v)", x, y, z)
	}

	// cross(y_hat, x_hat) = -z_hat
	callVecFn(t, s, "cross", vec3{0, 1, 0}, vec3{1, 0, 0})
	x, y, z = topVec(t, s)
	if x != 0 || y != 0 || z != -1 {
		t.Fatalf("cross(y,x): got (%v,%v,%v)", x, y, z)
	}

	// cross of parallel vectors -> 0
	callVecFn(t, s, "cross", vec3{1, 2, 3}, vec3{2, 4, 6})
	x, y, z = topVec(t, s)
	if x != 0 || y != 0 || z != 0 {
		t.Fatalf("cross parallel: got (%v,%v,%v)", x, y, z)
	}

	// dot(1,2,3,4,-5,6) = 1*4 + 2*-5 + 3*6 = 4 - 10 + 18 = 12
	callVecFn(t, s, "dot", vec3{1, 2, 3}, vec3{4, -5, 6})
	if got := topNum(t, s); got != 12 {
		t.Fatalf("dot: got %v want 12", got)
	}

	// dot of orthogonal vectors -> 0
	callVecFn(t, s, "dot", vec3{1, 0, 0}, vec3{0, 1, 0})
	if got := topNum(t, s); got != 0 {
		t.Fatalf("dot orthogonal: got %v", got)
	}
}

func TestVectorAngle(t *testing.T) {
	s := newVecState(t)

	// angle(x_hat, y_hat) = pi/2
	callVecFn(t, s, "angle", vec3{1, 0, 0}, vec3{0, 1, 0})
	if got := topNum(t, s); !approxEqF64(got, math.Pi/2, 1e-6) {
		t.Fatalf("angle(x,y): got %v want pi/2", got)
	}

	// angle(x_hat, -x_hat) = pi
	callVecFn(t, s, "angle", vec3{1, 0, 0}, vec3{-1, 0, 0})
	if got := topNum(t, s); !approxEqF64(got, math.Pi, 1e-6) {
		t.Fatalf("angle(x,-x): got %v want pi", got)
	}

	// angle(x_hat, x_hat) = 0
	callVecFn(t, s, "angle", vec3{1, 0, 0}, vec3{1, 0, 0})
	if got := topNum(t, s); !approxEqF64(got, 0, 1e-6) {
		t.Fatalf("angle(x,x): got %v want 0", got)
	}

	// a=(1,0,0), b=(cos t, sin t, 0). Pick t=pi/3.
	// cross . (+z) > 0, axis=+z -> positive angle.
	a := vec3{1, 0, 0}
	b := vec3{float32(math.Cos(math.Pi / 3)), float32(math.Sin(math.Pi / 3)), 0}
	callVecFn(t, s, "angle", a, b, vec3{0, 0, 1})
	if got := topNum(t, s); !approxEqF64(got, math.Pi/3, 1e-5) {
		t.Fatalf("angle(...,+z): got %v want pi/3", got)
	}
	// axis=-z -> negate.
	callVecFn(t, s, "angle", a, b, vec3{0, 0, -1})
	if got := topNum(t, s); !approxEqF64(got, -math.Pi/3, 1e-5) {
		t.Fatalf("angle(...,-z): got %v want -pi/3", got)
	}
}

func TestVectorPerComponent(t *testing.T) {
	s := newVecState(t)

	callVecFn(t, s, "floor", vec3{1.7, -1.2, 0.0})
	x, y, z := topVec(t, s)
	if x != 1 || y != -2 || z != 0 {
		t.Fatalf("floor: got (%v,%v,%v)", x, y, z)
	}

	callVecFn(t, s, "ceil", vec3{1.2, -1.7, 0.0})
	x, y, z = topVec(t, s)
	if x != 2 || y != -1 || z != 0 {
		t.Fatalf("ceil: got (%v,%v,%v)", x, y, z)
	}

	callVecFn(t, s, "abs", vec3{-3, 4, -0.5})
	x, y, z = topVec(t, s)
	if x != 3 || y != 4 || z != 0.5 {
		t.Fatalf("abs: got (%v,%v,%v)", x, y, z)
	}

	callVecFn(t, s, "sign", vec3{-3.5, 0, 7})
	x, y, z = topVec(t, s)
	if x != -1 || y != 0 || z != 1 {
		t.Fatalf("sign: got (%v,%v,%v)", x, y, z)
	}
}

func TestVectorClamp(t *testing.T) {
	s := newVecState(t)

	// All components below min -> clamped to min.
	callVecFn(t, s, "clamp", vec3{-5, -5, -5}, vec3{0, 0, 0}, vec3{10, 10, 10})
	x, y, z := topVec(t, s)
	if x != 0 || y != 0 || z != 0 {
		t.Fatalf("clamp below: got (%v,%v,%v)", x, y, z)
	}

	// All components above max.
	callVecFn(t, s, "clamp", vec3{15, 15, 15}, vec3{0, 0, 0}, vec3{10, 10, 10})
	x, y, z = topVec(t, s)
	if x != 10 || y != 10 || z != 10 {
		t.Fatalf("clamp above: got (%v,%v,%v)", x, y, z)
	}

	// Mixed.
	callVecFn(t, s, "clamp", vec3{-2, 5, 11}, vec3{0, 0, 0}, vec3{10, 10, 10})
	x, y, z = topVec(t, s)
	if x != 0 || y != 5 || z != 10 {
		t.Fatalf("clamp mixed: got (%v,%v,%v)", x, y, z)
	}

	// min > max -> error.
	s.GetGlobal("vector")
	s.GetField(-1, "clamp")
	s.Remove(-2)
	s.PushVector(0, 0, 0, 0)
	s.PushVector(5, 0, 0, 0)
	s.PushVector(0, 0, 0, 0)
	if st := s.PCall(3, 1, 0); st == vm.StatusOK {
		t.Fatalf("clamp with min.x > max.x should have errored")
	}
	s.Pop(1)
}

func TestVectorMinMax(t *testing.T) {
	s := newVecState(t)

	// min variadic.
	callVecFn(t, s, "min", vec3{1, 5, 9}, vec3{3, 2, 8}, vec3{0, 7, 10})
	x, y, z := topVec(t, s)
	if x != 0 || y != 2 || z != 8 {
		t.Fatalf("min: got (%v,%v,%v)", x, y, z)
	}

	// max variadic.
	callVecFn(t, s, "max", vec3{1, 5, 9}, vec3{3, 2, 8}, vec3{0, 7, 10})
	x, y, z = topVec(t, s)
	if x != 3 || y != 7 || z != 10 {
		t.Fatalf("max: got (%v,%v,%v)", x, y, z)
	}

	// Single-arg min/max -> identity.
	callVecFn(t, s, "min", vec3{1, 2, 3})
	x, y, z = topVec(t, s)
	if x != 1 || y != 2 || z != 3 {
		t.Fatalf("min single: got (%v,%v,%v)", x, y, z)
	}
	callVecFn(t, s, "max", vec3{4, 5, 6})
	x, y, z = topVec(t, s)
	if x != 4 || y != 5 || z != 6 {
		t.Fatalf("max single: got (%v,%v,%v)", x, y, z)
	}
}

func TestVectorLerp(t *testing.T) {
	s := newVecState(t)

	// t=0 -> a
	callVecFn(t, s, "lerp", vec3{1, 2, 3}, vec3{10, 20, 30}, float64(0))
	x, y, z := topVec(t, s)
	if x != 1 || y != 2 || z != 3 {
		t.Fatalf("lerp t=0: got (%v,%v,%v)", x, y, z)
	}

	// t=1 -> b exactly (per Luau spec).
	callVecFn(t, s, "lerp", vec3{1, 2, 3}, vec3{10, 20, 30}, float64(1))
	x, y, z = topVec(t, s)
	if x != 10 || y != 20 || z != 30 {
		t.Fatalf("lerp t=1: got (%v,%v,%v)", x, y, z)
	}

	// t=0.5 -> midpoint
	callVecFn(t, s, "lerp", vec3{0, 0, 0}, vec3{10, 20, 30}, float64(0.5))
	x, y, z = topVec(t, s)
	if !approxEqF32(x, 5, 1e-6) || !approxEqF32(y, 10, 1e-6) || !approxEqF32(z, 15, 1e-6) {
		t.Fatalf("lerp t=0.5: got (%v,%v,%v)", x, y, z)
	}

	// Extrapolation: t=2 -> 2b - a
	callVecFn(t, s, "lerp", vec3{1, 1, 1}, vec3{3, 3, 3}, float64(2))
	x, y, z = topVec(t, s)
	if x != 5 || y != 5 || z != 5 {
		t.Fatalf("lerp t=2: got (%v,%v,%v)", x, y, z)
	}
}
