package texture

import "fmt"

func init() {
	Register("even", "ignore beats; space images equally across the track", evenAlgorithm{})
}

// evenAlgorithm spaces images equally across the duration, ignoring beats.
// It is the baseline the others are measured against, and the sensible
// fallback for material with no discernible pulse.
type evenAlgorithm struct{}

func (evenAlgorithm) Assign(in Input) ([]Onset, error) {
	if len(in.Images) == 0 {
		return nil, fmt.Errorf("no images")
	}
	return onsetsFrom(in.Images, evenStarts(len(in.Images), in.Duration)), nil
}
