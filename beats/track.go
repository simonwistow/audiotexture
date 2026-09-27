package beats

import "math"

// trackBeats finds the best sequence of beat frames through env, given a
// target period in frames.
//
// This is the dynamic program from Ellis (2007) section 4. Each beat is scored
// on two things: how much onset strength sits at that instant (the local
// match) and how close the gap to the previous beat is to the target period
// (the transition cost). Maximising their sum over the whole track is exactly
// the shape a DP solves, and it is why the result stays on the grid through a
// passage with no percussion instead of drifting the way a greedy peak-picker
// does.
func trackBeats(env []float64, period float64, tightness float64) []int {
	if len(env) == 0 || period <= 0 {
		return nil
	}

	local := localScore(env, period)

	// Candidate previous-beat offsets: between half and twice the period.
	lo := int(math.Round(period / 2))
	hi := int(math.Round(period * 2))
	if lo < 1 {
		lo = 1
	}
	if hi < lo {
		hi = lo
	}

	// Transition cost, precomputed per offset. It peaks at exactly one period
	// and falls off as the square of the log ratio, so being out by a factor
	// of two costs the same whichever direction you are out by.
	cost := make([]float64, hi+1)
	for d := lo; d <= hi; d++ {
		ratio := math.Log(float64(d) / period)
		cost[d] = -tightness * ratio * ratio
	}

	cumulative := make([]float64, len(local))
	backlink := make([]int, len(local))

	var maxLocal float64
	for _, v := range local {
		maxLocal = math.Max(maxLocal, v)
	}
	firstBeat := true

	for i := range local {
		best, bestPrev := math.Inf(-1), -1
		for d := lo; d <= hi; d++ {
			j := i - d
			if j < 0 {
				break
			}
			if s := cumulative[j] + cost[d]; s > best {
				best, bestPrev = s, j
			}
		}

		if bestPrev < 0 {
			// Nothing far enough back to link to yet.
			cumulative[i] = local[i]
			backlink[i] = -1
			continue
		}

		cumulative[i] = local[i] + best
		// Do not anchor the first beat on near-silence: a track that opens
		// with a fade would otherwise get a beat at frame 0.
		if firstBeat && local[i] < 0.01*maxLocal {
			backlink[i] = -1
		} else {
			backlink[i] = bestPrev
			firstBeat = false
		}
	}

	return trimBeats(backtrace(cumulative, backlink), local)
}

// localScore normalises the onset envelope and gives each peak a narrow
// Gaussian skirt, so a beat landing a frame either side of a transient still
// collects most of its score.
//
// The window is deliberately narrow -- one standard deviation is a
// thirty-second of the beat period, well under a frame at typical tempos.
// Widening it past a few frames smears each onset into its neighbours and the
// tracker loses the position information it is there to use, producing an
// evenly spaced grid at slightly the wrong tempo that drifts against the
// music.
func localScore(env []float64, period float64) []float64 {
	var mean, variance float64
	for _, v := range env {
		mean += v
	}
	mean /= float64(len(env))
	for _, v := range env {
		variance += (v - mean) * (v - mean)
	}
	std := math.Sqrt(variance / float64(len(env)))
	if std == 0 {
		std = 1
	}

	width := max(int(math.Round(period)), 1)
	window := make([]float64, 2*width+1)
	sigma := period / 32
	for i := range window {
		d := float64(i-width) / sigma
		window[i] = math.Exp(-0.5 * d * d)
	}

	out := make([]float64, len(env))
	for i := range env {
		var sum float64
		for k, w := range window {
			j := i + k - width
			if j < 0 || j >= len(env) {
				continue
			}
			sum += (env[j] / std) * w
		}
		out[i] = sum
	}
	return out
}

// trimBeats drops beats at the start and end that rest on nothing.
//
// The DP has to begin and end somewhere, so the chain often opens on frame 0
// and closes on the last frame whether or not there is any onset there. A
// track that fades in, or simply ends, would otherwise get a cut at silence.
func trimBeats(beats []int, local []float64) []int {
	if len(beats) == 0 {
		return nil
	}

	// Score each beat by its neighbourhood rather than its own frame, so one
	// weak beat inside a steady passage is not treated as an edge.
	window := hann(5)
	scores := make([]float64, len(beats))
	var meanSquare float64
	for i := range beats {
		var sum float64
		for k, w := range window {
			j := i + k - len(window)/2
			if j < 0 || j >= len(beats) {
				continue
			}
			sum += local[beats[j]] * w
		}
		scores[i] = sum
		meanSquare += sum * sum
	}
	threshold := 0.5 * math.Sqrt(meanSquare/float64(len(beats)))

	first, last := -1, -1
	for i, s := range scores {
		if s > threshold {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return beats
	}
	return beats[first : last+1]
}

// backtrace walks the backlinks from the best end state and returns the beat
// frames in order.
func backtrace(cumulative []float64, backlink []int) []int {
	// Start from the strongest score in the last stretch of the track rather
	// than the global maximum, so the chain covers the whole piece.
	tail := len(cumulative) - 1
	best, bestIdx := math.Inf(-1), -1
	for i := tail; i >= 0 && i > tail-len(cumulative); i-- {
		if backlink[i] >= 0 && cumulative[i] > best {
			best, bestIdx = cumulative[i], i
		}
	}
	if bestIdx < 0 {
		return nil
	}

	var reversed []int
	for i := bestIdx; i >= 0; i = backlink[i] {
		reversed = append(reversed, i)
		if backlink[i] < 0 {
			break
		}
	}

	beats := make([]int, len(reversed))
	for i, v := range reversed {
		beats[len(reversed)-1-i] = v
	}
	return beats
}
