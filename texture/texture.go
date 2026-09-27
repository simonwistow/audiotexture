// Package texture implements the pluggable "texture" algorithms that decide
// which image is shown at which moment in the output, given the source
// images, detected beat/onset times, and the audio duration.
package texture

import (
	"fmt"
	"sort"
)

// Onset is a point in time at which the given image becomes the current one.
type Onset struct {
	Image string
	Start float64 // seconds
}

// Algorithm assigns images to onsets in time across the audio duration.
// beats is the set of detected beat/onset times in seconds (empty until the
// beat-detection stage exists); it may be ignored by algorithms that don't
// use it.
type Algorithm interface {
	Assign(images []string, beats []float64, duration float64) ([]Onset, error)
}

var registry = map[string]Algorithm{}

func Register(name string, a Algorithm) {
	registry[name] = a
}

func Get(name string) (Algorithm, error) {
	a, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown algorithm %q (available: %v)", name, List())
	}
	return a, nil
}

func List() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
