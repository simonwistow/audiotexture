package beats_test

import (
	"math"
	"testing"

	"github.com/simonwistow/audiotexture/audio"
	"github.com/simonwistow/audiotexture/beats"
	"github.com/simonwistow/audiotexture/internal/testaudio"
)

const rampRate = 22050

func rampPCM(duration, startBPM, endBPM float64) (*audio.PCM, []float64) {
	samples, ts := testaudio.RampedClickTrack(rampRate, duration, startBPM, endBPM)
	return &audio.PCM{Samples: samples, SampleRate: rampRate}, ts
}

// TestTempoFollowsARamp is the property a single global tempo cannot have.
// The track speeds up from 100 to 140 BPM; one number for the whole thing is
// wrong by 20% at both ends however it is chosen.
func TestTempoFollowsARamp(t *testing.T) {
	const duration = 60.0
	pcm, _ := rampPCM(duration, 100, 140)

	r, err := beats.Detect(pcm, nil)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(r.Tempo) == 0 {
		t.Fatal("no tempo curve; the tempogram should have applied here")
	}

	// Sample the curve away from the edges, where a window centred on the
	// start or end necessarily straddles less of the ramp than it should.
	for _, at := range []float64{15, 30, 45} {
		want := 100 + 40*at/duration
		got := r.Tempo[int(at/r.TempoSeconds)]
		if math.Abs(got-want) > 0.06*want {
			t.Errorf("tempo at %.0fs = %.2f BPM, want about %.2f", at, got, want)
		}
	}

	// And the reported headline figure should be the middle of the ramp.
	if want := 120.0; math.Abs(r.BPM-want) > 6 {
		t.Errorf("BPM = %.2f, want about %v for a 100-140 ramp", r.BPM, want)
	}
}

// TestBeatsFollowARamp is what the curve is for: the beats themselves should
// stay on the clicks all the way through, not just at whatever tempo the
// middle of the track happens to run at.
func TestBeatsFollowARamp(t *testing.T) {
	const duration = 60.0
	pcm, want := rampPCM(duration, 100, 140)

	r, err := beats.Detect(pcm, nil)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	var worst float64
	var worstAt float64
	for _, got := range r.Times {
		nearest := math.Inf(1)
		for _, w := range want {
			nearest = math.Min(nearest, math.Abs(got-w))
		}
		if nearest > worst {
			worst, worstAt = nearest, got
		}
	}
	// The same 50 ms allowed for a steady click track: a dB-domain flux
	// leads the attack by about a quarter window.
	if worst > 0.05 {
		t.Errorf("worst beat is %.3fs from a click (at %.2fs); the grid is not following the ramp", worst, worstAt)
	}
	if len(r.Times) < len(want)-6 {
		t.Errorf("found %d beats, want about %d", len(r.Times), len(want))
	}
}

// TestTempoStaysSteadyOnASteadyTrack is the other half: following the tempo
// must not mean inventing wobble that is not there.
func TestTempoStaysSteadyOnASteadyTrack(t *testing.T) {
	const bpm = 128.0
	samples := testaudio.ClickTrack(rampRate, 60, bpm)

	r, err := beats.Detect(&audio.PCM{Samples: samples, SampleRate: rampRate}, nil)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(r.Tempo) == 0 {
		t.Fatal("no tempo curve")
	}
	for i, got := range r.Tempo {
		if math.Abs(got-bpm) > 0.03*bpm {
			t.Fatalf("tempo at %.1fs = %.2f BPM, want a steady %v", float64(i)*r.TempoSeconds, got, bpm)
		}
	}
}

// TestTempoWillNotChangeMetricalLevel: the tempogram of a track whose second
// half is played at double speed still has to produce one coherent grid.
// Following the audio into double time would leave the cuts meaning a
// different thing in each half, so TempoDrift bounds the search.
func TestTempoWillNotChangeMetricalLevel(t *testing.T) {
	const half = 40.0
	slow := testaudio.ClickTrack(rampRate, half, 100)
	fast := testaudio.ClickTrack(rampRate, half, 200)

	r, err := beats.Detect(&audio.PCM{Samples: append(slow, fast...), SampleRate: rampRate}, nil)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range r.Tempo {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	if hi/lo > 1+2*beats.DefaultTempoDrift {
		t.Errorf("tempo ranged over %.2f-%.2f BPM; the drift band should have held it together", lo, hi)
	}
}

// TestFixedBPMSkipsTheTempogram: an explicit tempo means exactly that tempo,
// with no curve to follow.
func TestFixedBPMSkipsTheTempogram(t *testing.T) {
	pcm, _ := rampPCM(60, 100, 140)
	r, err := beats.Detect(pcm, &beats.Options{FixedBPM: 111})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if r.BPM != 111 {
		t.Errorf("BPM = %v, want the fixed 111", r.BPM)
	}
	if len(r.Tempo) != 0 {
		t.Errorf("got a %d-point tempo curve for a fixed tempo, want none", len(r.Tempo))
	}
}

// TestShortTrackFallsBackToOneTempo: below a few tempogram columns there is
// nothing a windowed analysis can say that a whole-track one cannot.
func TestShortTrackFallsBackToOneTempo(t *testing.T) {
	samples := testaudio.ClickTrack(rampRate, 8, 120)
	r, err := beats.Detect(&audio.PCM{Samples: samples, SampleRate: rampRate}, nil)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(r.Tempo) != 0 {
		t.Errorf("got a tempo curve for an 8-second track, want the global fallback")
	}
	if math.Abs(r.BPM-120) > 3 {
		t.Errorf("BPM = %.2f, want about 120", r.BPM)
	}
}
