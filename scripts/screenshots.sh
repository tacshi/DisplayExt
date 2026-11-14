#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/build.sh"
cd "$ROOT_DIR"

case "${1:-}" in
-h | --help)
	cat <<'USAGE'
Renders the images the READMEs show into docs/images:

  logo.png                   the app icon, 256×256
  settings-light.png         the settings window, light, a display mounted
  settings-dark.png          the same in dark mode
  settings-unmounted.png     a display that is not mounted
  general-light.png          the General page, light
  general-dark.png           the General page, dark

and each window again in Chinese, as <name>.zh-Hans.png.

The windows are drawn by the app's own view at 2×, headless, with the test
backend, then framed as macOS windows. Needs rsvg-convert and ImageMagick
(brew install librsvg imagemagick).
USAGE
	exit 0
	;;
"") ;;
*) die "unknown option: $1" ;;
esac

check_tools go rsvg-convert magick
OUT="$ROOT_DIR/docs/images"
RAW="$(mktemp -d)"
trap 'rm -rf -- "$RAW"' EXIT
mkdir -p "$OUT"

info "rendering the settings window"
DISPLAYEXT_SNAPSHOT_DIR="$RAW" go test -count=1 -run '^TestSnapshot$' ./internal/app/ >/dev/null

frame() {
	local in="$1" out="$2" dark="$3" w h edge
	w="$(magick identify -format '%w' "$in")"
	h="$(magick identify -format '%h' "$in")"
	edge='rgba(0,0,0,0.22)'
	if [[ "$dark" == true ]]; then edge='rgba(255,255,255,0.16)'; fi
	magick -size "${w}x${h}" xc:none -fill white -draw "roundrectangle 0,0 $((w - 1)),$((h - 1)) 22,22" "$RAW/mask.png"
	magick "$in" "$RAW/mask.png" -alpha set -compose DstIn -composite \
		-stroke 'rgba(0,0,0,0.12)' -strokewidth 1 \
		-fill '#FF5F57' -draw 'circle 40,38 52,38' \
		-fill '#FEBC2E' -draw 'circle 80,38 92,38' \
		-fill '#28C840' -draw 'circle 120,38 132,38' \
		-fill none -stroke "$edge" -strokewidth 2 -draw "roundrectangle 1,1 $((w - 2)),$((h - 2)) 21,21" \
		"$RAW/window.png"
	magick "$RAW/window.png" \( +clone -background black -shadow 32x24+0+18 \) +swap \
		-background none -layers merge +repage "$out"
}

for name in settings-light settings-dark settings-unmounted general-light general-dark; do
	dark=false
	if [[ "$name" == *-dark ]]; then dark=true; fi
	frame "$RAW/$name.png" "$OUT/$name.png" "$dark"
	frame "$RAW/$name.zh-Hans.png" "$OUT/$name.zh-Hans.png" "$dark"
done
rsvg-convert -w 256 -h 256 design/logo.svg -o "$OUT/logo.png"
info "wrote $(ls "$OUT" | wc -l | tr -d ' ') images to docs/images"
