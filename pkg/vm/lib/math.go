// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/luaugo/luaugo/pkg/vm"
)

// math.go: Luau standard math library. Ports upstream lmathlib.cpp
// (see .upstream/VM/src/lmathlib.cpp) function for function. All
// functions take their arguments from the Lua stack and return results
// via the stack, following the lmathlib convention of using
// LCheckNumber for required numeric arguments.
//
// The random number generator is a package-level *rand.Rand guarded by
// a mutex so concurrent States share a deterministic stream after
// randomseed. Upstream uses a per-state PCG32; we don't have access to
// per-state RNG fields from this package, so we follow the simpler
// "single seeded source" model. This is the same observable
// determinism guarantee the brief requires.

const (
	luauPI           = math.Pi
	radiansPerDegree = luauPI / 180.0
	luauE            = 2.71828182845904523536
	luauPhi          = 1.61803398874989484820
	luauSqrt2        = 1.41421356237309504880
	luauTau          = 6.28318530717958647692
)

// Package-level RNG state. rngMu serialises access to rng so it is safe
// to call math.random from multiple goroutines (e.g. concurrent VMs).
var (
	rngMu sync.Mutex
	rng   = rand.New(rand.NewSource(defaultSeed()))
)

// defaultSeed returns a non-deterministic seed used at package init.
// Tests that depend on determinism must call math.randomseed first.
func defaultSeed() int64 {
	return time.Now().UnixNano()
}

func openMath(s *vm.State) {
	s.CreateTable(0, 40)

	s.LRegisterList([]vm.LFnEntry{
		{Name: "abs", Fn: mathAbs},
		{Name: "acos", Fn: mathAcos},
		{Name: "asin", Fn: mathAsin},
		{Name: "atan2", Fn: mathAtan2},
		{Name: "atan", Fn: mathAtan},
		{Name: "ceil", Fn: mathCeil},
		{Name: "cosh", Fn: mathCosh},
		{Name: "cos", Fn: mathCos},
		{Name: "deg", Fn: mathDeg},
		{Name: "exp", Fn: mathExp},
		{Name: "floor", Fn: mathFloor},
		{Name: "fmod", Fn: mathFmod},
		{Name: "frexp", Fn: mathFrexp},
		{Name: "ldexp", Fn: mathLdexp},
		{Name: "log10", Fn: mathLog10},
		{Name: "log", Fn: mathLog},
		{Name: "max", Fn: mathMax},
		{Name: "min", Fn: mathMin},
		{Name: "modf", Fn: mathModf},
		{Name: "pow", Fn: mathPow},
		{Name: "rad", Fn: mathRad},
		{Name: "random", Fn: mathRandom},
		{Name: "randomseed", Fn: mathRandomseed},
		{Name: "sinh", Fn: mathSinh},
		{Name: "sin", Fn: mathSin},
		{Name: "sqrt", Fn: mathSqrt},
		{Name: "tanh", Fn: mathTanh},
		{Name: "tan", Fn: mathTan},
		{Name: "noise", Fn: mathNoise},
		{Name: "clamp", Fn: mathClamp},
		{Name: "sign", Fn: mathSign},
		{Name: "round", Fn: mathRound},
		{Name: "map", Fn: mathMap},
		{Name: "lerp", Fn: mathLerp},
		{Name: "isnan", Fn: mathIsnan},
		{Name: "isinf", Fn: mathIsinf},
		{Name: "isfinite", Fn: mathIsfinite},
	})

	// Numeric constants. Mirror upstream's luaopen_math.
	s.PushNumber(luauPI)
	s.SetField(-2, "pi")
	s.PushNumber(math.Inf(1))
	s.SetField(-2, "huge")
	s.PushNumber(math.NaN())
	s.SetField(-2, "nan")
	s.PushNumber(luauE)
	s.SetField(-2, "e")
	s.PushNumber(luauPhi)
	s.SetField(-2, "phi")
	s.PushNumber(luauSqrt2)
	s.SetField(-2, "sqrt2")
	s.PushNumber(luauTau)
	s.SetField(-2, "tau")

	// Install as the `math` global. The table remains on the stack for
	// the caller to consume; pop it after assigning.
	s.PushValue(-1)
	s.SetGlobal("math")
	s.Pop(1)
}

// ----------------------------------------------------------------------
// Trig / pow / log family
// ----------------------------------------------------------------------

func mathAbs(s *vm.State) int  { s.PushNumber(math.Abs(s.LCheckNumber(1))); return 1 }
func mathSin(s *vm.State) int  { s.PushNumber(math.Sin(s.LCheckNumber(1))); return 1 }
func mathSinh(s *vm.State) int { s.PushNumber(math.Sinh(s.LCheckNumber(1))); return 1 }
func mathCos(s *vm.State) int  { s.PushNumber(math.Cos(s.LCheckNumber(1))); return 1 }
func mathCosh(s *vm.State) int { s.PushNumber(math.Cosh(s.LCheckNumber(1))); return 1 }
func mathTan(s *vm.State) int  { s.PushNumber(math.Tan(s.LCheckNumber(1))); return 1 }
func mathTanh(s *vm.State) int { s.PushNumber(math.Tanh(s.LCheckNumber(1))); return 1 }
func mathAsin(s *vm.State) int { s.PushNumber(math.Asin(s.LCheckNumber(1))); return 1 }
func mathAcos(s *vm.State) int { s.PushNumber(math.Acos(s.LCheckNumber(1))); return 1 }
func mathAtan(s *vm.State) int { s.PushNumber(math.Atan(s.LCheckNumber(1))); return 1 }

func mathAtan2(s *vm.State) int {
	s.PushNumber(math.Atan2(s.LCheckNumber(1), s.LCheckNumber(2)))
	return 1
}

func mathCeil(s *vm.State) int  { s.PushNumber(math.Ceil(s.LCheckNumber(1))); return 1 }
func mathFloor(s *vm.State) int { s.PushNumber(math.Floor(s.LCheckNumber(1))); return 1 }

func mathFmod(s *vm.State) int {
	s.PushNumber(math.Mod(s.LCheckNumber(1), s.LCheckNumber(2)))
	return 1
}

// math.modf returns (int_part, frac_part) both carrying the sign of x.
// Go's math.Modf already has these semantics.
func mathModf(s *vm.State) int {
	ip, fp := math.Modf(s.LCheckNumber(1))
	s.PushNumber(ip)
	s.PushNumber(fp)
	return 2
}

func mathSqrt(s *vm.State) int { s.PushNumber(math.Sqrt(s.LCheckNumber(1))); return 1 }
func mathPow(s *vm.State) int {
	s.PushNumber(math.Pow(s.LCheckNumber(1), s.LCheckNumber(2)))
	return 1
}

// math.log(x[, base]): natural log if no base; log_base(x) otherwise.
// Upstream short-circuits base==2 and base==10 to dedicated routines
// for accuracy; we do the same.
func mathLog(s *vm.State) int {
	x := s.LCheckNumber(1)
	var res float64
	if s.IsNoneOrNil(2) {
		res = math.Log(x)
	} else {
		base := s.LCheckNumber(2)
		switch base {
		case 2.0:
			res = math.Log2(x)
		case 10.0:
			res = math.Log10(x)
		default:
			res = math.Log(x) / math.Log(base)
		}
	}
	s.PushNumber(res)
	return 1
}

func mathLog10(s *vm.State) int { s.PushNumber(math.Log10(s.LCheckNumber(1))); return 1 }
func mathExp(s *vm.State) int   { s.PushNumber(math.Exp(s.LCheckNumber(1))); return 1 }

func mathDeg(s *vm.State) int {
	s.PushNumber(s.LCheckNumber(1) / radiansPerDegree)
	return 1
}
func mathRad(s *vm.State) int {
	s.PushNumber(s.LCheckNumber(1) * radiansPerDegree)
	return 1
}

// math.frexp / math.ldexp: standard C-style mantissa/exponent.
func mathFrexp(s *vm.State) int {
	mant, exp := math.Frexp(s.LCheckNumber(1))
	s.PushNumber(mant)
	s.PushInteger(int64(exp))
	return 2
}
func mathLdexp(s *vm.State) int {
	m := s.LCheckNumber(1)
	e := s.LCheckInteger(2)
	s.PushNumber(math.Ldexp(m, int(e)))
	return 1
}

// math.min / math.max: variadic. Following lmathlib, NaNs propagate
// naturally because comparisons against NaN are always false (so the
// "current min/max" stays put unless a strictly-better value appears).
func mathMin(s *vm.State) int {
	n := s.Top()
	dmin := s.LCheckNumber(1)
	for i := 2; i <= n; i++ {
		d := s.LCheckNumber(i)
		if d < dmin {
			dmin = d
		}
	}
	s.PushNumber(dmin)
	return 1
}
func mathMax(s *vm.State) int {
	n := s.Top()
	dmax := s.LCheckNumber(1)
	for i := 2; i <= n; i++ {
		d := s.LCheckNumber(i)
		if d > dmax {
			dmax = d
		}
	}
	s.PushNumber(dmax)
	return 1
}

// ----------------------------------------------------------------------
// Random
// ----------------------------------------------------------------------

// math.random: 0 args -> float in [0,1); 1 arg m -> integer in [1,m];
// 2 args (l, u) -> integer in [l, u]. All bounds inclusive. Matches
// upstream lmathlib.cpp's math_random.
func mathRandom(s *vm.State) int {
	n := s.Top()
	switch n {
	case 0:
		rngMu.Lock()
		v := rng.Float64() // [0,1)
		rngMu.Unlock()
		s.PushNumber(v)
		return 1
	case 1:
		u := s.LCheckInteger(1)
		if u < 1 {
			s.LArgError(1, "interval is empty")
		}
		rngMu.Lock()
		// rand.Int63n returns [0, u); +1 -> [1, u].
		r := rng.Int63n(u) + 1
		rngMu.Unlock()
		s.PushInteger(r)
		return 1
	case 2:
		l := s.LCheckInteger(1)
		u := s.LCheckInteger(2)
		if l > u {
			s.LArgError(2, "interval is empty")
		}
		width := u - l + 1
		if width <= 0 {
			s.LArgError(2, "interval is too large")
		}
		rngMu.Lock()
		r := rng.Int63n(width) + l
		rngMu.Unlock()
		s.PushInteger(r)
		return 1
	default:
		s.LError("wrong number of arguments")
		return 0
	}
}

// math.randomseed(seed): reseed the package-level RNG so subsequent
// math.random calls produce a deterministic stream. The brief allows
// rand or rand/v2 as the backing implementation; we use math/rand with
// a fresh Source so the stream is bit-identical for a given seed
// across runs on the same Go toolchain version.
func mathRandomseed(s *vm.State) int {
	seed := s.LCheckInteger(1)
	rngMu.Lock()
	rng = rand.New(rand.NewSource(seed))
	rngMu.Unlock()
	return 0
}

// ----------------------------------------------------------------------
// Perlin noise (3D), ported from upstream lmathlib.cpp.
// ----------------------------------------------------------------------

// kPerlinHash is the standard Perlin permutation table (length 257 to
// avoid wraparound on the +1 indexing pattern). Copied byte-for-byte
// from upstream lmathlib.cpp.
var kPerlinHash = [257]byte{
	151, 160, 137, 91, 90, 15, 131, 13, 201, 95, 96, 53, 194, 233, 7, 225, 140, 36, 103, 30, 69, 142, 8, 99, 37, 240, 21, 10, 23,
	190, 6, 148, 247, 120, 234, 75, 0, 26, 197, 62, 94, 252, 219, 203, 117, 35, 11, 32, 57, 177, 33, 88, 237, 149, 56, 87, 174, 20,
	125, 136, 171, 168, 68, 175, 74, 165, 71, 134, 139, 48, 27, 166, 77, 146, 158, 231, 83, 111, 229, 122, 60, 211, 133, 230, 220, 105, 92,
	41, 55, 46, 245, 40, 244, 102, 143, 54, 65, 25, 63, 161, 1, 216, 80, 73, 209, 76, 132, 187, 208, 89, 18, 169, 200, 196, 135, 130,
	116, 188, 159, 86, 164, 100, 109, 198, 173, 186, 3, 64, 52, 217, 226, 250, 124, 123, 5, 202, 38, 147, 118, 126, 255, 82, 85, 212, 207,
	206, 59, 227, 47, 16, 58, 17, 182, 189, 28, 42, 223, 183, 170, 213, 119, 248, 152, 2, 44, 154, 163, 70, 221, 153, 101, 155, 167, 43,
	172, 9, 129, 22, 39, 253, 19, 98, 108, 110, 79, 113, 224, 232, 178, 185, 112, 104, 218, 246, 97, 228, 251, 34, 242, 193, 238, 210, 144,
	12, 191, 179, 162, 241, 81, 51, 145, 235, 249, 14, 239, 107, 49, 192, 214, 31, 181, 199, 106, 157, 184, 84, 204, 176, 115, 121, 50, 45,
	127, 4, 150, 254, 138, 236, 205, 93, 222, 114, 67, 29, 24, 72, 243, 141, 128, 195, 78, 66, 215, 61, 156, 180, 151,
}

// kPerlinGrad is the 16-entry gradient table used by 3D Perlin noise.
// Copied verbatim from upstream's `kPerlinGrad`.
var kPerlinGrad = [16][3]float32{
	{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0},
	{1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1},
	{1, 1, 0}, {0, -1, 1}, {-1, 1, 0}, {0, -1, -1},
}

func perlinFade(t float32) float32 { return t * t * t * (t*(t*6-15) + 10) }

func perlinLerp(t, a, b float32) float32 { return a + t*(b-a) }

func perlinGrad(hash int, x, y, z float32) float32 {
	g := kPerlinGrad[hash&15]
	return g[0]*x + g[1]*y + g[2]*z
}

func perlin(x, y, z float32) float32 {
	// floor-then-cast guards against the negative-input rounding bug
	// that a naive int(x) cast would cause.
	xflr := float32(math.Floor(float64(x)))
	yflr := float32(math.Floor(float64(y)))
	zflr := float32(math.Floor(float64(z)))

	xi := int(xflr) & 255
	yi := int(yflr) & 255
	zi := int(zflr) & 255

	xf := x - xflr
	yf := y - yflr
	zf := z - zflr

	u := perlinFade(xf)
	v := perlinFade(yf)
	w := perlinFade(zf)

	p := kPerlinHash[:]

	a := (int(p[xi]) + yi) & 255
	aa := (int(p[a]) + zi) & 255
	ab := (int(p[a+1]) + zi) & 255

	b := (int(p[xi+1]) + yi) & 255
	ba := (int(p[b]) + zi) & 255
	bb := (int(p[b+1]) + zi) & 255

	la := perlinLerp(u, perlinGrad(int(p[aa]), xf, yf, zf), perlinGrad(int(p[ba]), xf-1, yf, zf))
	lb := perlinLerp(u, perlinGrad(int(p[ab]), xf, yf-1, zf), perlinGrad(int(p[bb]), xf-1, yf-1, zf))
	la1 := perlinLerp(u, perlinGrad(int(p[aa+1]), xf, yf, zf-1), perlinGrad(int(p[ba+1]), xf-1, yf, zf-1))
	lb1 := perlinLerp(u, perlinGrad(int(p[ab+1]), xf, yf-1, zf-1), perlinGrad(int(p[bb+1]), xf-1, yf-1, zf-1))

	return perlinLerp(w, perlinLerp(v, la, lb), perlinLerp(v, la1, lb1))
}

// math.noise(x[, y[, z]]). Missing dimensions default to 0. Matches
// upstream's FFlag::FixMathNoisePrecision wrap-around behavior so large
// inputs don't collapse to 0 due to float32 mantissa exhaustion.
func mathNoise(s *vm.State) int {
	x := s.LCheckNumber(1)
	y := 0.0
	z := 0.0
	if !s.IsNoneOrNil(2) {
		y = s.LCheckNumber(2)
	}
	if !s.IsNoneOrNil(3) {
		z = s.LCheckNumber(3)
	}
	// Wrap into the noise period (256) before downcasting to float32.
	x = math.Mod(x, 256.0)
	y = math.Mod(y, 256.0)
	z = math.Mod(z, 256.0)

	r := perlin(float32(x), float32(y), float32(z))
	s.PushNumber(float64(r))
	return 1
}

// ----------------------------------------------------------------------
// Luau extras: clamp, sign, round, map, lerp, isnan/isinf/isfinite
// ----------------------------------------------------------------------

func mathClamp(s *vm.State) int {
	v := s.LCheckNumber(1)
	mn := s.LCheckNumber(2)
	mx := s.LCheckNumber(3)
	if mn > mx {
		s.LArgError(3, "max must be greater than or equal to min")
	}
	r := v
	if r < mn {
		r = mn
	}
	if r > mx {
		r = mx
	}
	s.PushNumber(r)
	return 1
}

func mathSign(s *vm.State) int {
	v := s.LCheckNumber(1)
	switch {
	case v > 0:
		s.PushNumber(1)
	case v < 0:
		s.PushNumber(-1)
	default:
		// includes NaN (v > 0 and v < 0 are both false) -> 0, matching
		// upstream's ternary.
		s.PushNumber(0)
	}
	return 1
}

// math.round uses banker-free round-half-away-from-zero, matching the C
// `round()` semantics used by upstream.
func mathRound(s *vm.State) int {
	s.PushNumber(math.Round(s.LCheckNumber(1)))
	return 1
}

func mathMap(s *vm.State) int {
	x := s.LCheckNumber(1)
	inMin := s.LCheckNumber(2)
	inMax := s.LCheckNumber(3)
	outMin := s.LCheckNumber(4)
	outMax := s.LCheckNumber(5)
	r := outMin + (x-inMin)*(outMax-outMin)/(inMax-inMin)
	s.PushNumber(r)
	return 1
}

// math.lerp uses the t==1 short-circuit so callers can rely on the
// result being exactly b at t=1 (no FP drift from a + (b-a)*t).
func mathLerp(s *vm.State) int {
	a := s.LCheckNumber(1)
	b := s.LCheckNumber(2)
	t := s.LCheckNumber(3)
	var r float64
	if t == 1.0 {
		r = b
	} else {
		r = a + (b-a)*t
	}
	s.PushNumber(r)
	return 1
}

func mathIsnan(s *vm.State) int {
	s.PushBoolean(math.IsNaN(s.LCheckNumber(1)))
	return 1
}
func mathIsinf(s *vm.State) int {
	s.PushBoolean(math.IsInf(s.LCheckNumber(1), 0))
	return 1
}
func mathIsfinite(s *vm.State) int {
	x := s.LCheckNumber(1)
	s.PushBoolean(!math.IsNaN(x) && !math.IsInf(x, 0))
	return 1
}
