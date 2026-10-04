package texture

import (
	"fmt"
	"math"
)

func init() {
	Register("bars", "hold each image a whole number of beats, quantised to bar lengths", Bars)
}

// musicalLengths are the shot lengths, in beats, that sound deliberate: whole
// bars and their common subdivisions in both duple and triple time.
var musicalLengths = []int{1, 2, 3, 4, 6, 8, 12, 16, 24, 32}

// barsAlgorithm gives every image the same length, measured in beats rather
// than in seconds.
//
// The other algorithms start from "the images should be evenly spaced in time"
// and pull the cuts towards beats. This starts from the other end: pick a
// number of beats to hold each image for, and cut every that-many beats, all
// the way through. Every shot is then exactly one bar, or two, or half --
// which is how a cut edit to music is usually built, and it reads as
// deliberate in a way that near-even spacing does not.
//
// The count is snapped to a musically sensible value when the images and beats
// are close to allowing it, because four beats per image sounds like an edit
// and five sounds like an accident.
//
// The cost is that the images no longer fill the duration evenly if the tempo
// drifts, and the last image may run long. That is the trade.
type barsAlgorithm struct{}

func (barsAlgorithm) String() string { return "bars" }

func (barsAlgorithm) Assign(in Input) ([]Onset, error) {
	n := len(in.Images)
	if n == 0 {
		return nil, fmt.Errorf("no images")
	}
	if in.Duration <= 0 {
		return nil, fmt.Errorf("duration must be positive, got %v", in.Duration)
	}
	// Nothing to quantise to.
	if len(in.Beats) < 2 || n == 1 {
		return onsetsFrom(in.Images, evenStarts(n, in.Duration)), nil
	}

	beats := in.Beats
	ideal := float64(len(beats)) / float64(n)
	// Never overrun the beats we have: the last image must still get one.
	limit := float64(len(beats)-1) / float64(n-1)

	step := snapToMusical(ideal, limit)
	if step < 1 {
		step = 1
	}

	starts := make([]float64, n)
	placed := n
	for i := range n {
		idx := i * step
		if idx >= len(beats) {
			placed = i
			break
		}
		starts[i] = beats[idx]
	}

	// If the beats ran out, spread whatever is left between the last beat we
	// used and the end of the track rather than piling them on the last beat.
	if placed < n {
		from := starts[placed-1]
		gap := (in.Duration - from) / float64(n-placed+1)
		for i := placed; i < n; i++ {
			starts[i] = from + float64(i-placed+1)*gap
		}
	}

	// The first image starts with the music, not with the first detected beat,
	// which may be some way in.
	starts[0] = 0
	return onsetsFrom(in.Images, starts), nil
}

// snapToMusical returns the beats-per-image to use: the nearest musically
// sensible count when one is close enough and fits, otherwise the plain
// rounded value.
func snapToMusical(ideal, limit float64) int {
	const tolerance = 0.3 // accept a musical length within 30% of ideal

	best, bestErr := 0, math.Inf(1)
	for _, m := range musicalLengths {
		if float64(m) > limit {
			break
		}
		relative := math.Abs(float64(m)-ideal) / ideal
		if relative <= tolerance && relative < bestErr {
			best, bestErr = m, relative
		}
	}
	if best > 0 {
		return best
	}

	plain := int(math.Round(ideal))
	if float64(plain) > limit {
		plain = int(math.Floor(limit))
	}
	return max(plain, 1)
}
