// Package audio decodes audio to mono PCM for analysis.
//
// Decoding goes through libav* (via go-astiav), so anything the local FFmpeg
// build can demux and decode works: MP3, AAC/M4A, FLAC, Ogg Vorbis, Opus, WAV,
// AIFF, WMA and so on. There is no format allow-list here on purpose.
package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/asticode/go-astiav"

	"github.com/simonwistow/audiotexture/internal/avutil"
)

// DefaultSampleRate is the rate audio is resampled to for analysis. 22050 Hz
// is the librosa default and is plenty for onset detection: it puts Nyquist at
// 11 kHz, well above the percussive energy that drives an onset envelope,
// while halving the FFT work relative to 44.1 kHz.
const DefaultSampleRate = 22050

// resampleChunk is the output frame size handed to libswresample.
const resampleChunk = 4096

// PCM is a single channel of decoded audio, normalised to [-1, 1].
type PCM struct {
	Samples    []float64
	SampleRate int
}

// Duration returns the length of the signal in seconds.
func (p *PCM) Duration() float64 {
	if p.SampleRate <= 0 {
		return 0
	}
	return float64(len(p.Samples)) / float64(p.SampleRate)
}

// Decode reads the whole of r, from its start, and returns it as mono PCM
// resampled to sampleRate. The format is detected from the content, not from
// a name. Multi-channel input is downmixed to mono by libswresample. Pass 0
// for sampleRate to use DefaultSampleRate.
func Decode(r io.ReadSeeker, sampleRate int) (*PCM, error) {
	if sampleRate <= 0 {
		sampleRate = DefaultSampleRate
	}
	d := &decoder{sampleRate: sampleRate}
	defer d.close()
	if err := d.open(r); err != nil {
		return nil, err
	}
	if err := d.run(); err != nil {
		return nil, err
	}
	if len(d.out) == 0 {
		return nil, errors.New("decoded no audio")
	}
	return &PCM{Samples: d.out, SampleRate: sampleRate}, nil
}

// decoder holds the libav* objects for one Decode call. Every field that owns
// C memory is released by close, which is safe to call at any point.
type decoder struct {
	sampleRate int

	in        *avutil.Input
	fc        *astiav.FormatContext
	stream    *astiav.Stream
	cc        *astiav.CodecContext
	swr       *astiav.SoftwareResampleContext
	pkt       *astiav.Packet
	decoded   *astiav.Frame
	resampled *astiav.Frame

	out []float64
	// corrupt counts packets the decoder refused outright.
	corrupt int
}

func (d *decoder) open(r io.ReadSeeker) error {
	in, err := avutil.OpenAudio(r)
	if err != nil {
		return err
	}
	d.in, d.fc, d.stream = in, in.FC, in.Stream
	stream, codec := in.Stream, in.Codec

	if d.cc = astiav.AllocCodecContext(codec); d.cc == nil {
		return errors.New("allocating codec context failed")
	}
	if err := stream.CodecParameters().ToCodecContext(d.cc); err != nil {
		return fmt.Errorf("copying codec parameters: %w", err)
	}
	if err := d.cc.Open(codec, nil); err != nil {
		return fmt.Errorf("opening decoder %s: %w", codec.Name(), err)
	}

	// Resample everything to mono packed float32 at the analysis rate.
	d.swr = astiav.AllocSoftwareResampleContext()
	d.resampled = astiav.AllocFrame()
	d.resampled.SetChannelLayout(astiav.ChannelLayoutMono)
	d.resampled.SetSampleFormat(astiav.SampleFormatFlt)
	d.resampled.SetSampleRate(d.sampleRate)
	d.resampled.SetNbSamples(resampleChunk)
	if err := d.resampled.AllocBuffer(0); err != nil {
		return fmt.Errorf("allocating resample buffer: %w", err)
	}

	d.pkt = astiav.AllocPacket()
	d.decoded = astiav.AllocFrame()

	// Pre-size from the container duration when it reports one; for a VBR MP3
	// this is an estimate, so it is a hint to append and nothing more.
	if us := d.fc.Duration(); us > 0 {
		d.out = make([]float64, 0, int(float64(us)/1e6*float64(d.sampleRate)))
	}
	return nil
}

func (d *decoder) close() {
	// Reverse allocation order; each field may be nil if open failed early.
	if d.decoded != nil {
		d.decoded.Free()
	}
	if d.pkt != nil {
		d.pkt.Free()
	}
	if d.resampled != nil {
		d.resampled.Free()
	}
	if d.swr != nil {
		d.swr.Free()
	}
	if d.cc != nil {
		d.cc.Free()
	}
	if d.in != nil {
		d.in.Close()
	}
}

// run demuxes the audio stream, decodes it, and resamples it into d.out.
func (d *decoder) run() error {
	for {
		if err := d.fc.ReadFrame(d.pkt); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				break
			}
			return fmt.Errorf("reading packet: %w", err)
		}
		if err := d.sendPacket(); err != nil {
			return err
		}
	}

	// Flush the decoder, then the resampler.
	if err := d.cc.SendPacket(nil); err != nil && !errors.Is(err, astiav.ErrEof) {
		return fmt.Errorf("flushing decoder: %w", err)
	}
	if err := d.receiveFrames(); err != nil {
		return err
	}
	return d.flushResampler()
}

func (d *decoder) sendPacket() error {
	defer d.pkt.Unref()
	if d.pkt.StreamIndex() != d.stream.Index() {
		return nil
	}
	if err := d.cc.SendPacket(d.pkt); err != nil {
		// A malformed frame is not a reason to abandon the file. Real-world
		// MP3s contain them -- ffmpeg logs "Error submitting packet to
		// decoder" and carries on -- and one bad frame in a four-minute track
		// is inaudible. Skip it and keep going; if the whole file is
		// unreadable, Decode still fails on having produced no samples.
		d.corrupt++
		return nil
	}
	return d.receiveFrames()
}

// receiveFrames pulls every frame the decoder has ready and resamples each.
func (d *decoder) receiveFrames() error {
	for {
		if err := d.cc.ReceiveFrame(d.decoded); err != nil {
			if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
				return nil
			}
			return fmt.Errorf("receiving frame from decoder: %w", err)
		}
		if err := d.resample(); err != nil {
			return err
		}
	}
}

func (d *decoder) resample() error {
	defer d.decoded.Unref()
	avutil.NormalizeChannelLayout(d.decoded)
	if err := d.swr.ConvertFrame(d.decoded, d.resampled); err != nil {
		return fmt.Errorf("resampling: %w", err)
	}
	if err := d.appendResampled(); err != nil {
		return err
	}
	// One input frame can yield more than resampleChunk output samples; keep
	// pulling while the resampler is holding at least a full chunk.
	for d.swr.Delay(int64(d.sampleRate)) >= resampleChunk {
		if err := d.swr.ConvertFrame(nil, d.resampled); err != nil {
			return fmt.Errorf("resampling: %w", err)
		}
		if d.resampled.NbSamples() == 0 {
			break
		}
		if err := d.appendResampled(); err != nil {
			return err
		}
	}
	return nil
}

// flushResampler drains whatever libswresample is still holding at EOF.
func (d *decoder) flushResampler() error {
	for {
		if err := d.swr.ConvertFrame(nil, d.resampled); err != nil {
			return fmt.Errorf("flushing resampler: %w", err)
		}
		if d.resampled.NbSamples() == 0 {
			return nil
		}
		if err := d.appendResampled(); err != nil {
			return err
		}
	}
}

// appendResampled converts the mono float32 output frame to float64.
func (d *decoder) appendResampled() error {
	n := d.resampled.NbSamples()
	if n <= 0 {
		return nil
	}
	b, err := d.resampled.Data().Bytes(0)
	if err != nil {
		return fmt.Errorf("reading frame data: %w", err)
	}
	if want := n * 4; len(b) < want {
		return fmt.Errorf("short resampled frame: got %d bytes, want %d", len(b), want)
	} else {
		b = b[:want]
	}
	for i := 0; i < n; i++ {
		d.out = append(d.out, float64(math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))))
	}
	return nil
}
