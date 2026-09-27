package images

import (
	"bufio"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"

	// Registered for their side effect: image.Decode sniffs the format.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

var supportedExt = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".tif":  true,
	".tiff": true,
	".bmp":  true,
	".webp": true,
}

// Load returns the supported image files in dir, lexically sorted by filename.
//
// Lexical order is the whole ordering model, inherited from the original Perl:
// name your files so they sort into the sequence you want.
func Load(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading image directory: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if supportedExt[strings.ToLower(filepath.Ext(e.Name()))] {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no supported images found in %s", dir)
	}

	sort.Strings(names)

	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = filepath.Join(dir, n)
	}
	return paths, nil
}

// Decode reads a still image from path.
//
// Stills go through the Go image packages rather than libav*: it hands back an
// image.Image directly, which is what the high-quality resampling in
// x/image/draw wants, and it keeps still-image support independent of how the
// local FFmpeg happens to be configured.
func Decode(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(bufio.NewReader(f))
	if err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	return img, nil
}
