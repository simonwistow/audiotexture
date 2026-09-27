// Package testaudio synthesises audio fixtures for tests.
//
// Tests generate their input rather than checking binary fixtures into the
// repository: a click track with a known tempo is both a smaller artifact and
// a stricter test, since the expected beat times are known exactly.
package testaudio

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
)

// ClickTrack returns mono samples containing a short percussive click at every
// beat for bpm over the given duration, over a quiet sine bed so the signal is
// not pure silence between clicks.
func ClickTrack(sampleRate int, duration, bpm float64) []float64 {
	n := int(duration * float64(sampleRate))
	out := make([]float64, n)

	// Quiet 220 Hz bed.
	for i := range out {
		out[i] = 0.02 * math.Sin(2*math.Pi*220*float64(i)/float64(sampleRate))
	}

	// Exponentially decaying noise burst at each beat.
	period := 60.0 / bpm
	decay := 0.012 * float64(sampleRate) // ~12 ms
	rng := newRNG(1)
	for t := 0.0; t < duration; t += period {
		start := int(t * float64(sampleRate))
		for j := 0; j < int(6*decay) && start+j < n; j++ {
			env := math.Exp(-float64(j) / decay)
			out[start+j] += 0.8 * env * (2*rng() - 1)
		}
	}

	for i, v := range out {
		out[i] = math.Max(-1, math.Min(1, v))
	}
	return out
}

// BeatTimes returns the exact click times ClickTrack used, for comparison.
func BeatTimes(duration, bpm float64) []float64 {
	var ts []float64
	period := 60.0 / bpm
	for t := 0.0; t < duration; t += period {
		ts = append(ts, t)
	}
	return ts
}

// WriteWAV writes samples as a 16-bit mono PCM WAV file and returns its path.
func WriteWAV(dir, name string, samples []float64, sampleRate int) (string, error) {
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	dataLen := len(samples) * 2
	hdr := make([]byte, 0, 44)
	le := binary.LittleEndian
	u32 := func(v uint32) { hdr = le.AppendUint32(hdr, v) }
	u16 := func(v uint16) { hdr = le.AppendUint16(hdr, v) }

	hdr = append(hdr, "RIFF"...)
	u32(uint32(36 + dataLen))
	hdr = append(hdr, "WAVEfmt "...)
	u32(16)                     // fmt chunk size
	u16(1)                      // PCM
	u16(1)                      // mono
	u32(uint32(sampleRate))     //
	u32(uint32(sampleRate * 2)) // byte rate
	u16(2)                      // block align
	u16(16)                     // bits per sample
	hdr = append(hdr, "data"...)
	u32(uint32(dataLen))
	if _, err := f.Write(hdr); err != nil {
		return "", err
	}

	buf := make([]byte, 0, dataLen)
	for _, s := range samples {
		buf = le.AppendUint16(buf, uint16(int16(math.Round(math.Max(-1, math.Min(1, s))*32767))))
	}
	if _, err := f.Write(buf); err != nil {
		return "", err
	}
	return path, nil
}

// newRNG returns a deterministic uniform [0,1) generator so fixtures are
// byte-identical between runs.
func newRNG(seed uint64) func() float64 {
	state := seed
	return func() float64 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return float64(state>>11) / float64(1<<53)
	}
}
