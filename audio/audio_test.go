package audio_test

import (
	"math"
	"os"
	"path/filepath"
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

// TestDecodeSkipsCorruptPackets checks that one unreadable frame does not
// abandon the whole file.
//
// This is not hypothetical: a 2010-era MP3 in the archive this was built
// against contains a malformed frame, and ffmpeg itself logs "Error
// submitting packet to decoder" and carries on. Aborting there meant a
// four-minute track failed to render over one bad frame.
func TestDecodeSkipsCorruptPackets(t *testing.T) {
	const srcRate = 44100
	dir := t.TempDir()
	path, err := testaudio.WriteWAV(dir, "click.wav", testaudio.ClickTrack(srcRate, 3.0, 120), srcRate)
	if err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	// Corrupt a run of bytes in the middle of the sample data, past the
	// 44-byte header.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	clean, err := audio.Decode(path, 0)
	if err != nil {
		t.Fatalf("Decode of the intact file: %v", err)
	}

	for i := len(raw) / 2; i < len(raw)/2+512 && i < len(raw); i++ {
		raw[i] = 0xFF
	}
	damaged := filepath.Join(dir, "damaged.wav")
	if err := os.WriteFile(damaged, raw, 0o644); err != nil {
		t.Fatalf("writing damaged fixture: %v", err)
	}

	got, err := audio.Decode(damaged, 0)
	if err != nil {
		t.Fatalf("Decode of the damaged file: %v", err)
	}
	// Still essentially the whole track, not a truncated fragment.
	if ratio := float64(len(got.Samples)) / float64(len(clean.Samples)); ratio < 0.9 {
		t.Errorf("decoded %.1f%% of the samples the intact file gave, want most of them", 100*ratio)
	}
}

// TestDecodeRejectsUndecodableFile checks the skip does not paper over a file
// with nothing readable in it at all.
func TestDecodeRejectsUndecodableFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "junk.wav")
	if err := os.WriteFile(p, []byte("RIFF....WAVEfmt junk junk junk"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	if _, err := audio.Decode(p, 0); err == nil {
		t.Error("expected an error for a file with no decodable audio")
	}
}
