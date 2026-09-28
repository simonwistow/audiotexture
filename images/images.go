// Package images finds and decodes the still images a slideshow is built from.
//
// Ordering is by filename, lexically, and that is the whole ordering model:
// name the files so they sort into the sequence you want. It is inherited from
// the 2010 Perl this is a port of, and it is why "10.png" comes before "2.png".
// Nothing here looks at timestamps or EXIF capture dates.
//
// JPEG, PNG, GIF, TIFF, BMP and WebP are supported. Decoding goes through the
// Go image packages rather than libav*, so which formats work does not depend
// on how the local FFmpeg happens to be configured.
//
// # EXIF orientation is ignored
//
// Decode returns the pixels as they are stored and never applies the EXIF
// orientation tag, which matters more than it sounds. The 2010 Perl ignored
// EXIF too, so this matches it -- and the photographs that script was written
// for carry an orientation tag of 8, "rotate 90 degrees", which is simply
// wrong: the stored pixels are already upright. Honouring the tag would turn
// every frame on its side. Anything that does honour it, which is ffmpeg and
// most image viewers, shows those images rotated.
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
