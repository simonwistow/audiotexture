package main

import (
	"flag"
	"fmt"
	"os"

	"audiotexture/internal/images"
	"audiotexture/internal/render"
	"audiotexture/internal/texture"
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
			fmt.Println(name)
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
  audiotexture generate --images <dir> --out <dir> --duration <seconds> [flags]
  audiotexture list-algorithms

generate flags:`)
	flag.CommandLine.PrintDefaults()
}

func runGenerate(args []string) error {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	imagesDir := fs.String("images", "", "directory of source images (required)")
	outDir := fs.String("out", "", "output directory for rendered frames (required)")
	duration := fs.Float64("duration", 0, "audio duration in seconds (required; stand-in until audio decoding lands)")
	framerate := fs.Float64("framerate", 24, "output frame rate")
	algorithm := fs.String("algorithm", "even", "texture algorithm to use (see list-algorithms)")
	fs.Parse(args)

	if *imagesDir == "" || *outDir == "" || *duration <= 0 {
		fs.PrintDefaults()
		return fmt.Errorf("--images, --out, and a positive --duration are required")
	}

	algo, err := texture.Get(*algorithm)
	if err != nil {
		return err
	}

	imgs, err := images.Load(*imagesDir)
	if err != nil {
		return err
	}

	onsets, err := algo.Assign(imgs, nil, *duration)
	if err != nil {
		return fmt.Errorf("running algorithm %q: %w", *algorithm, err)
	}

	n, err := render.FrameDirectory(*outDir, onsets, *duration, *framerate)
	if err != nil {
		return err
	}

	fmt.Printf("Found %d images. Wrote %d frames to %s at %g fps.\n", len(imgs), n, *outDir, *framerate)
	return nil
}
