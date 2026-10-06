#!/usr/bin/env bash
#
# Package a built audiotexture with the FFmpeg it links against, so it runs
# on a machine with no FFmpeg 8 of its own:
#
#   bundle.sh PREFIX BINARY OUTDIR NAME
#
# PREFIX is the FFmpeg that build-ffmpeg.sh installed and BINARY was linked
# against. The result is OUTDIR/NAME.tar.gz, holding NAME/bin/audiotexture,
# NAME/lib with the shared libraries, and the licence and notes.
#
# The libraries are shared (see build-ffmpeg.sh), so the binary and each
# library are rewritten to find their dependencies relative to themselves
# rather than at PREFIX, which will not exist anywhere else. The script then
# checks that the binary really does load the bundled copies.
set -euo pipefail

prefix="${1:?usage: bundle.sh PREFIX BINARY OUTDIR NAME}"
binary="${2:?usage: bundle.sh PREFIX BINARY OUTDIR NAME}"
outdir="${3:?usage: bundle.sh PREFIX BINARY OUTDIR NAME}"
name="${4:?usage: bundle.sh PREFIX BINARY OUTDIR NAME}"
root="$(cd "$(dirname "$0")/../.." && pwd)"

# The libraries that come from PREFIX, by name. Not just "libsw": macOS has
# system libraries called libswift*.
ours='lib(avcodec|avdevice|avfilter|avformat|avutil|swresample|swscale|x264)[.]'

mkdir -p "$outdir"
outdir="$(cd "$outdir" && pwd -P)"
dir="$outdir/$name"
rm -rf "$dir"
mkdir -p "$dir/bin" "$dir/lib"

cp "$binary" "$dir/bin/audiotexture"
chmod 755 "$dir/bin/audiotexture"
cp "$root/LICENSE" "$root/README.md" "$root/CHANGELOG.md" "$dir/"

{
	echo "This package includes shared libraries from FFmpeg and x264, built"
	echo "as follows (component, version, exact commit, upstream):"
	echo
	sed 's/^/    /' "$prefix/BUILD-INFO"
	echo
	echo "They are distributed under the GNU General Public License, version 2"
	echo "or later, as audiotexture is under version 3 or later (see LICENSE)."
	echo "The complete source for these exact versions is attached to the same"
	echo "GitHub release as this package, and is available from upstream."
} >"$dir/THIRD-PARTY"

case "$(uname -s)" in
Linux)
	# -P keeps the soname symlinks (libavcodec.so.62 -> libavcodec.so.62.x)
	# as links rather than three copies of each library.
	cp -P "$prefix"/lib/*.so* "$dir/lib/"

	# A library's own dependencies are found through its own RUNPATH, not
	# the executable's, so each library gets one too. $ORIGIN is for the
	# dynamic linker, not the shell, hence the single quotes.
	# shellcheck disable=SC2016
	patchelf --set-rpath '$ORIGIN/../lib' "$dir/bin/audiotexture"
	for lib in "$dir"/lib/*.so*; do
		[ -L "$lib" ] && continue
		# shellcheck disable=SC2016
		patchelf --set-rpath '$ORIGIN' "$lib"
	done

	echo "==> checking which libraries load"
	loaded="$(ldd "$dir/bin/audiotexture")"
	echo "$loaded"
	if grep -q 'not found' <<<"$loaded"; then
		echo "error: unresolved libraries" >&2
		exit 1
	fi
	# ldd reports the path as the RUNPATH spelled it, bin/../lib, so resolve
	# each one before comparing.
	outside=0
	while read -r lib file; do
		case "$(readlink -f "$file")" in
		"$dir"/lib/*) ;;
		*)
			echo "error: $lib loads from $file, outside the bundle" >&2
			outside=1
			;;
		esac
	done < <(grep -E "$ours" <<<"$loaded" | awk '{ print $1, $3 }')
	[ "$outside" -eq 0 ] || exit 1
	;;

Darwin)
	cp -P "$prefix"/lib/*.dylib "$dir/lib/"

	# Install names are absolute paths into PREFIX, both in each library's
	# own id and in every reference to another. Make them all @rpath, and
	# give the binary the one rpath it needs: its own ../lib.
	relink() {
		local file="$1" dep
		while read -r dep; do
			install_name_tool -change "$dep" "@rpath/$(basename "$dep")" "$file"
		done < <(otool -L "$file" | awk 'NR > 1 { print $1 }' | grep -F "$prefix/lib/" || true)
		while read -r dep; do
			install_name_tool -delete_rpath "$dep" "$file"
		done < <(otool -l "$file" | awk '/LC_RPATH/ { getline; getline; print $2 }' | grep -F "$prefix" || true)
	}
	for lib in "$dir"/lib/*.dylib; do
		[ -L "$lib" ] && continue
		install_name_tool -id "@rpath/$(basename "$(otool -D "$lib" | tail -1)")" "$lib"
		relink "$lib"
	done
	relink "$dir/bin/audiotexture"
	install_name_tool -add_rpath "@executable_path/../lib" "$dir/bin/audiotexture"

	# Editing a Mach-O invalidates its signature, and arm64 macOS will not
	# run code without one. An ad-hoc signature is enough to run.
	for f in "$dir"/lib/*.dylib "$dir/bin/audiotexture"; do
		[ -L "$f" ] && continue
		codesign --force --sign - "$f"
	done

	echo "==> checking which libraries load"
	if otool -L "$dir/bin/audiotexture" "$dir"/lib/*.dylib | grep -F "$prefix"; then
		echo "error: references to $prefix remain" >&2
		exit 1
	fi
	loaded="$(DYLD_PRINT_LIBRARIES=1 "$dir/bin/audiotexture" version 2>&1 >/dev/null)"
	echo "$loaded" | grep -E "$ours"
	if echo "$loaded" | grep -E "$ours" | grep -v -F "$dir/lib/"; then
		echo "error: the libraries above load from outside the bundle" >&2
		exit 1
	fi
	;;

*)
	echo "error: cannot bundle on $(uname -s)" >&2
	exit 1
	;;
esac

"$dir/bin/audiotexture" version

# COPYFILE_DISABLE keeps macOS tar from adding ._ resource-fork files.
COPYFILE_DISABLE=1 tar -C "$outdir" -czf "$outdir/$name.tar.gz" "$name"
echo "==> $outdir/$name.tar.gz"
