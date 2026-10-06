package images

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
	"testing/fstest"
)

// tiffWithOrientation builds the smallest TIFF structure carrying an
// orientation tag: a header and a one-entry IFD.
func tiffWithOrientation(order binary.AppendByteOrder, o int) []byte {
	var b []byte
	if order == binary.LittleEndian {
		b = append(b, "II"...)
	} else {
		b = append(b, "MM"...)
	}
	b = order.AppendUint16(b, 42)
	b = order.AppendUint32(b, 8) // IFD0 straight after the header
	b = order.AppendUint16(b, 1) // one entry
	b = order.AppendUint16(b, 0x0112)
	b = order.AppendUint16(b, 3) // SHORT
	b = order.AppendUint32(b, 1)
	b = order.AppendUint16(b, uint16(o))
	b = order.AppendUint16(b, 0) // padding out the 4-byte value field
	b = order.AppendUint32(b, 0) // no next IFD
	return b
}

// withJPEGEXIF inserts an APP1 EXIF segment after the SOI marker and an
// APP0 segment, as a camera writes them.
func withJPEGEXIF(jpg, tiff []byte) []byte {
	app0 := []byte{0xFF, 0xE0, 0x00, 0x07, 'J', 'F', 'I', 'F', 0x00}
	payload := append(append([]byte(nil), exifHeader...), tiff...)
	app1 := []byte{0xFF, 0xE1}
	app1 = binary.BigEndian.AppendUint16(app1, uint16(2+len(payload)))
	app1 = append(app1, payload...)

	out := append([]byte(nil), jpg[:2]...)
	out = append(out, app0...)
	out = append(out, app1...)
	return append(out, jpg[2:]...)
}

// withPNGEXIF inserts an eXIf chunk straight after IHDR.
func withPNGEXIF(p, tiff []byte) []byte {
	const afterIHDR = 8 + 8 + 13 + 4 // signature, then IHDR's length, type, data, CRC
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(tiff)))
	chunk = append(chunk, "eXIf"...)
	chunk = append(chunk, tiff...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))

	out := append([]byte(nil), p[:afterIHDR]...)
	out = append(out, chunk...)
	return append(out, p[afterIHDR:]...)
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding PNG: %v", err)
	}
	return buf.Bytes()
}

func TestOrientationContainers(t *testing.T) {
	small := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, small, nil); err != nil {
		t.Fatalf("encoding JPEG: %v", err)
	}
	pngBytes := encodePNG(t, small)

	webp := func(chunk []byte) []byte {
		b := []byte("RIFF\x00\x00\x00\x00WEBP")
		b = append(b, "VP8X"...) // a chunk to skip first, odd-sized to test padding
		b = binary.LittleEndian.AppendUint32(b, 3)
		b = append(b, 0, 0, 0, 0)
		b = append(b, "EXIF"...)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(chunk)))
		return append(b, chunk...)
	}

	for _, tc := range []struct {
		name string
		b    []byte
		want int
	}{
		{"jpeg", withJPEGEXIF(jpg.Bytes(), tiffWithOrientation(binary.LittleEndian, 6)), 6},
		{"jpeg big-endian", withJPEGEXIF(jpg.Bytes(), tiffWithOrientation(binary.BigEndian, 8)), 8},
		{"jpeg without exif", jpg.Bytes(), orientNormal},
		{"tiff", tiffWithOrientation(binary.BigEndian, 3), 3},
		{"png", withPNGEXIF(pngBytes, tiffWithOrientation(binary.LittleEndian, 5)), 5},
		{"png without exif", pngBytes, orientNormal},
		{"webp", webp(tiffWithOrientation(binary.LittleEndian, 7)), 7},
		{"webp with exif header", webp(append(append([]byte(nil), exifHeader...), tiffWithOrientation(binary.LittleEndian, 2)...)), 2},
		{"out of range", tiffWithOrientation(binary.LittleEndian, 9), orientNormal},
		{"empty", nil, orientNormal},
		{"gif", []byte("GIF89a"), orientNormal},
	} {
		if got := orientation(tc.b); got != tc.want {
			t.Errorf("%s: orientation = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestOrientationTruncated checks a damaged file cannot panic the parser:
// every prefix of each container is tried.
func TestOrientationTruncated(t *testing.T) {
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatalf("encoding JPEG: %v", err)
	}
	tiff := tiffWithOrientation(binary.LittleEndian, 6)
	for _, b := range [][]byte{
		withJPEGEXIF(jpg.Bytes(), tiff),
		withPNGEXIF(encodePNG(t, image.NewRGBA(image.Rect(0, 0, 4, 4))), tiff),
		tiff,
	} {
		for n := range len(b) {
			orientation(b[:n])
		}
	}
}

// TestOrient checks each transform against what the tag means, by following
// two marked pixels of a 3x2 image: the top-left and the one to its right.
func TestOrient(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	green := color.RGBA{0, 255, 0, 255}
	stored := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for i := range stored.Pix {
		stored.Pix[i] = 0x80
	}
	stored.SetRGBA(0, 0, red)
	stored.SetRGBA(1, 0, green)

	for _, tc := range []struct {
		o              int
		w, h           int
		redAt, greenAt image.Point
	}{
		{orientNormal, 3, 2, image.Pt(0, 0), image.Pt(1, 0)},
		{orientFlipH, 3, 2, image.Pt(2, 0), image.Pt(1, 0)},
		{orientRotate180, 3, 2, image.Pt(2, 1), image.Pt(1, 1)},
		{orientFlipV, 3, 2, image.Pt(0, 1), image.Pt(1, 1)},
		{orientTranspose, 2, 3, image.Pt(0, 0), image.Pt(0, 1)},
		// Rotating clockwise takes the top-left corner to the top-right, and
		// the top row down the right-hand side.
		{orientRotate90, 2, 3, image.Pt(1, 0), image.Pt(1, 1)},
		{orientTransverse, 2, 3, image.Pt(1, 2), image.Pt(1, 1)},
		// Anticlockwise takes it to the bottom-left, and the top row up the
		// left-hand side.
		{orientRotate270, 2, 3, image.Pt(0, 2), image.Pt(0, 1)},
	} {
		got := orient(stored, tc.o)
		if b := got.Bounds(); b.Dx() != tc.w || b.Dy() != tc.h {
			t.Errorf("orientation %d: %dx%d, want %dx%d", tc.o, b.Dx(), b.Dy(), tc.w, tc.h)
			continue
		}
		if c := color.RGBAModel.Convert(got.At(tc.redAt.X, tc.redAt.Y)); c != red {
			t.Errorf("orientation %d: %v at %v, want the top-left pixel", tc.o, c, tc.redAt)
		}
		if c := color.RGBAModel.Convert(got.At(tc.greenAt.X, tc.greenAt.Y)); c != green {
			t.Errorf("orientation %d: %v at %v, want the pixel right of the top-left", tc.o, c, tc.greenAt)
		}
	}
}

// TestOrientNonZeroOrigin checks an image whose bounds do not start at zero,
// which a SubImage has, is handled rather than read out of place.
func TestOrientNonZeroOrigin(t *testing.T) {
	big := image.NewRGBA(image.Rect(0, 0, 5, 5))
	big.SetRGBA(2, 2, color.RGBA{255, 0, 0, 255})
	sub := big.SubImage(image.Rect(2, 2, 5, 4)) // 3x2, red at its top-left

	got := orient(sub, orientRotate180)
	if c := color.RGBAModel.Convert(got.At(2, 1)); c != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("rotated sub-image has %v at the bottom-right, want red", c)
	}
}

// TestFSImageOrientation checks FS applies the tag by default and leaves the
// pixels alone when told to.
func TestFSImageOrientation(t *testing.T) {
	landscape := encodePNG(t, image.NewRGBA(image.Rect(0, 0, 4, 3)))
	tagged := withPNGEXIF(landscape, tiffWithOrientation(binary.LittleEndian, orientRotate90))

	src, err := FromFS(fstest.MapFS{"photo.png": {Data: tagged}})
	if err != nil {
		t.Fatalf("FromFS: %v", err)
	}

	img, err := src.Image("photo.png")
	if err != nil {
		t.Fatalf("Image: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 3 || b.Dy() != 4 {
		t.Errorf("by default: %dx%d, want 3x4, turned upright", b.Dx(), b.Dy())
	}

	src.IgnoreOrientation = true
	if img, err = src.Image("photo.png"); err != nil {
		t.Fatalf("Image: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 4 || b.Dy() != 3 {
		t.Errorf("with IgnoreOrientation: %dx%d, want 4x3, as stored", b.Dx(), b.Dy())
	}
}
