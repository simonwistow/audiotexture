#!/usr/bin/env bash
#
# Build the FFmpeg that audiotexture links against, into the prefix given as
# the first argument.
#
# CI builds its own rather than installing a package because no distribution
# ships FFmpeg 8 yet, and go-astiav is pinned to one major version of the
# libav* ABI. x264 goes into the same prefix, so the tree this produces is
# self-contained and can be cached and restored on its own -- nothing outside
# it has to still be installed for a cache hit to be usable.
#
# Both are built shared. cgo asks pkg-config for `--libs`, not
# `--libs --static`, and a static FFmpeg puts -lx264 and friends in
# Libs.private where that will not find them.
#
# What was built is recorded in PREFIX/BUILD-INFO, with the exact commits:
# x264's "stable" is a branch, so the name alone does not say. If
# SOURCE_ARCHIVE_DIR is set, the source of both is also saved there as
# tarballs. A release ships these libraries, and they are GPL, so their
# source has to go out alongside.
set -euo pipefail

prefix="${1:?usage: build-ffmpeg.sh PREFIX}"
ffmpegVersion="${FFMPEG_VERSION:-n8.0}"
x264Version="${X264_VERSION:-stable}"

src="$(mktemp -d)"
trap 'rm -rf "$src"' EXIT

jobs="$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)"
export PKG_CONFIG_PATH="$prefix/lib/pkgconfig"

echo "==> x264 $x264Version"
git clone --quiet --depth 1 --branch "$x264Version" \
	https://code.videolan.org/videolan/x264.git "$src/x264"
cd "$src/x264"
x264Commit="$(git rev-parse HEAD)"
./configure --prefix="$prefix" --enable-shared --disable-cli
make -j"$jobs"
make install

echo "==> ffmpeg $ffmpegVersion"
git clone --quiet --depth 1 --branch "$ffmpegVersion" \
	https://github.com/FFmpeg/FFmpeg.git "$src/ffmpeg"
cd "$src/ffmpeg"
ffmpegCommit="$(git rev-parse HEAD)"
# --disable-programs drops the ffmpeg and ffprobe binaries, which is most of
# the build and none of what this project uses: the whole point is to link the
# libraries rather than shell out to the tools.
#
# --disable-autodetect stops configure linking whatever optional libraries
# the build machine happens to have -- X11, ALSA, SDL, VA-API -- none of
# which audiotexture uses. Without it the libraries quietly depend on them,
# and a release built on one machine fails to load on another.
./configure \
	--prefix="$prefix" \
	--enable-shared \
	--disable-static \
	--enable-gpl \
	--enable-libx264 \
	--disable-programs \
	--disable-doc \
	--disable-debug \
	--disable-autodetect \
	--extra-cflags="-I$prefix/include" \
	--extra-ldflags="-L$prefix/lib -Wl,-rpath,$prefix/lib"
make -j"$jobs"
make install

cat >"$prefix/BUILD-INFO" <<EOF
ffmpeg $ffmpegVersion $ffmpegCommit https://github.com/FFmpeg/FFmpeg
x264 $x264Version $x264Commit https://code.videolan.org/videolan/x264
EOF

if [ -n "${SOURCE_ARCHIVE_DIR:-}" ]; then
	mkdir -p "$SOURCE_ARCHIVE_DIR"
	git -C "$src/ffmpeg" archive --prefix="ffmpeg-$ffmpegVersion/" \
		-o "$SOURCE_ARCHIVE_DIR/ffmpeg-$ffmpegVersion-source.tar.gz" HEAD
	git -C "$src/x264" archive --prefix="x264-${x264Commit:0:10}/" \
		-o "$SOURCE_ARCHIVE_DIR/x264-${x264Commit:0:10}-source.tar.gz" HEAD
fi

echo "==> built"
cat "$prefix/BUILD-INFO"
PKG_CONFIG_PATH="$prefix/lib/pkgconfig" pkg-config --modversion \
	libavcodec libavdevice libavfilter libavformat libswresample libswscale libavutil
