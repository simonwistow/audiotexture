# Releasing

Releases follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) and
are tagged `vX.Y.Z`. The `v` is what Go modules expect, so the tag is also what
`go install github.com/simonwistow/audiotexture/cmd/audiotexture@vX.Y.Z`
resolves to. `CHANGELOG.md` follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and its section for a
version becomes the release notes.

## As you go

Add each notable change to the `## [Unreleased]` section at the top of
`CHANGELOG.md` when it lands, not at release time. Keep a Changelog groups
entries under these headings, in this order, using only the ones a release
needs:

- `### Added` for new features
- `### Changed` for changes in existing behaviour
- `### Deprecated` for features that will be removed
- `### Removed` for features that have been removed
- `### Fixed` for bug fixes
- `### Security` for vulnerabilities

CI runs `go run ./internal/cmd/changelog lint` on every push, which checks
that `[Unreleased]` comes first, that every release has a date and a link, and
that releases run newest first.

## Cutting a release

1. Turn `[Unreleased]` into the new version:

   ```sh
   go run ./internal/cmd/changelog release 0.1.0
   ```

   This retitles the section `## [0.1.0] - <today>`, opens an empty
   `## [Unreleased]` above it, and updates the links at the bottom of the file
   so each version links to its diff. It refuses if `[Unreleased]` is empty or
   the version is not newer than the last one. Pass `-date YYYY-MM-DD` before
   `release` to use a different date.

2. Read the result, then commit and tag:

   ```sh
   git commit -am "Release v0.1.0"
   git tag -a v0.1.0 -m "audiotexture v0.1.0"
   git push origin main v0.1.0
   ```

3. Pushing the tag runs `.github/workflows/release.yml`, which:

   - refuses to continue unless `CHANGELOG.md` has a dated `## [0.1.0]`
     section, at the top, with entries in it
   - builds FFmpeg and x264 from source, runs the tests, and builds the
     command with `audiotexture version` reporting `v0.1.0`
   - bundles it with its FFmpeg libraries for Linux x86-64 and macOS Apple
     silicon, and checks each bundle loads only its own copies; the Linux one
     is also run in a clean Ubuntu 22.04 container
   - creates the GitHub release, with the changelog section as its notes

A tag with a hyphen, such as `v0.2.0-rc1`, is published as a pre-release. It
needs its own `## [0.2.0-rc1]` section, like any other version.

If the workflow fails before publishing, fix the problem, then move the tag
onto the fixed commit and push it again:

```sh
git tag -d v0.1.0 && git push origin :refs/tags/v0.1.0
git tag -a v0.1.0 -m "audiotexture v0.1.0" && git push origin v0.1.0
```

Do not move a tag once the release is published. The Go module proxy caches
a version the first time anyone fetches it and will not pick up a change, so
release a new version instead.

## What a release contains

- `audiotexture-vX.Y.Z-linux-amd64.tar.gz` and
  `audiotexture-vX.Y.Z-darwin-arm64.tar.gz`: `bin/audiotexture`, the FFmpeg
  and x264 shared libraries in `lib/`, and `LICENSE`, `README.md`,
  `CHANGELOG.md` and `THIRD-PARTY`. The binary finds the libraries relative to
  itself, so the two directories must stay together.
- `ffmpeg-n8.0-source.tar.gz` and `x264-<commit>-source.tar.gz`: the exact
  source the bundled libraries were built from. They are GPL, and distributing
  their binaries means making their source available too; `THIRD-PARTY` in
  each bundle records the commits.
- `SHA256SUMS` for all of the above.

The Linux build needs glibc 2.35 (Ubuntu 22.04) or newer. The macOS build
needs macOS 13 or newer. It is signed ad hoc, not notarised, so a copy
downloaded with a browser needs its quarantine flag cleared before it will
run:

```sh
xattr -dr com.apple.quarantine audiotexture-vX.Y.Z-darwin-arm64
```
