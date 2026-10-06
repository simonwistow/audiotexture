package audiotexture_test

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/simonwistow/audiotexture"
	"github.com/simonwistow/audiotexture/images"
	"github.com/simonwistow/audiotexture/internal/testaudio"
	"github.com/simonwistow/audiotexture/texture"
	"github.com/simonwistow/audiotexture/video"
)

// slideshowFixture writes four images and a soundtrack into dir, standing in
// for a directory of photographs and a track.
func slideshowFixture(dir string) (imagesDir, audioPath string) {
	imagesDir = filepath.Join(dir, "pix")
	if err := os.Mkdir(imagesDir, 0o755); err != nil {
		log.Fatal(err)
	}
	shades := []color.RGBA{{R: 200, A: 255}, {G: 200, A: 255}, {B: 200, A: 255}, {R: 200, G: 200, A: 255}}
	for i, c := range shades {
		img := image.NewRGBA(image.Rect(0, 0, 64, 48))
		for y := range 48 {
			for x := range 64 {
				img.SetRGBA(x, y, c)
			}
		}
		// Lexical filename order is the running order, so pad the numbers.
		f, err := os.Create(filepath.Join(imagesDir, fmt.Sprintf("%03d.png", i+1)))
		if err != nil {
			log.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			log.Fatal(err)
		}
		f.Close()
	}

	audioPath, err := testaudio.WriteWAV(dir, "track.wav", testaudio.ClickTrack(44100, 8, 120), 44100)
	if err != nil {
		log.Fatal(err)
	}
	return imagesDir, audioPath
}

// Generate is the whole pipeline in one call: read the images, analyse the
// track, decide when each image appears, and write the movie. It works on
// interfaces, so the images, the soundtrack and the movie can each live
// anywhere; here they are ordinary files.
func ExampleGenerate() {
	dir, err := os.MkdirTemp("", "audiotexture")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	imagesDir, audioPath := slideshowFixture(dir)

	// Any fs.FS will do: os.DirFS, an embed.FS, a zip.Reader.
	imgs, err := images.FromFS(os.DirFS(imagesDir))
	if err != nil {
		log.Fatal(err)
	}
	// Any io.ReadSeeker.
	track, err := os.Open(audioPath)
	if err != nil {
		log.Fatal(err)
	}
	defer track.Close()
	// Any io.WriteSeeker.
	out, err := os.Create(filepath.Join(dir, "out.mp4"))
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	res, err := audiotexture.Generate(imgs, track, out, audiotexture.Options{
		Algorithm: texture.Bars,
		Video: video.Options{
			Width:  320,
			Height: 240,
			Preset: "ultrafast",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%d images over %.1fs at %.0f BPM\n", res.Images, res.Duration, res.BPM)
	for _, o := range res.Onsets {
		fmt.Printf("  %.2fs %s\n", o.Start, o.Image)
	}
	// Output:
	// 4 images over 8.0s at 120 BPM
	//   0.00s 001.png
	//   1.97s 002.png
	//   3.97s 003.png
	//   5.97s 004.png
}

// GenerateFiles takes paths instead, and the zero Options is usable, so the
// shortest form supplies only the three paths. That gives texture.Optimal at
// the default 1280x720 and 24 fps, in the container the output's extension
// names.
func ExampleGenerateFiles() {
	dir, err := os.MkdirTemp("", "audiotexture")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	imagesDir, audioPath := slideshowFixture(dir)

	// Kept small and fast here; drop the Video options entirely for 720p.
	res, err := audiotexture.GenerateFiles(imagesDir, audioPath, filepath.Join(dir, "out.mp4"),
		audiotexture.Options{Video: video.Options{Width: 320, Height: 240, Preset: "ultrafast"}})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%d onsets, first at %.2fs\n", len(res.Onsets), res.Onsets[0].Start)
	// Output:
	// 4 onsets, first at 0.00s
}
