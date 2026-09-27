package audio_test

import (
	"math"
	"testing"

	"github.com/simonwistow/audiotexture/audio"
	"github.com/simonwistow/audiotexture/internal/testaudio"
)

func TestDecodeWAV(t *testing.T) {
	const (
		srcRate  = 44100
		duration = 4.0
		bpm      = 120.0
	)
	dir := t.TempDir()
	path, err := testaudio.WriteWAV(dir, "click.wav", testaudio.ClickTrack(srcRate, duration, bpm), srcRate)
	if err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	pcm, err := audio.Decode(path, 0)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if pcm.SampleRate != audio.DefaultSampleRate {
		t.Errorf("SampleRate = %d, want %d", pcm.SampleRate, audio.DefaultSampleRate)
	}
	// Resampling may drop or pad a few samples at the edges; a millisecond of
	// slack is well inside what matters for beat tracking.
	if got := pcm.Duration(); math.Abs(got-duration) > 0.01 {
		t.Errorf("Duration = %v, want %v (+/- 0.01)", got, duration)
	}
	if len(pcm.Samples) == 0 {
		t.Fatal("no samples decoded")
	}

	// The clicks should dominate: peak well above the 0.02 sine bed.
	var peak float64
	for _, s := range pcm.Samples {
		peak = math.Max(peak, math.Abs(s))
	}
	if peak < 0.2 {
		t.Errorf("peak amplitude = %v, want the clicks to survive decoding", peak)
	}
}

func TestDecodeExplicitSampleRate(t *testing.T) {
	const srcRate = 44100
	dir := t.TempDir()
	path, err := testaudio.WriteWAV(dir, "click.wav", testaudio.ClickTrack(srcRate, 2.0, 120), srcRate)
	if err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	pcm, err := audio.Decode(path, 8000)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if pcm.SampleRate != 8000 {
		t.Errorf("SampleRate = %d, want 8000", pcm.SampleRate)
	}
	if math.Abs(pcm.Duration()-2.0) > 0.01 {
		t.Errorf("Duration = %v, want 2.0", pcm.Duration())
	}
}

func TestDecodeMissingFile(t *testing.T) {
	if _, err := audio.Decode("/nonexistent/nope.mp3", 0); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
