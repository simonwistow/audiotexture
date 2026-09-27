package beats

import "math"

// onsetStrength computes a perceptually weighted spectral flux envelope, the
// input to both tempo estimation and beat tracking.
//
// This follows Ellis (2007) section 2, and matches what librosa's
// onset_strength does: take a mel-scaled magnitude spectrogram, convert it to
// dB, take the first-order difference along time, half-wave rectify it, and
// average across mel bands. Rectifying is what makes it an *onset* detector
// rather than a change detector -- energy appearing counts, energy decaying
// does not.
//
// The returned envelope has one value per STFT hop.
func onsetStrength(samples []float64, sampleRate int, o *Options) ([]float64, [][]float64) {
	spec := melSpectrogram(samples, sampleRate, o)
	if len(spec) < 2 {
		return nil, nil
	}
	toDB(spec)
	return fluxFrom(spec), spec
}

// toDB converts a power spectrogram to decibels in place, with the usual 80 dB
// floor below the loudest bin so that near-silence does not produce enormous
// negative swings.
func toDB(spec [][]float64) {
	var peak float64
	for _, frame := range spec {
		for _, v := range frame {
			peak = math.Max(peak, v)
		}
	}
	const minDB = -80.0
	floor := 1e-10
	if peak > 0 {
		floor = peak * math.Pow(10, minDB/10)
	}
	for _, frame := range spec {
		for b, v := range frame {
			frame[b] = 10 * math.Log10(math.Max(v, floor))
		}
	}
}

// fluxFrom takes the half-wave rectified first difference of a dB
// spectrogram, averaged across bands.
func fluxFrom(spec [][]float64) []float64 {
	bands := len(spec[0])
	env := make([]float64, len(spec))
	for t := 1; t < len(spec); t++ {
		var sum float64
		for b := range bands {
			if d := spec[t][b] - spec[t-1][b]; d > 0 {
				sum += d
			}
		}
		env[t] = sum / float64(bands)
	}
	// The first frame has no predecessor; copying the second avoids a
	// spurious zero that the DP would read as "definitely not a beat".
	env[0] = env[1]
	return env
}

// melSpectrogram returns one mel-band power spectrum per STFT hop.
//
// Frames are centred: the signal is padded by half a window so that frame t
// is centred on sample t*HopSize, and its time is therefore exactly
// t*HopSize/sampleRate. Without this every detected onset lags the real one by
// most of a window -- about 90 ms at the default settings, which is audibly
// late on a cut.
func melSpectrogram(samples []float64, sampleRate int, o *Options) [][]float64 {
	n := o.WindowSize
	hop := o.HopSize
	if len(samples) < n {
		return nil
	}
	samples = padCenter(samples, n/2)

	plan := newFFTPlan(n)
	window := hann(n)
	filters := melFilterBank(o.MelBands, n, sampleRate, o.MinFreq, o.MaxFreq)

	frames := 1 + (len(samples)-n)/hop
	out := make([][]float64, frames)

	re := make([]float64, n)
	im := make([]float64, n)
	power := make([]float64, n/2+1)

	for t := range frames {
		start := t * hop
		for i := range n {
			re[i] = samples[start+i] * window[i]
			im[i] = 0
		}
		plan.transform(re, im)

		for k := range power {
			power[k] = re[k]*re[k] + im[k]*im[k]
		}

		band := make([]float64, len(filters))
		for b, f := range filters {
			var sum float64
			for k := f.start; k < f.end; k++ {
				sum += power[k] * f.weights[k-f.start]
			}
			band[b] = sum
		}
		out[t] = band
	}
	return out
}

// padCenter extends x at both ends by pad samples, reflecting the signal
// rather than padding with zeros so that the edges do not read as onsets.
func padCenter(x []float64, pad int) []float64 {
	if pad <= 0 {
		return x
	}
	out := make([]float64, 0, len(x)+2*pad)
	for i := pad; i > 0; i-- {
		out = append(out, x[min(i, len(x)-1)])
	}
	out = append(out, x...)
	for i := 1; i <= pad; i++ {
		out = append(out, x[max(len(x)-1-i, 0)])
	}
	return out
}

// melFilter is one triangular band, stored as the FFT bin range it touches
// plus the weight for each of those bins.
type melFilter struct {
	start, end int
	weights    []float64
}

// melFilterBank builds triangular filters equally spaced on the mel scale.
func melFilterBank(bands, fftSize, sampleRate int, minFreq, maxFreq float64) []melFilter {
	if maxFreq <= 0 || maxFreq > float64(sampleRate)/2 {
		maxFreq = float64(sampleRate) / 2
	}

	// bands+2 edges give bands overlapping triangles.
	edges := make([]float64, bands+2)
	loMel, hiMel := hzToMel(minFreq), hzToMel(maxFreq)
	for i := range edges {
		edges[i] = melToHz(loMel + (hiMel-loMel)*float64(i)/float64(bands+1))
	}

	bins := fftSize/2 + 1
	binHz := float64(sampleRate) / float64(fftSize)
	toBin := func(hz float64) float64 { return hz / binHz }

	filters := make([]melFilter, bands)
	for b := range bands {
		lo, mid, hi := toBin(edges[b]), toBin(edges[b+1]), toBin(edges[b+2])

		start := max(int(math.Ceil(lo)), 0)
		end := min(int(math.Floor(hi))+1, bins)
		if end <= start {
			// Degenerate band (more bands than FFT resolution); centre it on
			// a single bin so it still contributes something.
			start = min(max(int(math.Round(mid)), 0), bins-1)
			end = start + 1
		}

		weights := make([]float64, end-start)
		for k := start; k < end; k++ {
			x := float64(k)
			var w float64
			switch {
			case x <= lo || x >= hi:
				w = 0
			case x <= mid:
				if mid > lo {
					w = (x - lo) / (mid - lo)
				} else {
					w = 1
				}
			default:
				if hi > mid {
					w = (hi - x) / (hi - mid)
				} else {
					w = 1
				}
			}
			weights[k-start] = w
		}
		filters[b] = melFilter{start: start, end: end, weights: weights}
	}
	return filters
}

// hzToMel and melToHz use the HTK formulation.
func hzToMel(hz float64) float64 { return 2595 * math.Log10(1+hz/700) }
func melToHz(mel float64) float64 {
	return 700 * (math.Pow(10, mel/2595) - 1)
}
