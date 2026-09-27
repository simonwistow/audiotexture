package beats

import "math"

// fftPlan holds the twiddle factors and bit-reversal permutation for one
// transform size, so a long STFT does not recompute them per frame.
type fftPlan struct {
	n       int
	cosTab  []float64
	sinTab  []float64
	reverse []int
}

// newFFTPlan builds a plan for size n, which must be a power of two.
func newFFTPlan(n int) *fftPlan {
	p := &fftPlan{
		n:       n,
		cosTab:  make([]float64, n/2),
		sinTab:  make([]float64, n/2),
		reverse: make([]int, n),
	}
	for i := range p.cosTab {
		angle := -2 * math.Pi * float64(i) / float64(n)
		p.cosTab[i] = math.Cos(angle)
		p.sinTab[i] = math.Sin(angle)
	}

	bits := 0
	for 1<<bits < n {
		bits++
	}
	for i := range p.reverse {
		r := 0
		for b := 0; b < bits; b++ {
			r |= ((i >> b) & 1) << (bits - 1 - b)
		}
		p.reverse[i] = r
	}
	return p
}

// transform runs an in-place radix-2 Cooley-Tukey FFT on re/im, which must
// both have length p.n.
func (p *fftPlan) transform(re, im []float64) {
	n := p.n

	for i, r := range p.reverse {
		if i < r {
			re[i], re[r] = re[r], re[i]
			im[i], im[r] = im[r], im[i]
		}
	}

	for size := 2; size <= n; size <<= 1 {
		half := size / 2
		step := n / size
		for i := 0; i < n; i += size {
			for j, k := i, 0; j < i+half; j, k = j+1, k+step {
				c, s := p.cosTab[k], p.sinTab[k]
				tre := re[j+half]*c - im[j+half]*s
				tim := re[j+half]*s + im[j+half]*c
				re[j+half] = re[j] - tre
				im[j+half] = im[j] - tim
				re[j] += tre
				im[j] += tim
			}
		}
	}
}

// nextPow2 returns the smallest power of two >= n.
func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// hann returns a periodic Hann window of length n, matching what STFT
// implementations conventionally use.
func hann(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n))
	}
	return w
}
