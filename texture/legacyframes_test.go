package texture_test

import (
	"math"
	"testing"

	"github.com/simonwistow/audiotexture/texture"
)

// renderLoop is what render.FrameDirectory and video.Encode do: show the image
// belonging to the latest onset at or before each frame's timestamp.
func renderLoop(onsets []texture.Onset, duration, frameRate float64) []string {
	total := int(duration*frameRate) + 1
	out := make([]string, total)
	j := 0
	for n := range total {
		t := float64(n) / frameRate
		for j+1 < len(onsets) && onsets[j+1].Start <= t {
			j++
		}
		out[n] = onsets[j].Image
	}
	return out
}

// perlFrameLoop is the frame-writing loop at the end of slideshow.pl,
// transcribed directly.
func perlFrameLoop(onsets []texture.Onset, duration, frameRate float64) []string {
	starts := make([]float64, 0, len(onsets)+1)
	for _, o := range onsets {
		starts = append(starts, o.Start)
	}
	starts = append(starts, duration)

	var out []string
	n, j := 0, 0
	for range int(duration*frameRate) + 1 {
		if j < len(starts) && float64(n) >= frameRate*starts[j] {
			j++
		}
		if j < len(onsets) && j < len(starts) {
			out = append(out, onsets[j].Image)
			n++
		}
	}
	return out
}

// TestLegacyFrameSequenceMatchesPerlLoop is the point of the whole function:
// rendering its output normally must produce the same frames the original
// wrote.
func TestLegacyFrameSequenceMatchesPerlLoop(t *testing.T) {
	const frameRate = 24.0

	for _, tc := range []struct {
		name     string
		images   int
		duration float64
		// spacing builds the onset times; the interesting cases are the ones
		// where onsets crowd closer than one frame apart, since that is where
		// the Perl loop's one-advance-per-frame limit starts to lag.
		spacing func(i, n int, duration float64) float64
	}{
		{"even", 40, 60, func(i, n int, d float64) float64 { return float64(i) * d / float64(n) }},
		{"many images", 680, 293.5, func(i, n int, d float64) float64 { return float64(i) * d / float64(n) }},
		{"more images than frames", 200, 2, func(i, n int, d float64) float64 { return float64(i) * d / float64(n) }},
		{"front loaded", 50, 60, func(i, n int, d float64) float64 {
			x := float64(i) / float64(n)
			return d * x * x
		}},
		{"back loaded", 50, 60, func(i, n int, d float64) float64 {
			x := float64(i) / float64(n)
			return d * math.Sqrt(x)
		}},
		{"clustered", 60, 60, func(i, n int, d float64) float64 {
			// Groups of three within a few frames, then a long gap.
			return float64(i/3)*(d/float64((n+2)/3)) + float64(i%3)*0.02
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			onsets := make([]texture.Onset, tc.images)
			for i := range onsets {
				onsets[i] = texture.Onset{
					Image: names(tc.images)[i],
					Start: tc.spacing(i, tc.images, tc.duration),
				}
			}

			want := perlFrameLoop(onsets, tc.duration, frameRate)
			gotOnsets, gotDuration := texture.LegacyFrameSequence(onsets, tc.duration, frameRate)
			got := renderLoop(gotOnsets, gotDuration, frameRate)

			if len(got) != len(want) {
				t.Fatalf("rendered %d frames, the original loop wrote %d", len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("frame %d shows %s, the original loop wrote %s", i, got[i], want[i])
				}
			}
		})
	}
}

// TestLegacyFrameSequenceDropsTheFirstImage pins the most visible of the
// original's quirks.
func TestLegacyFrameSequenceDropsTheFirstImage(t *testing.T) {
	onsets := []texture.Onset{
		{Image: "001.jpg", Start: 0},
		{Image: "002.jpg", Start: 1},
		{Image: "003.jpg", Start: 2},
	}
	got, duration := texture.LegacyFrameSequence(onsets, 3, 24)
	if len(got) == 0 {
		t.Fatal("no onsets returned")
	}
	if got[0].Image != "002.jpg" {
		t.Errorf("first image shown is %s, want 002.jpg: the original never showed the first image",
			got[0].Image)
	}
	if got[0].Start != 0 {
		t.Errorf("first onset at %v, want 0", got[0].Start)
	}
	// And the movie stops at the last onset, not at the end of the audio.
	if duration >= 3 {
		t.Errorf("duration %v, want less than the 3s of audio", duration)
	}
}

func TestLegacyFrameSequenceDegenerate(t *testing.T) {
	// Nothing to do, and nothing that should panic.
	for _, tc := range []struct {
		name     string
		onsets   []texture.Onset
		duration float64
		rate     float64
	}{
		{"no onsets", nil, 10, 24},
		{"zero duration", []texture.Onset{{Image: "a"}}, 0, 24},
		{"zero frame rate", []texture.Onset{{Image: "a"}}, 10, 0},
		{"single image", []texture.Onset{{Image: "a"}}, 10, 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, duration := texture.LegacyFrameSequence(tc.onsets, tc.duration, tc.rate)
			if len(tc.onsets) > 0 && len(got) == 0 {
				t.Error("returned no onsets for a non-empty input")
			}
			if duration < 0 {
				t.Errorf("negative duration %v", duration)
			}
		})
	}
}
