// Command audiotexture builds a beat-synced slideshow movie from a directory
// of images and an audio track.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/asticode/go-astiav"

	"github.com/simonwistow/audiotexture/audio"
	"github.com/simonwistow/audiotexture/beats"
	"github.com/simonwistow/audiotexture/images"
	"github.com/simonwistow/audiotexture/texture"
	"github.com/simonwistow/audiotexture/video"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "generate":
		err = runGenerate(os.Args[2:])
	case "analyse", "analyze":
		err = runAnalyse(os.Args[2:])
	case "list-algorithms":
		for _, name := range texture.List() {
			fmt.Printf("%-8s %s\n", name, texture.Describe(name))
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `audiotexture - beat-synced slideshow generator

Usage:
  audiotexture generate --images <dir> --audio <file> --out <file.mp4> [flags]
  audiotexture analyse --audio <file>
  audiotexture list-algorithms

Images are used in lexical filename order.`)
}

func runGenerate(args []string) error {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	imagesDir := fs.String("images", "", "directory of source images (required)")
	audioPath := fs.String("audio", "", "soundtrack: mp3, m4a, flac, ogg, wav, ... (required)")
	outPath := fs.String("out", "", "output movie file (required)")
	algorithm := fs.String("algorithm", "even", "texture algorithm (see list-algorithms)")
	framerate := fs.Float64("framerate", video.DefaultFrameRate, "output frame rate")
	width := fs.Int("width", video.DefaultWidth, "output width in pixels")
	height := fs.Int("height", video.DefaultHeight, "output height in pixels")
	crf := fs.Int("crf", video.DefaultCRF, "x264 quality: lower is better, 18-24 is sane")
	preset := fs.String("preset", video.DefaultPreset, "x264 preset: ultrafast ... veryslow")
	bpm := fs.Float64("bpm", 0, "override beat detection with a fixed tempo")
	startBPM := fs.Float64("start-bpm", beats.DefaultStartBPM, "centre of the tempo prior")
	tightness := fs.Float64("tightness", beats.DefaultTightness, "how strictly to hold an even beat grid")
	quiet := fs.Bool("quiet", false, "suppress progress output")
	verbose := fs.Bool("verbose", false, "show FFmpeg's own logging")
	fs.Usage = func() {
		usage()
		fmt.Fprintln(os.Stderr, "\ngenerate flags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *imagesDir == "" || *audioPath == "" || *outPath == "" {
		fs.Usage()
		return fmt.Errorf("--images, --audio and --out are all required")
	}

	// libav* logs to stderr by default, including harmless complaints about
	// MP3 gapless-playback padding. Stay quiet unless asked.
	if *verbose {
		astiav.SetLogLevel(astiav.LogLevelInfo)
	} else {
		astiav.SetLogLevel(astiav.LogLevelQuiet)
	}

	algo, err := texture.Get(*algorithm)
	if err != nil {
		return err
	}

	imgs, err := images.Load(*imagesDir)
	if err != nil {
		return err
	}

	pcm, err := audio.Decode(*audioPath, 0)
	if err != nil {
		return err
	}
	duration := pcm.Duration()

	detected, err := beats.Detect(pcm, &beats.Options{
		FixedBPM:  *bpm,
		StartBPM:  *startBPM,
		Tightness: *tightness,
	})
	if err != nil {
		return fmt.Errorf("detecting beats: %w", err)
	}

	onsets, err := algo.Assign(texture.Input{
		Images:    imgs,
		Beats:     detected.Times,
		Duration:  duration,
		FrameRate: *framerate,
		Strength:  detected.Strength,
	})
	if err != nil {
		return fmt.Errorf("running algorithm %q: %w", *algorithm, err)
	}

	if !*quiet {
		fmt.Printf("%d images, %s of audio, %d beats at %.1f BPM, %s algorithm\n",
			len(imgs), formatDuration(duration), len(detected.Times), detected.BPM, *algorithm)
	}

	opts := video.Options{
		Width:     *width,
		Height:    *height,
		FrameRate: *framerate,
		CRF:       *crf,
		Preset:    *preset,
	}
	if !*quiet && isTerminal(os.Stdout) {
		opts.Progress = progressBar()
	}

	if err := video.Encode(*outPath, onsets, *audioPath, duration, opts); err != nil {
		return err
	}

	if !*quiet {
		if opts.Progress != nil {
			fmt.Println()
		}
		fmt.Printf("Wrote %s\n", *outPath)
	}
	return nil
}

func runAnalyse(args []string) error {
	fs := flag.NewFlagSet("analyse", flag.ExitOnError)
	audioPath := fs.String("audio", "", "audio file to analyse (required)")
	bpm := fs.Float64("bpm", 0, "override beat detection with a fixed tempo")
	startBPM := fs.Float64("start-bpm", beats.DefaultStartBPM, "centre of the tempo prior")
	tightness := fs.Float64("tightness", beats.DefaultTightness, "how strictly to hold an even beat grid")
	times := fs.Bool("times", false, "print every beat time, one per line")
	verbose := fs.Bool("verbose", false, "show FFmpeg's own logging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *audioPath == "" {
		fs.PrintDefaults()
		return fmt.Errorf("--audio is required")
	}

	if *verbose {
		astiav.SetLogLevel(astiav.LogLevelInfo)
	} else {
		astiav.SetLogLevel(astiav.LogLevelQuiet)
	}

	pcm, err := audio.Decode(*audioPath, 0)
	if err != nil {
		return err
	}
	detected, err := beats.Detect(pcm, &beats.Options{
		FixedBPM:  *bpm,
		StartBPM:  *startBPM,
		Tightness: *tightness,
	})
	if err != nil {
		return err
	}

	fmt.Printf("duration %s\n", formatDuration(pcm.Duration()))
	fmt.Printf("tempo    %.2f BPM\n", detected.BPM)
	fmt.Printf("beats    %d\n", len(detected.Times))
	if *times {
		for _, t := range detected.Times {
			fmt.Printf("%.4f\n", t)
		}
	}
	return nil
}

// isTerminal reports whether f is a character device, so the redrawing
// progress bar does not spam a log file or a pipe with carriage returns.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// progressBar returns a Progress callback that redraws a single line.
func progressBar() func(frame, total int) {
	const width = 40
	last := -1
	return func(frame, total int) {
		if total <= 0 {
			return
		}
		pct := frame * 100 / total
		if pct == last && frame != total {
			return
		}
		last = pct
		filled := pct * width / 100
		fmt.Printf("\r[%s%s] %3d%% (%d/%d)",
			strings.Repeat("#", filled), strings.Repeat(" ", width-filled), pct, frame, total)
	}
}

func formatDuration(seconds float64) string {
	m := int(seconds) / 60
	s := seconds - float64(m*60)
	if m > 0 {
		return fmt.Sprintf("%dm%04.1fs", m, s)
	}
	return fmt.Sprintf("%.1fs", s)
}
