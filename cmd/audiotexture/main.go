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
	case "list-algorithms":
		for _, name := range texture.List() {
			fmt.Printf("%-10s %s\n", name, texture.Describe(name))
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

	onsets, err := algo.Assign(imgs, nil, duration)
	if err != nil {
		return fmt.Errorf("running algorithm %q: %w", *algorithm, err)
	}

	if !*quiet {
		fmt.Printf("%d images, %s of audio, %s algorithm\n",
			len(imgs), formatDuration(duration), *algorithm)
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
