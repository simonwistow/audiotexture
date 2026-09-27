package texture

import "math"

// candidate is a time a cut could be made at, and how much that time is worth
// beyond its position. bonus is subtracted from the placement cost, so a
// larger bonus makes a candidate more attractive.
type candidate struct {
	time  float64
	bonus float64
}

// assignByDP places the images at candidate times, minimising the total
// squared deviation from equal spacing, less each chosen candidate's bonus.
//
// This is the same objective the legacy algorithm goes after -- land on beats,
// stay near even spacing -- solved exactly instead of greedily. The legacy
// version walks forward taking the first acceptable beat for each image and
// patches up the images that found nothing afterwards, so an early choice it
// cannot revisit can force a worse run later. A dynamic program considers
// every combination, in O(images x candidates), because the cost of placing
// image i at candidate j depends on the past only through the best way to have
// reached some earlier candidate -- a prefix minimum that carries forward for
// free.
//
// Image 0 is always at time zero, matching the original.
func assignByDP(images []string, cands []candidate, duration float64) []Onset {
	n := len(images)
	if n == 1 {
		return onsetsFrom(images, []float64{0})
	}

	// Cuts strictly after the start, and there must be enough of them.
	usable := make([]candidate, 0, len(cands))
	for _, c := range cands {
		if c.time > 0 && c.time < duration {
			usable = append(usable, c)
		}
	}
	if len(usable) < n-1 {
		return onsetsFrom(images, evenStarts(n, duration))
	}

	shot := duration / float64(n)
	cost := func(i, j int) float64 {
		deviation := (usable[j].time - float64(i)*shot) / shot
		return deviation*deviation - usable[j].bonus
	}

	m := len(usable)
	prev := make([]float64, m)
	cur := make([]float64, m)
	back := make([][]int32, n)

	// Image 1 links back to image 0, which is pinned at time zero.
	for j := range m {
		prev[j] = cost(1, j)
	}

	for i := 2; i < n; i++ {
		back[i] = make([]int32, m)
		bestVal, bestIdx := math.Inf(1), -1
		for j := range m {
			// Running minimum over every candidate strictly before j, which
			// is what keeps the cuts in order.
			if j > 0 && prev[j-1] < bestVal {
				bestVal, bestIdx = prev[j-1], j-1
			}
			if bestIdx < 0 {
				cur[j] = math.Inf(1)
				back[i][j] = -1
				continue
			}
			cur[j] = cost(i, j) + bestVal
			back[i][j] = int32(bestIdx)
		}
		prev, cur = cur, prev
	}

	best, bestIdx := math.Inf(1), -1
	for j := range m {
		if prev[j] < best {
			best, bestIdx = prev[j], j
		}
	}
	if bestIdx < 0 {
		return onsetsFrom(images, evenStarts(n, duration))
	}

	starts := make([]float64, n)
	for i := n - 1; i >= 1; i-- {
		starts[i] = usable[bestIdx].time
		if i >= 2 {
			bestIdx = int(back[i][bestIdx])
			if bestIdx < 0 {
				return onsetsFrom(images, evenStarts(n, duration))
			}
		}
	}
	starts[0] = 0
	return onsetsFrom(images, starts)
}

// beatCandidates builds the candidate set for the DP algorithms: every beat,
// plus the ideal evenly spaced positions as a fallback.
//
// Including the ideal positions is what lets these algorithms cope with music
// that has long unbeaten stretches, or with more images than beats. Without
// them the DP would have to crowd images onto whatever beats exist; with them
// it can simply place a cut where the even layout wanted it and pay the cost
// of not landing on a beat.
func beatCandidates(in Input, bonus func(t float64) float64) []candidate {
	n := len(in.Images)
	cands := make([]candidate, 0, len(in.Beats)+n)
	for _, t := range in.Beats {
		cands = append(cands, candidate{time: t, bonus: bonus(t)})
	}
	for _, t := range evenStarts(n, in.Duration) {
		cands = append(cands, candidate{time: t})
	}
	sortCandidates(cands)
	return cands
}

// sortCandidates orders by time, keeping the higher bonus first when two
// candidates coincide, and drops the duplicates.
func sortCandidates(cands []candidate) {
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0; j-- {
			if cands[j].time < cands[j-1].time ||
				(cands[j].time == cands[j-1].time && cands[j].bonus > cands[j-1].bonus) {
				cands[j], cands[j-1] = cands[j-1], cands[j]
				continue
			}
			break
		}
	}
}
