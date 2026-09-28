package beats

import (
	"math"
	"sort"
)

// tempogram is a windowed autocorrelation of the onset envelope: one column
// every TempoStep seconds, one row per candidate beat period in frames.
//
// A single autocorrelation over the whole track assumes the tempo never
// changes. That holds for a sequenced record and fails for anything played by
// people: a live take drifts, and a track that alternates a half-time verse
// with a driving chorus has no one right answer at all. Correlating short
// windows instead asks the question separately at each instant, and leaves
// picking a coherent path through the answers to trackTempo.
type tempogram struct {
	minLag, maxLag int
	step           int         // frames between columns
	cols           [][]float64 // cols[c][lag], normalised to a peak of 1
}

// newTempogram returns nil when the envelope is too short to window, which
// leaves the caller to fall back on a single global estimate.
func newTempogram(env []float64, hopSeconds float64, o *Options) *tempogram {
	maxLag := int(60.0 / (o.MinBPM * hopSeconds))
	minLag := int(60.0 / (o.MaxBPM * hopSeconds))
	if minLag < 1 {
		minLag = 1
	}
	if maxLag <= minLag {
		return nil
	}

	// A window has to hold several periods of the slowest tempo we look for,
	// or its autocorrelation at that lag rests on one or two laps.
	win := int(math.Round(o.TempoWindow / hopSeconds))
	if win < 3*maxLag {
		win = 3 * maxLag
	}
	// Two windows' worth at minimum. With less than that every column sees
	// most of the same signal, so the tempogram says nothing a whole-track
	// autocorrelation does not -- and says it with a fraction of the lag
	// resolution.
	if len(env) < 2*win {
		return nil
	}

	step := int(math.Round(o.TempoStep / hopSeconds))
	if step < 1 {
		step = 1
	}

	// Smooth once, not per window: onset peaks are a frame or two wide and a
	// real beat period is rarely a whole number of frames, so correlating the
	// raw envelope misaligns every peak at the true lag while a lag that
	// happens to land on a frame boundary scores higher. See estimateTempo.
	smoothed := smooth(env, 1.0)

	tg := &tempogram{minLag: minLag, maxLag: maxLag, step: step}
	for centre := 0; centre < len(smoothed); centre += step {
		// Slide the window to stay wholly inside the envelope, so every
		// column correlates the same amount of signal and their peaks stay
		// comparable.
		s := centre - win/2
		if s < 0 {
			s = 0
		}
		if s+win > len(smoothed) {
			s = len(smoothed) - win
		}

		ac := autocorrelate(smoothed[s:s+win], maxLag)
		tg.cols = append(tg.cols, normaliseLags(ac, minLag, maxLag))
	}
	if len(tg.cols) == 0 {
		return nil
	}
	return tg
}

// normaliseLags scales the searched range to a peak of 1 so that a loud
// passage does not outvote a quiet one in the Viterbi. Correlation magnitude
// says how much energy there is, not how confident we are about the tempo.
func normaliseLags(ac []float64, minLag, maxLag int) []float64 {
	peak := 0.0
	for lag := minLag; lag <= maxLag && lag < len(ac); lag++ {
		peak = math.Max(peak, ac[lag])
	}
	out := make([]float64, maxLag+1)
	if peak <= 0 {
		return out
	}
	for lag := minLag; lag <= maxLag && lag < len(ac); lag++ {
		out[lag] = ac[lag] / peak
	}
	return out
}

// globalLag returns one beat period in frames for the whole track: the peak
// of the tempogram summed over time, under the prior over plausible tempos.
//
// Summing normalised columns rather than autocorrelating the whole envelope at
// once is the point. A track that drifts smears its peak when correlated end
// to end, and one loud section can outvote the rest of the piece; per-column
// normalisation gives every second of the track one equal vote on the tempo.
func globalLag(tg *tempogram, hopSeconds float64, o *Options) float64 {
	total := make([]float64, tg.maxLag+1)
	for _, col := range tg.cols {
		for lag := tg.minLag; lag <= tg.maxLag; lag++ {
			total[lag] += col[lag]
		}
	}

	best, bestLag := math.Inf(-1), tg.minLag
	for lag := tg.minLag; lag <= tg.maxLag; lag++ {
		octaves := math.Log2(60.0 / (float64(lag) * hopSeconds) / o.StartBPM)
		total[lag] *= math.Exp(-0.5 * (octaves / o.TempoSpread) * (octaves / o.TempoSpread))
		if total[lag] > best {
			best, bestLag = total[lag], lag
		}
	}
	return refinePeak(total, bestLag)
}

// trackTempo finds the best tempo path through the tempogram, returning one
// beat period in frames per column.
//
// This is a Viterbi decode, the same shape as the beat DP one level up: each
// column contributes how well its autocorrelation supports a period, and each
// step between columns pays TempoInertia times the squared change in octaves.
//
// The search is confined to a band of TempoDrift either side of the one tempo
// globalLag found for the whole track, and that band is what keeps the answer
// coherent. A squared-change penalty is cheap to pay in many small steps -- an
// unconstrained path will happily walk from 137 BPM down to 78 and back over a
// minute or two, collecting whatever each passage correlates best with and
// leaving a beat grid that means something different in every section. Bounding
// the band says the thing that is actually true of a piece of music: it has one
// tempo, which it may wander around, and if it really does change metrical
// level then following it there would make the cuts less coherent, not more.
func trackTempo(tg *tempogram, hopSeconds float64, o *Options) []float64 {
	base := globalLag(tg, hopSeconds, o)
	lo := max(int(math.Floor(base/(1+o.TempoDrift))), tg.minLag)
	hi := min(int(math.Ceil(base*(1+o.TempoDrift))), tg.maxLag)
	if hi <= lo {
		return constantPeriod(base, len(tg.cols))
	}
	span := hi - lo + 1

	logLag := make([]float64, span)
	for i := range span {
		logLag[i] = math.Log2(float64(lo + i))
	}

	score := make([]float64, span)
	next := make([]float64, span)
	back := make([][]int32, len(tg.cols))
	for i := range span {
		score[i] = tg.cols[0][lo+i]
	}

	for c := 1; c < len(tg.cols); c++ {
		back[c] = make([]int32, span)
		col := tg.cols[c]
		for i := range span {
			best, bestFrom := math.Inf(-1), 0
			for j := range span {
				// Periods are lags, so the octave distance between two
				// tempos is the octave distance between their lags.
				d := logLag[i] - logLag[j]
				if s := score[j] - o.TempoInertia*d*d; s > best {
					best, bestFrom = s, j
				}
			}
			next[i] = col[lo+i] + best
			back[c][i] = int32(bestFrom)
		}
		score, next = next, score
	}

	end := 0
	for i := range span {
		if score[i] > score[end] {
			end = i
		}
	}

	// A whole frame is about 3 BPM at 120 BPM, coarser than anything else in
	// the pipeline, so the path's integer lags need a sub-frame offset. It
	// comes from the global peak rather than from each column: one column
	// holds a dozen or so beats and its correlation peak is correspondingly
	// blunt, while the summed tempogram has the whole track's worth of
	// periods behind it and resolves the fraction properly. 126 BPM is lag
	// 20.5 at the default settings -- exactly between two frames, and a
	// per-column parabola gets it wrong by the full 2.5%.
	offset := base - math.Round(base)

	periods := make([]float64, len(tg.cols))
	i := end
	for c := len(tg.cols) - 1; c >= 0; c-- {
		periods[c] = float64(lo+i) + offset
		if c > 0 {
			i = int(back[c][i])
		}
	}
	return periods
}

// periodPerFrame expands one period per tempogram column into one per
// envelope frame, interpolating between column centres.
func periodPerFrame(periods []float64, step, frames int) []float64 {
	out := make([]float64, frames)
	for i := range frames {
		x := float64(i) / float64(step)
		c := int(x)
		switch {
		case c >= len(periods)-1:
			out[i] = periods[len(periods)-1]
		default:
			f := x - float64(c)
			out[i] = periods[c]*(1-f) + periods[c+1]*f
		}
	}
	return out
}

// medianPeriod returns the middle value of periods, which stands in for the
// track as a single reported tempo. The median rather than the mean because a
// few columns over an intro or a breakdown should not move the headline
// figure.
func medianPeriod(periods []float64) float64 {
	if len(periods) == 0 {
		return 0
	}
	sorted := append([]float64(nil), periods...)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

// tempoPath returns the beat period in frames for every frame of env, along
// with the tempo it followed as one BPM value per tempogram column and the
// seconds between those columns.
//
// It falls back to a single global tempo when the caller fixed one, and when
// the track is shorter than two tempogram windows -- below that every column
// sees most of the same signal, so the tempogram says nothing a whole-track
// autocorrelation does not, and says it with less lag resolution.
func tempoPath(env []float64, hopSeconds float64, o *Options) (perFrame, curve []float64, curveSeconds float64) {
	if o.FixedBPM > 0 {
		return constantPeriod(60.0/(o.FixedBPM*hopSeconds), len(env)), nil, 0
	}

	tg := newTempogram(env, hopSeconds, o)
	if tg == nil || len(tg.cols) < 4 {
		bpm, _ := estimateTempo(env, hopSeconds, o)
		return constantPeriod(60.0/(bpm*hopSeconds), len(env)), nil, 0
	}

	periods := trackTempo(tg, hopSeconds, o)
	curve = make([]float64, len(periods))
	for i, p := range periods {
		curve[i] = 60.0 / (p * hopSeconds)
	}
	return periodPerFrame(periods, tg.step, len(env)), curve, float64(tg.step) * hopSeconds
}

func constantPeriod(period float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = period
	}
	return out
}
