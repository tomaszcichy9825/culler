#!/usr/bin/env bash
#
# Renders assets/brand into every icon artefact the wails3 packager consumes:
#
#   build/appicon.png              source image for wails3, and the Linux
#                                  package icon (build/linux/nfpm/nfpm.yaml)
#   build/appicon.icon/            Icon Composer input; actool compiles it to
#                                  build/darwin/Assets.car, which is what
#                                  macOS 26 resolves for the Dock icon via
#                                  CFBundleIconName
#   build/darwin/icons.icns        copied into <app>.app/Contents/Resources
#                                  and used as the .dmg volume icon
#   build/darwin/dmg-file-icon.*   the app's file icon inside the .dmg
#   build/windows/icon.ico         the .exe icon and the NSIS installer icon
#
# Rerunning is a no-op: every artefact is rebuilt from the SVGs, never patched.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
brand="$root/assets/brand"
build="$root/build"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

die() { printf 'icons: %s\n' "$1" >&2; exit 1; }

need() {
	command -v "$1" >/dev/null 2>&1 && return 0
	die "$1 not found. Install it with:
    brew install $2   (Debian/Ubuntu: apt install $3)"
}

need rsvg-convert librsvg librsvg2-bin

# 16, 32 and 64 have their own drawings: the spec's downscale ramp thickens the
# mark and opens up the plate radius as the pixels run out, and at 16 the
# outlined frame gives up its inner hole entirely.
svg_for() {
	case "$1" in
	16 | 32 | 64) printf '%s/icon-%s.svg' "$brand" "$1" ;;
	*) printf '%s/icon-macos.svg' "$brand" ;;
	esac
}

render() { # svg size out
	rsvg-convert -w "$2" -h "$2" "$1" -o "$3"
}

echo "icons: rendering macOS ramp"
for size in 16 32 64 128 256 512 1024; do
	render "$(svg_for "$size")" "$size" "$work/mac-$size.png"
done

# --- macOS .icns ---------------------------------------------------------
# Each iconset slot takes the drawing made for its pixel size, so a 16pt @2x
# slot gets the 32px drawing rather than a resampled 16px one.
iconset="$work/culler.iconset"
mkdir -p "$iconset"
cp "$work/mac-16.png" "$iconset/icon_16x16.png"
cp "$work/mac-32.png" "$iconset/icon_16x16@2x.png"
cp "$work/mac-32.png" "$iconset/icon_32x32.png"
cp "$work/mac-64.png" "$iconset/icon_32x32@2x.png"
cp "$work/mac-128.png" "$iconset/icon_128x128.png"
cp "$work/mac-256.png" "$iconset/icon_128x128@2x.png"
cp "$work/mac-256.png" "$iconset/icon_256x256.png"
cp "$work/mac-512.png" "$iconset/icon_256x256@2x.png"
cp "$work/mac-512.png" "$iconset/icon_512x512.png"
cp "$work/mac-1024.png" "$iconset/icon_512x512@2x.png"

if [ "$(uname -s)" = "Darwin" ]; then
	command -v iconutil >/dev/null 2>&1 ||
		die "iconutil not found. Install it with:
    xcode-select --install"
	iconutil -c icns "$iconset" -o "$work/culler.icns"
	echo "icons: built culler.icns"
else
	echo "icons: not macOS, skipping .icns (iconutil is macOS-only)" >&2
fi

# --- Windows .ico --------------------------------------------------------
# The Windows plate is full bleed with a 2px radius, so it downscales cleanly
# everywhere except 16, which falls back to the shared 16px drawing.
ico_pngs=("$work/mac-16.png")
for size in 32 48 64 128 256; do
	render "$brand/icon-windows.svg" "$size" "$work/win-$size.png"
	ico_pngs+=("$work/win-$size.png")
done

if command -v magick >/dev/null 2>&1; then
	magick "${ico_pngs[@]}" "$work/culler.ico"
elif command -v png2ico >/dev/null 2>&1; then
	png2ico "$work/culler.ico" "${ico_pngs[@]}"
else
	die "neither magick nor png2ico found. Install one with:
    brew install imagemagick   (Debian/Ubuntu: apt install imagemagick)"
fi
echo "icons: built culler.ico"

# --- inputs the wails3 icon task reads -----------------------------------
cp "$work/mac-1024.png" "$build/appicon.png"

# The Icon Composer layer is the bare mark on the 1024pt canvas actool expects;
# the plate, shadow and specular highlight come from icon.json. Its coordinates
# are icon-macos.svg's, scaled from the 132px plate up to 1024 (x7.757576).
cat >"$build/appicon.icon/Assets/culler_mark.svg" <<'SVG'
<svg width="1024" height="1024" viewBox="0 0 1024 1024" xmlns="http://www.w3.org/2000/svg">
  <g transform="scale(7.757576)">
    <rect x="54.5" y="32.5" width="45" height="45" rx="5"
          fill="none" stroke="#4a5560" stroke-width="3"/>
    <rect x="31"   y="53"   width="48" height="48" rx="5" fill="#56b6c2"/>
  </g>
</svg>
SVG

# Compile Assets.car through wails' own task rather than calling actool here,
# so the task runner records the source checksums. Without that, the next
# `make build` would rerun the task and replace the .icns and .ico below with
# its own plain downscales of appicon.png. The task has to be invoked under the
# platform namespace the build uses, because that is the checksum key the build
# consults; the task runner fingerprints sources only, so overwriting its
# outputs afterwards does not make it stale again.
case "$(uname -s)" in
Darwin) ns=darwin ;;
Linux) ns=linux ;;
*) ns=windows ;;
esac
wails3=$(command -v wails3 2>/dev/null || echo "$(go env GOPATH 2>/dev/null)/bin/wails3")
if [ -x "$wails3" ]; then
	echo "icons: compiling Assets.car"
	find "$root/.task/checksum" -name '*generate-icons*' -delete 2>/dev/null || true
	PATH="$PATH:$(go env GOPATH)/bin" "$wails3" task "$ns:common:generate:icons"
elif [ "$(uname -s)" = "Darwin" ]; then
	die "wails3 not found; the macOS Dock icon comes from Assets.car, which
    only wails3 can compile. Install it with:
    make tools"
else
	echo "icons: wails3 not found, skipping Assets.car" >&2
fi

# --- publish -------------------------------------------------------------
# Last, because the wails3 task above writes its own icons.icns and icon.ico
# from appicon.png and would otherwise clobber the ramp-aware ones.
cp "$work/culler.ico" "$build/windows/icon.ico"
if [ -f "$work/culler.icns" ]; then
	cp "$work/culler.icns" "$build/darwin/icons.icns"
	cp "$work/culler.icns" "$build/darwin/dmg-file-icon.icns"
	cp "$work/mac-1024.png" "$build/darwin/dmg-file-icon.png"
fi

echo "icons: done"
