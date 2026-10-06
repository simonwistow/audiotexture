package images

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
)

// Orientation values from the EXIF/TIFF orientation tag. Each says how the
// stored pixels must be transformed to display the picture upright.
const (
	orientNormal     = 1
	orientFlipH      = 2
	orientRotate180  = 3
	orientFlipV      = 4
	orientTranspose  = 5 // flip about the top-left to bottom-right diagonal
	orientRotate90   = 6 // rotate 90 degrees clockwise
	orientTransverse = 7 // flip about the other diagonal
	orientRotate270  = 8 // rotate 90 degrees anticlockwise
)

// orientation returns the EXIF orientation recorded in an encoded image, or
// orientNormal when there is none or it cannot be read. Only the containers
// that can carry EXIF are looked into: JPEG, TIFF, PNG and WebP.
func orientation(b []byte) int {
	var tiff []byte
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8}):
		tiff = jpegEXIF(b)
	case bytes.HasPrefix(b, []byte("II*\x00")), bytes.HasPrefix(b, []byte("MM\x00*")):
		tiff = b
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		tiff = pngEXIF(b)
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		tiff = webpEXIF(b)
	}
	if o := tiffOrientation(tiff); o >= orientNormal && o <= orientRotate270 {
		return o
	}
	return orientNormal
}

// exifHeader prefixes the TIFF structure inside a JPEG APP1 segment, and
// sometimes inside a WebP EXIF chunk too.
var exifHeader = []byte("Exif\x00\x00")

// jpegEXIF returns the TIFF structure from a JPEG's EXIF APP1 segment.
func jpegEXIF(b []byte) []byte {
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			return nil
		}
		marker := b[i+1]
		switch {
		case marker == 0xFF: // fill byte before a marker
			i++
			continue
		case marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7):
			i += 2 // markers with no length or payload
			continue
		case marker == 0xDA || marker == 0xD9:
			return nil // start of scan or end of image: no EXIF before it
		}
		end := i + 2 + int(binary.BigEndian.Uint16(b[i+2:]))
		if end > len(b) {
			return nil
		}
		if seg := b[i+4 : end]; marker == 0xE1 && bytes.HasPrefix(seg, exifHeader) {
			return seg[len(exifHeader):]
		}
		i = end
	}
	return nil
}

// pngEXIF returns the contents of a PNG's eXIf chunk.
func pngEXIF(b []byte) []byte {
	for i := 8; i+8 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[i:]))
		typ := string(b[i+4 : i+8])
		if n < 0 || i+8+n > len(b) {
			return nil
		}
		if typ == "eXIf" {
			return b[i+8 : i+8+n]
		}
		if typ == "IEND" {
			return nil
		}
		i += 12 + n // length, type, data, CRC
	}
	return nil
}

// webpEXIF returns the contents of a WebP's EXIF chunk.
func webpEXIF(b []byte) []byte {
	for i := 12; i+8 <= len(b); {
		n := int(binary.LittleEndian.Uint32(b[i+4:]))
		if n < 0 || i+8+n > len(b) {
			return nil
		}
		if string(b[i:i+4]) == "EXIF" {
			return bytes.TrimPrefix(b[i+8:i+8+n], exifHeader)
		}
		i += 8 + n + n%2 // chunks are padded to an even length
	}
	return nil
}

// tiffOrientation reads tag 0x0112 from the first IFD of a TIFF structure,
// returning 0 if it is not there.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	if order.Uint16(t[2:]) != 42 {
		return 0
	}
	ifd := int64(order.Uint32(t[4:]))
	if ifd+2 > int64(len(t)) {
		return 0
	}
	count := int64(order.Uint16(t[ifd:]))
	for e := ifd + 2; e+12 <= int64(len(t)) && e < ifd+2+count*12; e += 12 {
		const tagOrientation, typeShort = 0x0112, 3
		if order.Uint16(t[e:]) == tagOrientation && order.Uint16(t[e+2:]) == typeShort {
			return int(order.Uint16(t[e+8:]))
		}
	}
	return 0
}

// orient returns img transformed as the orientation tag o says, so that it
// displays upright. orientNormal returns img unchanged.
func orient(img image.Image, o int) image.Image {
	if o == orientNormal {
		return img
	}

	// Work on packed RGBA with its origin at zero, so the transform is a
	// copy of four bytes per pixel rather than a call through image.Image.
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	src, ok := img.(*image.RGBA)
	if !ok || b.Min != (image.Point{}) {
		src = image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	}

	dw, dh := w, h
	if o >= orientTranspose {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))

	// For each destination pixel, which source pixel lands there.
	from := func(x, y int) (int, int) {
		switch o {
		case orientFlipH:
			return w - 1 - x, y
		case orientRotate180:
			return w - 1 - x, h - 1 - y
		case orientFlipV:
			return x, h - 1 - y
		case orientTranspose:
			return y, x
		case orientRotate90:
			return y, h - 1 - x
		case orientTransverse:
			return w - 1 - y, h - 1 - x
		default: // orientRotate270
			return w - 1 - y, x
		}
	}
	for y := range dh {
		for x := range dw {
			sx, sy := from(x, y)
			copy(dst.Pix[dst.PixOffset(x, y):][:4], src.Pix[src.PixOffset(sx, sy):][:4])
		}
	}
	return dst
}
