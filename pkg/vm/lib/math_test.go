// Copyright (c) luaugo contributors. Licensed under the MIT License.

package lib_test

import (
	"math"
	"strconv"
	"testing"

	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
	"github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

// runScript compiles src as a chunk and runs it on a fresh State with
// OpenBase + OpenMath opened. It returns the State after the call;
// callers read the chunk's return values off the stack and must Close.
//
// OpenBase may be a panic stub during the parallel Tier-4 swarm; the
// recover here silently continues because no math_test scenario
// requires base globals.
func runScript(t *testing.T, src string, nresults int) *vm.State {
	t.Helper()
	s := vm.NewState()
	t.Cleanup(s.Close)

	func() {
		defer func() { _ = recover() }()
		lib.OpenBase(s)
	}()
	lib.OpenMath(s)

	blob, err := compiler.CompileBinary("=math_test", []byte(src), compiler.Defaults())
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	if len(blob) == 0 || blob[0] == 0 {
		t.Fatalf("compile produced error blob: %q", blob)
	}
	if err := s.Load("=math_test", blob, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	s.Call(0, nresults)
	return s
}

// nearly reports whether got and want agree within tol absolute error.
// Used for trig results where last-bit differences are expected.
func nearly(got, want, tol float64) bool {
	if math.IsNaN(got) && math.IsNaN(want) {
		return true
	}
	d := got - want
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func TestMathBasics(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want float64
	}{
		{"abs_neg", "return math.abs(-3.5)", 3.5},
		{"abs_pos", "return math.abs(7)", 7},
		{"floor", "return math.floor(2.9)", 2},
		{"floor_neg", "return math.floor(-2.1)", -3},
		{"ceil", "return math.ceil(2.1)", 3},
		{"ceil_neg", "return math.ceil(-2.9)", -2}, // ceil(-2.9) = -2 (rounds toward +inf)
		{"sqrt", "return math.sqrt(16)", 4},
		{"sqrt_two", "return math.sqrt(2)", math.Sqrt2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := runScript(t, tc.expr, 1)
			got, ok := s.ToNumber(-1)
			if !ok {
				t.Fatalf("%s: result not a number", tc.expr)
			}
			if !nearly(got, tc.want, 1e-12) {
				t.Fatalf("%s: got %v want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestMathTrig(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want float64
	}{
		{"sin0", "return math.sin(0)", 0},
		{"sin_pi", "return math.sin(math.pi)", 0},
		{"cos0", "return math.cos(0)", 1},
		{"cos_pi", "return math.cos(math.pi)", -1},
		// atan2 quadrants -- y, x.
		{"atan2_q1", "return math.atan2(1, 1)", math.Pi / 4},
		{"atan2_q2", "return math.atan2(1, -1)", 3 * math.Pi / 4},
		{"atan2_q3", "return math.atan2(-1, -1)", -3 * math.Pi / 4},
		{"atan2_q4", "return math.atan2(-1, 1)", -math.Pi / 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := runScript(t, tc.expr, 1)
			got, ok := s.ToNumber(-1)
			if !ok {
				t.Fatalf("%s: result not a number", tc.expr)
			}
			if !nearly(got, tc.want, 1e-9) {
				t.Fatalf("%s: got %v want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestMathConstants(t *testing.T) {
	s := runScript(t, "return math.pi, math.huge, math.tau, math.e", 4)
	if got, _ := s.ToNumber(1); !nearly(got, math.Pi, 1e-9) {
		t.Fatalf("math.pi: got %v want ~pi", got)
	}
	huge, _ := s.ToNumber(2)
	if !math.IsInf(huge, 1) {
		t.Fatalf("math.huge: got %v want +Inf", huge)
	}
	if got, _ := s.ToNumber(3); !nearly(got, 2*math.Pi, 1e-9) {
		t.Fatalf("math.tau: got %v want ~2*pi", got)
	}
	if got, _ := s.ToNumber(4); !nearly(got, math.E, 1e-9) {
		t.Fatalf("math.e: got %v want ~e", got)
	}
}

func TestMathRandom(t *testing.T) {
	t.Run("zero_args_in_unit_interval", func(t *testing.T) {
		s := runScript(t, "return math.random()", 1)
		v, ok := s.ToNumber(-1)
		if !ok {
			t.Fatal("not a number")
		}
		if v < 0 || v >= 1 {
			t.Fatalf("expected [0,1), got %v", v)
		}
	})

	t.Run("one_arg_inclusive_range", func(t *testing.T) {
		// Sample a bunch to catch boundary bugs without making the
		// test flaky -- we ONLY check that every sample is in [1,10].
		for i := 0; i < 100; i++ {
			s := runScript(t, "return math.random(10)", 1)
			v, ok := s.ToNumber(-1)
			if !ok {
				t.Fatal("not a number")
			}
			if v != math.Floor(v) {
				t.Fatalf("expected integer, got %v", v)
			}
			if v < 1 || v > 10 {
				t.Fatalf("expected [1,10], got %v", v)
			}
		}
	})

	t.Run("two_arg_inclusive_range", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			s := runScript(t, "return math.random(5, 7)", 1)
			v, _ := s.ToNumber(-1)
			if v < 5 || v > 7 {
				t.Fatalf("expected [5,7], got %v", v)
			}
		}
	})

	t.Run("randomseed_determinism", func(t *testing.T) {
		// Same seed -> same first three samples. We seed once and
		// then sample one value per script invocation, because the
		// VM's OpCall fast path currently truncates the operand
		// stack after a 0-return Go call (see contract bugs), making
		// `randomseed(...); return random(), random()` blow up
		// mid-chunk. Driving each call from a fresh chunk avoids the
		// VM bug while still proving randomseed determinism.
		sample := func(seed int) [3]float64 {
			// First chunk seeds; the next three each pull one number.
			runScript(t, "math.randomseed("+strconv.Itoa(seed)+")", 0)
			s1 := runScript(t, "return math.random()", 1)
			v1, _ := s1.ToNumber(-1)
			s2 := runScript(t, "return math.random(1000)", 1)
			v2, _ := s2.ToNumber(-1)
			s3 := runScript(t, "return math.random(-50, 50)", 1)
			v3, _ := s3.ToNumber(-1)
			return [3]float64{v1, v2, v3}
		}
		a := sample(12345)
		b := sample(12345)
		if a != b {
			t.Fatalf("randomseed not deterministic: %v vs %v", a, b)
		}
		// And a different seed produces (with overwhelming probability) a different stream.
		c := sample(67890)
		if a == c {
			t.Fatalf("different seeds produced same stream: %v", a)
		}
	})
}

func TestMathLerp(t *testing.T) {
	s := runScript(t, "return math.lerp(0, 10, 0.25)", 1)
	got, _ := s.ToNumber(-1)
	if got != 2.5 {
		t.Fatalf("lerp(0,10,0.25): got %v want 2.5", got)
	}

	// t == 1 must return b *exactly*, not via the a + (b-a)*t formula
	// which can drift.
	s = runScript(t, "return math.lerp(0, 10, 1)", 1)
	got, _ = s.ToNumber(-1)
	if got != 10 {
		t.Fatalf("lerp(0,10,1): got %v want exactly 10", got)
	}

	// Sanity at t=0.
	s = runScript(t, "return math.lerp(3, 7, 0)", 1)
	got, _ = s.ToNumber(-1)
	if got != 3 {
		t.Fatalf("lerp(3,7,0): got %v want 3", got)
	}
}

func TestMathClamp(t *testing.T) {
	s := runScript(t, "return math.clamp(5, 1, 3)", 1)
	got, _ := s.ToNumber(-1)
	if got != 3 {
		t.Fatalf("clamp(5,1,3): got %v want 3", got)
	}

	s = runScript(t, "return math.clamp(-1, 0, 10)", 1)
	got, _ = s.ToNumber(-1)
	if got != 0 {
		t.Fatalf("clamp(-1,0,10): got %v want 0", got)
	}

	s = runScript(t, "return math.clamp(5, 0, 10)", 1)
	got, _ = s.ToNumber(-1)
	if got != 5 {
		t.Fatalf("clamp(5,0,10): got %v want 5", got)
	}
}

func TestMathModf(t *testing.T) {
	s := runScript(t, "return math.modf(3.75)", 2)
	ip, _ := s.ToNumber(1)
	fp, _ := s.ToNumber(2)
	if ip != 3 || !nearly(fp, 0.75, 1e-12) {
		t.Fatalf("modf(3.75): got (%v,%v) want (3, 0.75)", ip, fp)
	}

	// Negative: both parts must carry the sign.
	s = runScript(t, "return math.modf(-3.75)", 2)
	ip, _ = s.ToNumber(1)
	fp, _ = s.ToNumber(2)
	if ip != -3 || !nearly(fp, -0.75, 1e-12) {
		t.Fatalf("modf(-3.75): got (%v,%v) want (-3, -0.75)", ip, fp)
	}
}

func TestMathFrexpLdexp(t *testing.T) {
	cases := []float64{1.0, 2.5, -3.5, 0.125, 12345.6789, -1e-9, 1e9}
	for _, x := range cases {
		s := runScript(t, "return math.frexp("+ftoa(x)+")", 2)
		mant, _ := s.ToNumber(1)
		exp, _ := s.ToNumber(2)
		if x != 0 {
			absM := math.Abs(mant)
			if absM < 0.5 || absM >= 1.0 {
				t.Fatalf("frexp(%v): mantissa %v out of [0.5,1)", x, mant)
			}
		}
		// Round-trip via ldexp.
		s = runScript(t, "return math.ldexp("+ftoa(mant)+", "+itoa(int(exp))+")", 1)
		got, _ := s.ToNumber(-1)
		if !nearly(got, x, math.Abs(x)*1e-14+1e-14) {
			t.Fatalf("ldexp(frexp(%v)) round-trip: got %v", x, got)
		}
	}
}

// Helpers: produce a Luau-source-friendly literal for a Go float / int
// without depending on fmt's formatting choices.
func ftoa(x float64) string {
	// strconv with 'g' precision -1 gives the shortest round-trippable
	// form, which is exactly what Luau's lexer accepts.
	return strconv.FormatFloat(x, 'g', -1, 64)
}
func itoa(x int) string { return strconv.Itoa(x) }
