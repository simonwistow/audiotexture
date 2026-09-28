package texture_test

import (
	"fmt"
	"log"

	"github.com/simonwistow/audiotexture/texture"
)

// everyOtherBeat cuts on alternate beats and no more often.
type everyOtherBeat struct{}

func (everyOtherBeat) Assign(in texture.Input) ([]texture.Onset, error) {
	var out []texture.Onset
	for i, img := range in.Images {
		beat := 2 * i
		if beat >= len(in.Beats) {
			break // ran out of music
		}
		out = append(out, texture.Onset{Image: img, Start: in.Beats[beat]})
	}
	return out, nil
}

// An algorithm decides which image is on screen when. Register one from an
// init function and it becomes available everywhere the built-in names are,
// including the command line.
func ExampleRegister() {
	texture.Register("every-other", "cut on alternate beats", everyOtherBeat{})

	algo, err := texture.Get("every-other")
	if err != nil {
		log.Fatal(err)
	}

	onsets, err := algo.Assign(texture.Input{
		Images:    []string{"a.jpg", "b.jpg", "c.jpg"},
		Beats:     []float64{0, 0.5, 1.0, 1.5, 2.0, 2.5},
		Duration:  3.0,
		FrameRate: 24,
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, o := range onsets {
		fmt.Printf("%.1fs %s\n", o.Start, o.Image)
	}
	// Output:
	// 0.0s a.jpg
	// 1.0s b.jpg
	// 2.0s c.jpg
}

// Describe returns the one-line summary given at registration, which is what
// the command line's list-algorithms prints.
func ExampleDescribe() {
	fmt.Println(texture.Describe("even"))
	fmt.Println(texture.Describe("bars"))
	// Output:
	// ignore beats; space images equally across the track
	// hold each image a whole number of beats, quantised to bar lengths
}
