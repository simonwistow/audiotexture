package texture

import "fmt"

func init() {
	Register("novelty", "cut on structural boundaries: beats weighted by audio novelty", Novelty)
}

// noveltyWeight scales the novelty curve's pull relative to an ordinary beat.
// At 3, a beat at a maximal structural boundary is worth four times as much as
// a plain one, which is enough to drag a cut a whole shot-length onto a
// section change but not enough to abandon even spacing altogether.
const noveltyWeight = 3.0

// noveltyAlgorithm is the optimal algorithm with the beats weighted by how
// much the music changes there.
//
// The other algorithms treat every beat as equally good to cut on, so a
// slideshow lands on a regular pulse and stays there. But not every beat is
// equally meaningful: the downbeat where the drums enter, the bar where the
// chorus arrives and the moment a texture drops away are the places a human
// editor would cut, and they are not louder than their neighbours -- they are
// where the music stops resembling what came before.
//
// Audio novelty, from Foote, Cooper and Girgensohn, "Creating Music Videos
// using Automatic Media Analysis" (ACM MM 2002), measures exactly that by
// sliding a checkerboard kernel down the audio's self-similarity matrix.
// Weighting the beats by it means the cuts still fall on the pulse, but given
// a choice of nearby beats the DP prefers the one where the music turns.
//
// With no novelty curve available -- beats loaded from a file, say -- this
// degrades to the optimal algorithm rather than failing.
type noveltyAlgorithm struct{}

func (noveltyAlgorithm) String() string { return "novelty" }

func (noveltyAlgorithm) Assign(in Input) ([]Onset, error) {
	if len(in.Images) == 0 {
		return nil, fmt.Errorf("no images")
	}
	if in.Duration <= 0 {
		return nil, fmt.Errorf("duration must be positive, got %v", in.Duration)
	}
	cands := beatCandidates(in, func(t float64) float64 {
		return beatBonus * (1 + noveltyWeight*in.noveltyAt(t))
	})
	return assignByDP(in.Images, cands, in.Duration), nil
}
