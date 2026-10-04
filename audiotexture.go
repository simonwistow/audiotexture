// Package audiotexture generates beat-synced slideshow movies from a set of
// images and an audio track.
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
// Generate is the whole pipeline in one call, reading from and writing to
// interfaces; GenerateFiles is the same thing for paths on disk. Reach for the
// packages directly when you want to reuse an analysis, supply your own beat
// times, or plug in an algorithm of your own.
package audiotexture

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/simonwistow/audiotexture/audio"
	"github.com/simonwistow/audiotexture/beats"
	"github.com/simonwistow/audiotexture/images"
	"github.com/simonwistow/audiotexture/texture"
	"github.com/simonwistow/audiotexture/video"
)

// Options configures Generate. The zero value is usable.
type Options struct {
	// Algorithm decides when each image appears: one of the built-ins such
	// as texture.Bars, or any texture.Algorithm of your own. Defaults to
	// texture.Legacy, the behaviour of the original. To choose one by name,
	// as a command line does, look it up with texture.Get.
	Algorithm texture.Algorithm

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

	// Video configures the output, including its container format.
	Video video.Options

	// Reproduce2010 replays the original Perl's frame loop, including its
	// bugs, so that a render matches the 2010 videos frame for frame. See
	// texture.LegacyFrameSequence. Only meaningful with texture.Legacy.
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

// Generate works out when each of imgs should appear against the soundtrack
// in track, and writes the movie to out.
//
// track is read twice, once to find the beats and once to encode it, and each
// time in full from its start. out is written from its current position; see
// video.Encode for what it needs and why an *os.File does best.
func Generate(imgs images.Images, track io.ReadSeeker, out io.WriteSeeker, opts Options) (*Result, error) {
	if imgs == nil || track == nil || out == nil {
		return nil, errors.New("images, soundtrack and output are all required")
	}
	algo := opts.Algorithm
	if algo == nil {
		algo = texture.Legacy
	}

	names := imgs.Names()
	if len(names) == 0 {
		return nil, errors.New("no images")
	}

	pcm, err := audio.Decode(track, 0)
	if err != nil {
		return nil, fmt.Errorf("decoding soundtrack: %w", err)
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
		Images:    names,
		Beats:     detected.Times,
		Duration:  duration,
		FrameRate: frameRate,
		Strength:  detected.Strength,
		Novelty:   detected.NoveltyAt,
	})
	if err != nil {
		return nil, fmt.Errorf("running algorithm %s: %w", algorithmName(algo), err)
	}

	if opts.Reproduce2010 {
		onsets, duration = texture.LegacyFrameSequence(onsets, duration, frameRate)
	}

	if err := video.Encode(out, onsets, imgs, track, duration, opts.Video); err != nil {
		return nil, err
	}

	return &Result{
		Images:   len(names),
		Duration: duration,
		BPM:      detected.BPM,
		Beats:    detected.Times,
		Onsets:   onsets,
	}, nil
}

// GenerateFiles is Generate for paths: the images in the directory imagesDir,
// in lexical filename order, the soundtrack at audioPath, and the movie
// written to a new file at outPath. Unless opts.Video.Format says otherwise,
// the container follows outPath's extension, as video.FormatFor reports it.
// Nothing is left at outPath if it fails.
func GenerateFiles(imagesDir, audioPath, outPath string, opts Options) (res *Result, err error) {
	if opts.Video.Format == "" {
		if opts.Video.Format, err = video.FormatFor(outPath); err != nil {
			return nil, err
		}
	}

	imgs, err := images.FromDir(imagesDir)
	if err != nil {
		return nil, err
	}

	track, err := os.Open(audioPath)
	if err != nil {
		return nil, err
	}
	defer track.Close()

	out, err := os.Create(outPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := out.Close(); err == nil && cerr != nil {
			res, err = nil, cerr
		}
		if err != nil {
			os.Remove(outPath)
		}
	}()

	return Generate(imgs, track, out, opts)
}

// algorithmName is how errors refer to a: its name if it has one, as the
// built-ins do, and otherwise its type.
func algorithmName(a texture.Algorithm) string {
	if s, ok := a.(fmt.Stringer); ok {
		return s.String()
	}
	return fmt.Sprintf("%T", a)
}
