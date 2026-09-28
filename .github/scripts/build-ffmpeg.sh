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
./configure --prefix="$prefix" --enable-shared --disable-cli
make -j"$jobs"
make install

echo "==> ffmpeg $ffmpegVersion"
git clone --quiet --depth 1 --branch "$ffmpegVersion" \
	https://github.com/FFmpeg/FFmpeg.git "$src/ffmpeg"
cd "$src/ffmpeg"
# --disable-programs drops the ffmpeg and ffprobe binaries, which is most of
# the build and none of what this project uses: the whole point is to link the
# libraries rather than shell out to the tools.
./configure \
	--prefix="$prefix" \
	--enable-shared \
	--disable-static \
	--enable-gpl \
	--enable-libx264 \
	--disable-programs \
	--disable-doc \
	--disable-debug \
	--extra-cflags="-I$prefix/include" \
	--extra-ldflags="-L$prefix/lib -Wl,-rpath,$prefix/lib"
make -j"$jobs"
make install

echo "==> built"
PKG_CONFIG_PATH="$prefix/lib/pkgconfig" pkg-config --modversion \
	libavcodec libavdevice libavfilter libavformat libswresample libswscale libavutil
