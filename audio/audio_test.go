package audio_test

import (
	"bytes"
	"io"
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
	wav := testaudio.WAV(testaudio.ClickTrack(srcRate, duration, bpm), srcRate)

	pcm, err := audio.Decode(bytes.NewReader(wav), 0)
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
	wav := testaudio.WAV(testaudio.ClickTrack(srcRate, 2.0, 120), srcRate)

	pcm, err := audio.Decode(bytes.NewReader(wav), 8000)
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

func TestDecodeEmpty(t *testing.T) {
	if _, err := audio.Decode(bytes.NewReader(nil), 0); err == nil {
		t.Fatal("expected an error for empty input")
	}
}

// TestDecodeRewinds checks that the whole stream is decoded wherever the
// reader happens to be positioned, since libav* addresses it from its start.
func TestDecodeRewinds(t *testing.T) {
	r := bytes.NewReader(testaudio.WAV(testaudio.ClickTrack(44100, 2.0, 120), 44100))
	if _, err := r.Seek(1000, io.SeekStart); err != nil {
		t.Fatalf("seeking: %v", err)
	}
	pcm, err := audio.Decode(r, 0)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if math.Abs(pcm.Duration()-2.0) > 0.01 {
		t.Errorf("Duration = %v, want 2.0", pcm.Duration())
	}
}

// TestDecodeDataWithEOF covers a reader that returns its last bytes together
// with io.EOF, which io.Reader allows. Taking the EOF at face value would
// drop the bytes that came with it.
func TestDecodeDataWithEOF(t *testing.T) {
	wav := testaudio.WAV(testaudio.ClickTrack(44100, 2.0, 120), 44100)
	want, err := audio.Decode(bytes.NewReader(wav), 0)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got, err := audio.Decode(dataErrReadSeeker{bytes.NewReader(wav)}, 0)
	if err != nil {
		t.Fatalf("Decode through a data-with-EOF reader: %v", err)
	}
	if len(got.Samples) != len(want.Samples) {
		t.Errorf("decoded %d samples, want %d", len(got.Samples), len(want.Samples))
	}
}

// dataErrReadSeeker returns the final read's data together with io.EOF, as
// iotest.DataErrReader does, but stays seekable.
type dataErrReadSeeker struct{ *bytes.Reader }

func (r dataErrReadSeeker) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == nil && r.Len() == 0 {
		err = io.EOF
	}
	return n, err
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
	raw := testaudio.WAV(testaudio.ClickTrack(srcRate, 3.0, 120), srcRate)

	clean, err := audio.Decode(bytes.NewReader(raw), 0)
	if err != nil {
		t.Fatalf("Decode of the intact file: %v", err)
	}

	// Corrupt a run of bytes in the middle of the sample data, past the
	// 44-byte header.
	for i := len(raw) / 2; i < len(raw)/2+512 && i < len(raw); i++ {
		raw[i] = 0xFF
	}

	got, err := audio.Decode(bytes.NewReader(raw), 0)
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
	junk := []byte("RIFF....WAVEfmt junk junk junk")
	if _, err := audio.Decode(bytes.NewReader(junk), 0); err == nil {
		t.Error("expected an error for a file with no decodable audio")
	}
}
