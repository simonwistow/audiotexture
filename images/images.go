// Package images finds and decodes the still images a slideshow is built from.
//
// The Images interface supplies them, in order. FS, the implementation for a
// directory of files, orders by filename, lexically, and that is its whole
// ordering model: name the files so they sort into the sequence you want. It
// is inherited from the 2010 Perl this is a port of, and it is why "10.png"
// comes before "2.png". Nothing here looks at timestamps or EXIF capture
// dates; implement Images yourself for any other order.
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
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

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

// Images is a slideshow's pictures, in the order they appear.
//
// Implement it to supply images from anywhere -- a database, an archive,
// pictures generated on the fly -- or in an order of your own choosing. FS
// covers the common case of a directory of files.
type Images interface {
	// Names returns a unique name for each image, in the order they appear.
	// The name is how an image is referred to from then on: it is what
	// texture.Onset.Image holds and what Image is later asked for.
	Names() []string
	// Image decodes the named image. It is called once each time the image
	// comes on screen, not once up front, so a long slideshow never needs
	// every picture in memory at once.
	Image(name string) (image.Image, error)
}

// FS is the supported image files at the top level of a file system, in
// lexical filename order. Subdirectories and other files are skipped.
type FS struct {
	fsys  fs.FS
	names []string
}

// FromFS lists the images in fsys. It works with anything that implements
// fs.FS: os.DirFS, an embed.FS, a zip.Reader, an fstest.MapFS.
func FromFS(fsys fs.FS) (*FS, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("reading image directory: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if supportedExt[strings.ToLower(path.Ext(e.Name()))] {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no supported images found")
	}
	sort.Strings(names)
	return &FS{fsys: fsys, names: names}, nil
}

// FromDir lists the images in the directory dir.
func FromDir(dir string) (*FS, error) {
	s, err := FromFS(os.DirFS(dir))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	return s, nil
}

// Names returns the image file names, in lexical order.
//
// Lexical order is the whole ordering model, inherited from the original
// Perl: name your files so they sort into the sequence you want.
func (s *FS) Names() []string {
	return append([]string(nil), s.names...)
}

// Image opens and decodes the named file.
func (s *FS) Image(name string) (image.Image, error) {
	f, err := s.fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, err := Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decoding %s: %w", name, err)
	}
	return img, nil
}

// Decode reads a still image in any of the supported formats from r.
//
// Stills go through the Go image packages rather than libav*: it hands back an
// image.Image directly, which is what the high-quality resampling in
// x/image/draw wants, and it keeps still-image support independent of how the
// local FFmpeg happens to be configured.
func Decode(r io.Reader) (image.Image, error) {
	img, _, err := image.Decode(bufio.NewReader(r))
	return img, err
}
