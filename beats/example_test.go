package beats_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/simonwistow/audiotexture/audio"
	"github.com/simonwistow/audiotexture/beats"
	"github.com/simonwistow/audiotexture/internal/testaudio"
)

// Detect takes decoded audio and returns the beat times, the tempo it settled
// on, and the onset strength envelope behind both.
func ExampleDetect() {
	// Stand in for a real track: thirty seconds of clicks at 120 BPM.
	pcm := &audio.PCM{Samples: testaudio.ClickTrack(22050, 30, 120), SampleRate: 22050}

	r, err := beats.Detect(pcm, nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%.0f BPM\n", r.BPM)
	fmt.Printf("%d beats, first at %.2fs\n", len(r.Times), r.Times[0])
	// Output:
	// 120 BPM
	// 60 beats, first at 0.00s
}

// The tempo is tracked over time rather than assumed constant, so a take that
// speeds up is followed rather than averaged into a grid that is wrong at both
// ends. Result.Tempo holds one BPM value every Result.TempoSeconds. Here the
// clicks accelerate from 100 to 140 BPM and the tracked tempo climbs with
// them; each value is read off an eight-second window, so it lags the
// instantaneous rate slightly.
func ExampleResult_tempoCurve() {
	samples, _ := testaudio.RampedClickTrack(22050, 60, 100, 140)
	r, err := beats.Detect(&audio.PCM{Samples: samples, SampleRate: 22050}, nil)
	if err != nil {
		log.Fatal(err)
	}

	for _, at := range []float64{15, 30, 45} {
		fmt.Printf("%.0fs: %.0f BPM\n", at, r.Tempo[int(at/r.TempoSeconds)])
	}
	// Output:
	// 15s: 107 BPM
	// 30s: 117 BPM
	// 45s: 129 BPM
}

// Options tunes detection. The zero value is usable; the field worth reaching
// for first is StartBPM, which centres the prior over plausible tempos and is
// how you break a track that has locked onto half or double time.
func ExampleOptions() {
	pcm := &audio.PCM{Samples: testaudio.ClickTrack(22050, 30, 90), SampleRate: 22050}

	r, err := beats.Detect(pcm, &beats.Options{StartBPM: 90, Tightness: 100})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%.0f BPM\n", r.BPM)
	// Output:
	// 90 BPM
}

// LoadFile reads beat times analysed elsewhere, so a track can be rendered
// with timings captured years ago rather than whatever Detect makes of it now.
// It reads the sidecar format the original Perl wrote, and plainer forms too.
func ExampleLoadFile() {
	dir, err := os.MkdirTemp("", "beats")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "track.txt")
	sidecar := "beats(track.mp3) provided by echonest.com\ndur=12.5\n0.25,0.75,1.25,1.75\n"
	if err := os.WriteFile(path, []byte(sidecar), 0o600); err != nil {
		log.Fatal(err)
	}

	r, err := beats.LoadFile(path)
	if err != nil {
		log.Fatal(err)
	}

	// The duration comes from the file too: prefer it over a local decode when
	// reproducing an old render, since the spacing is computed from it.
	fmt.Printf("%d beats over %.1fs at %.0f BPM\n", len(r.Times), r.Duration, r.BPM)
	// Output:
	// 4 beats over 12.5s at 120 BPM
}
