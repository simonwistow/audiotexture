package video

import (
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"

	"github.com/simonwistow/audiotexture/internal/avutil"
)

// audioTrack transcodes the soundtrack for the output file: demux and decode
// the source, resample to whatever the encoder wants, buffer through a FIFO so
// the encoder gets exactly the frame size it asks for, and encode.
//
// Next yields one encoder-ready frame at a time so the muxing loop can
// interleave audio and video by timestamp instead of buffering a whole track.
type audioTrack struct {
	// Input.
	in      *avutil.Input
	dec     *astiav.CodecContext
	pkt     *astiav.Packet
	decoded *astiav.Frame

	// Resampling into the encoder's format.
	swr       *astiav.SoftwareResampleContext
	resampled *astiav.Frame
	fifo      *astiav.AudioFifo

	// Output.
	enc       *astiav.CodecContext
	frameSize int
	out       *astiav.Frame
	nextPTS   int64

	inputDone bool
	done      bool
	// corrupt counts packets the decoder refused outright.
	corrupt int
}

// newAudioTrack prepares in for encoding with enc. It takes ownership of in,
// which it closes on failure and on Close.
func newAudioTrack(in *avutil.Input, enc *astiav.CodecContext) (*audioTrack, error) {
	t := &audioTrack{in: in, enc: enc, frameSize: enc.FrameSize()}

	// Some encoders accept any frame size; pick something reasonable.
	if t.frameSize <= 0 {
		t.frameSize = 1024
	}

	if err := t.openDecoder(); err != nil {
		t.Close()
		return nil, err
	}
	if err := t.setupResampler(); err != nil {
		t.Close()
		return nil, err
	}
	return t, nil
}

func (t *audioTrack) openDecoder() error {
	stream, codec := t.in.Stream, t.in.Codec
	if t.dec = astiav.AllocCodecContext(codec); t.dec == nil {
		return errors.New("allocating decoder context failed")
	}
	if err := stream.CodecParameters().ToCodecContext(t.dec); err != nil {
		return fmt.Errorf("copying codec parameters: %w", err)
	}
	if err := t.dec.Open(codec, nil); err != nil {
		return fmt.Errorf("opening decoder %s: %w", codec.Name(), err)
	}

	t.pkt = astiav.AllocPacket()
	t.decoded = astiav.AllocFrame()
	return nil
}

func (t *audioTrack) setupResampler() error {
	t.swr = astiav.AllocSoftwareResampleContext()

	t.resampled = astiav.AllocFrame()
	t.resampled.SetChannelLayout(t.enc.ChannelLayout())
	t.resampled.SetSampleFormat(t.enc.SampleFormat())
	t.resampled.SetSampleRate(t.enc.SampleRate())
	t.resampled.SetNbSamples(t.frameSize)
	if err := t.resampled.AllocBuffer(0); err != nil {
		return fmt.Errorf("allocating resample buffer: %w", err)
	}

	t.fifo = astiav.AllocAudioFifo(t.enc.SampleFormat(), t.enc.ChannelLayout().Channels(), t.frameSize)
	if t.fifo == nil {
		return errors.New("allocating audio FIFO failed")
	}

	t.out = astiav.AllocFrame()
	t.out.SetChannelLayout(t.enc.ChannelLayout())
	t.out.SetSampleFormat(t.enc.SampleFormat())
	t.out.SetSampleRate(t.enc.SampleRate())
	t.out.SetNbSamples(t.frameSize)
	if err := t.out.AllocBuffer(0); err != nil {
		return fmt.Errorf("allocating encoder frame buffer: %w", err)
	}
	return nil
}

func (t *audioTrack) Close() {
	if t.out != nil {
		t.out.Free()
		t.out = nil
	}
	if t.fifo != nil {
		t.fifo.Free()
		t.fifo = nil
	}
	if t.resampled != nil {
		t.resampled.Free()
		t.resampled = nil
	}
	if t.swr != nil {
		t.swr.Free()
		t.swr = nil
	}
	if t.decoded != nil {
		t.decoded.Free()
		t.decoded = nil
	}
	if t.pkt != nil {
		t.pkt.Free()
		t.pkt = nil
	}
	if t.dec != nil {
		t.dec.Free()
		t.dec = nil
	}
	if t.in != nil {
		t.in.Close()
		t.in = nil
	}
}

// NextPTS is the timestamp, in seconds, of the frame Next will return. Used by
// the muxing loop to decide whether audio or video is further behind.
func (t *audioTrack) NextPTS() float64 {
	return float64(t.nextPTS) / float64(t.enc.SampleRate())
}

// Next returns the next frame to hand to the encoder, or nil once the track is
// exhausted. The returned frame is reused between calls.
func (t *audioTrack) Next() (*astiav.Frame, error) {
	if t.done {
		return nil, nil
	}
	if err := t.fill(); err != nil {
		return nil, err
	}

	avail := t.fifo.Size()
	if avail == 0 {
		t.done = true
		return nil, nil
	}

	// The last frame of a track is usually short.
	n := min(avail, t.frameSize)
	if err := t.out.MakeWritable(); err != nil {
		return nil, fmt.Errorf("making audio frame writable: %w", err)
	}
	t.out.SetNbSamples(n)
	if _, err := t.fifo.Read(t.out); err != nil {
		return nil, fmt.Errorf("reading from audio FIFO: %w", err)
	}
	t.out.SetPts(t.nextPTS)
	t.nextPTS += int64(n)
	return t.out, nil
}

// fill decodes and resamples until the FIFO holds a full frame or the input
// runs out.
func (t *audioTrack) fill() error {
	for !t.inputDone && t.fifo.Size() < t.frameSize {
		if err := t.in.FC.ReadFrame(t.pkt); err != nil {
			if !errors.Is(err, astiav.ErrEof) {
				return fmt.Errorf("reading audio packet: %w", err)
			}
			if err := t.finishInput(); err != nil {
				return err
			}
			continue
		}
		if err := t.handlePacket(); err != nil {
			return err
		}
	}
	return nil
}

// finishInput flushes the decoder and the resampler at end of input.
func (t *audioTrack) finishInput() error {
	t.inputDone = true
	if err := t.dec.SendPacket(nil); err != nil && !errors.Is(err, astiav.ErrEof) {
		return fmt.Errorf("flushing audio decoder: %w", err)
	}
	if err := t.receive(); err != nil {
		return err
	}
	for {
		if err := t.swr.ConvertFrame(nil, t.resampled); err != nil {
			return fmt.Errorf("flushing audio resampler: %w", err)
		}
		if t.resampled.NbSamples() == 0 {
			return nil
		}
		if err := t.writeFifo(); err != nil {
			return err
		}
	}
}

func (t *audioTrack) handlePacket() error {
	defer t.pkt.Unref()
	if t.pkt.StreamIndex() != t.in.Stream.Index() {
		return nil
	}
	if err := t.dec.SendPacket(t.pkt); err != nil {
		// Skip malformed frames rather than abandoning the soundtrack; see
		// the same decision in package audio.
		t.corrupt++
		return nil
	}
	return t.receive()
}

func (t *audioTrack) receive() error {
	for {
		if err := t.dec.ReceiveFrame(t.decoded); err != nil {
			if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
				return nil
			}
			return fmt.Errorf("receiving audio frame: %w", err)
		}
		if err := t.resample(); err != nil {
			return err
		}
	}
}

func (t *audioTrack) resample() error {
	defer t.decoded.Unref()
	avutil.NormalizeChannelLayout(t.decoded)
	if err := t.swr.ConvertFrame(t.decoded, t.resampled); err != nil {
		return fmt.Errorf("resampling audio: %w", err)
	}
	if err := t.writeFifo(); err != nil {
		return err
	}
	for t.swr.Delay(int64(t.enc.SampleRate())) >= int64(t.frameSize) {
		if err := t.swr.ConvertFrame(nil, t.resampled); err != nil {
			return fmt.Errorf("resampling audio: %w", err)
		}
		if t.resampled.NbSamples() == 0 {
			break
		}
		if err := t.writeFifo(); err != nil {
			return err
		}
	}
	return nil
}

func (t *audioTrack) writeFifo() error {
	if t.resampled.NbSamples() <= 0 {
		return nil
	}
	if _, err := t.fifo.Write(t.resampled); err != nil {
		return fmt.Errorf("writing to audio FIFO: %w", err)
	}
	return nil
}
