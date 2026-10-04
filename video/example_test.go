package video_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/simonwistow/audiotexture/images"
	"github.com/simonwistow/audiotexture/internal/testaudio"
	"github.com/simonwistow/audiotexture/texture"
	"github.com/simonwistow/audiotexture/video"
)

func writeSolidPNG(path string, c color.RGBA) error {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// Encode takes an assignment of images to instants and writes a movie. Nothing
// about it depends on how the onsets were chosen, so you can hand it a list you
// built yourself rather than one an Algorithm produced.
func ExampleEncode() {
	dir, err := os.MkdirTemp("", "video")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	for i, c := range []color.RGBA{{R: 220, A: 255}, {G: 220, A: 255}, {B: 220, A: 255}} {
		if err := writeSolidPNG(filepath.Join(dir, fmt.Sprintf("%d.png", i)), c); err != nil {
			log.Fatal(err)
		}
	}
	imgs, err := images.FromDir(dir)
	if err != nil {
		log.Fatal(err)
	}

	// Each image takes over at its Start and holds until the next one.
	var onsets []texture.Onset
	for i, name := range imgs.Names() {
		onsets = append(onsets, texture.Onset{Image: name, Start: float64(i)})
	}

	// The soundtrack is any io.ReadSeeker; here, a WAV built in memory.
	audio := bytes.NewReader(testaudio.WAV(testaudio.ClickTrack(44100, 3, 120), 44100))

	// The output is any io.WriteSeeker. An *os.File also gets faststart.
	out, err := os.Create(filepath.Join(dir, "slideshow.mp4"))
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	err = video.Encode(out, onsets, imgs, audio, 3.0, video.Options{
		Width:  320,
		Height: 240,
		Preset: "ultrafast",
	})
	if err != nil {
		log.Fatal(err)
	}

	info, err := out.Stat()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote a movie:", info.Size() > 0)
	// Output:
	// wrote a movie: true
}
