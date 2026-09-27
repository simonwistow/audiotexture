// Package avutil holds small libav* helpers shared by the decoding and
// encoding sides of audiotexture.
package avutil

import (
	"regexp"

	"github.com/asticode/go-astiav"
)

// unspecifiedLayout matches how FFmpeg describes a channel layout whose order
// is AV_CHANNEL_ORDER_UNSPEC, e.g. "1 channels" or "2 channels".
var unspecifiedLayout = regexp.MustCompile(`^\d+ channels$`)

// canonicalLayouts mirrors av_channel_layout_default for the channel counts we
// are likely to meet. Used only to replace an unspecified layout.
var canonicalLayouts = map[int]astiav.ChannelLayout{
	1: astiav.ChannelLayoutMono,
	2: astiav.ChannelLayoutStereo,
	3: astiav.ChannelLayoutSurround,
	4: astiav.ChannelLayoutQuad,
	5: astiav.ChannelLayout5Point0,
	6: astiav.ChannelLayout5Point1,
	7: astiav.ChannelLayout6Point1,
	8: astiav.ChannelLayout7Point1,
}

// NormalizeChannelLayout replaces an unspecified channel layout on f with the
// canonical layout for that channel count.
//
// Some decoders (notably PCM in WAV) report a layout with no channel order,
// described as "N channels". libswresample normalizes that to a real layout
// when it configures itself from the first frame, but then compares every
// later frame against the normalized version — so the second call onwards
// fails with AVERROR_INPUT_CHANGED even though nothing actually changed.
// Doing the normalization up front keeps the comparison stable.
//
// Layouts that do specify an order are left alone, so a genuine 5.1(side)
// stream is not silently reinterpreted as 5.1.
func NormalizeChannelLayout(f *astiav.Frame) {
	l := f.ChannelLayout()
	if !unspecifiedLayout.MatchString(l.String()) {
		return
	}
	if canonical, ok := canonicalLayouts[l.Channels()]; ok {
		f.SetChannelLayout(canonical)
	}
}
