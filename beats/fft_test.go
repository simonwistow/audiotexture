package beats

import (
	"math"
	"math/cmplx"
	"testing"
)

// naiveDFT is the O(n^2) definition, used to check the fast transform.
func naiveDFT(re, im []float64) ([]float64, []float64) {
	n := len(re)
	outRe := make([]float64, n)
	outIm := make([]float64, n)
	for k := range n {
		var sum complex128
		for t := range n {
			angle := -2 * math.Pi * float64(k) * float64(t) / float64(n)
			sum += complex(re[t], im[t]) * cmplx.Exp(complex(0, angle))
		}
		outRe[k], outIm[k] = real(sum), imag(sum)
	}
	return outRe, outIm
}

func TestFFTMatchesDFT(t *testing.T) {
	for _, n := range []int{2, 4, 8, 16, 64, 256} {
		re := make([]float64, n)
		im := make([]float64, n)
		// A deterministic but not-symmetric signal.
		for i := range n {
			re[i] = math.Sin(float64(i)*0.7) + 0.3*float64(i%5)
			im[i] = math.Cos(float64(i) * 0.31)
		}
		wantRe, wantIm := naiveDFT(re, im)

		newFFTPlan(n).transform(re, im)

		for k := range n {
			if math.Abs(re[k]-wantRe[k]) > 1e-9 || math.Abs(im[k]-wantIm[k]) > 1e-9 {
				t.Fatalf("n=%d bin %d: got (%v,%v), want (%v,%v)", n, k, re[k], im[k], wantRe[k], wantIm[k])
			}
		}
	}
}

func TestFFTPureTone(t *testing.T) {
	// A tone at exactly bin 8 should put all its energy in bins 8 and n-8.
	const n = 128
	re := make([]float64, n)
	im := make([]float64, n)
	for i := range n {
		re[i] = math.Cos(2 * math.Pi * 8 * float64(i) / float64(n))
	}
	newFFTPlan(n).transform(re, im)

	for k := range n / 2 {
		mag := math.Hypot(re[k], im[k])
		if k == 8 {
			if math.Abs(mag-float64(n)/2) > 1e-6 {
				t.Errorf("bin 8 magnitude = %v, want %v", mag, float64(n)/2)
			}
		} else if mag > 1e-6 {
			t.Errorf("bin %d magnitude = %v, want ~0", k, mag)
		}
	}
}

func TestNextPow2(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{1, 1}, {2, 2}, {3, 4}, {1000, 1024}, {1024, 1024}, {1025, 2048}} {
		if got := nextPow2(tc.in); got != tc.want {
			t.Errorf("nextPow2(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
