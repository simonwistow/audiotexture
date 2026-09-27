package beats_test

import (
	"math"
	"testing"

	"github.com/simonwistow/audiotexture/audio"
	"github.com/simonwistow/audiotexture/beats"
	"github.com/simonwistow/audiotexture/internal/testaudio"
)

// clickPCM builds a click track directly, skipping the file round trip.
func clickPCM(duration, bpm float64) *audio.PCM {
	const rate = 22050
	return &audio.PCM{Samples: testaudio.ClickTrack(rate, duration, bpm), SampleRate: rate}
}

func TestDetectTempo(t *testing.T) {
	for _, bpm := range []float64{90, 100, 120, 140, 160} {
		r, err := beats.Detect(clickPCM(20, bpm), nil)
		if err != nil {
			t.Fatalf("%v BPM: Detect: %v", bpm, err)
		}
		if math.Abs(r.BPM-bpm) > 2 {
			t.Errorf("BPM = %.2f, want %v (+/- 2)", r.BPM, bpm)
		}
	}
}

func TestDetectBeatPositions(t *testing.T) {
	const (
		duration = 20.0
		bpm      = 120.0
	)
	r, err := beats.Detect(clickPCM(duration, bpm), nil)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	want := testaudio.BeatTimes(duration, bpm)
	// The tracker should find nearly all of them; allow for edges.
	if len(r.Times) < len(want)-4 {
		t.Fatalf("found %d beats, want about %d", len(r.Times), len(want))
	}

	// Every detected beat should be close to a real click.
	//
	// A dB-domain spectral flux fires as soon as a transient reaches the
	// leading edge of the analysis window, so onsets lead the true attack by
	// roughly a quarter of a window -- about 30 ms at the default 2048-sample
	// window. That is smaller than a single frame of 24 fps video (42 ms), so
	// it costs nothing on a cut and is not worth trading spectral resolution
	// to chase.
	const tolerance = 0.05
	for _, got := range r.Times {
		nearest := math.Inf(1)
		for _, w := range want {
			nearest = math.Min(nearest, math.Abs(got-w))
		}
		if nearest > tolerance {
			t.Errorf("beat at %.3fs is %.3fs from the nearest click", got, nearest)
		}
	}

	// And the spacing should be the beat period throughout.
	// Beats land on whole STFT frames, and 120 BPM is 21.53 frames, so gaps
	// alternate between 21 and 22 frames either side of the true period.
	period := 60.0 / bpm
	for i := 1; i < len(r.Times); i++ {
		if gap := r.Times[i] - r.Times[i-1]; math.Abs(gap-period) > tolerance {
			t.Errorf("gap %d = %.3fs, want %.3fs", i, gap, period)
		}
	}
}

// TestDetectHoldsThroughSilence is the property a greedy peak-picker does not
// have: the grid should carry on through a stretch with no transients.
func TestDetectHoldsThroughSilence(t *testing.T) {
	const (
		duration = 24.0
		bpm      = 120.0
		rate     = 22050
	)
	samples := testaudio.ClickTrack(rate, duration, bpm)
	// Mute seconds 10 to 14, four beats' worth.
	for i := 10 * rate; i < 14*rate && i < len(samples); i++ {
		samples[i] = 0
	}

	r, err := beats.Detect(&audio.PCM{Samples: samples, SampleRate: rate}, nil)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	var inGap int
	for _, ts := range r.Times {
		if ts >= 10 && ts <= 14 {
			inGap++
		}
	}
	if inGap < 6 {
		t.Errorf("found %d beats in the silent stretch, want the grid to carry through (about 8)", inGap)
	}
	if math.Abs(r.BPM-bpm) > 2 {
		t.Errorf("BPM = %.2f, want %v", r.BPM, bpm)
	}
}

func TestDetectFixedBPM(t *testing.T) {
	r, err := beats.Detect(clickPCM(10, 120), &beats.Options{FixedBPM: 90})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if r.BPM != 90 {
		t.Errorf("BPM = %v, want the fixed 90", r.BPM)
	}
}

func TestDetectErrors(t *testing.T) {
	if _, err := beats.Detect(nil, nil); err == nil {
		t.Error("expected an error for nil PCM")
	}
	if _, err := beats.Detect(&audio.PCM{Samples: []float64{}, SampleRate: 22050}, nil); err == nil {
		t.Error("expected an error for empty audio")
	}
	if _, err := beats.Detect(&audio.PCM{Samples: make([]float64, 100), SampleRate: 22050}, nil); err == nil {
		t.Error("expected an error for audio shorter than a window")
	}
	if _, err := beats.Detect(&audio.PCM{Samples: make([]float64, 100000), SampleRate: 0}, nil); err == nil {
		t.Error("expected an error for a zero sample rate")
	}
}

func TestResultStrength(t *testing.T) {
	r, err := beats.Detect(clickPCM(10, 120), nil)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if r.Strength(-1) != 0 {
		t.Error("Strength before the start should be 0")
	}
	if r.Strength(1e6) != 0 {
		t.Error("Strength past the end should be 0")
	}
	// A click instant should be stronger than the middle of a gap.
	onBeat := r.Strength(4.0)   // 120 BPM: a click lands on every 0.5s
	offBeat := r.Strength(4.25) // halfway between two clicks
	if onBeat <= offBeat {
		t.Errorf("onset strength on the beat (%v) should exceed off the beat (%v)", onBeat, offBeat)
	}
}

func BenchmarkDetect(b *testing.B) {
	pcm := clickPCM(180, 128) // three minutes
	b.ResetTimer()
	for b.Loop() {
		if _, err := beats.Detect(pcm, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// TestNoveltyFindsStructuralBoundary checks that the novelty curve peaks where
// the music changes character, not merely where it is loud.
func TestNoveltyFindsStructuralBoundary(t *testing.T) {
	const (
		rate     = 22050
		duration = 30.0
		boundary = 15.0
	)
	// Two halves with the same tempo and the same loudness, differing only in
	// timbre: a low bed before the boundary, a bright one after. An onset
	// detector sees nothing special at 15s; a novelty curve should.
	samples := testaudio.ClickTrack(rate, duration, 120)
	for i := range samples {
		tSec := float64(i) / rate
		freq := 150.0
		if tSec >= boundary {
			freq = 3000.0
		}
		samples[i] += 0.3 * math.Sin(2*math.Pi*freq*tSec)
	}

	r, err := beats.Detect(&audio.PCM{Samples: samples, SampleRate: rate}, nil)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(r.Novelty) == 0 {
		t.Fatal("no novelty curve computed")
	}

	// The peak should be within a couple of seconds of the change.
	var peak float64
	peakAt := -1.0
	for i, v := range r.Novelty {
		if v > peak {
			peak, peakAt = v, float64(i)*r.NoveltySeconds
		}
	}
	if math.Abs(peakAt-boundary) > 2.0 {
		t.Errorf("novelty peaks at %.2fs, want within 2s of %.1fs", peakAt, boundary)
	}

	// And it should be much higher there than in the middle of a section.
	if atBoundary, midSection := r.NoveltyAt(boundary), r.NoveltyAt(7.0); atBoundary <= 3*midSection {
		t.Errorf("novelty at the boundary (%.3f) should dominate mid-section (%.3f)", atBoundary, midSection)
	}

	if r.NoveltyAt(-1) != 0 || r.NoveltyAt(1e6) != 0 {
		t.Error("novelty outside the analysed range should be 0")
	}
}
