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
	"testing/fstest"

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

func TestFromDirSortsLexically(t *testing.T) {
	dir := t.TempDir()
	// Written out of order, and with names that sort differently as numbers.
	for _, n := range []string{"10.png", "2.png", "1.png", "20.png"} {
		writeImage(t, dir, n)
	}

	src, err := images.FromDir(dir)
	if err != nil {
		t.Fatalf("FromDir: %v", err)
	}
	got := src.Names()
	want := []string{"1.png", "10.png", "2.png", "20.png"}
	if len(got) != len(want) {
		t.Fatalf("got %d images, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("image %d = %q, want %q (lexical, not numeric, order)", i, got[i], want[i])
		}
	}
}

func TestFromDirFiltersAndIgnoresDirs(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "a.png")
	writeImage(t, dir, "b.jpg")
	writeImage(t, dir, "c.gif")
	writeImage(t, dir, "notes.txt")
	writeImage(t, dir, "d.PNG") // extension matching is case insensitive
	if err := os.Mkdir(filepath.Join(dir, "sub.png"), 0o755); err != nil {
		t.Fatalf("creating subdirectory: %v", err)
	}

	src, err := images.FromDir(dir)
	if err != nil {
		t.Fatalf("FromDir: %v", err)
	}
	got := src.Names()
	want := []string{"a.png", "b.jpg", "c.gif", "d.PNG"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("image %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFromDirErrors(t *testing.T) {
	if _, err := images.FromDir(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected an error for a missing directory")
	}
	if _, err := images.FromDir(t.TempDir()); err == nil {
		t.Error("expected an error for a directory with no images")
	}
}

// TestFromFS checks any fs.FS works, not only a directory on disk.
func TestFromFS(t *testing.T) {
	dir := t.TempDir()
	png, err := os.ReadFile(writeImage(t, dir, "a.png"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	fsys := fstest.MapFS{
		"b.png":     {Data: png},
		"a.png":     {Data: png},
		"notes.txt": {Data: []byte("skipped")},
		"sub/c.png": {Data: png}, // only the top level is listed
	}

	src, err := images.FromFS(fsys)
	if err != nil {
		t.Fatalf("FromFS: %v", err)
	}
	if got := src.Names(); len(got) != 2 || got[0] != "a.png" || got[1] != "b.png" {
		t.Fatalf("Names = %v, want [a.png b.png]", got)
	}
	img, err := src.Image("b.png")
	if err != nil {
		t.Fatalf("Image: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 4 || b.Dy() != 3 {
		t.Errorf("bounds %v, want 4x3", b)
	}
}

// TestNamesIsACopy checks a caller cannot reorder the slideshow by editing
// the slice it was handed.
func TestNamesIsACopy(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "a.png")
	writeImage(t, dir, "b.png")
	src, err := images.FromDir(dir)
	if err != nil {
		t.Fatalf("FromDir: %v", err)
	}
	src.Names()[0] = "changed"
	if got := src.Names()[0]; got != "a.png" {
		t.Errorf("Names()[0] = %q after editing an earlier result, want a.png", got)
	}
}

func TestImage(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.png", "b.jpg", "c.gif"} {
		writeImage(t, dir, name)
	}
	src, err := images.FromDir(dir)
	if err != nil {
		t.Fatalf("FromDir: %v", err)
	}
	for _, name := range src.Names() {
		img, err := src.Image(name)
		if err != nil {
			t.Errorf("Image(%s): %v", name, err)
			continue
		}
		if b := img.Bounds(); b.Dx() != 4 || b.Dy() != 3 {
			t.Errorf("Image(%s): bounds %v, want 4x3", name, b)
		}
	}
}

func TestImageErrors(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "a.png")
	// .png by name, so it is listed, but not an image.
	if err := os.WriteFile(filepath.Join(dir, "broken.png"), []byte("not an image"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	src, err := images.FromDir(dir)
	if err != nil {
		t.Fatalf("FromDir: %v", err)
	}
	if _, err := src.Image("missing.png"); err == nil {
		t.Error("expected an error for a missing file")
	}
	if _, err := src.Image("broken.png"); err == nil {
		t.Error("expected an error for a file that is not an image")
	}
}
