package beats

import "math"

// estimateTempo returns the global tempo in BPM, following Ellis (2007)
// section 3: autocorrelate the onset envelope, weight the result by a
// log-Gaussian centred on a prior tempo, and take the strongest lag.
//
// The weighting is what stops the autocorrelation peak at twice or half the
// real tempo from winning, which it otherwise routinely does -- a steady beat
// correlates just as well at every metrical level. The prior encodes "people
// tap at around StartBPM", not "the tempo is StartBPM".
func estimateTempo(env []float64, hopSeconds float64, o *Options) (bpm float64, strength []float64) {
	maxLag := int(60.0 / (o.MinBPM * hopSeconds))
	minLag := int(60.0 / (o.MaxBPM * hopSeconds))
	if minLag < 1 {
		minLag = 1
	}
	if maxLag >= len(env) {
		maxLag = len(env) - 1
	}
	if maxLag <= minLag {
		return o.StartBPM, nil
	}

	// Smooth before correlating. Onset peaks are only a frame or two wide,
	// but a real beat period is rarely a whole number of frames -- 120 BPM is
	// 21.53 frames at the default settings. Correlating the raw envelope then
	// misaligns every peak by up to half a frame at the true lag, while the
	// double-period lag (43.07) happens to land almost exactly on a frame and
	// scores higher. Widening the peaks removes that quantisation artifact
	// and lets the tempo prior do its job.
	ac := autocorrelate(smooth(env, 1.0), maxLag+1)

	// Log-Gaussian prior over tempo, in octaves away from StartBPM.
	strength = make([]float64, maxLag+1)
	best, bestLag := math.Inf(-1), minLag
	for lag := minLag; lag <= maxLag; lag++ {
		candidate := 60.0 / (float64(lag) * hopSeconds)
		octaves := math.Log2(candidate / o.StartBPM)
		weight := math.Exp(-0.5 * (octaves / o.TempoSpread) * (octaves / o.TempoSpread))
		strength[lag] = ac[lag] * weight
		if strength[lag] > best {
			best, bestLag = strength[lag], lag
		}
	}

	// Interpolate between neighbouring lags. A whole frame is about 3 BPM at
	// 120 BPM, which is more error than the rest of the pipeline introduces.
	return 60.0 / (refinePeak(strength, bestLag) * hopSeconds), strength
}

// refinePeak fits a parabola through the winning lag and its two neighbours
// and returns the position of its vertex, giving sub-frame resolution.
func refinePeak(y []float64, i int) float64 {
	if i <= 0 || i >= len(y)-1 {
		return float64(i)
	}
	denom := y[i-1] - 2*y[i] + y[i+1]
	if denom == 0 {
		return float64(i)
	}
	offset := 0.5 * (y[i-1] - y[i+1]) / denom
	if math.Abs(offset) > 0.5 {
		return float64(i)
	}
	return float64(i) + offset
}

// smooth convolves x with a Gaussian of the given standard deviation in
// frames, truncated at three sigma.
func smooth(x []float64, sigma float64) []float64 {
	if sigma <= 0 {
		return x
	}
	radius := int(math.Ceil(3 * sigma))
	kernel := make([]float64, 2*radius+1)
	var total float64
	for i := range kernel {
		d := float64(i - radius)
		kernel[i] = math.Exp(-0.5 * d * d / (sigma * sigma))
		total += kernel[i]
	}
	for i := range kernel {
		kernel[i] /= total
	}

	out := make([]float64, len(x))
	for i := range x {
		var sum float64
		for k, w := range kernel {
			j := i + k - radius
			if j < 0 {
				j = 0
			} else if j >= len(x) {
				j = len(x) - 1
			}
			sum += x[j] * w
		}
		out[i] = sum
	}
	return out
}

// autocorrelate returns the unnormalised autocorrelation of x for lags
// [0, maxLag], computed via the FFT because the direct sum is O(n*maxLag) and
// a five-minute track at a 512-sample hop makes that noticeable.
func autocorrelate(x []float64, maxLag int) []float64 {
	n := nextPow2(2 * len(x))
	re := make([]float64, n)
	im := make([]float64, n)

	// Remove the mean first: a large DC offset swamps the periodic structure.
	var mean float64
	for _, v := range x {
		mean += v
	}
	mean /= float64(len(x))
	for i, v := range x {
		re[i] = v - mean
	}

	plan := newFFTPlan(n)
	plan.transform(re, im)

	// Power spectrum; its inverse transform is the autocorrelation.
	for i := range n {
		re[i] = re[i]*re[i] + im[i]*im[i]
		im[i] = 0
	}

	// Inverse via conjugation: conj(FFT(conj(X)))/n.
	for i := range n {
		im[i] = -im[i]
	}
	plan.transform(re, im)

	out := make([]float64, maxLag+1)
	for lag := 0; lag <= maxLag && lag < n; lag++ {
		out[lag] = re[lag] / float64(n)
	}
	return out
}
