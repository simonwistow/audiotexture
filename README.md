# audiotexture

[![test](https://github.com/simonwistow/audiotexture/actions/workflows/test.yml/badge.svg)](https://github.com/simonwistow/audiotexture/actions/workflows/test.yml)

Generate beat-synced slideshow movies from a directory of images and an audio
track. Beats are detected locally and the movie is encoded in-process — no
external APIs, no `ffmpeg` subprocess, no intermediate frame directory.

This is a Go port of a 2010 hack I did which used the (long-dead) Echo Nest API
for beat detection and emitted a directory of numbered frames for something else
to encode.

`audiotexture` does the analysis and the encoding itself.

## Install

### Prebuilt

Each [release](https://github.com/simonwistow/audiotexture/releases) has a
download for Linux x86-64 (glibc 2.35 or newer, so Ubuntu 22.04 onwards) and
for macOS on Apple silicon (13 or newer). Each bundles the FFmpeg it needs, so
nothing else has to be installed:

```sh
tar -xzf audiotexture-v0.1.0-linux-amd64.tar.gz
audiotexture-v0.1.0-linux-amd64/bin/audiotexture version
```

Keep `bin` and `lib` together: the binary finds its libraries relative to
itself. On macOS, a download from a browser is quarantined, and the build is
not notarised, so clear the flag first with
`xattr -dr com.apple.quarantine audiotexture-v0.1.0-darwin-arm64`.

### From source

The audio decoding and movie encoding go through
[go-astiav](https://github.com/asticode/go-astiav), cgo bindings to libav\*, so
**FFmpeg 8.x development libraries** are required at build time — `libavcodec`,
`libavdevice`, `libavfilter`, `libavformat`, `libavutil`, `libswresample` and
`libswscale` — built with an H.264 encoder (libx264) if you want to write MP4.
The bindings are pinned to that major version of the ABI and will not build
against 7 or 9.

Check what you have with `pkg-config --modversion libavcodec`: FFmpeg 8.x
reports `62.x`, and 7.x reports `61.x`.

#### macOS

```sh
brew install ffmpeg pkg-config      # if Homebrew is currently on 8.x
go install github.com/simonwistow/audiotexture/cmd/audiotexture@latest
```

#### Fedora

RPM Fusion's `ffmpeg-devel` has the headers and libraries, but check its
version first; if it is not 8.x, build FFmpeg as below.

```sh
sudo dnf install https://mirrors.rpmfusion.org/free/fedora/rpmfusion-free-release-$(rpm -E %fedora).noarch.rpm \
                 https://mirrors.rpmfusion.org/nonfree/fedora/rpmfusion-nonfree-release-$(rpm -E %fedora).noarch.rpm
sudo dnf install ffmpeg-devel pkgconfig
go install github.com/simonwistow/audiotexture/cmd/audiotexture@latest
```

#### Ubuntu, Debian and anything else without FFmpeg 8

Distribution packages are still on 6.x or 7.x, so build FFmpeg yourself.
`.github/scripts/build-ffmpeg.sh PREFIX` builds exactly what CI builds: FFmpeg
and x264, shared, into a prefix of your choosing, with nothing installed
system-wide. Shared rather than static because cgo asks `pkg-config` for
`--libs` and not `--libs --static`, which would leave `-lx264` in
`Libs.private` where the linker never looks.

```sh
sudo apt update
sudo apt install build-essential git nasm pkg-config    # dnf: gcc make git nasm pkgconfig

git clone https://github.com/simonwistow/audiotexture.git
cd audiotexture
PREFIX=$HOME/ffmpeg
.github/scripts/build-ffmpeg.sh $PREFIX
```

Then point cgo at that FFmpeg, since it is not where `pkg-config` looks by
default, and install:

```sh
export PKG_CONFIG_PATH=$PREFIX/lib/pkgconfig
export CGO_CFLAGS=-I$PREFIX/include
export CGO_LDFLAGS="-L$PREFIX/lib -Wl,-rpath,$PREFIX/lib"   # rpath: Linux only
go install ./cmd/audiotexture
```

The rpath is how an ELF executable finds shared libraries outside the default
search path; macOS dylibs carry their own absolute install name and do not
need it.

## Use

```sh
audiotexture generate --images ./pix --audio track.mp3 --out movie.mp4
```

Images appear in lexical filename order — name them so they sort into the
sequence you want, exactly as the original required.

```
generate flags:
  --images       directory of source images            (required)
  --audio        soundtrack: mp3, m4a, flac, ogg, wav  (required)
  --out          output movie file                     (--out or --frames)
  --frames       also write numbered frames here, as the original Perl did
  --beats        read beat times from a file instead of detecting them
  --algorithm    texture algorithm (default "optimal"; "legacy" with --reproduce-2010)
  --ignore-exif  use images as stored, ignoring the EXIF orientation tag
  --framerate    output frame rate (default 24)
  --width        output width (default 1280)
  --height       output height (default 720)
  --crf          x264 quality, lower is better (default 20)
  --preset       x264 preset (default "medium")
  --quiet        suppress progress output
  --verbose      show FFmpeg's own logging

tempo flags (generate and analyse both take these):
  --bpm            skip detection and use a fixed tempo
  --start-bpm      centre of the tempo prior (default 120)
  --tempo-spread   width of that prior, in octaves (default 1)
  --tempo-drift    how far the tempo may stray from the track's own (default 0.25)
  --tempo-inertia  cost of changing tempo (default 400)
  --tightness      how strictly to hold an even beat grid (default 100)
```

Inspect the analysis on its own:

```sh
audiotexture analyse --audio track.mp3          # tempo, range and beat count
audiotexture analyse --audio track.mp3 --times  # every beat time
audiotexture analyse --audio track.mp3 --tempo  # the tracked tempo over time
audiotexture list-algorithms
```

### When the tempo comes out wrong

The BPM detection has two failure modes, both with different fixes.

**It picks the wrong metrical level** — 160 BPM for an 80 BPM track, because
the eighth notes are as strong in the onset envelope as the beats, and nothing
in the audio says which level is the *beat*. Move the prior: `--start-bpm 60`
for a track you expect to be slow, `--start-bpm 160` for one you expect to be
fast. The prior says "people tap at around here", not "the tempo is this", so
it only has to be on the right side. `--bpm` settles it outright.

**It does not follow a tempo that moves.** `--tempo-drift` bounds how far the
tracker may stray from the track's own tempo; raise it for a take that really
does pull about, lower it towards 0 to insist on a metronomic grid.
`--tempo-inertia` is the same knob from the other end: how much a *change*
costs rather than how large a change is allowed.

`--tempo` prints the curve, which is usually enough to see which of the two is
happening.

### Reproducing the 2010 renders

The 2010 Perl script had a certain rugged charm but was also written in
during a bout of insomnia and had several bugs and very few comments.

That said I did find the original code in an old backup: 680 source images,
eight tracks with their original Echo Nest analyses, and seven rendered videos.

That made things easier to test against by making this version bug compatible. I
then fixed the bugs but you can, if you'd like, reproduce the original.

The archive is not distributed with the repository, so the `data/` paths below
are for example purposes only - it's really about the flags.

```sh
audiotexture generate --images data/input/images \
    --audio data/input/songs/creep/creep.mp3 \
    --beats data/input/songs/creep/creep.txt \
    --algorithm legacy --reproduce-2010 --ignore-exif --framerate 24 \
    --out creep.mp4
```

Four things have to line up, and only the first is obvious.

**The beat times.** The Echo Nest API is gone and no local detector will agree
with it beat for beat, but the original cached every analysis in a `.txt`
sidecar next to the track. `--beats` reads that format directly.

**The duration.** The beats file's `dur=` line and a local MP3 decode disagree by
around 80 ms. Since the algorithm spreads the images across the duration, using
the wrong one shifts every onset — enough to change the rendered frame count.
`--beats` takes the duration from the file that supplied the beats.

**The frame loop.** `--reproduce-2010` replays the original's frame-emitting
loop, which does not do what its own algorithm computed. This caused three bugs:

1. The first image is never shown
2. The movie ends at the last onset rather than at the end of the audio
3. An image can be skipped when two onsets land within a frame of each other.

Without this the output is only about 2% frame-identical to the original. With it, it's exact.

You should always leave `--reproduce-2010` off for new work — then every image is shown, starting
with the first, and the movie lasts as long as the music.

**The orientation.** Every one of the original 680 photographs carries an EXIF
orientation tag of 8, "rotate 90°", and it is wrong: the stored pixels are
already upright. The 2010 script never read EXIF, so it never noticed.
audiotexture applies the tag by default, as image viewers do, so
the `--ignore-exif` flag should be passed.

`--frames` reproduces the original's actual output, a directory of `%06d.{ext}`
hardlinks, for diffing against archived frames or feeding into something else.

#### The verification process

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
indistinguishable from the predicted one, and the cut positions (what the
algorithm actually decides) agree exactly.

## Formats supported

**Images:** JPEG, PNG, GIF, TIFF, BMP and WebP (`.jpg`, `.jpeg`, `.png`,
`.gif`, `.tif`, `.tiff`, `.bmp`, `.webp`, in any letter case). Other files in
the directory are skipped. Images are decoded by Go's own image packages, not
FFmpeg, so this list is the same everywhere. A photo's EXIF orientation tag
is applied, so pictures taken sideways come out upright; `--ignore-exif` uses
the pixels exactly as stored instead.

**Audio:** anything your FFmpeg build can decode, which in practice means MP3,
AAC/M4A, FLAC, Ogg Vorbis, Opus and WAV. It is downmixed to mono for beat
detection; the soundtrack in the movie keeps its channels.

**Output:** H.264 video (libx264 if it is available, otherwise whichever H.264
encoder FFmpeg has) with AAC audio. The container is chosen from the `--out`
file extension. Use `.mp4`. `.mov` and `.mkv` take the same codecs and should
also work, but `.webm` will not, because WebM does not allow H.264. `--frames`
writes each frame as a numbered hard link to, or copy of, its source image,
so frames keep the source image's format.

**Beat files** (`--beats`): plain text. Either the original Echo Nest sidecar
(a comment line, a `dur=` line, then comma-separated beat times), or just beat
times in seconds, one per line or separated by commas or spaces. Blank lines
and lines starting with `#` are ignored.

## Library

```go
import "github.com/simonwistow/audiotexture"

res, err := audiotexture.GenerateFiles("./pix", "track.mp3", "movie.mp4", audiotexture.Options{
    Algorithm: texture.Bars,
    Video:     video.Options{Width: 1920, Height: 1080},
})
```

`GenerateFiles` is the convenience form. Underneath it, `Generate` takes
interfaces rather than paths, so none of the three has to be a file:

```go
func Generate(imgs images.Images, track io.ReadSeeker, out io.WriteSeeker, opts Options) (*Result, error)
```

- **Images** are an `images.Images`: the image names, in order, and a way to
  decode each one. `images.FromFS` lists the images in any `fs.FS` (a
  directory, an `embed.FS`, a zip file) in lexical filename order. Implement
  the interface yourself to choose a different order or to supply images from
  somewhere else. Each image is decoded only when it comes on screen, so they
  are never all in memory at once.
- **The soundtrack** is any `io.ReadSeeker`. It is read twice, once for beat
  detection and once for encoding, and each time from its start. The format
  is detected from the content.
- **The output** is any `io.WriteSeeker`. It has to seek because the muxer
  goes back to fill in the header once it knows what the file holds. There is
  no file name to guess the container from, so `video.Options.Format` names it
  (default `"mp4"`). When the writer is an `*os.File` positioned at its start,
  an MP4 also gets its index moved to the front (faststart) so it can play
  before it has finished downloading. FFmpeg does that by reopening the file
  by name, which other writers don't have, so for those the index stays at
  the end. That is fine for playing a local file but slower to start
  streaming.

The pipeline is four decoupled packages, each usable on its own:

| Package   | Does                                                     |
|-----------|----------------------------------------------------------|
| `audio`   | decode any FFmpeg-supported audio to mono PCM             |
| `beats`   | onset detection, tempo estimation, beat tracking          |
| `texture` | assign images to onset times (pluggable algorithms)       |
| `video`   | encode the assignment plus the audio into a movie         |

`Options.Algorithm` takes the algorithm itself, a `texture.Algorithm`, rather
than its name. The built-ins are `texture.Even`, `texture.Legacy`,
`texture.Optimal` (the default, or `texture.Legacy` with `Reproduce2010`),
`texture.Novelty` and `texture.Bars`. Your own is
anything with an `Assign` method:

```go
type myAlgorithm struct{}

func (myAlgorithm) Assign(in texture.Input) ([]texture.Onset, error) { ... }

res, err := audiotexture.Generate(imgs, track, out, audiotexture.Options{Algorithm: myAlgorithm{}})
```

To make it selectable by name as well, with `--algorithm` or from a config
file, register it in an `init`. `texture.Get` then looks it up:

```go
func init() {
    texture.Register("mine", "one-line description", myAlgorithm{})
}
```

## Algorithms

- **`even`** — ignore beats, space images equally. The baseline, and the
  sensible fallback for material with no discernible pulse.
- **`legacy`** — the 2010 algorithm: pre-space onsets evenly, snap each to a
  beat within half a shot-length, stack the ones that find nothing and spread
  them out once a later image does. Greedy and not optimal, but it is what
  produced the original videos.
- **`optimal`** — the default. The same objective solved exactly, by dynamic
  programming over every beat instead of taking the first that fits. Against
  a steady pulse this is barely distinguishable from `legacy`; it earns its
  keep when the beats are sparse or clustered, which is where the greedy
  version runs out of reachable beats and falls back to spreading images
  evenly. `--reproduce-2010` uses `legacy` instead, unless `--algorithm` says
  otherwise.
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
are new. In Go, each is the value of the same name in `texture`, such as
`texture.Bars`. To add your own, see [Library](#library) above.

## How the analysis works

### Beat detection

An implementation of Ellis (2007), with the tempo allowed to move.

A mel-scaled spectral flux "onset strength" envelope says how much new energy
appears at each instant. A dynamic program then picks the beat sequence
maximising onset strength landed on, minus a penalty for straying from the
expected beat period. Solving that step as a DP rather than greedily is the
point: it finds the globally best sequence, so the grid holds its place through
a quiet passage instead of latching onto whatever transient happens to be
nearby.

Ellis takes the beat period as one number for the whole track, from the
autocorrelation of the entire envelope under a log-Gaussian prior over
plausible tempos. That assumes the tempo never changes, which holds for a
sequenced record and fails for anything played by people. So instead this
autocorrelates eight-second windows — a *tempogram*, one column every half
second — and decodes a path through it with a second Viterbi: each column votes
for a period, and each step pays for changing tempo.

Two details earn their keep. Each column is normalised to a peak of 1 before
it votes, so a loud chorus does not outvote the rest of the piece. And the
search is confined to a band 25% either side of one tempo taken from the summed
tempogram, because a squared-change penalty is cheap to pay in many small
steps: left unbounded, a path will happily walk from 137 BPM down to 78 and
back over a minute, collecting whatever each passage correlates best with and
leaving a beat grid that means a different thing in every section. A piece of
music has one tempo, which it may wander around.

Measured against the archived Echo Nest analyses of the eight tracks, at the
standard ±70 ms tolerance, following the tempo is never worse than holding it
fixed and is worth about 7% of F-measure on the one track that noticeably
drifts. It does not help with the metrical level: a track whose eighth notes
are as strong as its beats is genuinely ambiguous, and no statistic tried here
separated it from the tracks that came out right. That is what `--start-bpm`
is for.

The sub-frame part of the period comes from the whole-track peak rather than
from each column. One column holds a dozen beats and its correlation peak is
correspondingly blunt; 126 BPM is lag 20.5 at the default hop, exactly between
two frames, and a per-column parabola gets it wrong by the full 2.5%.

About 200 ms for a three-minute track.

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

GPL, version 3 or (at your option) any later version. See
[LICENSE](LICENSE).

This fits the dependencies: libx264 requires FFmpeg to be configured with
`--enable-gpl`, which is what `brew install ffmpeg` and the build script here
both do. A binary built from this code is therefore under the GPL either way.
