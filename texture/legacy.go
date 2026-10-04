package texture

import (
	"fmt"
	"sort"
)

func init() {
	Register("legacy", "the 2010 Perl algorithm: even spacing snapped to nearby beats", Legacy)
}

// legacyAlgorithm is a port of the original slideshow.pl.
//
// The shape of it: lay the images out at equal intervals, then let each one
// look for a beat close to where it already is and move onto it. Images that
// find nothing within their search window are stacked up, and the moment a
// later image does find a beat, the whole stack is spread evenly between the
// last placed image and that one.
//
// The author's own comment on it was "remember, this was a weekend project.
// the algorithm was adapted from a totally different (abandoned) approach".
// It is greedy and it is not optimal -- see the "optimal" algorithm for the
// same objective solved properly -- but it has a particular feel, and this is
// the version that produced the original videos, so it is kept exactly.
//
// Everything happens on the output frame grid, because the original was
// written to emit numbered frame files and thought in frame numbers
// throughout. That quantisation is part of the behaviour, not an accident.
type legacyAlgorithm struct{}

func (legacyAlgorithm) String() string { return "legacy" }

func (legacyAlgorithm) Assign(in Input) ([]Onset, error) {
	n := len(in.Images)
	if n == 0 {
		return nil, fmt.Errorf("no images")
	}
	if in.Duration <= 0 {
		return nil, fmt.Errorf("duration must be positive, got %v", in.Duration)
	}
	frameRate := in.FrameRate
	if frameRate <= 0 {
		frameRate = 24 // the original's hardcoded $FR
	}

	// Special case from the original: exactly as many beats as images means
	// the images simply land on the beats, one each.
	if len(in.Beats) == n {
		starts := append([]float64(nil), in.Beats...)
		sort.Float64s(starts)
		return onsetsFrom(in.Images, starts), nil
	}

	// %peak in the original: the set of frames a beat falls on, rounded.
	peak := make(map[int]bool, len(in.Beats))
	for _, b := range in.Beats {
		peak[int(0.5+frameRate*b)] = true
	}

	starts := evenStarts(n, in.Duration)

	// The search window is half the average shot length, in frames, so an
	// image may move up to halfway towards either neighbour but no further.
	window := 1 + int(0.5*frameRate*in.Duration/float64(n))
	if window < 1 {
		window = 1
	}

	// Indices that found no beat and are waiting to be spread out.
	var stack []int

	// Image 0 is never moved: the show starts when the music does.
	for i := 1; i < n; i++ {
		frame := int(starts[i] * frameRate)

		// Widening search from the image's current position. At equal
		// distance the earlier frame wins, so a cut lands on the beat rather
		// than just after it.
		found := false
		for j := 0; j <= window && !found; j++ {
			switch {
			case peak[frame-j]:
				starts[i] = float64(frame-j) / frameRate
				found = true
			case peak[frame+j]:
				starts[i] = float64(frame+j) / frameRate
				found = true
			}
		}

		if !found {
			stack = append(stack, i)
			continue
		}

		if len(stack) > 0 {
			// Spread the stacked images evenly between the last image placed
			// before them and this one.
			first := stack[0]
			gaps := len(stack) + 1
			from, to := starts[first-1], starts[i]
			for _, idx := range stack {
				starts[idx] = from + float64(idx-first+1)*(to-from)/float64(gaps)
			}
			stack = nil
		}
	}

	// Anything still stacked when the images run out keeps its original even
	// spacing, exactly as the original left it.

	// Snapping can move an image past its neighbour, so the original sorted
	// the times at the end "just in case". Sorting the times alone is right:
	// the images keep their filename order and take the times in sequence.
	sort.Float64s(starts)
	return onsetsFrom(in.Images, starts), nil
}
