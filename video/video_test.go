package video_test

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/asticode/go-astiav"

	"github.com/simonwistow/audiotexture/internal/testaudio"
	"github.com/simonwistow/audiotexture/texture"
	"github.com/simonwistow/audiotexture/video"
)

func TestMain(m *testing.M) {
	// libx264 and the mp4 muxer are chatty on stderr; the tests assert on
	// decoded output, not on logs.
	astiav.SetLogLevel(astiav.LogLevelQuiet)
	os.Exit(m.Run())
}

var palette = []color.RGBA{
	{220, 40, 40, 255},  // red
	{40, 200, 80, 255},  // green
	{60, 90, 230, 255},  // blue
	{230, 200, 40, 255}, // yellow
}

// sizes give each test image a different aspect ratio so letterboxing runs.
var sizes = [][2]int{{800, 600}, {600, 900}, {1600, 500}, {640, 640}}

func TestEncodeRoundTrip(t *testing.T) {
	const (
		duration  = 4.0
		frameRate = 24.0
		width     = 320
		height    = 180
	)

	dir := t.TempDir()
	imgs := writeImages(t, dir)
	audioPath, err := testaudio.WriteWAV(dir, "track.wav", testaudio.ClickTrack(44100, duration, 120), 44100)
	if err != nil {
		t.Fatalf("writing audio fixture: %v", err)
	}

	// One image per second.
	onsets := make([]texture.Onset, len(imgs))
	for i, p := range imgs {
		onsets[i] = texture.Onset{Image: p, Start: float64(i)}
	}

	out := filepath.Join(dir, "out.mp4")
	var lastProgress int
	err = video.Encode(out, onsets, audioPath, duration, video.Options{
		Width:     width,
		Height:    height,
		FrameRate: frameRate,
		Preset:    "ultrafast",
		Progress:  func(frame, total int) { lastProgress = frame },
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if want := int(duration * frameRate); lastProgress != want {
		t.Errorf("Progress reported %d frames, want %d", lastProgress, want)
	}

	m := probe(t, out)
	if m.videoCodec != "h264" {
		t.Errorf("video codec = %q, want h264", m.videoCodec)
	}
	if m.audioCodec != "aac" {
		t.Errorf("audio codec = %q, want aac", m.audioCodec)
	}
	if m.width != width || m.height != height {
		t.Errorf("resolution = %dx%d, want %dx%d", m.width, m.height, width, height)
	}
	// The final frame's presentation time is one frame short of the duration.
	if got := m.duration; math.Abs(got-duration) > 1.0/frameRate+0.05 {
		t.Errorf("duration = %v, want %v", got, duration)
	}
	if want := int(duration * frameRate); m.frames != want {
		t.Errorf("frame count = %d, want %d", m.frames, want)
	}

	// The centre pixel mid-shot should be the colour of that shot's image.
	for i, want := range palette {
		at := float64(i) + 0.5
		got, ok := m.centreAt(at)
		if !ok {
			t.Errorf("no frame decoded at t=%vs", at)
			continue
		}
		if !closeEnough(got, want) {
			t.Errorf("colour at t=%vs = %v, want image %d (%v)", at, got, i, want)
		}
	}
}

func TestEncodeWithoutAudio(t *testing.T) {
	dir := t.TempDir()
	imgs := writeImages(t, dir)
	onsets := []texture.Onset{{Image: imgs[0], Start: 0}}

	out := filepath.Join(dir, "silent.mp4")
	if err := video.Encode(out, onsets, "", 1.0, video.Options{Width: 160, Height: 90, Preset: "ultrafast"}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if m := probe(t, out); m.audioCodec != "" {
		t.Errorf("audio codec = %q, want no audio stream", m.audioCodec)
	}
}

func TestEncodeUnsortedOnsets(t *testing.T) {
	dir := t.TempDir()
	imgs := writeImages(t, dir)

	// Deliberately out of order; Encode should sort a copy rather than
	// producing a file whose shots are scrambled.
	onsets := []texture.Onset{
		{Image: imgs[2], Start: 2},
		{Image: imgs[0], Start: 0},
		{Image: imgs[1], Start: 1},
	}
	original := append([]texture.Onset(nil), onsets...)

	out := filepath.Join(dir, "unsorted.mp4")
	if err := video.Encode(out, onsets, "", 3.0, video.Options{Width: 160, Height: 90, Preset: "ultrafast"}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for i := range onsets {
		if onsets[i] != original[i] {
			t.Fatalf("Encode mutated the caller's slice at %d", i)
		}
	}

	m := probe(t, out)
	for i := range 3 {
		got, ok := m.centreAt(float64(i) + 0.5)
		if !ok {
			t.Fatalf("no frame at t=%v", float64(i)+0.5)
		}
		if !closeEnough(got, palette[i]) {
			t.Errorf("colour at shot %d = %v, want %v", i, got, palette[i])
		}
	}
}

func TestEncodeErrors(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "x.mp4")
	if err := video.Encode(out, nil, "", 1, video.Options{}); err == nil {
		t.Error("expected an error for no onsets")
	}
	if err := video.Encode(out, []texture.Onset{{Image: "a.png"}}, "", 0, video.Options{}); err == nil {
		t.Error("expected an error for zero duration")
	}
	if err := video.Encode(out, []texture.Onset{{Image: filepath.Join(dir, "missing.png")}}, "", 1, video.Options{Preset: "ultrafast"}); err == nil {
		t.Error("expected an error for a missing image")
	}
}

// writeImages writes one solid-colour PNG per palette entry and returns the
// paths in order.
func writeImages(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	for i, c := range palette {
		w, h := sizes[i][0], sizes[i][1]
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				img.SetRGBA(x, y, c)
			}
		}
		p := filepath.Join(dir, fmt.Sprintf("%02d.png", i))
		f, err := os.Create(p)
		if err != nil {
			t.Fatalf("creating %s: %v", p, err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatalf("encoding %s: %v", p, err)
		}
		f.Close()
		paths = append(paths, p)
	}
	return paths
}

// movie is what probe extracts from an encoded file.
type movie struct {
	videoCodec, audioCodec string
	width, height          int
	duration               float64
	frames                 int
	centres                []framePixel // centre pixel of every decoded frame
}

type framePixel struct {
	t float64
	c color.RGBA
}

// centreAt returns the centre pixel of the frame on screen at time t.
func (m movie) centreAt(t float64) (color.RGBA, bool) {
	var got color.RGBA
	var ok bool
	for _, f := range m.centres {
		if f.t <= t {
			got, ok = f.c, true
		}
	}
	return got, ok
}

// probe decodes path with libav* and reports what came back. Verifying the
// output by decoding it with the same stack that wrote it keeps the test from
// depending on an ffmpeg binary being installed.
func probe(t *testing.T, path string) movie {
	t.Helper()
	var m movie

	fc := astiav.AllocFormatContext()
	if fc == nil {
		t.Fatal("allocating format context failed")
	}
	defer fc.Free()
	if err := fc.OpenInput(path, nil, nil); err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer fc.CloseInput()
	if err := fc.FindStreamInfo(nil); err != nil {
		t.Fatalf("finding stream info: %v", err)
	}

	var videoStream *astiav.Stream
	var cc *astiav.CodecContext
	for _, s := range fc.Streams() {
		cp := s.CodecParameters()
		switch cp.MediaType() {
		case astiav.MediaTypeVideo:
			m.videoCodec = astiav.FindDecoder(cp.CodecID()).Name()
			m.width, m.height = cp.Width(), cp.Height()
			videoStream = s
		case astiav.MediaTypeAudio:
			m.audioCodec = astiav.FindDecoder(cp.CodecID()).Name()
		}
	}
	if videoStream == nil {
		t.Fatal("no video stream in output")
	}

	codec := astiav.FindDecoder(videoStream.CodecParameters().CodecID())
	if cc = astiav.AllocCodecContext(codec); cc == nil {
		t.Fatal("allocating decoder context failed")
	}
	defer cc.Free()
	if err := videoStream.CodecParameters().ToCodecContext(cc); err != nil {
		t.Fatalf("copying codec parameters: %v", err)
	}
	if err := cc.Open(codec, nil); err != nil {
		t.Fatalf("opening decoder: %v", err)
	}

	pkt := astiav.AllocPacket()
	defer pkt.Free()
	f := astiav.AllocFrame()
	defer f.Free()

	conv := newRGBAConverter(t, m.width, m.height, cc.PixelFormat())
	defer conv.close()

	tb := videoStream.TimeBase()
	collect := func() {
		for {
			if err := cc.ReceiveFrame(f); err != nil {
				if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
					return
				}
				t.Fatalf("receiving frame: %v", err)
			}
			m.frames++
			ts := float64(f.Pts()) * float64(tb.Num()) / float64(tb.Den())
			m.duration = math.Max(m.duration, ts+float64(tb.Num())/float64(tb.Den()))
			m.centres = append(m.centres, framePixel{t: ts, c: conv.centrePixel(t, f)})
			f.Unref()
		}
	}

	for {
		if err := fc.ReadFrame(pkt); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				break
			}
			t.Fatalf("reading packet: %v", err)
		}
		if pkt.StreamIndex() == videoStream.Index() {
			if err := cc.SendPacket(pkt); err != nil {
				t.Fatalf("sending packet: %v", err)
			}
			collect()
		}
		pkt.Unref()
	}
	if err := cc.SendPacket(nil); err != nil && !errors.Is(err, astiav.ErrEof) {
		t.Fatalf("flushing decoder: %v", err)
	}
	collect()

	// Frames come out in presentation order only after sorting by PTS.
	for i := 1; i < len(m.centres); i++ {
		for j := i; j > 0 && m.centres[j].t < m.centres[j-1].t; j-- {
			m.centres[j], m.centres[j-1] = m.centres[j-1], m.centres[j]
		}
	}
	return m
}

// rgbaConverter turns decoded YUV frames back into RGBA.
//
// Reading image.YCbCr directly would apply the full-range JPEG matrix to what
// is limited-range video, which skews saturated colours by more than the
// compression itself does. Going back through swscale applies the same
// inverse transform a player would.
type rgbaConverter struct {
	sws  *astiav.SoftwareScaleContext
	dst  *astiav.Frame
	img  *image.RGBA
	w, h int
}

func newRGBAConverter(t *testing.T, w, h int, src astiav.PixelFormat) *rgbaConverter {
	t.Helper()
	sws, err := astiav.CreateSoftwareScaleContext(w, h, src, w, h, astiav.PixelFormatRgba,
		astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagBilinear))
	if err != nil {
		t.Fatalf("creating scale context: %v", err)
	}
	dst := astiav.AllocFrame()
	dst.SetWidth(w)
	dst.SetHeight(h)
	dst.SetPixelFormat(astiav.PixelFormatRgba)
	if err := dst.AllocBuffer(1); err != nil {
		t.Fatalf("allocating RGBA frame: %v", err)
	}
	return &rgbaConverter{sws: sws, dst: dst, img: image.NewRGBA(image.Rect(0, 0, w, h)), w: w, h: h}
}

func (c *rgbaConverter) close() {
	c.sws.Free()
	c.dst.Free()
}

// centrePixel converts a decoded frame to RGBA and reads its middle pixel.
func (c *rgbaConverter) centrePixel(t *testing.T, f *astiav.Frame) color.RGBA {
	t.Helper()
	if err := c.sws.ScaleFrame(f, c.dst); err != nil {
		t.Fatalf("scaling frame to RGBA: %v", err)
	}
	if err := c.dst.Data().ToImage(c.img); err != nil {
		t.Fatalf("converting frame to image: %v", err)
	}
	return c.img.RGBAAt(c.w/2, c.h/2)
}

// closeEnough allows for the round trip through 4:2:0 chroma subsampling and
// lossy compression, which shifts flat colours by a few levels.
func closeEnough(got, want color.RGBA) bool {
	const tolerance = 6
	d := func(a, b uint8) int { return int(a) - int(b) }
	return abs(d(got.R, want.R)) <= tolerance &&
		abs(d(got.G, want.G)) <= tolerance &&
		abs(d(got.B, want.B)) <= tolerance
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
