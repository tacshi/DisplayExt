#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/build.sh"
cd "$ROOT_DIR"

usage() {
	cat <<'EOF'
Builds DisplayExt Debug, which runs beside a release copy: it keeps MyGo's
development features, and is named "DisplayExt Debug" with the identifier
com.displayext.app.debug, so it has its own settings, display identities,
log and single-instance lock. It never updates itself, so a release never
replaces it.

  scripts/build-debug.sh              # for this Mac's architecture
  scripts/build-debug.sh --open       # build, then launch it
  scripts/build-debug.sh --arch x64   # Intel

The app goes to build/mac-debug/DisplayExt Debug.app, signed ad hoc.
Quit a running DisplayExt Debug before --open, or the running copy stays.
EOF
}

ARCH=""
OPEN=false
while (($#)); do
	case "$1" in
	--arch)
		[[ $# -ge 2 ]] || die "--arch needs arm64 or x64"
		ARCH="$2"
		shift
		;;
	--open) OPEN=true ;;
	-h | --help)
		usage
		exit 0
		;;
	*) die "unknown option: $1" ;;
	esac
	shift
done

check_macos
check_tools go python3
[[ -n "$ARCH" ]] || ARCH="$(host_arch)"
GOARCH="$(goarch "$ARCH")"
[[ "$GOARCH" != universal ]] || die "debug builds are arm64 or x64"

NAME="DisplayExt Debug"
patch_config "name=$NAME" "slug=displayext-debug" "identifier=$(config_value identifier).debug" -updates

OUT="build/mac-debug"
BUILT="$OUT/darwin-$GOARCH/$NAME.app"
APP="$OUT/$NAME.app"
info "building $NAME for darwin/$GOARCH"
go tool mygo build -debug -skip-dmg -sign - -platform "darwin/$GOARCH" -o "$OUT"
[[ -d "$BUILT" ]] || die "expected $BUILT after the build"
rm -rf "$APP"
mv "$BUILT" "$APP"
rmdir "$(dirname "$BUILT")" 2>/dev/null || true
info "built $ROOT_DIR/$APP"
if $OPEN; then open "$APP"; fi
