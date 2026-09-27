package beats_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonwistow/audiotexture/beats"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "beats.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return p
}

func TestLoadFileEchoNestFormat(t *testing.T) {
	// Exactly what the original Perl wrote next to each track.
	r, err := beats.LoadFile(writeFile(t, `beats(track.mp3) provided by echonest.com
dur=4.5
0.5,1.0,1.5,2.0,2.5,3.0,3.5,4.0
`))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	want := []float64{0.5, 1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0}
	if len(r.Times) != len(want) {
		t.Fatalf("got %d beats, want %d", len(r.Times), len(want))
	}
	for i := range want {
		if math.Abs(r.Times[i]-want[i]) > 1e-9 {
			t.Errorf("beat %d = %v, want %v", i, r.Times[i], want[i])
		}
	}
	if math.Abs(r.Duration-4.5) > 1e-9 {
		t.Errorf("Duration = %v, want 4.5", r.Duration)
	}
	if math.Abs(r.BPM-120) > 1e-6 {
		t.Errorf("BPM = %v, want 120", r.BPM)
	}
}

func TestLoadFilePlainFormats(t *testing.T) {
	for name, content := range map[string]string{
		"one per line":      "0.5\n1.0\n1.5\n2.0\n",
		"whitespace":        "0.5 1.0 1.5 2.0\n",
		"comma no header":   "0.5,1.0,1.5,2.0\n",
		"comments and gaps": "# my beats\n\n0.5,1.0\n\n1.5,2.0\n",
		"unsorted":          "1.5,0.5,2.0,1.0\n",
	} {
		t.Run(name, func(t *testing.T) {
			r, err := beats.LoadFile(writeFile(t, content))
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			want := []float64{0.5, 1.0, 1.5, 2.0}
			if len(r.Times) != len(want) {
				t.Fatalf("got %v, want %v", r.Times, want)
			}
			for i := range want {
				if math.Abs(r.Times[i]-want[i]) > 1e-9 {
					t.Errorf("beat %d = %v, want %v", i, r.Times[i], want[i])
				}
			}
		})
	}
}

func TestLoadFileErrors(t *testing.T) {
	if _, err := beats.LoadFile("/nonexistent/beats.txt"); err == nil {
		t.Error("expected an error for a missing file")
	}
	if _, err := beats.LoadFile(writeFile(t, "no numbers here at all\n")); err == nil {
		t.Error("expected an error for a file with no beat times")
	}
}

// TestLoadFileMedianBPM checks the tempo estimate ignores an outlier gap
// rather than being dragged by it.
func TestLoadFileMedianBPM(t *testing.T) {
	r, err := beats.LoadFile(writeFile(t, "0,0.5,1.0,1.5,2.0,2.5,3.0,9.0,9.5,10.0,10.5\n"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if math.Abs(r.BPM-120) > 1e-6 {
		t.Errorf("BPM = %v, want 120 despite the 6.5s gap", r.BPM)
	}
}
