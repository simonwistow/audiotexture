package images_test

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"slices"

	"github.com/simonwistow/audiotexture/images"
)

// writeSolidPNG is a stand-in for a real photograph.
func writeSolidPNG(dir, name string) error {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{A: 255})
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// FromDir lists images in lexical filename order, which is deliberately not
// numeric order: "10.png" sorts before "2.png". Zero-pad names, or accept the
// sequence the sort gives you.
func ExampleFromDir() {
	dir, err := os.MkdirTemp("", "slideshow")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Created in an order that has nothing to do with the order they load in.
	for _, name := range []string{"10.png", "2.png", "1.png"} {
		if err := writeSolidPNG(dir, name); err != nil {
			log.Fatal(err)
		}
	}
	// Anything that is not a supported image is skipped.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o600); err != nil {
		log.Fatal(err)
	}

	src, err := images.FromDir(dir)
	if err != nil {
		log.Fatal(err)
	}
	for _, name := range src.Names() {
		fmt.Println(name)
	}
	// Output:
	// 1.png
	// 10.png
	// 2.png
}

// reversed shows the Images interface used for an order of your own: here the
// same pictures, last first. Anything with Names and Image will do.
type reversed struct{ images.Images }

func (r reversed) Names() []string {
	names := r.Images.Names()
	slices.Reverse(names)
	return names
}

// Implement Images to choose the order yourself, or to supply pictures from
// somewhere other than a directory.
func ExampleImages() {
	dir, err := os.MkdirTemp("", "slideshow")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	for _, name := range []string{"a.png", "b.png", "c.png"} {
		if err := writeSolidPNG(dir, name); err != nil {
			log.Fatal(err)
		}
	}

	src, err := images.FromDir(dir)
	if err != nil {
		log.Fatal(err)
	}
	var imgs images.Images = reversed{src}
	fmt.Println(imgs.Names())
	// Output:
	// [c.png b.png a.png]
}
