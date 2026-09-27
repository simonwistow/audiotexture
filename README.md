# audiotexture

Generate beat-synced slideshow movies from a directory of images and an audio
track. Beats are detected locally — no external APIs, no shelling out to
`ffmpeg`.

This is a Go port of a 2010 hack I did which used the (long-dead) Echo Nest API
for beat detection and emitted a directory of numbered frames for something else
to encode.

`audiotexture` does the analysis and the encoding itself.

## Status

Under construction. See `TODO` below.

## Install

Requires FFmpeg 8.x development libraries, since the audio decoding and movie
encoding go through [go-astiav](https://github.com/asticode/go-astiav)
(cgo bindings to libav*).

```sh
brew install ffmpeg pkg-config      # macOS
apt install libavcodec-dev libavformat-dev libavutil-dev \
            libswscale-dev libswresample-dev pkg-config   # Debian/Ubuntu

go install github.com/simonwistow/audiotexture/cmd/audiotexture@latest
```

## Use

```sh
audiotexture generate --images ./pix --audio track.mp3 --out movie.mp4
audiotexture list-algorithms
```

## Library

```go
import "github.com/simonwistow/audiotexture"
```

The pipeline is four decoupled stages, each usable on its own:

| Package    | Does                                                      |
|------------|-----------------------------------------------------------|
| `audio`    | decode any FFmpeg-supported audio file to mono PCM         |
| `beats`    | onset strength envelope, tempo estimation, beat tracking   |
| `texture`  | assign images to onset times (pluggable algorithms)        |
| `video`    | encode the assignment plus the audio into a movie          |

## Algorithms

`texture.Algorithm` is a registry, so alternatives are pluggable:

- `even` — ignore beats, space images equally. The trivial baseline.
- `legacy` — a faithful port of the 2010 Perl algorithm: pre-space onsets
  evenly, snap each to a beat within a window, and redistribute any that
  found no beat.
- `optimal` — the same objective as `legacy` (land on beats, stay close to
  even spacing) solved exactly by dynamic programming instead of greedily.
- `novelty` — `optimal`, but beats are weighted by audio novelty from a
  self-similarity matrix, so cuts prefer structural boundaries.

## References

- D. Ellis, [Beat Tracking by Dynamic Programming](https://www.ee.columbia.edu/~dpwe/pubs/Ellis07-beattrack.pdf),
  *J. New Music Research* 36(1), 2007 — the beat tracker.
- J. Foote, M. Cooper, A. Girgensohn, [Creating Music Videos using Automatic
  Media Analysis](https://dl.acm.org/doi/10.1145/641007.641119), ACM MM 2002 —
  audio novelty via self-similarity.

## Licence

MIT. See [LICENSE](LICENSE).
