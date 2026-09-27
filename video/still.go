package video

import (
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/asticode/go-astiav"
	"golang.org/x/image/draw"

	"github.com/simonwistow/audiotexture/images"
)

// stillConverter turns a source image file into a YUV420P frame at the output
// resolution, preserving aspect ratio and letterboxing the remainder.
//
// It keeps its scratch buffers between calls, since a slideshow converts one
// image per shot rather than one per frame.
type stillConverter struct {
	width, height int
	background    color.Color

	scratch   *image.RGBA   // output-sized RGBA the source is drawn into
	rgbaFrame *astiav.Frame // the same pixels, as an AVFrame
	sws       *astiav.SoftwareScaleContext
}

func newStillConverter(width, height int, background color.Color) (*stillConverter, error) {
	c := &stillConverter{
		width:      width,
		height:     height,
		background: background,
		scratch:    image.NewRGBA(image.Rect(0, 0, width, height)),
	}

	c.rgbaFrame = astiav.AllocFrame()
	c.rgbaFrame.SetWidth(width)
	c.rgbaFrame.SetHeight(height)
	c.rgbaFrame.SetPixelFormat(astiav.PixelFormatRgba)
	if err := c.rgbaFrame.AllocBuffer(1); err != nil {
		c.Close()
		return nil, fmt.Errorf("allocating RGBA frame: %w", err)
	}

	sws, err := astiav.CreateSoftwareScaleContext(
		width, height, astiav.PixelFormatRgba,
		width, height, astiav.PixelFormatYuv420P,
		astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagBilinear),
	)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("creating colour conversion context: %w", err)
	}
	c.sws = sws
	return c, nil
}

func (c *stillConverter) Close() {
	if c.sws != nil {
		c.sws.Free()
		c.sws = nil
	}
	if c.rgbaFrame != nil {
		c.rgbaFrame.Free()
		c.rgbaFrame = nil
	}
}

// Convert decodes path and writes it into dst, which must already be an
// allocated YUV420P frame at the converter's resolution.
func (c *stillConverter) Convert(path string, dst *astiav.Frame) error {
	src, err := images.Decode(path)
	if err != nil {
		return err
	}

	// Letterbox: fill the background, then draw the image scaled to fit.
	draw.Draw(c.scratch, c.scratch.Bounds(), &image.Uniform{C: c.background}, image.Point{}, draw.Src)
	if fit := fitRect(src.Bounds(), c.width, c.height); !fit.Empty() {
		draw.CatmullRom.Scale(c.scratch, fit, src, src.Bounds(), draw.Over, nil)
	}

	if err := c.rgbaFrame.MakeWritable(); err != nil {
		return fmt.Errorf("making RGBA frame writable: %w", err)
	}
	if err := c.rgbaFrame.Data().FromImage(c.scratch); err != nil {
		return fmt.Errorf("copying %s into a frame: %w", path, err)
	}
	if err := dst.MakeWritable(); err != nil {
		return fmt.Errorf("making destination frame writable: %w", err)
	}
	if err := c.sws.ScaleFrame(c.rgbaFrame, dst); err != nil {
		return fmt.Errorf("converting %s to YUV: %w", path, err)
	}
	return nil
}

// fitRect returns the largest centred rectangle inside width x height with the
// same aspect ratio as src.
func fitRect(src image.Rectangle, width, height int) image.Rectangle {
	sw, sh := src.Dx(), src.Dy()
	if sw <= 0 || sh <= 0 {
		return image.Rectangle{}
	}

	// Compare sw/sh against width/height without floating point.
	w, h := width, height
	if sw*height > width*sh {
		h = sh * width / sw // source is wider: full width, bars top and bottom
	} else {
		w = sw * height / sh // source is taller: full height, bars left and right
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}

	x := (width - w) / 2
	y := (height - h) / 2
	return image.Rect(x, y, x+w, y+h)
}

// allocVideoFrame returns an allocated YUV420P frame at the given size.
func allocVideoFrame(width, height int) (*astiav.Frame, error) {
	f := astiav.AllocFrame()
	if f == nil {
		return nil, errors.New("allocating video frame failed")
	}
	f.SetWidth(width)
	f.SetHeight(height)
	f.SetPixelFormat(astiav.PixelFormatYuv420P)
	if err := f.AllocBuffer(1); err != nil {
		f.Free()
		return nil, fmt.Errorf("allocating video frame buffer: %w", err)
	}
	return f, nil
}
