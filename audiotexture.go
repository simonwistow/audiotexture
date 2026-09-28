// Package audiotexture generates beat-synced slideshow movies from a
// directory of images and an audio track.
//
// Beats are detected locally and the movie is encoded in-process through
// libav*: there is no external API and no ffmpeg subprocess.
//
// The four stages are separate packages and each is usable on its own:
//
//	audio    decode any FFmpeg-supported audio file to mono PCM
//	beats    onset detection, tempo estimation and beat tracking
//	texture  assign images to onset times, with pluggable algorithms
//	video    encode the assignment and the soundtrack into a movie
//
// Generate is the whole pipeline in one call. Reach for the packages directly
// when you want to reuse an analysis, supply your own beat times, or plug in
// an algorithm of your own.
package audiotexture

import (
	"fmt"

	"github.com/simonwistow/audiotexture/audio"
	"github.com/simonwistow/audiotexture/beats"
	"github.com/simonwistow/audiotexture/images"
	"github.com/simonwistow/audiotexture/texture"
	"github.com/simonwistow/audiotexture/video"
)

// Options configures Generate. The zero value is usable.
type Options struct {
	// Algorithm names a registered texture algorithm. Defaults to "legacy",
	// the behaviour of the original.
	Algorithm string

	// Beats, if non-nil, is used instead of analysing the audio. Useful for
	// reproducing a render from beat times captured elsewhere.
	Beats []float64

	// Duration overrides the decoded audio length as the span the images are
	// spread across. Set it when reproducing a render whose beat times came
	// with a duration of their own, since spacing is computed from it and a
	// difference of a few tens of milliseconds moves every onset.
	Duration float64

	// BeatOptions tunes detection when Beats is nil.
	BeatOptions *beats.Options

	// Video configures the output file.
	Video video.Options

	// Reproduce2010 replays the original Perl's frame loop, including its
	// bugs, so that a render matches the 2010 videos frame for frame. See
	// texture.LegacyFrameSequence. Only meaningful with Algorithm "legacy".
	Reproduce2010 bool
}

// Result reports what Generate did.
type Result struct {
	Images   int
	Duration float64
	BPM      float64
	Beats    []float64
	Onsets   []texture.Onset
}

// Generate reads the images in imagesDir and the soundtrack at audioPath,
// works out when each image should appear, and writes a movie to outPath.
func Generate(imagesDir, audioPath, outPath string, opts Options) (*Result, error) {
	if opts.Algorithm == "" {
		opts.Algorithm = "legacy"
	}
	algo, err := texture.Get(opts.Algorithm)
	if err != nil {
		return nil, err
	}

	imgs, err := images.Load(imagesDir)
	if err != nil {
		return nil, err
	}

	pcm, err := audio.Decode(audioPath, 0)
	if err != nil {
		return nil, err
	}
	duration := pcm.Duration()
	if opts.Duration > 0 {
		duration = opts.Duration
	}

	detected := &beats.Result{Times: opts.Beats}
	if opts.Beats == nil {
		if detected, err = beats.Detect(pcm, opts.BeatOptions); err != nil {
			return nil, fmt.Errorf("detecting beats: %w", err)
		}
	}

	frameRate := opts.Video.FrameRate
	if frameRate <= 0 {
		frameRate = video.DefaultFrameRate
	}

	onsets, err := algo.Assign(texture.Input{
		Images:    imgs,
		Beats:     detected.Times,
		Duration:  duration,
		FrameRate: frameRate,
		Strength:  detected.Strength,
		Novelty:   detected.NoveltyAt,
	})
	if err != nil {
		return nil, fmt.Errorf("running algorithm %q: %w", opts.Algorithm, err)
	}

	if opts.Reproduce2010 {
		onsets, duration = texture.LegacyFrameSequence(onsets, duration, frameRate)
	}

	if err := video.Encode(outPath, onsets, audioPath, duration, opts.Video); err != nil {
		return nil, err
	}

	return &Result{
		Images:   len(imgs),
		Duration: duration,
		BPM:      detected.BPM,
		Beats:    detected.Times,
		Onsets:   onsets,
	}, nil
}
