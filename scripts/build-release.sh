#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/build.sh"
cd "$ROOT_DIR"

usage() {
	cat <<'EOF'
Builds DisplayExt's release disk images, signed with a Developer ID and
notarized.

  scripts/build-release.sh                   # arm64, x64 and universal, the version in mygo.json
  scripts/build-release.sh 0.2.0             # another version
  scripts/build-release.sh --arch arm64      # only some: arm64, x64, universal
  scripts/build-release.sh --skip-notarize   # signed, not notarized (faster)
  scripts/build-release.sh -o out            # another output directory

The disk images go to build/release: DisplayExt-<version>-{arm64,x64,universal}.dmg
for Apple silicon, Intel and both, beside a folder per architecture with the app.

Signs with DEVELOPER_ID_APPLICATION, or the keychain's one Developer ID
Application identity when it is not set, and notarizes with the notarytool
keychain profile DISPLAYEXT_NOTARY_PROFILE, "DisplayExt" by default, made with
  xcrun notarytool store-credentials DisplayExt --apple-id <id> --team-id <team>

Paths given with -o are from where this is run.
EOF
}

VERSION=""
ARCHES=()
NOTARIZE=true
OUT_DIR="$ROOT_DIR/build/release"
while (($#)); do
	case "$1" in
	--arch)
		[[ $# -ge 2 ]] || die "--arch needs arm64, x64 or universal"
		ARCHES+=("$2")
		shift
		;;
	--skip-notarize) NOTARIZE=false ;;
	-o)
		[[ $# -ge 2 ]] || die "-o needs a directory"
		OUT_DIR="$(abs_path "$2")"
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	-*) die "unknown option: $1" ;;
	*)
		[[ -z "$VERSION" ]] || die "one version, please: $VERSION or $1"
		VERSION="${1#v}"
		;;
	esac
	shift
done

check_macos
check_tools go python3 xcrun security
[[ -n "$VERSION" ]] || VERSION="$(config_value version)"
check_version "$VERSION"
((${#ARCHES[@]} > 0)) || ARCHES=(arm64 x64 universal)

PLATFORMS=()
for arch in "${ARCHES[@]}"; do
	if [[ "$arch" == amd64 ]]; then arch=x64; fi
	PLATFORMS+=("darwin/$(goarch "$arch")")
done

IDENTITY="$(signing_identity)"
BUILD_ARGS=(-sign "$IDENTITY")
CONFIG=("version=$VERSION")
if $NOTARIZE; then
	CONFIG+=("macos.notarize.keychainProfile=$(notary_profile)")
else
	BUILD_ARGS+=(-skip-notarize)
fi
patch_config "${CONFIG[@]}"

SLUG="$(config_value slug)"
info "building DisplayExt $VERSION for ${PLATFORMS[*]}, signed by $IDENTITY"
go tool mygo build -platform "$(
	IFS=,
	echo "${PLATFORMS[*]}"
)" -o "$OUT_DIR" "${BUILD_ARGS[@]}"

for arch in "${ARCHES[@]}"; do
	if [[ "$arch" == amd64 ]]; then arch=x64; fi
	case "$arch" in
	universal) dir=darwin-universal dmg="$SLUG-$VERSION.dmg" ;;
	*) dir="darwin-$(goarch "$arch")" dmg="$SLUG-$VERSION-$(goarch "$arch").dmg" ;;
	esac
	[[ -f "$OUT_DIR/$dir/$dmg" ]] || die "expected $OUT_DIR/$dir/$dmg after the build"
	codesign --verify --strict "$OUT_DIR/$dir/DisplayExt.app" || die "$OUT_DIR/$dir/DisplayExt.app is not validly signed"
	if $NOTARIZE; then
		xcrun stapler validate "$OUT_DIR/$dir/$dmg" >/dev/null || die "$OUT_DIR/$dir/$dmg is not notarized"
	fi
	mv -f "$OUT_DIR/$dir/$dmg" "$OUT_DIR/DisplayExt-$VERSION-$arch.dmg"
	info "$OUT_DIR/DisplayExt-$VERSION-$arch.dmg"
done
