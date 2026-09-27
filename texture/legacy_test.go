package texture_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/simonwistow/audiotexture/texture"
)

// grid returns beats every period seconds up to duration.
func grid(period, duration float64) []float64 {
	var out []float64
	for i := 0; float64(i)*period < duration; i++ {
		out = append(out, float64(i)*period)
	}
	return out
}

func names(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%02d.jpg", i)
	}
	return out
}

// TestLegacyMatchesPerl pins the port to the original slideshow.pl.
//
// These expectations were produced by running lines 60-100 of the 2010 Perl
// verbatim over the same inputs and recording what it printed. The port was
// checked against it across 27 scenarios -- steady and irregular beat grids,
// image counts from 3 to 200, frame rates from 12 to 60, sparse beats that
// force the stack-and-redistribute path, and the degenerate no-beat and
// one-beat cases -- and agreed on every one. This table keeps the
// representative cases as a regression test without needing Perl.
func TestLegacyMatchesPerl(t *testing.T) {
	tests := []struct {
		name      string
		frameRate float64
		duration  float64
		images    int
		beats     []float64
		want      []float64
	}{
		{
			name:      "steady 120 BPM, 7 images",
			frameRate: 24, duration: 60, images: 7,
			beats: grid(0.5, 60),
			want:  []float64{0, 8.5, 17, 25.5, 34, 43, 51.5},
		},
		{
			name:      "steady 120 BPM, 40 images",
			frameRate: 24, duration: 60, images: 40,
			beats: grid(0.5, 60),
			want: []float64{
				0, 1.5, 3, 4.5, 6, 7.5, 9, 10.5, 12, 13.5, 15, 16.5, 18, 19.5,
				21, 22.5, 24, 25.5, 27, 28.5, 30, 31.5, 33, 34.5, 36, 37.5, 39,
				40.5, 42, 43.5, 45, 46.5, 48, 49.5, 51, 52.5, 54, 55.5, 57, 58.5,
			},
		},
		{
			// The original's special case: one beat per image, used directly.
			name:      "beats == images",
			frameRate: 24, duration: 60, images: 8,
			beats: []float64{0, 7, 14, 21, 28, 35, 42, 49},
			want:  []float64{0, 7, 14, 21, 28, 35, 42, 49},
		},
		{
			// Most images find no beat in range, so they stack up and get
			// spread between the ones that do. The final 56 is a straggler
			// left on the stack at the end, keeping its even-spacing position.
			name:      "sparse beats, many stacked",
			frameRate: 24, duration: 60, images: 15,
			beats: []float64{0, 7.3, 14.6, 21.9, 29.2, 36.5, 43.8, 51.1, 58.4},
			want: []float64{
				0, 3.645833, 7.291667, 10.9375, 14.583333, 21.916667, 25.5625,
				29.208333, 32.854167, 36.5, 40.145833, 43.791667, 47.4375,
				51.083333, 56,
			},
		},
		{
			name:      "no beats at all",
			frameRate: 24, duration: 60, images: 5,
			beats: nil,
			want:  []float64{0, 12, 24, 36, 48},
		},
		{
			name:      "single beat",
			frameRate: 24, duration: 60, images: 6,
			beats: []float64{30},
			want:  []float64{0, 10, 20, 30, 40, 50},
		},
		{
			name:      "25 fps",
			frameRate: 25, duration: 33.3, images: 12,
			beats: grid(0.47, 33.3),
			want: []float64{
				0, 2.84, 5.64, 8.44, 11.28, 13.64, 16.44, 19.28, 22.08, 24.92,
				27.72, 30.56,
			},
		},
	}

	algo, err := texture.Get("legacy")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := algo.Assign(texture.Input{
				Images:    names(tc.images),
				Beats:     tc.beats,
				Duration:  tc.duration,
				FrameRate: tc.frameRate,
			})
			if err != nil {
				t.Fatalf("Assign: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d onsets, want %d", len(got), len(tc.want))
			}
			for i, o := range got {
				if math.Abs(o.Start-tc.want[i]) > 1e-6 {
					t.Errorf("onset %d = %.6f, want %.6f", i, o.Start, tc.want[i])
				}
				if want := fmt.Sprintf("%02d.jpg", i); o.Image != want {
					t.Errorf("onset %d image = %q, want %q", i, o.Image, want)
				}
			}
		})
	}
}

// TestLegacyStaysOrdered is the invariant the original's closing sort exists
// to guarantee: snapping can push an image past its neighbour.
func TestLegacyStaysOrdered(t *testing.T) {
	algo, _ := texture.Get("legacy")
	for _, period := range []float64{0.13, 0.37, 0.5, 1.7, 3.1} {
		got, err := algo.Assign(texture.Input{
			Images:    names(23),
			Beats:     grid(period, 60),
			Duration:  60,
			FrameRate: 24,
		})
		if err != nil {
			t.Fatalf("period %v: Assign: %v", period, err)
		}
		for i := 1; i < len(got); i++ {
			if got[i].Start < got[i-1].Start {
				t.Errorf("period %v: onset %d (%.3f) precedes %d (%.3f)",
					period, i, got[i].Start, i-1, got[i-1].Start)
			}
		}
	}
}

func TestLegacyErrors(t *testing.T) {
	algo, _ := texture.Get("legacy")
	if _, err := algo.Assign(texture.Input{Duration: 10, FrameRate: 24}); err == nil {
		t.Error("expected an error for no images")
	}
	if _, err := algo.Assign(texture.Input{Images: names(3), Duration: 0, FrameRate: 24}); err == nil {
		t.Error("expected an error for zero duration")
	}
}

func TestEven(t *testing.T) {
	algo, err := texture.Get("even")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := algo.Assign(texture.Input{Images: names(4), Duration: 8, FrameRate: 24})
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}
	for i, want := range []float64{0, 2, 4, 6} {
		if math.Abs(got[i].Start-want) > 1e-9 {
			t.Errorf("onset %d = %v, want %v", i, got[i].Start, want)
		}
	}
}

func TestRegistry(t *testing.T) {
	for _, name := range texture.List() {
		if texture.Describe(name) == "" {
			t.Errorf("algorithm %q has no description", name)
		}
		if _, err := texture.Get(name); err != nil {
			t.Errorf("listed algorithm %q is not gettable: %v", name, err)
		}
	}
	if _, err := texture.Get("nope"); err == nil {
		t.Error("expected an error for an unknown algorithm")
	}
}
