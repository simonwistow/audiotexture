package beats

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// LoadFile reads beat times from a text file.
//
// It accepts the sidecar format the original Perl wrote next to each track --
// a comment line, a "dur=" line, then one comma-separated line of beat times:
//
//	beats(track.mp3) provided by echonest.com
//	dur=213.44
//	0.24812,0.72331,1.19424,...
//
// and also the obvious plainer forms: one time per line, or several
// comma-separated or whitespace-separated times per line, with blank lines and
// lines beginning with '#' ignored.
//
// This exists so that a track analysed years ago against an API that no longer
// exists can still be rendered with exactly its original timings, rather than
// with whatever Detect makes of it today.
func LoadFile(path string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening beat file: %w", err)
	}
	defer f.Close()

	var times []float64
	var duration float64

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24) // one very long line is normal here
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(text, "dur="); ok {
			duration, _ = strconv.ParseFloat(strings.TrimSpace(rest), 64)
			continue
		}

		fields := strings.FieldsFunc(text, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		})
		// A header line like "beats(x.mp3) provided by echonest.com" parses as
		// no numbers at all; skip it rather than failing.
		parsed := make([]float64, 0, len(fields))
		for _, field := range fields {
			v, err := strconv.ParseFloat(field, 64)
			if err != nil {
				parsed = parsed[:0]
				break
			}
			parsed = append(parsed, v)
		}
		times = append(times, parsed...)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading beat file: %w", err)
	}
	if len(times) == 0 {
		return nil, fmt.Errorf("no beat times found in %s", path)
	}

	sort.Float64s(times)

	return &Result{
		BPM:   bpmFromTimes(times),
		Times: times,
		// No envelope: algorithms that weight beats by onset strength fall
		// back to weighting them all equally.
		HopSeconds: 0,
		Duration:   duration,
	}, nil
}

// bpmFromTimes estimates tempo as the median inter-beat interval, which
// shrugs off the occasional doubled or dropped beat that a mean would not.
func bpmFromTimes(times []float64) float64 {
	if len(times) < 2 {
		return 0
	}
	gaps := make([]float64, 0, len(times)-1)
	for i := 1; i < len(times); i++ {
		if g := times[i] - times[i-1]; g > 0 {
			gaps = append(gaps, g)
		}
	}
	if len(gaps) == 0 {
		return 0
	}
	sort.Float64s(gaps)
	median := gaps[len(gaps)/2]
	if median <= 0 {
		return 0
	}
	return 60 / median
}
