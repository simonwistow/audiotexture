// Package video encodes a texture assignment and its soundtrack into a movie
// file.
//
// Encoding goes through libav* (via go-astiav) in-process: nothing is written
// to a scratch directory and no external binary is executed.
package video

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"path/filepath"
	"sort"

	"github.com/asticode/go-astiav"

	"github.com/simonwistow/audiotexture/texture"
)

// Default output settings.
const (
	DefaultWidth        = 1280
	DefaultHeight       = 720
	DefaultFrameRate    = 24 // what the original Perl script assumed
	DefaultCRF          = 20
	DefaultPreset       = "medium"
	DefaultAudioBitRate = 192_000
)

// Options controls the output file. The zero value is usable: every field
// falls back to the Default* constant above.
type Options struct {
	Width, Height int
	FrameRate     float64
	// CRF is the x264 quality target: lower is better, 18-24 is a sane range.
	// Ignored if the available H.264 encoder is not libx264.
	CRF int
	// Preset is the x264 speed/compression tradeoff, e.g. "veryfast",
	// "medium", "slow". Ignored if the encoder is not libx264.
	Preset string
	// Background fills the letterbox bars. Defaults to opaque black.
	Background   color.Color
	AudioBitRate int64
	// Progress, if set, is called as frames are encoded.
	Progress func(frame, total int)
}

func (o *Options) applyDefaults() {
	if o.Width <= 0 {
		o.Width = DefaultWidth
	}
	if o.Height <= 0 {
		o.Height = DefaultHeight
	}
	// H.264 with 4:2:0 chroma needs even dimensions.
	o.Width += o.Width % 2
	o.Height += o.Height % 2

	if o.FrameRate <= 0 {
		o.FrameRate = DefaultFrameRate
	}
	if o.CRF <= 0 {
		o.CRF = DefaultCRF
	}
	if o.Preset == "" {
		o.Preset = DefaultPreset
	}
	if o.Background == nil {
		o.Background = color.Black
	}
	if o.AudioBitRate <= 0 {
		o.AudioBitRate = DefaultAudioBitRate
	}
}

// Encode writes a movie to outPath showing each onset's image from its Start
// until the next one, for duration seconds, with audioPath as the soundtrack.
//
// onsets must be sorted ascending by Start; Encode sorts a copy if it is not.
func Encode(outPath string, onsets []texture.Onset, audioPath string, duration float64, opts Options) error {
	if len(onsets) == 0 {
		return errors.New("no onsets to encode")
	}
	if duration <= 0 {
		return fmt.Errorf("duration must be positive, got %v", duration)
	}
	opts.applyDefaults()

	sorted := make([]texture.Onset, len(onsets))
	copy(sorted, onsets)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })

	e := &encoder{opts: opts, onsets: sorted, duration: duration}
	defer e.close()
	if err := e.open(outPath, audioPath); err != nil {
		return err
	}
	return e.run()
}

type encoder struct {
	opts     Options
	onsets   []texture.Onset
	duration float64

	ofc      *astiav.FormatContext
	io       *astiav.IOContext
	wroteHdr bool

	videoStream *astiav.Stream
	videoCC     *astiav.CodecContext
	videoFrame  *astiav.Frame
	still       *stillConverter

	audioStream *astiav.Stream
	audioCC     *astiav.CodecContext
	audio       *audioTrack

	pkt         *astiav.Packet
	timeBase    astiav.Rational
	totalFrames int
}

func (e *encoder) open(outPath, audioPath string) error {
	ofc, err := astiav.AllocOutputFormatContext(nil, "", outPath)
	if err != nil {
		return fmt.Errorf("allocating output context for %s: %w", outPath, err)
	}
	if ofc == nil {
		return fmt.Errorf("could not guess an output format for %s", filepath.Base(outPath))
	}
	e.ofc = ofc

	if err := e.openVideo(); err != nil {
		return err
	}
	if audioPath != "" {
		if err := e.openAudio(audioPath); err != nil {
			return err
		}
	}

	if !e.ofc.OutputFormat().Flags().Has(astiav.IOFormatFlagNofile) {
		io, err := astiav.OpenIOContext(outPath, astiav.NewIOContextFlags(astiav.IOContextFlagWrite), nil, nil)
		if err != nil {
			return fmt.Errorf("creating %s: %w", outPath, err)
		}
		e.io = io
		e.ofc.SetPb(io)
	}

	// faststart moves the index to the front so the file streams without a
	// full download; harmless for local playback, useful for uploads.
	dict := astiav.NewDictionary()
	defer dict.Free()
	if err := dict.Set("movflags", "+faststart", astiav.NewDictionaryFlags()); err != nil {
		return fmt.Errorf("setting muxer options: %w", err)
	}
	if err := e.ofc.WriteHeader(dict); err != nil {
		return fmt.Errorf("writing header to %s: %w", outPath, err)
	}
	e.wroteHdr = true

	e.pkt = astiav.AllocPacket()
	e.totalFrames = int(math.Ceil(e.duration * e.opts.FrameRate))
	return nil
}

func (e *encoder) openVideo() error {
	codec := astiav.FindEncoderByName("libx264")
	if codec == nil {
		codec = astiav.FindEncoder(astiav.CodecIDH264)
	}
	if codec == nil {
		return errors.New("no H.264 encoder in this FFmpeg build")
	}

	if e.videoStream = e.ofc.NewStream(nil); e.videoStream == nil {
		return errors.New("allocating video stream failed")
	}
	if e.videoCC = astiav.AllocCodecContext(codec); e.videoCC == nil {
		return errors.New("allocating video encoder context failed")
	}

	num, den := frameRateRational(e.opts.FrameRate)
	e.timeBase = astiav.NewRational(den, num)

	e.videoCC.SetWidth(e.opts.Width)
	e.videoCC.SetHeight(e.opts.Height)
	e.videoCC.SetPixelFormat(astiav.PixelFormatYuv420P)
	e.videoCC.SetTimeBase(e.timeBase)
	e.videoCC.SetFramerate(astiav.NewRational(num, den))
	// A keyframe every two seconds keeps seeking responsive without costing
	// much: consecutive frames are usually identical, so P-frames are tiny.
	e.videoCC.SetGopSize(int(math.Round(e.opts.FrameRate * 2)))
	if e.ofc.OutputFormat().Flags().Has(astiav.IOFormatFlagGlobalheader) {
		e.videoCC.SetFlags(e.videoCC.Flags().Add(astiav.CodecContextFlagGlobalHeader))
	}

	dict := astiav.NewDictionary()
	defer dict.Free()
	if codec.Name() == "libx264" {
		if err := dict.Set("crf", fmt.Sprint(e.opts.CRF), astiav.NewDictionaryFlags()); err != nil {
			return fmt.Errorf("setting crf: %w", err)
		}
		if err := dict.Set("preset", e.opts.Preset, astiav.NewDictionaryFlags()); err != nil {
			return fmt.Errorf("setting preset: %w", err)
		}
	}
	if err := e.videoCC.Open(codec, dict); err != nil {
		return fmt.Errorf("opening %s encoder: %w", codec.Name(), err)
	}
	if err := e.videoStream.CodecParameters().FromCodecContext(e.videoCC); err != nil {
		return fmt.Errorf("copying video codec parameters: %w", err)
	}
	e.videoStream.SetTimeBase(e.videoCC.TimeBase())

	frame, err := allocVideoFrame(e.opts.Width, e.opts.Height)
	if err != nil {
		return err
	}
	e.videoFrame = frame

	still, err := newStillConverter(e.opts.Width, e.opts.Height, e.opts.Background)
	if err != nil {
		return err
	}
	e.still = still
	return nil
}

func (e *encoder) openAudio(audioPath string) error {
	codec := astiav.FindEncoderByName("aac")
	if codec == nil {
		codec = astiav.FindEncoder(astiav.CodecIDAac)
	}
	if codec == nil {
		return errors.New("no AAC encoder in this FFmpeg build")
	}

	rate, layout, err := audioParams(audioPath, codec)
	if err != nil {
		return err
	}

	if e.audioStream = e.ofc.NewStream(nil); e.audioStream == nil {
		return errors.New("allocating audio stream failed")
	}
	if e.audioCC = astiav.AllocCodecContext(codec); e.audioCC == nil {
		return errors.New("allocating audio encoder context failed")
	}

	sampleFormat := astiav.SampleFormatFltp
	if formats := codec.SupportedSampleFormats(); len(formats) > 0 {
		sampleFormat = formats[0]
	}
	e.audioCC.SetSampleFormat(sampleFormat)
	e.audioCC.SetSampleRate(rate)
	e.audioCC.SetChannelLayout(layout)
	e.audioCC.SetBitRate(e.opts.AudioBitRate)
	e.audioCC.SetTimeBase(astiav.NewRational(1, rate))
	if e.ofc.OutputFormat().Flags().Has(astiav.IOFormatFlagGlobalheader) {
		e.audioCC.SetFlags(e.audioCC.Flags().Add(astiav.CodecContextFlagGlobalHeader))
	}

	if err := e.audioCC.Open(codec, nil); err != nil {
		return fmt.Errorf("opening AAC encoder: %w", err)
	}
	if err := e.audioStream.CodecParameters().FromCodecContext(e.audioCC); err != nil {
		return fmt.Errorf("copying audio codec parameters: %w", err)
	}
	e.audioStream.SetTimeBase(e.audioCC.TimeBase())

	track, err := newAudioTrack(audioPath, e.audioCC)
	if err != nil {
		return err
	}
	e.audio = track
	return nil
}

func (e *encoder) close() {
	if e.audio != nil {
		e.audio.Close()
	}
	if e.still != nil {
		e.still.Close()
	}
	if e.videoFrame != nil {
		e.videoFrame.Free()
	}
	if e.pkt != nil {
		e.pkt.Free()
	}
	if e.audioCC != nil {
		e.audioCC.Free()
	}
	if e.videoCC != nil {
		e.videoCC.Free()
	}
	if e.io != nil {
		_ = e.io.Close()
	}
	if e.ofc != nil {
		e.ofc.Free()
	}
}

// run interleaves the two streams by timestamp, always advancing whichever is
// further behind, so the muxer never has to buffer a whole track.
func (e *encoder) run() error {
	frame := 0
	onset := 0
	var loaded string

	audioReady, err := e.nextAudio()
	if err != nil {
		return err
	}

	for frame < e.totalFrames || audioReady != nil {
		videoTime := float64(frame) / e.opts.FrameRate
		encodeVideo := frame < e.totalFrames &&
			(audioReady == nil || videoTime <= e.audio.NextPTS())

		if encodeVideo {
			// Advance to the shot covering this instant. Converting an image
			// costs far more than encoding a repeat of it, so only reconvert
			// when the shot actually changes.
			for onset+1 < len(e.onsets) && e.onsets[onset+1].Start <= videoTime {
				onset++
			}
			if e.onsets[onset].Image != loaded {
				if err := e.still.Convert(e.onsets[onset].Image, e.videoFrame); err != nil {
					return err
				}
				loaded = e.onsets[onset].Image
			}
			e.videoFrame.SetPts(int64(frame))
			if err := e.writeFrame(e.videoCC, e.videoStream, e.videoFrame); err != nil {
				return fmt.Errorf("encoding frame %d: %w", frame, err)
			}
			frame++
			if e.opts.Progress != nil {
				e.opts.Progress(frame, e.totalFrames)
			}
			continue
		}

		if err := e.writeFrame(e.audioCC, e.audioStream, audioReady); err != nil {
			return fmt.Errorf("encoding audio: %w", err)
		}
		if audioReady, err = e.nextAudio(); err != nil {
			return err
		}
	}

	// Flush both encoders, then finalise the container.
	if err := e.writeFrame(e.videoCC, e.videoStream, nil); err != nil {
		return fmt.Errorf("flushing video encoder: %w", err)
	}
	if e.audioCC != nil {
		if err := e.writeFrame(e.audioCC, e.audioStream, nil); err != nil {
			return fmt.Errorf("flushing audio encoder: %w", err)
		}
	}
	if err := e.ofc.WriteTrailer(); err != nil {
		return fmt.Errorf("writing trailer: %w", err)
	}
	return nil
}

func (e *encoder) nextAudio() (*astiav.Frame, error) {
	if e.audio == nil {
		return nil, nil
	}
	return e.audio.Next()
}

// writeFrame sends one frame to an encoder and muxes everything it returns.
// A nil frame flushes.
func (e *encoder) writeFrame(cc *astiav.CodecContext, stream *astiav.Stream, f *astiav.Frame) error {
	if err := cc.SendFrame(f); err != nil {
		return fmt.Errorf("sending frame to encoder: %w", err)
	}
	for {
		if err := cc.ReceivePacket(e.pkt); err != nil {
			if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
				return nil
			}
			return fmt.Errorf("receiving packet from encoder: %w", err)
		}
		err := func() error {
			defer e.pkt.Unref()
			e.pkt.SetStreamIndex(stream.Index())
			e.pkt.RescaleTs(cc.TimeBase(), stream.TimeBase())
			if err := e.ofc.WriteInterleavedFrame(e.pkt); err != nil {
				return fmt.Errorf("muxing packet: %w", err)
			}
			return nil
		}()
		if err != nil {
			return err
		}
	}
}

// audioParams picks a sample rate and channel layout the encoder supports,
// staying as close to the source as it can.
func audioParams(path string, codec *astiav.Codec) (int, astiav.ChannelLayout, error) {
	rate, channels, err := probeAudio(path)
	if err != nil {
		return 0, astiav.ChannelLayout{}, err
	}

	if supported := codec.SupportedSampleRates(); len(supported) > 0 {
		best, bestDiff := supported[0], math.MaxInt
		for _, r := range supported {
			if d := abs(r - rate); d < bestDiff {
				best, bestDiff = r, d
			}
		}
		rate = best
	}

	layout := astiav.ChannelLayoutStereo
	if channels == 1 {
		layout = astiav.ChannelLayoutMono
	}
	if supported := codec.SupportedChannelLayouts(); len(supported) > 0 {
		ok := false
		for _, l := range supported {
			if l.Equal(layout) {
				ok = true
				break
			}
		}
		if !ok {
			layout = supported[0]
		}
	}
	return rate, layout, nil
}

// probeAudio reports the sample rate and channel count of path's best audio
// stream without decoding it.
func probeAudio(path string) (rate, channels int, err error) {
	fc := astiav.AllocFormatContext()
	if fc == nil {
		return 0, 0, errors.New("allocating format context failed")
	}
	defer fc.Free()
	if err := fc.OpenInput(path, nil, nil); err != nil {
		return 0, 0, fmt.Errorf("opening %s: %w", path, err)
	}
	defer fc.CloseInput()
	if err := fc.FindStreamInfo(nil); err != nil {
		return 0, 0, fmt.Errorf("finding stream info in %s: %w", path, err)
	}
	stream, _, err := fc.FindBestStream(astiav.MediaTypeAudio, -1, -1)
	if err != nil {
		return 0, 0, fmt.Errorf("no audio stream in %s: %w", path, err)
	}
	cp := stream.CodecParameters()
	return cp.SampleRate(), cp.ChannelLayout().Channels(), nil
}

// frameRateRational expresses fps exactly where it can: whole rates become
// n/1, and the NTSC rates become the usual 1000/1001 fractions rather than a
// rounded decimal.
func frameRateRational(fps float64) (num, den int) {
	if r := math.Round(fps); math.Abs(fps-r) < 1e-9 {
		return int(r), 1
	}
	for _, base := range []int{24, 30, 60, 120} {
		if math.Abs(fps-float64(base)*1000/1001) < 1e-4 {
			return base * 1000, 1001
		}
	}
	return int(math.Round(fps * 1000)), 1000
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
