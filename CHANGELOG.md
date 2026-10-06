# Changelog

Notable changes to `audiotexture`. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Data Input:
    - Image Loading.
    - EXIF orientation applied to images by default, with `--ignore-exif` to turn it off.
    - Music Loading. Handles anything that FFmpeg can read.
- Music Analysis to give input to the texture algorithms:
    - Beat Detection algorithm using an implementation of Ellis (2007), replacing the Echo Nest API, which no longer exists.
    - Audio Novelty algorithm which reports where the music changes character based on Foote, Cooper and Girgensohn (2002).
- Audio Texture algorithms which generate image collages in time to the music:
    - `even` — ignore beats, space images equally.
    - `legacy` — the 2010 algorithm, a faithful port.
    - `optimal` — the same objective solved exactly by dynamic programming.
    - `novelty` — `optimal`, with beats weighted by audio novelty.
    - `bars` — hold each image a whole number of beats, snapped to a bar length.
- Movie Generation. Again, supporting anything FFmpeg can write.
- Command Line Tool with various options, including `version`.
- Prebuilt releases for Linux x86-64 and macOS on Apple silicon, bundled with the FFmpeg they need.
- Bug Compatibility with the 2010 original (optional).
  
### Known limitations

- Beat detection can settle on the wrong metrical level, reporting double or
  half the musical tempo, when a track's subdivisions carry as much energy as
  its beats. `--start-bpm` moves the prior; `--bpm` settles it outright.
- FFmpeg 8.x is required. The bindings are pinned to that ABI and will not
  build against 7 or 9.

[Unreleased]: https://github.com/simonwistow/audiotexture/commits/main
