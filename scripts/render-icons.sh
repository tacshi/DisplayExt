#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/build.sh"
cd "$ROOT_DIR/design"

case "${1:-}" in
-h | --help)
	cat <<'USAGE'
Renders the icons from the SVGs in design/:

  design/logo.svg         -> resources/icon.png (the app icon, 1024×1024)
  design/tray.svg         -> internal/app/tray.png (menu bar, idle)
  design/tray-active.svg  -> internal/app/tray-active.png (menu bar, a display mounted)

Needs rsvg-convert (brew install librsvg).
USAGE
	exit 0
	;;
"") ;;
*) die "unknown option: $1" ;;
esac

check_tools rsvg-convert
rsvg-convert -w 1024 -h 1024 logo.svg -o ../resources/icon.png
rsvg-convert -h 36 tray.svg -o ../internal/app/tray.png
rsvg-convert -h 36 tray-active.svg -o ../internal/app/tray-active.png
info "rendered resources/icon.png, internal/app/tray.png and internal/app/tray-active.png"
