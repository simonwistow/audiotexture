package avutil

import (
	"errors"
	"fmt"
	"io"

	"github.com/asticode/go-astiav"
)

// ioBufferSize is the buffer libav* reads and writes through. 64 KiB is what
// FFmpeg's own file protocol uses for a seekable file.
const ioBufferSize = 64 * 1024

// Flags FFmpeg ORs into the whence of a seek callback. They are fixed in
// avio.h and not exported by go-astiav.
const (
	avseekSize  = 0x10000 // AVSEEK_SIZE: report the stream size, do not move
	avseekForce = 0x20000 // AVSEEK_FORCE: a hint, safe to ignore
)

// NewReadContext wraps r so libav* can demux from it. FFmpeg addresses the
// stream by absolute offset from its start, so r is rewound first and read
// in full: the whole of r is the input, wherever it was positioned.
//
// The caller frees the context with Free, not Close: Close would treat it as
// one FFmpeg opened itself.
func NewReadContext(r io.ReadSeeker) (*astiav.IOContext, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewinding input: %w", err)
	}
	read := func(b []byte) (int, error) {
		// go-astiav drops any bytes that arrive together with an error, and
		// an io.Reader is allowed to return data and io.EOF in one call. Ask
		// for at least one byte, which yields either data and no error or no
		// data and an error, so the EOF arrives on the next call instead.
		return io.ReadAtLeast(r, b, 1)
	}
	return astiav.AllocIOContext(ioBufferSize, false, read, seeker(r, 0), nil)
}

// NewWriteContext wraps w so libav* can mux into it. Output starts at w's
// current position, which the muxer sees as offset zero, so a header written
// before the movie is left alone.
//
// The caller frees the context with Free, not Close, after the trailer is
// written.
func NewWriteContext(w io.WriteSeeker) (*astiav.IOContext, error) {
	base, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("finding output position: %w", err)
	}
	return astiav.AllocIOContext(ioBufferSize, true, nil, seeker(w, base), w.Write)
}

// seeker adapts s to FFmpeg's seek callback, with offset zero at base.
func seeker(s io.Seeker, base int64) astiav.IOContextSeekFunc {
	return func(offset int64, whence int) (int64, error) {
		if whence&avseekSize != 0 {
			cur, err := s.Seek(0, io.SeekCurrent)
			if err != nil {
				return 0, err
			}
			end, err := s.Seek(0, io.SeekEnd)
			if err != nil {
				return 0, err
			}
			if _, err := s.Seek(cur, io.SeekStart); err != nil {
				return 0, err
			}
			return end - base, nil
		}

		switch whence &^ avseekForce {
		case io.SeekStart:
			offset += base
		case io.SeekCurrent, io.SeekEnd:
		default:
			return 0, fmt.Errorf("unsupported seek whence %#x", whence)
		}
		pos, err := s.Seek(offset, whence&^avseekForce)
		return pos - base, err
	}
}

// Input is a demuxer reading an io.ReadSeeker, positioned on its best audio
// stream.
type Input struct {
	FC     *astiav.FormatContext
	Stream *astiav.Stream
	Codec  *astiav.Codec // a decoder for Stream

	pb *astiav.IOContext
}

// OpenAudio opens r for demuxing and finds its audio stream.
func OpenAudio(r io.ReadSeeker) (*Input, error) {
	in := &Input{}
	if err := in.open(r); err != nil {
		in.Close()
		return nil, err
	}
	return in, nil
}

func (in *Input) open(r io.ReadSeeker) error {
	pb, err := NewReadContext(r)
	if err != nil {
		return err
	}
	in.pb = pb

	if in.FC = astiav.AllocFormatContext(); in.FC == nil {
		return errors.New("allocating format context failed")
	}
	in.FC.SetPb(pb)
	// On failure avformat_open_input frees the context and clears the
	// pointer, so Close has nothing left to do for it but the IO context.
	if err := in.FC.OpenInput("", nil, nil); err != nil {
		return fmt.Errorf("opening audio: %w", err)
	}
	if err := in.FC.FindStreamInfo(nil); err != nil {
		return fmt.Errorf("finding audio stream info: %w", err)
	}
	if in.Stream, in.Codec, err = in.FC.FindBestStream(astiav.MediaTypeAudio, -1, -1); err != nil {
		return fmt.Errorf("no audio stream: %w", err)
	}
	return nil
}

// Close releases the demuxer and then the IO context under it. It is safe to
// call on a partly opened Input.
func (in *Input) Close() {
	if in.FC != nil {
		// A context opened with a caller-supplied pb is flagged custom IO,
		// so closing it leaves the pb for us to free.
		in.FC.CloseInput()
		in.FC.Free()
		in.FC = nil
	}
	if in.pb != nil {
		in.pb.Free()
		in.pb = nil
	}
}
