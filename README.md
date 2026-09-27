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
  --algorithm  texture algorithm (default "even")
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

### Reproducing a render from 2010

Two things have to match to get the old timings back: the algorithm and the
beat times. `--algorithm legacy` covers the first — it is a verified port of
the original, checked against the Perl across 27 scenarios.

The beats are harder, because the Echo Nest API is gone and no local detector
will agree with it beat for beat. But the original *cached* its results in a
`.txt` file next to each track:

```
beats(track.mp3) provided by echonest.com
dur=213.44
0.24812,0.72331,1.19424,...
```

If any of those sidecars survived, feed one straight back in and the timings
are exact:

```sh
audiotexture generate --images ./pix --audio track.mp3 \
    --beats track.txt --algorithm legacy --framerate 24 --out movie.mp4
```

`--frames` reproduces the original's actual output — a directory of
`%06d.ext` hardlinks — if you want to diff against archived frames.

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

## How the beat detection works

An implementation of Ellis (2007). A mel-scaled spectral flux "onset strength"
envelope says how much new energy appears at each instant. Its
autocorrelation, weighted by a log-Gaussian prior over plausible tempos, gives
one global tempo. A dynamic program then picks the beat sequence maximising
onset strength landed on, minus a penalty for straying from that tempo.

Solving the last step as a DP rather than greedily is the point: it finds the
globally best sequence, so the grid holds its place through a quiet passage
instead of latching onto whatever transient happens to be nearby.

About 170 ms for a three-minute track.

## References

- D. Ellis, [Beat Tracking by Dynamic Programming](https://www.ee.columbia.edu/~dpwe/pubs/Ellis07-beattrack.pdf),
  *J. New Music Research* 36(1), 2007.
- J. Foote, M. Cooper, A. Girgensohn, [Creating Music Videos using Automatic
  Media Analysis](https://dl.acm.org/doi/10.1145/641007.641119), ACM MM 2002.

## Licence

MIT. See [LICENSE](LICENSE).
