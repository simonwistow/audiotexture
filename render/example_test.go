package render_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/simonwistow/audiotexture/render"
	"github.com/simonwistow/audiotexture/texture"
)

// FrameDirectory writes one file per frame, named %06d with the source image's
// extension, so frame 0 is 000000.png. Each is a hard link to the image it
// shows rather than a copy, which is why a slideshow of thousands of frames
// costs almost nothing on disk.
func ExampleFrameDirectory() {
	dir, err := os.MkdirTemp("", "frames")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Two source images, standing in for photographs. Their contents are
	// distinguishable so the output can show which frame is which.
	var onsets []texture.Onset
	for i, name := range []string{"a.png", "b.png"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			log.Fatal(err)
		}
		onsets = append(onsets, texture.Onset{Image: path, Start: float64(i)})
	}

	// Two seconds at 10 fps: the first image holds for ten frames, then the
	// second takes over. The count is 21 rather than 20 because the last
	// instant gets a frame of its own, as it did in the original.
	out := filepath.Join(dir, "frames")
	n, err := render.FrameDirectory(out, onsets, 2.0, 10)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d frames\n", n)

	for _, frame := range []string{"000000.png", "000009.png", "000010.png"} {
		shows, err := os.ReadFile(filepath.Join(out, frame))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s shows %s\n", frame, shows)
	}
	// Output:
	// 21 frames
	// 000000.png shows a.png
	// 000009.png shows a.png
	// 000010.png shows b.png
}
