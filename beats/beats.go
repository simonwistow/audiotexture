// Package beats detects beat times in decoded audio.
//
// The implementation follows Daniel Ellis, "Beat Tracking by Dynamic
// Programming", Journal of New Music Research 36(1), 2007:
// https://www.ee.columbia.edu/~dpwe/pubs/Ellis07-beattrack.pdf
//
// There are three stages. A perceptually weighted spectral flux "onset
// strength" envelope says how much new energy appears at each instant. Its
// autocorrelation, weighted by a prior over plausible tempos, gives a single
// global tempo. A dynamic program then picks the beat sequence maximising the
// onset strength landed on, minus a penalty for straying from that tempo.
//
// Doing the last step as a DP rather than greedily is the whole point: it
// finds the globally best sequence, so the beat grid holds its place through a
// quiet passage instead of latching onto whatever transient happens to be
// nearby.
package beats

import (
	"fmt"

	"github.com/simonwistow/audiotexture/audio"
)

// Defaults for Options. The STFT settings match librosa's, which makes the
// envelope directly comparable when cross-checking against a reference
// implementation.
const (
	DefaultWindowSize  = 2048
	DefaultHopSize     = 512
	DefaultMelBands    = 128
	DefaultMinFreq     = 0.0
	DefaultMaxFreq     = 11025.0
	DefaultStartBPM    = 120.0
	DefaultTempoSpread = 1.0 // octaves, standard deviation of the tempo prior
	DefaultTightness   = 100.0
	DefaultMinBPM      = 40.0
	DefaultMaxBPM      = 240.0

	// Tempo tracking. A window has to hold several beats to correlate at
	// all, but the longer it is the more a real tempo change is smeared
	// across the columns either side of it.
	DefaultTempoWindow  = 8.0   // seconds of envelope per tempogram column
	DefaultTempoStep    = 0.5   // seconds between tempogram columns
	DefaultTempoInertia = 400.0 // cost of changing tempo, per squared octave
	DefaultTempoDrift   = 0.25  // how far the tempo may stray from the track's own
)

// Options tunes detection. The zero value is usable: every field falls back to
// the corresponding Default* constant.
type Options struct {
	// WindowSize and HopSize are the STFT frame and advance, in samples.
	// HopSize sets the resolution of the whole thing: 512 samples at 22050 Hz
	// is 23 ms, which is finer than the ear's sense of beat position.
	WindowSize int
	HopSize    int

	// MelBands, MinFreq and MaxFreq shape the mel filterbank.
	MelBands         int
	MinFreq, MaxFreq float64

	// StartBPM is the centre of the tempo prior and TempoSpread its standard
	// deviation in octaves. Widen the spread to trust the audio more and the
	// prior less.
	StartBPM    float64
	TempoSpread float64

	// MinBPM and MaxBPM bound the tempo search.
	MinBPM, MaxBPM float64

	// TempoWindow and TempoStep set the tempogram: how much onset envelope
	// each column autocorrelates and how far apart the columns sit, both in
	// seconds. TempoInertia is what it costs to change tempo between two
	// columns, per squared octave -- raise it to insist on a steadier tempo,
	// lower it to follow a track that really does speed up and slow down.
	TempoWindow, TempoStep, TempoInertia float64

	// TempoDrift bounds how far the tracked tempo may stray from the one
	// tempo found for the track as a whole, as a fraction: 0.25 allows a
	// quarter either way. It is what stops the tracker following a piece
	// into half or double time and leaving a beat grid that means something
	// different in each section.
	TempoDrift float64

	// Tightness is how strongly the tracker insists on an even spacing.
	// Higher keeps a stricter grid; lower follows the audio more closely.
	Tightness float64

	// FixedBPM, if positive, skips tempo estimation and uses this tempo.
	FixedBPM float64
}

func (o *Options) applyDefaults() {
	if o.WindowSize <= 0 {
		o.WindowSize = DefaultWindowSize
	}
	o.WindowSize = nextPow2(o.WindowSize)
	if o.HopSize <= 0 {
		o.HopSize = DefaultHopSize
	}
	if o.MelBands <= 0 {
		o.MelBands = DefaultMelBands
	}
	if o.MaxFreq <= 0 {
		o.MaxFreq = DefaultMaxFreq
	}
	if o.StartBPM <= 0 {
		o.StartBPM = DefaultStartBPM
	}
	if o.TempoSpread <= 0 {
		o.TempoSpread = DefaultTempoSpread
	}
	if o.MinBPM <= 0 {
		o.MinBPM = DefaultMinBPM
	}
	if o.MaxBPM <= 0 {
		o.MaxBPM = DefaultMaxBPM
	}
	if o.Tightness <= 0 {
		o.Tightness = DefaultTightness
	}
	if o.TempoWindow <= 0 {
		o.TempoWindow = DefaultTempoWindow
	}
	if o.TempoStep <= 0 {
		o.TempoStep = DefaultTempoStep
	}
	if o.TempoInertia <= 0 {
		o.TempoInertia = DefaultTempoInertia
	}
	if o.TempoDrift <= 0 {
		o.TempoDrift = DefaultTempoDrift
	}
}

// Result is what Detect found.
type Result struct {
	// BPM is the estimated tempo of the track as a whole: the median of
	// Tempo when there is one, since a few columns over an intro or a
	// breakdown should not move the headline figure.
	BPM float64

	// Tempo is the tracked tempo in BPM, one value per TempoSeconds. Empty
	// when the tempo was fixed, or the track too short to window, in which
	// case BPM holds for the whole of it.
	Tempo []float64
	// TempoSeconds is the time between consecutive Tempo values.
	TempoSeconds float64
	// Times are the detected beat instants, in seconds, ascending.
	Times []float64
	// Onset is the onset strength envelope, one value per hop. Exposed
	// because the texture algorithms can weight beats by it.
	Onset []float64
	// HopSeconds is the time between consecutive Onset values. Zero when the
	// beats came from a file rather than from analysis.
	HopSeconds float64
	// Duration is the track length in seconds, when the source reported one.
	// Detect leaves this zero; the caller already knows it from the audio.
	Duration float64

	// Novelty is the audio novelty curve: how much the music changes
	// character at each instant, in [0, 1]. Empty when the beats came from a
	// file rather than from analysis. Entry i covers the span
	// [i*NoveltySeconds, (i+1)*NoveltySeconds), so it describes the instant
	// at its centre.
	Novelty []float64
	// NoveltySeconds is the span each Novelty value covers.
	NoveltySeconds float64
}

// Strength returns the onset strength at time t in seconds, or 0 if t falls
// outside the analysed range.
func (r *Result) Strength(t float64) float64 {
	if r.HopSeconds <= 0 || len(r.Onset) == 0 {
		return 0
	}
	i := int(t/r.HopSeconds + 0.5)
	if i < 0 || i >= len(r.Onset) {
		return 0
	}
	return r.Onset[i]
}

// Detect finds the beats in pcm. Pass nil opts for the defaults.
func Detect(pcm *audio.PCM, opts *Options) (*Result, error) {
	if pcm == nil || len(pcm.Samples) == 0 {
		return nil, fmt.Errorf("no audio to analyse")
	}
	if pcm.SampleRate <= 0 {
		return nil, fmt.Errorf("invalid sample rate %d", pcm.SampleRate)
	}

	o := Options{}
	if opts != nil {
		o = *opts
	}
	o.applyDefaults()

	if len(pcm.Samples) < o.WindowSize {
		return nil, fmt.Errorf("audio is shorter than one analysis window (%d samples)", o.WindowSize)
	}

	env, spec := onsetStrength(pcm.Samples, pcm.SampleRate, &o)
	if len(env) < 2 {
		return nil, fmt.Errorf("audio is too short to analyse")
	}
	hopSeconds := float64(o.HopSize) / float64(pcm.SampleRate)

	periods, curve, curveSeconds := tempoPath(env, hopSeconds, &o)
	bpm := 60.0 / (medianPeriod(periods) * hopSeconds)
	frames := trackBeats(env, periods, o.Tightness)

	times := make([]float64, len(frames))
	for i, f := range frames {
		times[i] = float64(f) * hopSeconds
	}

	return &Result{
		BPM:            bpm,
		Tempo:          curve,
		TempoSeconds:   curveSeconds,
		Times:          times,
		Onset:          env,
		HopSeconds:     hopSeconds,
		Novelty:        novelty(spec, hopSeconds),
		NoveltySeconds: noveltyHopSeconds,
	}, nil
}

// NoveltyAt returns the audio novelty at time t in seconds, in [0, 1], or 0
// when no novelty curve is available.
//
// Each novelty value summarises a whole NoveltySeconds-long span rather than
// an instant, so the lookup takes the span containing t. Rounding to the
// nearest index instead would bias every reported boundary half a span early.
func (r *Result) NoveltyAt(t float64) float64 {
	if r.NoveltySeconds <= 0 || len(r.Novelty) == 0 || t < 0 {
		return 0
	}
	i := int(t / r.NoveltySeconds)
	if i >= len(r.Novelty) {
		return 0
	}
	return r.Novelty[i]
}

// NoveltyTime returns the instant novelty index i describes: the centre of the
// span it covers.
func (r *Result) NoveltyTime(i int) float64 {
	return (float64(i) + 0.5) * r.NoveltySeconds
}
