#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/build.sh"
cd "$ROOT_DIR"

usage() {
	cat <<'EOF'
Builds DisplayExt's release disk images, signed with a Developer ID and
notarized, and the update archives and manifests that installed copies update
from.

  scripts/build-release.sh                   # arm64, amd64 and universal, the version in mygo.json
  scripts/build-release.sh 0.2.0             # another version
  scripts/build-release.sh --arch arm64      # only some: arm64, amd64, universal
  scripts/build-release.sh --skip-notarize   # signed, not notarized (faster)
  scripts/build-release.sh -o out            # another output directory
  scripts/build-release.sh --notes notes.md  # release notes of the updates

The disk images go to build/release: DisplayExt-<version>-{arm64,amd64,universal}.dmg
for Apple silicon, Intel and both, beside a folder per architecture with the app.
With them go displayext-<version>-darwin-<arch>.tar.gz, the signed update
archives, update-darwin-<arch>.json, their manifests, and delta updates from
the versions before, displayext-<from>-to-<version>-darwin-<arch>.delta.

Updates are signed with MYGO_UPDATER_PRIVATE_KEY, else the key in the file
DISPLAYEXT_UPDATE_KEY, by default
  ~/Library/Application Support/mygo/update-keys/DisplayExt/mygo-update.key
made with
  go tool mygo keygen -o ~/Library/Application\ Support/mygo/update-keys/DisplayExt
Installed copies accept only updates signed with it: keep a copy safe.

The release notes in the manifests are the "## <version>" section of
--notes, a changelog, else of CHANGELOG.md when there is one.

Signs with DEVELOPER_ID_APPLICATION, or the keychain's one Developer ID
Application identity when it is not set, and notarizes with the notarytool
keychain profile DISPLAYEXT_NOTARY_PROFILE, "DisplayExt" by default, made with
  xcrun notarytool store-credentials DisplayExt --apple-id <id> --team-id <team>

Paths given with -o, --notes and DISPLAYEXT_UPDATE_KEY are from where this
is run.
EOF
}

VERSION=""
ARCHES=()
NOTARIZE=true
NOTES=""
OUT_DIR="$ROOT_DIR/build/release"
while (($#)); do
	case "$1" in
	--arch)
		[[ $# -ge 2 ]] || die "--arch needs arm64, amd64 or universal"
		ARCHES+=("$2")
		shift
		;;
	--skip-notarize) NOTARIZE=false ;;
	--notes)
		[[ $# -ge 2 ]] || die "--notes needs a changelog file"
		NOTES="$(abs_path "$2")"
		[[ -f "$NOTES" ]] || die "no changelog at $NOTES"
		shift
		;;
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
((${#ARCHES[@]} > 0)) || ARCHES=(arm64 amd64 universal)

PLATFORMS=()
for arch in "${ARCHES[@]}"; do
	if [[ "$arch" == x64 ]]; then arch=amd64; fi
	PLATFORMS+=("darwin/$(goarch "$arch")")
done

IDENTITY="$(signing_identity)"
BUILD_ARGS=(-sign "$IDENTITY")
CONFIG=("version=$VERSION")
UPDATE_KEY="$(update_key)"
[[ -z "$UPDATE_KEY" ]] || CONFIG+=("updates.privateKey=$UPDATE_KEY")
[[ -z "$NOTES" ]] || CONFIG+=("updates.changelog=$NOTES")
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
	if [[ "$arch" == x64 ]]; then arch=amd64; fi
	case "$arch" in
	universal) dir=darwin-universal dmg="$SLUG-$VERSION.dmg" ;;
	*) dir="darwin-$(goarch "$arch")" dmg="$SLUG-$VERSION-$(goarch "$arch").dmg" ;;
	esac
	for name in "$SLUG-$VERSION-$dir.tar.gz" "update-$dir.json"; do
		[[ -f "$OUT_DIR/$dir/$name" ]] || die "expected $OUT_DIR/$dir/$name after the build"
		mv -f "$OUT_DIR/$dir/$name" "$OUT_DIR/$name"
	done
	for delta in "$OUT_DIR/$dir"/*-to-"$VERSION-$dir.delta"; do
		if [[ -f "$delta" ]]; then mv -f "$delta" "$OUT_DIR/"; fi
	done
	[[ -f "$OUT_DIR/$dir/$dmg" ]] || die "expected $OUT_DIR/$dir/$dmg after the build"
	codesign --verify --strict "$OUT_DIR/$dir/DisplayExt.app" || die "$OUT_DIR/$dir/DisplayExt.app is not validly signed"
	if $NOTARIZE; then
		xcrun stapler validate "$OUT_DIR/$dir/$dmg" >/dev/null || die "$OUT_DIR/$dir/$dmg is not notarized"
	fi
	mv -f "$OUT_DIR/$dir/$dmg" "$OUT_DIR/DisplayExt-$VERSION-$arch.dmg"
	info "$OUT_DIR/DisplayExt-$VERSION-$arch.dmg"
done
