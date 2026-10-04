package render_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simonwistow/audiotexture/render"
	"github.com/simonwistow/audiotexture/texture"
)

// sourceImages writes n one-byte files into dir and returns their names.
func sourceImages(t *testing.T, dir string, n int) []string {
	t.Helper()
	var names []string
	for i := range n {
		name := string(rune('a'+i)) + ".png"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{byte(i)}, 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		names = append(names, name)
	}
	return names
}

func TestFrameDirectory(t *testing.T) {
	src := t.TempDir()
	out := filepath.Join(t.TempDir(), "frames")
	imgs := sourceImages(t, src, 3)

	onsets := []texture.Onset{
		{Image: imgs[0], Start: 0},
		{Image: imgs[1], Start: 1},
		{Image: imgs[2], Start: 2},
	}

	n, err := render.FrameDirectory(out, src, onsets, 3, 10)
	if err != nil {
		t.Fatalf("FrameDirectory: %v", err)
	}
	// The original's loop bound: int(duration*framerate) + 1.
	if want := 31; n != want {
		t.Errorf("wrote %d frames, want %d", n, want)
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if len(entries) != n {
		t.Errorf("found %d files, want %d", len(entries), n)
	}

	// Each frame should carry the content of the shot covering its timestamp.
	for _, tc := range []struct{ frame, want int }{{0, 0}, {9, 0}, {10, 1}, {19, 1}, {20, 2}, {30, 2}} {
		got, err := os.ReadFile(filepath.Join(out, framePath(tc.frame)))
		if err != nil {
			t.Fatalf("reading frame %d: %v", tc.frame, err)
		}
		if len(got) != 1 || int(got[0]) != tc.want {
			t.Errorf("frame %d holds image %v, want image %d", tc.frame, got, tc.want)
		}
	}
}

// TestFrameDirectoryClearsOnlyItsOwn checks a rerun removes previous frames
// but leaves anything else in the directory alone.
func TestFrameDirectoryClearsOnlyItsOwn(t *testing.T) {
	src := t.TempDir()
	out := filepath.Join(t.TempDir(), "frames")
	imgs := sourceImages(t, src, 2)

	if _, err := render.FrameDirectory(out, src, []texture.Onset{{Image: imgs[0]}}, 5, 10); err != nil {
		t.Fatalf("first run: %v", err)
	}
	keep := filepath.Join(out, "notes.txt")
	if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
		t.Fatalf("writing sentinel: %v", err)
	}

	// A shorter second run must not leave the first run's tail behind.
	n, err := render.FrameDirectory(out, src, []texture.Onset{{Image: imgs[1]}}, 1, 10)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if len(entries) != n+1 {
		t.Errorf("found %d files, want %d frames plus the sentinel", len(entries), n)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("sentinel file was removed: %v", err)
	}
}

func TestFrameDirectoryErrors(t *testing.T) {
	if _, err := render.FrameDirectory(t.TempDir(), t.TempDir(), nil, 1, 10); err == nil {
		t.Error("expected an error for no onsets")
	}
	out := filepath.Join(t.TempDir(), "frames")
	if _, err := render.FrameDirectory(out, t.TempDir(), []texture.Onset{{Image: "x.png"}}, 1, 10); err == nil {
		t.Error("expected an error for a missing source image")
	}
}

func framePath(n int) string {
	return string([]byte{
		byte('0' + n/100000%10), byte('0' + n/10000%10), byte('0' + n/1000%10),
		byte('0' + n/100%10), byte('0' + n/10%10), byte('0' + n%10),
	}) + ".png"
}
