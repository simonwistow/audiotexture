package texture

// LegacyFrameSequence rewrites an onset list to match how the original Perl
// actually put frames on screen, as opposed to what its algorithm computed.
// It returns the rewritten onsets and the duration to render them over.
//
// The algorithm in slideshow.pl produces one onset per image, but the loop
// that wrote the frames does not use them that way:
//
//	@onset=(@onset,$DUR);
//	my ($i1,$n,$j) = (int($DUR*$FR),0,0);
//	for my $i (0..$i1) {
//	  ($n>=$FR*$onset[$j]) and ++$j;
//	  if ($j<@pix and $j<@onset) { link "$pixdir/$pix[$j]", ...; ++$n; }
//	}
//
// Three things fall out of it, and the surviving 2010 videos exhibit all three.
//
// The first image is never shown. onset[0] is always 0, so the test on the
// first iteration is 0 >= 0 and j becomes 1 before anything is written. Every
// image then appears at the time computed for the image before it.
//
// The movie ends at the last computed onset rather than at the end of the
// audio, because the loop stops writing once j reaches the image count. That
// makes it a few frames shorter than the track.
//
// And an image can be skipped over. j advances at most once per iteration, so
// when two onsets land within a frame of each other the loop cannot catch up,
// and it runs a frame or more behind until the spacing opens out again.
//
// All three are bugs and none is what the author intended. They are also what
// every one of the 2010 videos was rendered with, so reproducing one exactly
// means reproducing them. Rendering this function's output over the duration
// it returns gives a frame-for-frame identical result.
//
// Leave it alone for new work: without it every image is shown, starting with
// the first, and the movie lasts as long as the music.
func LegacyFrameSequence(onsets []Onset, duration, frameRate float64) ([]Onset, float64) {
	if len(onsets) == 0 || frameRate <= 0 || duration <= 0 {
		return onsets, duration
	}

	// starts is the Perl's @onset after `@onset=(@onset,$DUR)`.
	starts := make([]float64, 0, len(onsets)+1)
	for _, o := range onsets {
		starts = append(starts, o.Start)
	}
	starts = append(starts, duration)

	var out []Onset
	last := -1
	frames := 0
	iterations := int(duration * frameRate)
	j := 0
	for range iterations + 1 {
		if j < len(starts) && float64(frames) >= frameRate*starts[j] {
			j++
		}
		if j >= len(onsets) || j >= len(starts) {
			continue
		}
		if j != last {
			// Anchor each change on the exact frame it happened, so an
			// ordinary renderer reproduces the sequence.
			out = append(out, Onset{Image: onsets[j].Image, Start: float64(frames) / frameRate})
			last = j
		}
		frames++
	}

	if frames == 0 {
		return onsets, duration
	}
	// Pick the duration that makes a renderer emitting int(duration*rate)+1
	// frames emit exactly the number the Perl loop wrote.
	return out, (float64(frames) - 0.5) / frameRate
}
