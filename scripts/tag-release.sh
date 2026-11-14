#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/build.sh"
cd "$ROOT_DIR"

usage() {
	cat <<'USAGE'
Tags a release: sets the version in mygo.json, commits it as
"Release v<version>", and creates the annotated tag v<version>. Pushing the
tag makes GitHub Actions check, build, sign, notarize and publish the
release (.github/workflows/release.yml).

  scripts/tag-release.sh 0.2.0          # commit and tag here
  scripts/tag-release.sh 0.2.0 --push   # and push the commit and the tag

When mygo.json already has the version, only the tag is made. The working
tree must be clean, and the version must come after the one in mygo.json.

The release workflow needs these repository secrets:
  MACOS_CERTIFICATE            the Developer ID Application certificate with its key, a .p12 in base64
  MACOS_CERTIFICATE_PASSWORD   the password of that .p12
  APPLE_ID                     the Apple Account that notarizes
  APPLE_TEAM_ID                its team ID
  APPLE_APP_PASSWORD           an app-specific password of that account
USAGE
}

VERSION=""
PUSH=false
while (($#)); do
	case "$1" in
	--push) PUSH=true ;;
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

[[ -n "$VERSION" ]] || die "which version? scripts/tag-release.sh 0.2.0"
check_version "$VERSION"
check_tools git python3
git rev-parse --git-dir >/dev/null 2>&1 || die "not a git repository"
[[ -z "$(git status --porcelain)" ]] || die "the working tree has changes; commit or stash them first"
! git rev-parse -q --verify "refs/tags/v$VERSION" >/dev/null || die "the tag v$VERSION exists already"
BRANCH="$(git branch --show-current)"
[[ "$BRANCH" == main ]] || warn "tagging on $BRANCH, not main"

CURRENT="$(config_value version)"
if [[ "$CURRENT" != "$VERSION" ]]; then
	version_after "$VERSION" "$CURRENT" || die "$VERSION is not after the version in mygo.json, $CURRENT"
	set_version "$VERSION"
	git add mygo.json
	git commit -q -m "Release v$VERSION"
	info "committed Release v$VERSION on $BRANCH"
fi
git tag -a "v$VERSION" -m "DisplayExt $VERSION"
info "tagged v$VERSION"

if $PUSH; then
	git push --atomic origin HEAD "refs/tags/v$VERSION"
	info "pushed; GitHub Actions builds and publishes the release"
else
	info "push with: git push --atomic origin HEAD v$VERSION"
fi
