die() {
	printf '%s: %s\n' "$(basename "$0")" "$*" >&2
	exit 1
}
info() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CALLER_DIR="${DISPLAYEXT_CALLER_DIR:-$PWD}"
export DISPLAYEXT_CALLER_DIR="$CALLER_DIR"

abs_path() {
	case "$1" in
	/*) printf '%s\n' "$1" ;;
	*) printf '%s\n' "$CALLER_DIR/$1" ;;
	esac
}

check_macos() {
	[[ "$(uname -s)" == "Darwin" ]] || die "DisplayExt is built on macOS"
}

check_tools() {
	local tool
	for tool in "$@"; do
		command -v "$tool" >/dev/null 2>&1 || die "needs $tool"
	done
}

check_version() {
	[[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "the version must look like 1.2.3, not $1"
}

host_arch() {
	case "$(uname -m)" in
	arm64) echo arm64 ;;
	x86_64) echo x64 ;;
	*) die "unsupported architecture: $(uname -m)" ;;
	esac
}

goarch() {
	case "$1" in
	x64 | amd64) echo amd64 ;;
	arm64) echo arm64 ;;
	universal) echo universal ;;
	*) die "unknown architecture $1: arm64, x64 or universal" ;;
	esac
}

CONFIG_LOCK="$ROOT_DIR/build/mygo.json.lock"
CONFIG_KEPT="$ROOT_DIR/build/mygo.json.committed"
CONFIG_RECOVER="$ROOT_DIR/build/mygo.json.recover"
CONFIG_HELD=false

read_config() {
	python3 -I -c '
import json, sys
value = json.load(open(sys.argv[1]))
for key in sys.argv[2].split("."):
    value = value[key]
print(value)' "$1" "$2"
}

config_value() {
	if ! $CONFIG_HELD && [[ -f "$CONFIG_KEPT" ]] && read_config "$CONFIG_KEPT" "$1" 2>/dev/null; then
		return
	fi
	read_config "$ROOT_DIR/mygo.json" "$1"
}

hold_config() {
	local holder said=false
	mkdir -p "$ROOT_DIR/build"
	until (
		set -o noclobber
		printf '%s\n' "$$" >"$CONFIG_LOCK"
	) 2>/dev/null; do
		holder="$(cat "$CONFIG_LOCK" 2>/dev/null || true)"
		if [[ -n "$holder" ]] && ! kill -0 "$holder" 2>/dev/null && recover_config "$holder"; then
			continue
		fi
		if ! $said; then
			info "waiting for another build of this checkout (process ${holder:-?}), which has mygo.json changed; $CONFIG_LOCK names it"
			said=true
		fi
		sleep 1
	done
	CONFIG_HELD=true
	trap release_config EXIT
	cp -p "$ROOT_DIR/mygo.json" "$CONFIG_KEPT.$$"
	mv -f "$CONFIG_KEPT.$$" "$CONFIG_KEPT"
}

release_config() {
	if [[ -f "$CONFIG_KEPT" ]]; then mv -f "$CONFIG_KEPT" "$ROOT_DIR/mygo.json"; fi
	rm -f "$CONFIG_LOCK"
	CONFIG_HELD=false
}

recover_config() {
	(
		set -o noclobber
		: >"$CONFIG_RECOVER"
	) 2>/dev/null || return 1
	if [[ "$(cat "$CONFIG_LOCK" 2>/dev/null || true)" == "$1" ]]; then
		if [[ -f "$CONFIG_KEPT" ]]; then
			warn "a build that ended early left mygo.json changed; putting the committed one back"
			mv -f "$CONFIG_KEPT" "$ROOT_DIR/mygo.json"
		fi
		rm -f "$CONFIG_LOCK"
	fi
	rm -f "$CONFIG_RECOVER"
}

patch_config() {
	$CONFIG_HELD || hold_config
	python3 -I - "$ROOT_DIR/mygo.json" "$@" <<'EOF'
import json, sys
path, pairs = sys.argv[1], sys.argv[2:]
with open(path) as f:
    config = json.load(f)
for pair in pairs:
    remove = pair.startswith("-")
    keys, _, value = pair.removeprefix("-").partition("=")
    keys = keys.split(".")
    node = config
    for key in keys[:-1]:
        node = node.setdefault(key, {})
    if remove:
        node.pop(keys[-1], None)
    else:
        node[keys[-1]] = value
with open(path, "w") as f:
    json.dump(config, f, indent=2, ensure_ascii=False)
    f.write("\n")
EOF
}

signing_identity() {
	local identities found id="${DEVELOPER_ID_APPLICATION:-}"
	identities="$(security find-identity -v -p codesigning)"
	if [[ -z "$id" ]]; then
		found="$(printf '%s\n' "$identities" | sed -n 's/.*"\(Developer ID Application: [^"]*\)".*/\1/p' | sort -u)"
		[[ -n "$found" ]] || die "no Developer ID Application identity in the keychain; set DEVELOPER_ID_APPLICATION"
		[[ "$(printf '%s\n' "$found" | wc -l)" -eq 1 ]] ||
			die "several Developer ID identities; set DEVELOPER_ID_APPLICATION to one of: $(printf '%s\n' "$found" | paste -sd ';' - | sed 's/;/; /g')"
		id="$found"
	fi
	printf '%s\n' "$identities" | grep -qF "\"$id\"" || die "no signing identity \"$id\" in the keychain"
	printf '%s\n' "$id"
}

notary_profile() {
	local profile="${DISPLAYEXT_NOTARY_PROFILE:-DisplayExt}"
	xcrun notarytool history --keychain-profile "$profile" >/dev/null 2>&1 ||
		die "the notarytool keychain profile \"$profile\" doesn't work; make it with xcrun notarytool store-credentials $profile, or set DISPLAYEXT_NOTARY_PROFILE to one you have"
	printf '%s\n' "$profile"
}

update_key() {
	[[ -z "${MYGO_UPDATER_PRIVATE_KEY:-}" ]] || return 0
	local file
	file="$(abs_path "${DISPLAYEXT_UPDATE_KEY:-$HOME/Library/Application Support/mygo/update-keys/DisplayExt/mygo-update.key}")"
	[[ -f "$file" ]] || die "no update signing key at $file; set DISPLAYEXT_UPDATE_KEY or MYGO_UPDATER_PRIVATE_KEY"
	printf '%s\n' "$file"
}

version_after() {
	local a b i
	IFS=. read -r -a a <<<"$1"
	IFS=. read -r -a b <<<"$2"
	for i in 0 1 2; do
		if ((10#${a[i]} != 10#${b[i]})); then
			((10#${a[i]} > 10#${b[i]}))
			return
		fi
	done
	return 1
}

set_version() {
	python3 -I - "$ROOT_DIR/mygo.json" "$1" <<'PY'
import json, sys
path, version = sys.argv[1:]
with open(path) as f:
    config = json.load(f)
config["version"] = version
with open(path, "w") as f:
    json.dump(config, f, indent=2, ensure_ascii=False)
    f.write("\n")
PY
}
