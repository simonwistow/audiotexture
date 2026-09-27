package texture

func init() {
	RegisterWithDescription("even", "ignore beats; space images equally across the track", evenAlgorithm{})
}

// evenAlgorithm spaces images equally across the duration, ignoring beats.
// It's the walking-skeleton default until beat detection and the "legacy"
// (Perl-derived) onset-snapping algorithm exist.
type evenAlgorithm struct{}

func (evenAlgorithm) Assign(images []string, beats []float64, duration float64) ([]Onset, error) {
	n := len(images)
	onsets := make([]Onset, n)
	for i, img := range images {
		onsets[i] = Onset{
			Image: img,
			Start: float64(i) * duration / float64(n),
		}
	}
	return onsets, nil
}
