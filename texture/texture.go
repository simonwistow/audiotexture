// Package texture implements the pluggable "texture" algorithms that decide
// which image is shown at which moment in the output, given the source images,
// the detected beat times, and the audio duration.
package texture

import (
	"fmt"
	"sort"
)

// Onset is a point in time at which the given image becomes the current one.
// An image is shown from its Start until the next Onset's Start.
type Onset struct {
	Image string
	Start float64 // seconds
}

// Input is everything an Algorithm has to work with.
type Input struct {
	// Images are the source image paths, in the order they should appear.
	Images []string
	// Beats are the detected beat times in seconds, ascending. May be empty,
	// in which case beat-driven algorithms should fall back to even spacing.
	Beats []float64
	// Duration is the length of the soundtrack in seconds.
	Duration float64
	// FrameRate is the output frame rate. The legacy algorithm quantises to
	// this grid, because the original Perl worked in frame numbers.
	FrameRate float64
	// Strength, if set, reports the onset strength at a given time: how much
	// new energy arrives there.
	Strength func(t float64) float64
	// Novelty, if set, reports the audio novelty at a given time in [0, 1]:
	// how much the music changes character there, as opposed to merely how
	// loud it is. Used to prefer structural boundaries over ordinary beats.
	Novelty func(t float64) float64
}

// noveltyAt returns the audio novelty at t, or 0 when none is available, so
// that callers can weight without a nil check.
func (in Input) noveltyAt(t float64) float64 {
	if in.Novelty == nil {
		return 0
	}
	return in.Novelty(t)
}

// Algorithm assigns images to onsets in time across the audio duration.
type Algorithm interface {
	Assign(Input) ([]Onset, error)
}

type registration struct {
	algorithm   Algorithm
	description string
}

var registry = map[string]registration{}

// Register makes a under name, with a one-line description for the CLI.
func Register(name, description string, a Algorithm) {
	registry[name] = registration{algorithm: a, description: description}
}

// Get returns the algorithm registered under name.
func Get(name string) (Algorithm, error) {
	r, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown algorithm %q (available: %v)", name, List())
	}
	return r.algorithm, nil
}

// Describe returns the one-line description registered for name.
func Describe(name string) string {
	return registry[name].description
}

// List returns the registered algorithm names, sorted.
func List() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// evenStarts returns n start times spread equally across duration. Several
// algorithms use this as their starting point.
func evenStarts(n int, duration float64) []float64 {
	starts := make([]float64, n)
	for i := range starts {
		starts[i] = float64(i) * duration / float64(n)
	}
	return starts
}

// onsetsFrom pairs images with start times, which must already be sorted
// ascending and the same length as images.
func onsetsFrom(images []string, starts []float64) []Onset {
	onsets := make([]Onset, len(images))
	for i, img := range images {
		onsets[i] = Onset{Image: img, Start: starts[i]}
	}
	return onsets
}
