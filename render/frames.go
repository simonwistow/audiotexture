// Package render writes a texture.Onset assignment out as a directory of
// numbered frame files at a fixed frame rate.
//
// This is what the 2010 Perl produced, and it is kept for comparing against
// archived frames: the files are hard links to the source images rather than
// copies, so a whole slideshow costs almost nothing on disk. For an actual
// movie, use the video package, which encodes in-process and needs no
// intermediate directory.
package render

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/simonwistow/audiotexture/texture"
)

var frameFilePattern = regexp.MustCompile(`^\d{6}\.[A-Za-z0-9]+$`)

// FrameDirectory writes one numbered file per frame (at framerate fps, for
// duration seconds) into outDir, each hardlinked (falling back to a copy) to
// the source image selected by onsets at that frame's timestamp. Each onset's
// Image names a file in srcDir, as images.FromDir lists them. onsets must be
// sorted ascending by Start and non-empty.
//
// Unlike the rest of the pipeline this works on paths rather than readers and
// writers: a hard link needs a real file at both ends.
func FrameDirectory(outDir, srcDir string, onsets []texture.Onset, duration, framerate float64) (int, error) {
	if len(onsets) == 0 {
		return 0, fmt.Errorf("no onsets to render")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return 0, fmt.Errorf("creating output directory: %w", err)
	}
	if err := clearPreviousFrames(outDir); err != nil {
		return 0, fmt.Errorf("clearing previous frames: %w", err)
	}

	totalFrames := int(duration*framerate) + 1
	j := 0
	for n := 0; n < totalFrames; n++ {
		t := float64(n) / framerate
		for j+1 < len(onsets) && onsets[j+1].Start <= t {
			j++
		}
		dst := filepath.Join(outDir, fmt.Sprintf("%06d%s", n, filepath.Ext(onsets[j].Image)))
		if err := linkOrCopy(filepath.Join(srcDir, onsets[j].Image), dst); err != nil {
			return n, fmt.Errorf("writing frame %d: %w", n, err)
		}
	}
	return totalFrames, nil
}

// clearPreviousFrames removes files left over from a prior run (matching our
// own NNNNNN.ext naming) without touching anything else in outDir.
func clearPreviousFrames(outDir string) error {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !frameFilePattern.MatchString(e.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(outDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func linkOrCopy(src, dst string) error {
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
