// Command verify checks this implementation against the 2010 originals.
//
// It is a development tool, not part of the published surface: it lives under
// internal/ and expects the archived data/ tree to be present.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/simonwistow/audiotexture/beats"
	"github.com/simonwistow/audiotexture/images"
	"github.com/simonwistow/audiotexture/texture"
)

func main() {
	imagesDir := flag.String("images", "data/input/images", "original image directory")
	originalsDir := flag.String("originals", "data/output/original", "original rendered movies")
	compareFrames := flag.Bool("frames", false, "decode every original frame and check which image it shows")
	songsDir := flag.String("songs", "data/input/songs", "original song directories")
	frameRate := flag.Float64("framerate", 24, "frame rate the originals were rendered at")
	flag.Parse()

	src, err := images.FromDir(*imagesDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	// The originals were rendered from the stored pixels; the archive's
	// orientation tags are wrong.
	src.IgnoreOrientation = true
	imgs := src.Names()
	fmt.Printf("%d source images, %s .. %s\n\n",
		len(imgs), filepath.Base(imgs[0]), filepath.Base(imgs[len(imgs)-1]))

	index := make(map[string]int, len(imgs))
	for i, p := range imgs {
		index[p] = i
	}

	entries, err := os.ReadDir(*songsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Printf("%-22s %8s %7s %8s %9s %9s\n", "song", "beats", "dur", "onsets", "frames", "perlend")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		beatFile := filepath.Join(*songsDir, name, name+".txt")
		r, err := beats.LoadFile(beatFile)
		if err != nil {
			fmt.Printf("%-22s ERROR %v\n", name, err)
			continue
		}

		onsets, err := texture.Legacy.Assign(texture.Input{
			Images:    imgs,
			Beats:     r.Times,
			Duration:  r.Duration,
			FrameRate: *frameRate,
		})
		if err != nil {
			fmt.Printf("%-22s ERROR %v\n", name, err)
			continue
		}

		predicted := simulatePerlFrameLoop(onsets, r.Duration, *frameRate, len(imgs))
		mine := simulateRenderLoop(onsets, r.Duration, *frameRate, index)
		shiftedOnsets, shiftedDuration := texture.LegacyFrameSequence(onsets, r.Duration, *frameRate)
		shifted := simulateRenderLoop(shiftedOnsets, shiftedDuration, *frameRate, index)
		fmt.Printf("  render loop vs perl loop: default %s, with LegacyFrameSequence %s\n",
			sequenceVerdict(mine, predicted), sequenceVerdict(shifted, predicted))
		fmt.Printf("%-22s %8d %7.1f %8d %9d %9d\n",
			name, len(r.Times), r.Duration, len(onsets), len(predicted), predicted[len(predicted)-1])
		if *compareFrames {
			compare(name, filepath.Join(*originalsDir, name+".avi"), shifted, src)
		}
	}
}

// compare decodes the original movie and checks it against this
// implementation's prediction, two independent ways.
//
// Identity: which source image each frame shows. This is limited by the
// source material -- many of the 680 photographs are consecutive frames of a
// stop-motion sequence and are near-identical -- so as well as an exact
// nearest-neighbour match it reports whether the predicted image is
// indistinguishable from the nearest, which is the fair question to ask of a
// near-duplicate.
//
// Cuts: where the image changes. This compares each frame to its predecessor
// rather than to a library, so it is immune to near-duplicate confusion, and
// it is the thing the algorithm actually decides.
func compare(name, aviPath string, predicted []int, src images.Images) {
	if _, err := os.Stat(aviPath); err != nil {
		fmt.Printf("  (no original movie to compare against)\n")
		return
	}
	if sourceHashes == nil {
		var err error
		if sourceHashes, err = hashSources(src); err != nil {
			fmt.Printf("  ERROR hashing sources: %v\n", err)
			return
		}
	}

	frames, err := frameHashes(aviPath)
	if err != nil {
		fmt.Printf("  ERROR decoding %s: %v\n", aviPath, err)
		return
	}

	status := "match"
	if len(frames) != len(predicted) {
		status = fmt.Sprintf("MISMATCH (original %d)", len(frames))
	}
	fmt.Printf("    frames   %d predicted, %s\n", len(predicted), status)

	n := min(len(frames), len(predicted))
	var exact, indistinguishable int
	for i := range n {
		best, bestIdx := math.Inf(1), -1
		for j, s := range sourceHashes {
			if d := distance(frames[i], s); d < best {
				best, bestIdx = d, j
			}
		}
		switch {
		case bestIdx == predicted[i]:
			exact++
			indistinguishable++
		case distance(frames[i], sourceHashes[predicted[i]]) <= 1.02*best:
			// The predicted image is as close a match as the winner; the
			// thumbnail simply cannot tell the two photographs apart.
			indistinguishable++
		}
	}
	fmt.Printf("    identity %.3f%% exact, %.3f%% indistinguishable-or-better\n",
		100*float64(exact)/float64(n), 100*float64(indistinguishable)/float64(n))

	compareCuts(frames[:n], predicted[:n])
}

// compareCuts checks that the image changes on exactly the frames this
// implementation says it should.
func compareCuts(frames []imageHash, predicted []int) {
	// Distance between consecutive frames. Within a shot this is compression
	// noise; across a cut it is the difference between two photographs.
	deltas := make([]float64, len(frames))
	for i := 1; i < len(frames); i++ {
		deltas[i] = distance(frames[i], frames[i-1])
	}

	var wantCuts []int
	for i := 1; i < len(predicted); i++ {
		if predicted[i] != predicted[i-1] {
			wantCuts = append(wantCuts, i)
		}
	}

	// Separate the two populations rather than guessing a threshold: compare
	// the deltas at predicted cuts against the deltas everywhere else.
	isCut := make([]bool, len(frames))
	for _, c := range wantCuts {
		isCut[c] = true
	}
	var cutDeltas, holdDeltas []float64
	for i := 1; i < len(frames); i++ {
		if isCut[i] {
			cutDeltas = append(cutDeltas, deltas[i])
		} else {
			holdDeltas = append(holdDeltas, deltas[i])
		}
	}
	if len(cutDeltas) == 0 || len(holdDeltas) == 0 {
		return
	}

	// Within a shot every frame is byte-identical to its predecessor, so the
	// hold median is 0 and a geometric mean between the populations collapses
	// to 0 too. The two populations are separated by three orders of
	// magnitude, so any fraction of the cut median works; a quarter leaves
	// plenty of room for compression noise.
	threshold := 0.25 * median(cutDeltas)

	var gotCuts []int
	for i := 1; i < len(frames); i++ {
		if deltas[i] > threshold {
			gotCuts = append(gotCuts, i)
		}
	}

	// How many detected cuts sit exactly where predicted.
	aligned := 0
	for _, g := range gotCuts {
		if isCut[g] {
			aligned++
		}
	}
	// And how many predicted cuts are visible at all. A predicted cut between
	// two near-identical photographs produces no visible change, so a miss
	// here is expected and not a disagreement.
	visible := 0
	for _, c := range wantCuts {
		if deltas[c] > threshold {
			visible++
		}
	}

	fmt.Printf("    cuts     %d predicted, %d/%d visible in the original; %d unpredicted cuts in the original\n",
		len(wantCuts), visible, len(wantCuts), len(gotCuts)-aligned)

}

// simulateRenderLoop is what render.FrameDirectory and video.Encode do: show
// the image belonging to the latest onset at or before each frame's timestamp.
// It returns image indices, not positions in the onset list, so the result is
// comparable with the Perl simulation after a shift has renumbered things.
func simulateRenderLoop(onsets []texture.Onset, duration, frameRate float64, index map[string]int) []int {
	total := int(duration*frameRate) + 1
	out := make([]int, total)
	j := 0
	for n := range total {
		t := float64(n) / frameRate
		for j+1 < len(onsets) && onsets[j+1].Start <= t {
			j++
		}
		out[n] = index[onsets[j].Image]
	}
	return out
}

// sequenceVerdict compares two frame-to-image sequences exactly.
func sequenceVerdict(got, want []int) string {
	if len(got) != len(want) {
		return fmt.Sprintf("DIFFERS (%d frames vs %d)", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			return fmt.Sprintf("DIFFERS (first at frame %d: %d vs %d)", i, got[i], want[i])
		}
	}
	return "identical"
}

func median(xs []float64) float64 {
	c := append([]float64(nil), xs...)
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && c[j] < c[j-1]; j-- {
			c[j], c[j-1] = c[j-1], c[j]
		}
	}
	return c[len(c)/2]
}

var sourceHashes []imageHash

// simulatePerlFrameLoop reproduces the frame-emitting loop at the end of
// slideshow.pl exactly, including its quirks, and reports how many frames it
// would write and which image index it stopped on.
//
//	@onset=(@onset,$DUR);
//	my ($i1,$n,$j) = (int($DUR*$FR),0,0);
//	for my $i (0..$i1) {
//	  ($n>=$FR*$onset[$j]) and ++$j;
//	  if ($j<@pix and $j<@onset) { link ...; ++$n; }
//	}
func simulatePerlFrameLoop(onsets []texture.Onset, duration, frameRate float64, npix int) []int {
	starts := make([]float64, 0, len(onsets)+1)
	for _, o := range onsets {
		starts = append(starts, o.Start)
	}
	starts = append(starts, duration)

	i1 := int(duration * frameRate)
	n, j := 0, 0
	var out []int
	for i := 0; i <= i1; i++ {
		if j < len(starts) && float64(n) >= frameRate*starts[j] {
			j++
		}
		if j < npix && j < len(starts) {
			out = append(out, j)
			n++
		}
	}
	return out
}
