package texture_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/simonwistow/audiotexture/texture"
)

// spacing returns the gaps between consecutive onsets, plus the gap from the
// last onset to the end of the track.
func spacing(onsets []texture.Onset, duration float64) []float64 {
	gaps := make([]float64, len(onsets))
	for i := range onsets {
		if i+1 < len(onsets) {
			gaps[i] = onsets[i+1].Start - onsets[i].Start
		} else {
			gaps[i] = duration - onsets[i].Start
		}
	}
	return gaps
}

// onBeat reports how many onsets land on one of the beats, within tolerance.
//
// A tolerance is necessary rather than pedantic: the legacy algorithm
// quantises to the output frame grid, so its cuts are never exactly equal to a
// beat time even when it has found one. Comparing exactly would score it zero
// by construction.
func onBeat(onsets []texture.Onset, beats []float64, tolerance float64) int {
	var n int
	for _, o := range onsets {
		for _, b := range beats {
			if math.Abs(o.Start-b) <= tolerance {
				n++
				break
			}
		}
	}
	return n
}

// halfFrame is half of one frame at 24 fps: the most a cut can be moved by
// quantising to the frame grid.
const halfFrame = 0.5 / 24

// TestAllAlgorithmsSharedInvariants holds every registered algorithm to the
// contract the encoder relies on.
func TestAllAlgorithmsSharedInvariants(t *testing.T) {
	const duration = 60.0
	beats := grid(0.5, duration)

	for _, name := range texture.List() {
		t.Run(name, func(t *testing.T) {
			algo, err := texture.Get(name)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			for _, count := range []int{1, 2, 5, 23, 119, 240} {
				got, err := algo.Assign(texture.Input{
					Images:    names(count),
					Beats:     beats,
					Duration:  duration,
					FrameRate: 24,
				})
				if err != nil {
					t.Fatalf("%d images: Assign: %v", count, err)
				}
				if len(got) != count {
					t.Fatalf("%d images: got %d onsets", count, len(got))
				}
				if got[0].Start != 0 {
					t.Errorf("%d images: first onset at %v, want 0", count, got[0].Start)
				}
				for i := range got {
					if i > 0 && got[i].Start < got[i-1].Start {
						t.Errorf("%d images: onset %d (%.3f) precedes %d (%.3f)",
							count, i, got[i].Start, i-1, got[i-1].Start)
					}
					if got[i].Start < 0 || got[i].Start > duration {
						t.Errorf("%d images: onset %d at %.3f is outside [0, %v]",
							count, i, got[i].Start, duration)
					}
					if want := names(count)[i]; got[i].Image != want {
						t.Errorf("%d images: onset %d image = %q, want %q", count, i, got[i].Image, want)
					}
				}
			}
		})
	}
}

// TestAllAlgorithmsWithoutBeats checks nothing falls over when detection found
// nothing, which happens on spoken word and ambient material.
func TestAllAlgorithmsWithoutBeats(t *testing.T) {
	for _, name := range texture.List() {
		algo, _ := texture.Get(name)
		got, err := algo.Assign(texture.Input{Images: names(6), Duration: 30, FrameRate: 24})
		if err != nil {
			t.Errorf("%s: Assign: %v", name, err)
			continue
		}
		if len(got) != 6 {
			t.Errorf("%s: got %d onsets, want 6", name, len(got))
		}
		// With no beats there is nothing to prefer, so everything should fall
		// back to even spacing.
		for i := range got {
			if want := float64(i) * 5; math.Abs(got[i].Start-want) > 1e-9 {
				t.Errorf("%s: onset %d = %v, want even spacing at %v", name, i, got[i].Start, want)
			}
		}
	}
}

// TestOptimalVersusLegacy measures both algorithms against the objective the
// legacy one pursues greedily, across beat patterns of different characters.
//
// The finding, which is worth being precise about rather than overclaiming:
// on a steady pulse the greedy algorithm is already very close to optimal,
// and the DP wins by a rounding error. Where it wins properly is on sparse or
// clustered beats -- exactly the cases where the greedy version runs out of
// reachable beats, stacks images up and falls back to redistributing them.
func TestOptimalVersusLegacy(t *testing.T) {
	const duration = 60.0

	var irregular, bursty, sparse []float64
	for x := 0.0; x < duration; x += 0.31 + 0.5*math.Abs(math.Sin(x)) {
		irregular = append(irregular, x)
	}
	for b := 0.0; b < duration; b += 5 { // three quick beats, then a long gap
		for k := range 3 {
			bursty = append(bursty, b+float64(k)*0.12)
		}
	}
	for x := 0.0; x < duration; x += 3.7 {
		sparse = append(sparse, x)
	}

	patterns := []struct {
		name  string
		beats []float64
		// wantBetterBy is the improvement expected with many images: the
		// margin by which the DP should beat greedy at 51 images.
		wantBetterBy float64
	}{
		{"regular 120 BPM", grid(0.5, duration), 0},
		{"irregular", irregular, 0},
		{"bursty", bursty, 0.5},
		{"sparse", sparse, 1.0},
	}

	legacy, _ := texture.Get("legacy")
	optimal, _ := texture.Get("optimal")

	for _, pattern := range patterns {
		t.Run(pattern.name, func(t *testing.T) {
			// Squared deviation from even spacing, less a bonus for landing
			// on a beat: the objective legacy is chasing.
			cost := func(onsets []texture.Onset, n int) float64 {
				shot := duration / float64(n)
				var total float64
				for i, o := range onsets {
					d := (o.Start - float64(i)*shot) / shot
					total += d * d
					for _, b := range pattern.beats {
						if math.Abs(o.Start-b) <= halfFrame {
							total -= 0.25
							break
						}
					}
				}
				return total
			}

			var deltaAtMany float64
			for _, n := range []int{7, 13, 29, 51} {
				in := texture.Input{Images: names(n), Beats: pattern.beats, Duration: duration, FrameRate: 24}
				l, err := legacy.Assign(in)
				if err != nil {
					t.Fatalf("legacy: %v", err)
				}
				o, err := optimal.Assign(in)
				if err != nil {
					t.Fatalf("optimal: %v", err)
				}
				lc, oc := cost(l, n), cost(o, n)

				// The DP should never be meaningfully worse. It can be a
				// hair worse, because legacy quantises to the frame grid and
				// a quantised time occasionally sits closer to the ideal
				// position than the exact beat time the DP is restricted to.
				const quantisationSlack = 0.05
				if oc > lc+quantisationSlack {
					t.Errorf("%d images: optimal cost %.4f is worse than legacy %.4f", n, oc, lc)
				}
				if n == 51 {
					deltaAtMany = lc - oc
				}
				t.Logf("%3d images: legacy %8.3f, optimal %8.3f, delta %+.4f", n, lc, oc, oc-lc)
			}

			if deltaAtMany < pattern.wantBetterBy {
				t.Errorf("at 51 images optimal beat legacy by only %.4f, want at least %.4f",
					deltaAtMany, pattern.wantBetterBy)
			}
		})
	}
}

// TestNoveltyPrefersBoundaries checks the novelty weighting actually moves
// cuts onto the marked instants.
func TestNoveltyPrefersBoundaries(t *testing.T) {
	const duration = 60.0
	beats := grid(0.5, duration)
	boundaries := []float64{10, 20, 30, 40, 50}

	in := texture.Input{
		Images:    names(6),
		Beats:     beats,
		Duration:  duration,
		FrameRate: 24,
		Novelty: func(t float64) float64 {
			for _, b := range boundaries {
				if math.Abs(t-b) < 0.01 {
					return 1
				}
			}
			return 0
		},
	}

	algo, _ := texture.Get("novelty")
	got, err := algo.Assign(in)
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}

	// Ideal even spacing for 6 images over 60s is every 10s, which here
	// coincides with the boundaries, so every cut should land on one.
	for i, o := range got[1:] {
		if math.Abs(o.Start-boundaries[i]) > 1e-6 {
			t.Errorf("onset %d at %.3f, want boundary at %v", i+1, o.Start, boundaries[i])
		}
	}
}

// TestNoveltyPullsCutsOffTheEvenGrid checks the weighting is strong enough to
// matter when the boundaries do not already line up with even spacing.
func TestNoveltyPullsCutsOffTheEvenGrid(t *testing.T) {
	const duration = 60.0
	beats := grid(0.5, duration)
	// Deliberately offset from the 15/30/45 the even layout wants.
	boundaries := []float64{17, 31, 47}

	base := texture.Input{Images: names(4), Beats: beats, Duration: duration, FrameRate: 24}
	withNovelty := base
	withNovelty.Novelty = func(t float64) float64 {
		for _, b := range boundaries {
			if math.Abs(t-b) < 0.01 {
				return 1
			}
		}
		return 0
	}

	optimal, _ := texture.Get("optimal")
	novelty, _ := texture.Get("novelty")
	plain, err := optimal.Assign(base)
	if err != nil {
		t.Fatalf("optimal: %v", err)
	}
	pulled, err := novelty.Assign(withNovelty)
	if err != nil {
		t.Fatalf("novelty: %v", err)
	}

	if onBeat(pulled, boundaries, 1e-6) == 0 {
		t.Errorf("novelty put no cut on a boundary: got %v, boundaries %v",
			[]float64{pulled[1].Start, pulled[2].Start, pulled[3].Start}, boundaries)
	}
	if onBeat(plain, boundaries, 1e-6) >= onBeat(pulled, boundaries, 1e-6) {
		t.Error("novelty weighting made no difference to where the cuts landed")
	}
}

// TestBarsHoldsAConstantBeatCount is what distinguishes bars from the rest:
// every shot lasts the same number of beats.
func TestBarsHoldsAConstantBeatCount(t *testing.T) {
	const duration = 64.0
	beats := grid(0.5, duration) // 128 beats

	algo, _ := texture.Get("bars")
	// 16 images over 128 beats is exactly 8 beats each, a musical length.
	got, err := algo.Assign(texture.Input{
		Images: names(16), Beats: beats, Duration: duration, FrameRate: 24,
	})
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}

	gaps := spacing(got, duration)
	// Every gap except the last should be 8 beats = 4 seconds.
	for i := 1; i < len(gaps)-1; i++ {
		if math.Abs(gaps[i]-4.0) > 1e-6 {
			t.Errorf("gap %d = %.3f, want 4.0 (8 beats)", i, gaps[i])
		}
	}
	if n := onBeat(got[1:], beats, 1e-6); n != len(got)-1 {
		t.Errorf("%d of %d cuts landed on a beat, want all of them", n, len(got)-1)
	}
}

// TestBarsSnapsToMusicalLengths checks an awkward image count still produces a
// round number of beats per shot.
func TestBarsSnapsToMusicalLengths(t *testing.T) {
	const duration = 60.0
	beats := grid(0.5, duration) // 120 beats
	algo, _ := texture.Get("bars")

	// 29 images over 120 beats is 4.14 beats each; 4 is within tolerance.
	got, err := algo.Assign(texture.Input{
		Images: names(29), Beats: beats, Duration: duration, FrameRate: 24,
	})
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}
	for i := 2; i < len(got); i++ {
		gap := got[i].Start - got[i-1].Start
		if math.Abs(gap-2.0) > 1e-6 { // 4 beats at 120 BPM
			t.Errorf("gap %d = %.3f, want 2.0 (4 beats)", i, gap)
			break
		}
	}
}

func TestAlgorithmErrors(t *testing.T) {
	for _, name := range texture.List() {
		algo, _ := texture.Get(name)
		if _, err := algo.Assign(texture.Input{Duration: 10, FrameRate: 24}); err == nil {
			t.Errorf("%s: expected an error for no images", name)
		}
	}
	for _, name := range []string{"legacy", "optimal", "novelty", "bars"} {
		algo, _ := texture.Get(name)
		if _, err := algo.Assign(texture.Input{Images: names(3), Duration: 0, FrameRate: 24}); err == nil {
			t.Errorf("%s: expected an error for zero duration", name)
		}
	}
}

// TestBuiltinsAreRegistered checks each exported algorithm is the one its
// name looks up, and that its String is that name, so choosing by value and
// choosing by name always agree.
func TestBuiltinsAreRegistered(t *testing.T) {
	for _, a := range []texture.Algorithm{texture.Even, texture.Legacy, texture.Optimal, texture.Novelty, texture.Bars} {
		name := a.(fmt.Stringer).String()
		got, err := texture.Get(name)
		if err != nil {
			t.Errorf("Get(%q): %v", name, err)
			continue
		}
		if got != a {
			t.Errorf("Get(%q) = %v, want the exported %v", name, got, a)
		}
	}
	if n := len(texture.List()); n != 5 {
		t.Errorf("%d algorithms registered, want the 5 built-ins", n)
	}
}
