package audio_test

import (
	"fmt"
	"log"
	"os"

	"github.com/simonwistow/audiotexture/audio"
	"github.com/simonwistow/audiotexture/internal/testaudio"
)

// Decode hands back one channel of samples in [-1, 1], resampled to whatever
// rate the analysis wants. Any format the local FFmpeg can read works the same
// way: mp3, m4a, flac, ogg, wav.
func ExampleDecode() {
	dir, err := os.MkdirTemp("", "audio")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Stand in for a real soundtrack: five seconds of clicks at 120 BPM.
	path, err := testaudio.WriteWAV(dir, "track.wav", testaudio.ClickTrack(44100, 5, 120), 44100)
	if err != nil {
		log.Fatal(err)
	}

	// 0 means the default analysis rate. Pass an explicit rate to override it.
	pcm, err := audio.Decode(path, 22050)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%d Hz, %d samples, %.1fs\n", pcm.SampleRate, len(pcm.Samples), pcm.Duration())
	// Output:
	// 22050 Hz, 110250 samples, 5.0s
}
