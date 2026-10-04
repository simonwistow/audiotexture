package main

import (
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"

	"github.com/asticode/go-astiav"
	"golang.org/x/image/draw"

	"github.com/simonwistow/audiotexture/images"
)

// hashSize is the edge of the thumbnail each frame and source image is reduced
// to before comparison. 16x16 greyscale is plenty to tell 680 photographs
// apart while being cheap enough to run over every frame of every video.
const hashSize = 16

type imageHash [hashSize * hashSize]float64

// hashImage reduces an image to a normalised greyscale thumbnail.
func hashImage(src image.Image) imageHash {
	thumb := image.NewRGBA(image.Rect(0, 0, hashSize, hashSize))
	draw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), src, src.Bounds(), draw.Src, nil)

	var h imageHash
	var mean float64
	for i := range hashSize * hashSize {
		r, g, b := thumb.Pix[i*4], thumb.Pix[i*4+1], thumb.Pix[i*4+2]
		h[i] = 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
		mean += h[i]
	}
	// Remove the mean so that the encoder's overall brightness shift between
	// the source JPEG and the decoded MPEG-4 frame does not count against a
	// match.
	mean /= hashSize * hashSize
	for i := range h {
		h[i] -= mean
	}
	return h
}

func distance(a, b imageHash) float64 {
	var sum float64
	for i := range a {
		d := a[i] - b[i]
		sum += d * d
	}
	return math.Sqrt(sum)
}

// hashSources hashes every source image once.
func hashSources(src images.Images) ([]imageHash, error) {
	names := src.Names()
	out := make([]imageHash, len(names))
	for i, name := range names {
		img, err := src.Image(name)
		if err != nil {
			return nil, err
		}
		out[i] = hashImage(img)
	}
	return out, nil
}

// frameHashes decodes every video frame of path and returns its hash, in
// presentation order.
func frameHashes(path string) ([]imageHash, error) {
	fc := astiav.AllocFormatContext()
	if fc == nil {
		return nil, errors.New("allocating format context failed")
	}
	defer fc.Free()
	if err := fc.OpenInput(path, nil, nil); err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer fc.CloseInput()
	if err := fc.FindStreamInfo(nil); err != nil {
		return nil, err
	}

	stream, codec, err := fc.FindBestStream(astiav.MediaTypeVideo, -1, -1)
	if err != nil {
		return nil, err
	}
	cc := astiav.AllocCodecContext(codec)
	if cc == nil {
		return nil, errors.New("allocating decoder failed")
	}
	defer cc.Free()
	if err := stream.CodecParameters().ToCodecContext(cc); err != nil {
		return nil, err
	}
	if err := cc.Open(codec, nil); err != nil {
		return nil, err
	}

	// Scale straight down to thumbnail size in the decoder's colour space:
	// hashing full 1568x2352 frames would be pointless work.
	sws, err := astiav.CreateSoftwareScaleContext(
		cc.Width(), cc.Height(), cc.PixelFormat(),
		hashSize, hashSize, astiav.PixelFormatRgba,
		astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagBilinear))
	if err != nil {
		return nil, err
	}
	defer sws.Free()

	small := astiav.AllocFrame()
	defer small.Free()
	small.SetWidth(hashSize)
	small.SetHeight(hashSize)
	small.SetPixelFormat(astiav.PixelFormatRgba)
	if err := small.AllocBuffer(1); err != nil {
		return nil, err
	}

	pkt := astiav.AllocPacket()
	defer pkt.Free()
	frame := astiav.AllocFrame()
	defer frame.Free()
	thumb := image.NewRGBA(image.Rect(0, 0, hashSize, hashSize))

	type stamped struct {
		pts  int64
		hash imageHash
	}
	var out []stamped

	collect := func() error {
		for {
			if err := cc.ReceiveFrame(frame); err != nil {
				if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
					return nil
				}
				return err
			}
			pts := frame.Pts()
			if err := sws.ScaleFrame(frame, small); err != nil {
				return err
			}
			if err := small.Data().ToImage(thumb); err != nil {
				return err
			}
			out = append(out, stamped{pts: pts, hash: hashImage(thumb)})
			frame.Unref()
		}
	}

	for {
		if err := fc.ReadFrame(pkt); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				break
			}
			return nil, err
		}
		if pkt.StreamIndex() == stream.Index() {
			if err := cc.SendPacket(pkt); err != nil {
				return nil, err
			}
			if err := collect(); err != nil {
				return nil, err
			}
		}
		pkt.Unref()
	}
	if err := cc.SendPacket(nil); err != nil && !errors.Is(err, astiav.ErrEof) {
		return nil, err
	}
	if err := collect(); err != nil {
		return nil, err
	}

	// Decode order is not presentation order.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].pts < out[j-1].pts; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	hashes := make([]imageHash, len(out))
	for i, s := range out {
		hashes[i] = s.hash
	}
	return hashes, nil
}

// nearest returns the index of the closest source hash, and the ratio of the
// best distance to the runner-up. A ratio well below 1 means the match is
// unambiguous.
func nearest(h imageHash, sources []imageHash) (index int, confidence float64) {
	best, second, bestIdx := math.Inf(1), math.Inf(1), -1
	for i, s := range sources {
		d := distance(h, s)
		if d < best {
			best, second, bestIdx = d, best, i
		} else if d < second {
			second = d
		}
	}
	if second == 0 {
		return bestIdx, 1
	}
	return bestIdx, best / second
}

func reportDir() string {
	if d := os.Getenv("VERIFY_OUT"); d != "" {
		return d
	}
	return filepath.Join(os.TempDir(), "audiotexture-verify")
}
