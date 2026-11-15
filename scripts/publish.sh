#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/build.sh"
cd "$ROOT_DIR"

usage() {
	cat <<'EOF'
Publishes a DisplayExt release to GitHub. It builds the disk images here,
signed with a Developer ID and notarized, puts them with SHA256SUMS on a draft
release, then tags the commit, pushes the tag and publishes the release.

  scripts/publish.sh                   # the version in mygo.json
  scripts/publish.sh 0.2.0             # another version
  scripts/publish.sh 0.2.0 --dry-run   # check, show the notes and the steps
  scripts/publish.sh --allow-branch    # release from a branch, not main

Signing and notarization are as in scripts/build-release.sh: the identity is
DEVELOPER_ID_APPLICATION, or the keychain's one Developer ID Application
identity, and the notarytool keychain profile is DISPLAYEXT_NOTARY_PROFILE,
"DisplayExt" by default. Uploads need the GitHub CLI, signed in.

The commit must be on origin, the working tree clean, and the version after
every v* tag's, here and on origin. The committed mygo.json is never edited:
the build gets the version for the run only.

The notes are the "## <version>" section of CHANGELOG.md, used verbatim when
it has one. Otherwise they are drafted from the subjects of the commits since
the last v* tag and shown for review: [y]es publishes them, [e]dit opens them
in $EDITOR and shows them again, anything else aborts.

A run that stops once the draft has its disk images, at the tag, its push or
the publishing, is finished by running it again from the same commit: it
builds nothing and takes the steps left, with the draft's notes. A draft of
another commit, or one short of a disk image, is refused; delete it with
gh release delete v<version> and run again.

A dry run makes every check and reports each one that fails, then shows the
notes and the commands it would run. It builds, tags, pushes and uploads
nothing, and leaves build/release alone.
EOF
}

VERSION=""
DRY_RUN=false
ALLOW_BRANCH=false
while (($#)); do
	case "$1" in
	--dry-run | -n) DRY_RUN=true ;;
	--allow-branch) ALLOW_BRANCH=true ;;
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
check_tools go git python3 xcrun security codesign shasum gh
[[ -n "$VERSION" ]] || VERSION="$(config_value version)"
check_version "$VERSION"

TAG="v$VERSION"
STAGE_DIR="$ROOT_DIR/build/release"
OUT_DIR="$STAGE_DIR/out"
if $DRY_RUN; then
	WORK_DIR="$(mktemp -d)"
	trap 'rm -rf "$WORK_DIR"' EXIT
else
	WORK_DIR="$STAGE_DIR"
fi
NOTES_FILE="$WORK_DIR/notes.md"
BODY_FILE="$WORK_DIR/body.md"
IMAGES=("DisplayExt-$VERSION-universal.dmg" "DisplayExt-$VERSION-arm64.dmg" "DisplayExt-$VERSION-amd64.dmg")
ASSETS=("${IMAGES[@]}" SHA256SUMS)

PROBLEMS=0
problem() {
	$DRY_RUN || die "$*"
	printf 'problem: %s\n' "$*" >&2
	PROBLEMS=$((PROBLEMS + 1))
}

check() {
	if ! $DRY_RUN; then
		"$@" >/dev/null
		return
	fi
	local err
	err="$({ "$@" >/dev/null; } 2>&1)" || problem "${err#"$(basename "$0"): "}"
}

run() {
	if $DRY_RUN; then
		local arg
		printf '  would run:'
		for arg in "$@"; do printf ' %q' "${arg#"$ROOT_DIR/"}"; done
		printf '\n'
	else
		"$@"
	fi
}

github_repo() {
	local url
	url="$(git remote get-url origin 2>/dev/null)" || die "there is no git remote origin to push $TAG to"
	url="${url%/}"
	url="${url%.git}"
	[[ "$url" =~ github\.com[:/]([^/]+/[^/]+)$ ]] || die "origin, $url, is not a GitHub repository"
	printf '%s\n' "${BASH_REMATCH[1]}"
}

gh_ready() {
	gh auth status >/dev/null 2>&1 || [[ -n "${GH_TOKEN:-}${GITHUB_TOKEN:-}" ]]
}

find_release() {
	local err why status=0
	err="$(mktemp)"
	RELEASE="$(gh release view "$TAG" --repo "$REPO" "$@" 2>"$err")" || status=$?
	why="$(cat "$err")"
	rm -f "$err"
	((status != 0)) || return 0
	if grep -qi 'release not found' <<<"$why"; then return 1; fi
	die "could not ask GitHub for the release $TAG of $REPO: ${why:-gh release view failed}"
}

changelog_notes() {
	[[ -f "$1" ]] || return 0
	python3 -I - "$1" "$2" <<'PY'
import sys
path, version = sys.argv[1:]
notes, inside = [], False
with open(path, encoding="utf-8") as f:
    lines = f.read().replace("\r\n", "\n").split("\n")
for line in lines:
    if line.startswith("## "):
        if inside:
            break
        fields = line[3:].split()
        heading = fields[0] if fields else ""
        if heading.startswith("["):
            heading = heading[1:].split("]", 1)[0]
        inside = heading.removeprefix("v") == version
        continue
    if inside:
        notes.append(line)
print("\n".join(notes).strip())
PY
}

has_notes() { grep -qE '[^[:space:]]' "$1"; }

show_notes() {
	printf '\n--- release notes for %s ---\n' "$VERSION"
	if has_notes "$1"; then cat "$1"; else printf '(none)\n'; fi
	printf -- '---\n'
}

write_body() {
	local macos
	macos="$(config_value macos.minimumSystemVersion)"
	macos="${macos%.0}"
	cat <<EOF
Signed and notarized builds of DisplayExt $VERSION for macOS $macos or later:

- \`${IMAGES[0]}\` runs on every Mac
- \`${IMAGES[1]}\` for Apple silicon
- \`${IMAGES[2]}\` for Intel

**Install**

\`\`\`sh
curl -LO https://github.com/$REPO/releases/download/$TAG/SHA256SUMS
curl -LO https://github.com/$REPO/releases/download/$TAG/${IMAGES[0]}
shasum -a 256 --check --ignore-missing SHA256SUMS
open ${IMAGES[0]}
\`\`\`

Then drag DisplayExt to Applications.

**Changes**

EOF
	cat "$NOTES_FILE"
}

BRANCH="$(git branch --show-current)"
COMMIT="$(git rev-parse HEAD)"
SHORT="$(git rev-parse --short HEAD)"
REPO="$(github_repo)"
info "DisplayExt $VERSION from ${BRANCH:-a detached HEAD} at $SHORT, as $TAG of $REPO"

check signing_identity
check notary_profile
GH_READY=false
if gh_ready; then GH_READY=true; else problem "uploads need gh auth login, or GH_TOKEN"; fi

if [[ "$BRANCH" != main ]] && ! $ALLOW_BRANCH; then
	problem "releases are cut from main, and this is ${BRANCH:-a detached HEAD} (--allow-branch to override)"
fi
[[ -z "$(git status --porcelain)" ]] || problem "the working tree has changes; commit or stash them first"
if $GH_READY && ! gh api "repos/$REPO/commits/$COMMIT" --silent >/dev/null 2>&1; then
	problem "$REPO does not have the commit $SHORT, which the release would tag; push it first"
fi

LOCAL_TAG="$(git rev-parse -q --verify "refs/tags/$TAG^{commit}" || true)"
REMOTE_REFS="$(git ls-remote --tags origin)" || die "could not list the tags of origin"
REMOTE_TAG="$(awk -v tag="refs/tags/$TAG" '
	$2 == tag { plain = $1 }
	$2 == tag "^{}" { peeled = $1 }
	END { print (peeled != "" ? peeled : plain) }' <<<"$REMOTE_REFS")"

RESUME=false
if $GH_READY && find_release --json isDraft,targetCommitish,assets --jq '.isDraft, .targetCommitish, .assets[].name'; then
	{
		read -r draft
		read -r target
		names="$(cat)"
	} <<<"$RELEASE"
	if [[ "$draft" != true ]]; then
		problem "the release $TAG of $REPO is already published; a new build needs a new version"
	elif [[ "$target" != "$COMMIT" ]]; then
		problem "the draft release $TAG of $REPO holds a build of ${target:0:7}, not of $SHORT; delete it with gh release delete $TAG --repo $REPO and run again"
	else
		missing=""
		for name in "${ASSETS[@]}"; do
			grep -qxF "$name" <<<"$names" || missing+=" $name"
		done
		if [[ -n "$missing" ]]; then
			problem "the draft release $TAG of $REPO has no$missing, so its build is not finished; delete it with gh release delete $TAG --repo $REPO and run again"
		else
			RESUME=true
		fi
	fi
fi

if [[ -n "$LOCAL_TAG" && "$LOCAL_TAG" != "$COMMIT" ]]; then
	problem "the tag $TAG already exists, at ${LOCAL_TAG:0:7}, not at $SHORT; delete it with git tag -d $TAG and run again"
elif [[ -n "$REMOTE_TAG" && "$REMOTE_TAG" != "$COMMIT" ]]; then
	problem "the tag $TAG already exists on origin, at ${REMOTE_TAG:0:7}, not at $SHORT; delete it with git push origin :refs/tags/$TAG and run again"
elif [[ -n "$LOCAL_TAG$REMOTE_TAG" ]] && ! $RESUME; then
	problem "the tag $TAG already exists, and $REPO has no draft release $TAG with a finished build to publish; delete the tag and run again"
fi

LATEST=""
while read -r tag; do
	[[ "$tag" != "$TAG" && "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || continue
	if [[ -z "$LATEST" ]] || version_after "${tag#v}" "${LATEST#v}"; then LATEST="$tag"; fi
done < <(
	git tag --list 'v*'
	awk '{ sub("^refs/tags/", "", $2); sub("\\^\\{\\}$", "", $2); print $2 }' <<<"$REMOTE_REFS"
)
if [[ -n "$LATEST" ]] && ! version_after "$VERSION" "${LATEST#v}"; then
	problem "$VERSION does not come after $LATEST, the latest release, and a release is marked the latest"
fi

$DRY_RUN || $RESUME || rm -rf "$STAGE_DIR"
mkdir -p "$WORK_DIR"

if $RESUME; then
	info "resuming: the draft release $TAG holds a finished build of $SHORT, so nothing is built"
	info "notes:    those of the draft release $TAG"
	gh release view "$TAG" --repo "$REPO" --json body --jq .body >"$BODY_FILE"
	has_notes "$BODY_FILE" || problem "the draft release $TAG has no notes; write them on the draft and run again"
	if $DRY_RUN; then show_notes "$BODY_FILE"; fi
else
	changelog_notes "$ROOT_DIR/CHANGELOG.md" "$VERSION" >"$NOTES_FILE"
	if has_notes "$NOTES_FILE"; then
		info "notes:    the $VERSION section of CHANGELOG.md"
		if $DRY_RUN; then show_notes "$NOTES_FILE"; fi
	else
		PREV_TAG="$(git describe --tags --abbrev=0 --match 'v*' --exclude "$TAG" 2>/dev/null || true)"
		if [[ -n "$PREV_TAG" ]]; then
			LOG_ARGS=("$PREV_TAG..HEAD")
			info "notes:    drafted from $PREV_TAG..HEAD (CHANGELOG.md has no $VERSION section)"
		else
			LOG_ARGS=(-n 30 HEAD)
			info "notes:    drafted from the last 30 commits (no previous tag, and CHANGELOG.md has no $VERSION section)"
		fi
		git log --no-merges --pretty=tformat:'- %s' "${LOG_ARGS[@]}" |
			grep -viE '^- (wip([^[:alnum:]]|$)|fixup!|squash!|amend!|merge (pull request|branch) )' >"$NOTES_FILE" || true

		KEEP_DRAFT="To keep the notes, add them to CHANGELOG.md under \"## $VERSION\" and commit
it, then run again: that section is used verbatim."
		if $DRY_RUN; then
			show_notes "$NOTES_FILE"
			info "a real run asks to publish these notes, [y]es / [e]dit / [N]o"
		elif [[ ! -t 0 ]]; then
			die "drafted release notes need an interactive review.
$KEEP_DRAFT"
		else
			while :; do
				show_notes "$NOTES_FILE"
				printf '\nPublish these notes? [y]es / [e]dit / [N]o: '
				read -r reply
				case "$reply" in
				y | Y) break ;;
				e | E) "${VISUAL:-${EDITOR:-vi}}" "$NOTES_FILE" ;;
				*) die "aborted. The draft is $NOTES_FILE.
$KEEP_DRAFT" ;;
				esac
			done
		fi
	fi
	has_notes "$NOTES_FILE" || problem "the notes for $VERSION are empty; write them in CHANGELOG.md under \"## $VERSION\""
	write_body >"$BODY_FILE"
fi

if ! $RESUME; then
	$DRY_RUN || info "building DisplayExt $VERSION at $SHORT"
	run "$ROOT_DIR/scripts/build-release.sh" "$VERSION" -o "$OUT_DIR"
	if $DRY_RUN; then
		printf '  would write:  %s\n' "${OUT_DIR#"$ROOT_DIR/"}/SHA256SUMS"
	else
		[[ "$(git rev-parse HEAD)" == "$COMMIT" && -z "$(git status --porcelain --untracked-files=no)" ]] ||
			die "the checkout changed while $SHORT was built, so the build is not of the commit $TAG would name; nothing was uploaded"
		(cd "$OUT_DIR" && shasum -a 256 "${IMAGES[@]}" >SHA256SUMS)
	fi
	FILES=()
	for name in "${ASSETS[@]}"; do FILES+=("$OUT_DIR/$name"); done
	run gh release create "$TAG" --repo "$REPO" --draft --target "$COMMIT" \
		--title "DisplayExt $VERSION" --notes-file "$BODY_FILE" "${FILES[@]}"
fi

if [[ -z "$LOCAL_TAG" && -z "$REMOTE_TAG" ]]; then
	run git tag -a "$TAG" -m "DisplayExt $VERSION" "$COMMIT"
fi
if [[ -z "$REMOTE_TAG" ]]; then
	run git push origin "refs/tags/$TAG"
fi
run gh release edit "$TAG" --repo "$REPO" --draft=false --latest --title "DisplayExt $VERSION"

if $DRY_RUN; then
	((PROBLEMS == 0)) || die "dry run: $PROBLEMS problem(s) above would stop this release"
	info "dry run: the checks pass; nothing was built, tagged or published"
else
	info "published https://github.com/$REPO/releases/tag/$TAG"
fi
