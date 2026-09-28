# audiotexture

Generate beat-synced slideshow movies from a directory of images and an audio
track. Beats are detected locally and the movie is encoded in-process — no
external APIs, no `ffmpeg` subprocess, no intermediate frame directory.

This is a Go port of a 2010 hack I did which used the (long-dead) Echo Nest API
for beat detection and emitted a directory of numbered frames for something else
to encode.

`audiotexture` does the analysis and the encoding itself.

## Install

The audio decoding and movie encoding go through
[go-astiav](https://github.com/asticode/go-astiav), cgo bindings to libav\*, so
**FFmpeg 8.x development libraries** are required at build time.

```sh
brew install ffmpeg pkg-config                                  # macOS
apt install libavcodec-dev libavformat-dev libavutil-dev \
            libswscale-dev libswresample-dev pkg-config         # Debian/Ubuntu

go install github.com/simonwistow/audiotexture/cmd/audiotexture@latest
```

If FFmpeg is somewhere `pkg-config` will not find, point cgo at it:

```sh
export PKG_CONFIG_PATH=/path/to/ffmpeg/lib/pkgconfig
export CGO_CFLAGS=-I/path/to/ffmpeg/include
export CGO_LDFLAGS=-L/path/to/ffmpeg/lib
```

## Use

```sh
audiotexture generate --images ./pix --audio track.mp3 --out movie.mp4
```

Images appear in lexical filename order — name them so they sort into the
sequence you want, exactly as the original required.

```
generate flags:
  --images     directory of source images            (required)
  --audio      soundtrack: mp3, m4a, flac, ogg, wav  (required)
  --out        output movie file                     (--out or --frames)
  --frames     also write numbered frames here, as the original Perl did
  --beats      read beat times from a file instead of detecting them
  --algorithm  texture algorithm (default "legacy")
  --framerate  output frame rate (default 24)
  --width      output width (default 1280)
  --height     output height (default 720)
  --crf        x264 quality, lower is better (default 20)
  --preset     x264 preset (default "medium")
  --bpm        override beat detection with a fixed tempo
  --start-bpm  centre of the tempo prior (default 120)
  --tightness  how strictly to hold an even beat grid (default 100)
  --quiet      suppress progress output
  --verbose    show FFmpeg's own logging
```

Inspect the analysis on its own:

```sh
audiotexture analyse --audio track.mp3          # tempo and beat count
audiotexture analyse --audio track.mp3 --times  # every beat time
audiotexture list-algorithms
```

### Reproducing the 2010 renders

Verified against the surviving archive: 680 source images, eight tracks with
their original Echo Nest analyses, and seven rendered videos.

```sh
audiotexture generate --images data/input/images \
    --audio data/input/songs/creep/creep.mp3 \
    --beats data/input/songs/creep/creep.txt \
    --algorithm legacy --reproduce-2010 --framerate 24 \
    --out creep.mp4
```

Three things have to line up, and only the first is obvious.

**The beat times.** The Echo Nest API is gone and no local detector will agree
with it beat for beat, but the original cached every analysis in a `.txt`
sidecar next to the track. `--beats` reads that format directly.

**The duration.** The sidecar's `dur=` line and a local MP3 decode disagree by
around 80 ms. Since the algorithm spreads the images across the duration, using
the wrong one shifts every onset — enough to change the rendered frame count.
`--beats` takes the duration from the file that supplied the beats.

**The frame loop.** `--reproduce-2010` replays the original's frame-emitting
loop, which does not do what its own algorithm computed. Three bugs, all
present in every surviving video: the first image is never shown, the movie
ends at the last onset rather than at the end of the audio, and an image can be
skipped when two onsets land within a frame of each other. Without this the
output is only about 2% frame-identical to the original; with it, exact.

Leave `--reproduce-2010` off for new work — then every image is shown, starting
with the first, and the movie lasts as long as the music.

`--frames` reproduces the original's actual output, a directory of `%06d.ext`
hardlinks, for diffing against archived frames.

#### What was checked

`internal/cmd/verify` decodes each original video and compares it, frame by
frame, against what this implementation predicts.

| Check | Result |
|-------|--------|
| Frame count | exact on all 7 videos |
| Cuts predicted but absent from the original | 0 |
| Cuts in the original not predicted | 0 |
| Frames showing the predicted image | 99.2–99.5% |

The residual is the image matcher, not the algorithm. Many of the 680
photographs are consecutive frames of a stop-motion sequence and are nearly
identical, so a thumbnail comparison cannot always tell which of an adjacent
pair is on screen. Allowing for that, 99.7–99.8% of frames show an image
indistinguishable from the predicted one, and the cut positions — what the
algorithm actually decides — agree exactly.

#### A trap worth knowing about

The source JPEGs carry an EXIF orientation tag of 8, which says to rotate them
90°. That tag is wrong: the stored pixels are already the right way up, and
applying it turns every frame on its side. The 2010 Perl ignored EXIF, and Go's
`image/jpeg` ignores it too, so this implementation matches. Anything that
honours EXIF — `ffmpeg`, most image viewers — will show these images rotated.

## Library

```go
import "github.com/simonwistow/audiotexture"

res, err := audiotexture.Generate("./pix", "track.mp3", "movie.mp4", audiotexture.Options{
    Algorithm: "legacy",
    Video:     video.Options{Width: 1920, Height: 1080},
})
```

The pipeline is four decoupled packages, each usable on its own:

| Package   | Does                                                     |
|-----------|----------------------------------------------------------|
| `audio`   | decode any FFmpeg-supported audio file to mono PCM        |
| `beats`   | onset detection, tempo estimation, beat tracking          |
| `texture` | assign images to onset times (pluggable algorithms)       |
| `video`   | encode the assignment plus the audio into a movie         |

Adding an algorithm is a `texture.Register` call in an `init`:

```go
func init() {
    texture.Register("mine", "one-line description", myAlgorithm{})
}

func (myAlgorithm) Assign(in texture.Input) ([]texture.Onset, error) { ... }
```

## Algorithms

- **`even`** — ignore beats, space images equally. The baseline, and the
  sensible fallback for material with no discernible pulse.
- **`legacy`** — the 2010 algorithm: pre-space onsets evenly, snap each to a
  beat within half a shot-length, stack the ones that find nothing and spread
  them out once a later image does. Greedy and not optimal, but it is what
  produced the original videos.
- **`optimal`** — the same objective solved exactly, by dynamic programming
  over every beat instead of taking the first that fits. Against a steady
  pulse this is barely distinguishable from `legacy`; it earns its keep when
  the beats are sparse or clustered, which is where the greedy version runs
  out of reachable beats and falls back to spreading images evenly.
- **`novelty`** — `optimal`, but beats are weighted by *audio novelty*: how
  much the music changes character there, rather than how loud it is. Cuts
  still land on the pulse, but given a choice of nearby beats they prefer the
  one where the drums enter or the section turns. Falls back to `optimal`
  when no novelty curve is available.
- **`bars`** — hold every image for the same number of *beats* rather than the
  same number of seconds, snapped to a musical length (1, 2, 4, 8, 16 …).
  Every shot is then exactly a bar, or two, or half, which reads as
  deliberate in a way near-even spacing does not.

`even` and `legacy` reproduce old behaviour; `optimal`, `novelty` and `bars`
are new. Adding your own is a `texture.Register` call — see below.

## How the analysis works

### Beat detection

An implementation of Ellis (2007). A mel-scaled spectral flux "onset strength"
envelope says how much new energy appears at each instant. Its
autocorrelation, weighted by a log-Gaussian prior over plausible tempos, gives
one global tempo. A dynamic program then picks the beat sequence maximising
onset strength landed on, minus a penalty for straying from that tempo.

Solving the last step as a DP rather than greedily is the point: it finds the
globally best sequence, so the grid holds its place through a quiet passage
instead of latching onto whatever transient happens to be nearby.

About 170 ms for a three-minute track.

### Audio novelty

Onset strength says how much new energy arrives at an instant. That is not the
same question as whether the music *changed* there: a snare hit is a strong
onset but no kind of boundary, while the bar where the drums first enter may be
no louder than the one before it.

Following Foote, Cooper and Girgensohn (2002), `audiotexture` builds a
self-similarity matrix of the track against itself and slides a checkerboard
kernel down its diagonal. The kernel rewards instants where the recent past is
self-similar, the near future is self-similar, and the two are unlike each
other — which is what a section boundary looks like. The `novelty` algorithm
uses the result to decide which beats are worth cutting on.

## References

- D. Ellis, [Beat Tracking by Dynamic Programming](https://www.ee.columbia.edu/~dpwe/pubs/Ellis07-beattrack.pdf),
  *J. New Music Research* 36(1), 2007.
- J. Foote, M. Cooper, A. Girgensohn, [Creating Music Videos using Automatic
  Media Analysis](https://dl.acm.org/doi/10.1145/641007.641119), ACM MM 2002.

## Licence

MIT. See [LICENSE](LICENSE).
