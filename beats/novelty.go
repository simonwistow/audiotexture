package beats

import "math"

// noveltyHopSeconds is the resolution of the novelty curve. Structural
// boundaries are a coarse phenomenon -- a chorus arriving is not a 23 ms event
// -- and an O(T^2) similarity matrix at the STFT hop rate would be 60 million
// comparisons for a three-minute track. A quarter of a second is ample and
// makes the matrix cheap.
const noveltyHopSeconds = 0.25

// noveltyKernelSeconds is the half-width of the checkerboard kernel: how much
// context either side of an instant is compared. Two seconds picks up phrase
// and section changes rather than individual notes.
const noveltyKernelSeconds = 2.0

// novelty computes an audio novelty curve from a dB mel spectrogram, after
// Foote, Cooper and Girgensohn, "Creating Music Videos using Automatic Media
// Analysis" (ACM MM 2002).
//
// The idea: build a self-similarity matrix of the audio against itself, then
// slide a checkerboard kernel down its diagonal. The kernel rewards instants
// where the recent past is self-similar, the near future is self-similar, and
// the two are unlike each other -- which is exactly what a section boundary
// looks like. The result peaks where the music changes character, not merely
// where something is loud.
//
// This is a different question from onset strength. A snare hit is a strong
// onset but no kind of boundary; the bar where the drums first enter may be no
// louder than the one before it but is a large novelty peak. Cutting on
// novelty is what makes a slideshow feel edited to the music rather than
// merely synchronised to it.
func novelty(spec [][]float64, hopSeconds float64) []float64 {
	if len(spec) == 0 || hopSeconds <= 0 {
		return nil
	}

	features := aggregate(spec, max(int(math.Round(noveltyHopSeconds/hopSeconds)), 1))
	if len(features) < 4 {
		return nil
	}
	for _, f := range features {
		normalize(f)
	}

	half := max(int(math.Round(noveltyKernelSeconds/noveltyHopSeconds)), 2)
	if half > len(features)/2 {
		half = len(features) / 2
	}
	if half < 2 {
		return nil
	}
	kernel := checkerboard(half)

	out := make([]float64, len(features))
	size := 2 * half
	for c := range features {
		var sum float64
		for i := range size {
			// Clamp at the edges so the curve is defined everywhere.
			fi := clamp(c-half+i, 0, len(features)-1)
			for j := range size {
				fj := clamp(c-half+j, 0, len(features)-1)
				sum += kernel[i][j] * dot(features[fi], features[fj])
			}
		}
		out[c] = sum
	}

	// Half-wave rectify and scale to [0, 1]: only increases in novelty are
	// boundaries, and the absolute magnitude is meaningless.
	var peak float64
	for i, v := range out {
		if v < 0 {
			out[i] = 0
		}
		peak = math.Max(peak, out[i])
	}
	if peak > 0 {
		for i := range out {
			out[i] /= peak
		}
	}
	return out
}

// aggregate averages groups of n consecutive frames.
func aggregate(spec [][]float64, n int) [][]float64 {
	if n <= 1 {
		out := make([][]float64, len(spec))
		for i, f := range spec {
			out[i] = append([]float64(nil), f...)
		}
		return out
	}
	bands := len(spec[0])
	out := make([][]float64, 0, (len(spec)+n-1)/n)
	for start := 0; start < len(spec); start += n {
		end := min(start+n, len(spec))
		mean := make([]float64, bands)
		for t := start; t < end; t++ {
			for b := range bands {
				mean[b] += spec[t][b]
			}
		}
		for b := range bands {
			mean[b] /= float64(end - start)
		}
		out = append(out, mean)
	}
	return out
}

// checkerboard returns a 2*half square Gaussian-tapered checkerboard kernel:
// positive on the two diagonal quadrants, negative on the two off-diagonal
// ones, fading out towards the corners.
func checkerboard(half int) [][]float64 {
	size := 2 * half
	sigma := float64(half) / 2
	k := make([][]float64, size)
	for i := range size {
		k[i] = make([]float64, size)
		for j := range size {
			di := float64(i-half) + 0.5
			dj := float64(j-half) + 0.5
			taper := math.Exp(-0.5 * (di*di + dj*dj) / (sigma * sigma))
			sign := 1.0
			if (di < 0) != (dj < 0) {
				sign = -1
			}
			k[i][j] = sign * taper
		}
	}
	return k
}

// normalize scales v to unit length so that dot is a cosine similarity.
// The mean is removed first: these are dB values with a large common offset,
// and without centring every frame looks similar to every other.
func normalize(v []float64) {
	var mean float64
	for _, x := range v {
		mean += x
	}
	mean /= float64(len(v))

	var norm float64
	for i := range v {
		v[i] -= mean
		norm += v[i] * v[i]
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return
	}
	for i := range v {
		v[i] /= norm
	}
}

func dot(a, b []float64) float64 {
	var sum float64
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}
