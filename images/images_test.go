package images_test

import (
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonwistow/audiotexture/images"
)

// writeImage writes a 4x3 solid image in the format implied by name.
func writeImage(t *testing.T, dir, name string) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := range 3 {
		for x := range 4 {
			img.SetRGBA(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("creating %s: %v", p, err)
	}
	defer f.Close()

	switch filepath.Ext(name) {
	case ".png":
		err = png.Encode(f, img)
	case ".jpg", ".jpeg":
		err = jpeg.Encode(f, img, nil)
	case ".gif":
		err = gif.Encode(f, img, nil)
	default:
		_, err = f.WriteString("not an image")
	}
	if err != nil {
		t.Fatalf("encoding %s: %v", p, err)
	}
	return p
}

func TestLoadSortsLexically(t *testing.T) {
	dir := t.TempDir()
	// Written out of order, and with names that sort differently as numbers.
	for _, n := range []string{"10.png", "2.png", "1.png", "20.png"} {
		writeImage(t, dir, n)
	}

	got, err := images.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"1.png", "10.png", "2.png", "20.png"}
	if len(got) != len(want) {
		t.Fatalf("got %d images, want %d", len(got), len(want))
	}
	for i := range want {
		if base := filepath.Base(got[i]); base != want[i] {
			t.Errorf("image %d = %q, want %q (lexical, not numeric, order)", i, base, want[i])
		}
	}
}

func TestLoadFiltersAndIgnoresDirs(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "a.png")
	writeImage(t, dir, "b.jpg")
	writeImage(t, dir, "c.gif")
	writeImage(t, dir, "notes.txt")
	writeImage(t, dir, "d.PNG") // extension matching is case insensitive
	if err := os.Mkdir(filepath.Join(dir, "sub.png"), 0o755); err != nil {
		t.Fatalf("creating subdirectory: %v", err)
	}

	got, err := images.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"a.png", "b.jpg", "c.gif", "d.PNG"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if base := filepath.Base(got[i]); base != want[i] {
			t.Errorf("image %d = %q, want %q", i, base, want[i])
		}
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := images.Load(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected an error for a missing directory")
	}
	if _, err := images.Load(t.TempDir()); err == nil {
		t.Error("expected an error for a directory with no images")
	}
}

func TestDecode(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.png", "b.jpg", "c.gif"} {
		p := writeImage(t, dir, name)
		img, err := images.Decode(p)
		if err != nil {
			t.Errorf("Decode(%s): %v", name, err)
			continue
		}
		if b := img.Bounds(); b.Dx() != 4 || b.Dy() != 3 {
			t.Errorf("Decode(%s): bounds %v, want 4x3", name, b)
		}
	}
}

func TestDecodeErrors(t *testing.T) {
	if _, err := images.Decode(filepath.Join(t.TempDir(), "missing.png")); err == nil {
		t.Error("expected an error for a missing file")
	}
	dir := t.TempDir()
	if _, err := images.Decode(writeImage(t, dir, "broken.txt")); err == nil {
		t.Error("expected an error for a file that is not an image")
	}
}
