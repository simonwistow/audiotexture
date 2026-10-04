package audiotexture_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonwistow/audiotexture"
	"github.com/simonwistow/audiotexture/images"
	"github.com/simonwistow/audiotexture/texture"
	"github.com/simonwistow/audiotexture/video"
)

// TestGenerateFilesCleansUp checks a failed run does not leave a truncated
// movie behind for something else to pick up.
func TestGenerateFilesCleansUp(t *testing.T) {
	dir := t.TempDir()
	imagesDir, _ := slideshowFixture(dir)
	notAudio := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notAudio, []byte("not a soundtrack"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	out := filepath.Join(dir, "out.mp4")
	if _, err := audiotexture.GenerateFiles(imagesDir, notAudio, out, audiotexture.Options{}); err == nil {
		t.Fatal("expected an error for a soundtrack that is not audio")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("output left behind after a failure (stat: %v)", err)
	}
}

// TestGenerateFilesFormatFromExtension checks the container follows the
// output's name, as it did when FFmpeg opened the file itself.
func TestGenerateFilesFormatFromExtension(t *testing.T) {
	dir := t.TempDir()
	imagesDir, audioPath := slideshowFixture(dir)

	out := filepath.Join(dir, "out.mkv")
	if _, err := audiotexture.GenerateFiles(imagesDir, audioPath, out, audiotexture.Options{}); err != nil {
		t.Fatalf("GenerateFiles: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	// Matroska files open with the EBML magic number.
	if len(raw) < 4 || string(raw[:4]) != "\x1a\x45\xdf\xa3" {
		t.Errorf("output starts % x, want a Matroska header", raw[:min(4, len(raw))])
	}
}

func TestGenerateRequiresInputs(t *testing.T) {
	if _, err := audiotexture.Generate(nil, nil, nil, audiotexture.Options{}); err == nil {
		t.Error("expected an error with no inputs")
	}
}

// everyOther is an algorithm that is not registered anywhere: Options takes
// the value itself, so nothing has to be looked up by name.
type everyOther struct{}

func (everyOther) Assign(in texture.Input) ([]texture.Onset, error) {
	onsets := make([]texture.Onset, len(in.Images))
	for i, name := range in.Images {
		onsets[i] = texture.Onset{Image: name, Start: float64(i) * 2}
	}
	return onsets, nil
}

func TestGenerateCustomAlgorithm(t *testing.T) {
	dir := t.TempDir()
	imagesDir, audioPath := slideshowFixture(dir)

	res, err := audiotexture.GenerateFiles(imagesDir, audioPath, filepath.Join(dir, "out.mp4"),
		audiotexture.Options{
			Algorithm: everyOther{},
			Video:     video.Options{Width: 160, Height: 90, Preset: "ultrafast"},
		})
	if err != nil {
		t.Fatalf("GenerateFiles: %v", err)
	}
	for i, o := range res.Onsets {
		if o.Start != float64(i)*2 {
			t.Errorf("onset %d at %vs, want %vs", i, o.Start, float64(i)*2)
		}
	}
}

// failing is an algorithm that always fails, to check how errors name it.
type failing struct{ name string }

func (failing) Assign(texture.Input) ([]texture.Onset, error) { return nil, errors.New("no") }

func (f failing) String() string { return f.name }

func TestGenerateNamesFailingAlgorithm(t *testing.T) {
	dir := t.TempDir()
	imagesDir, audioPath := slideshowFixture(dir)
	imgs, err := images.FromDir(imagesDir)
	if err != nil {
		t.Fatalf("FromDir: %v", err)
	}
	track, err := os.Open(audioPath)
	if err != nil {
		t.Fatalf("opening audio: %v", err)
	}
	defer track.Close()

	var out discard
	_, err = audiotexture.Generate(imgs, track, &out, audiotexture.Options{Algorithm: failing{"mine"}})
	if err == nil || err.Error() != "running algorithm mine: no" {
		t.Errorf("error = %v, want it to name the algorithm", err)
	}
}

// discard is a WriteSeeker that drops everything; the test using it fails
// before anything is encoded.
type discard struct{}

func (*discard) Write(p []byte) (int, error)    { return len(p), nil }
func (*discard) Seek(int64, int) (int64, error) { return 0, nil }
