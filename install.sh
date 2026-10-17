#!/bin/sh
set -eu

fail() { printf 'Error: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || fail 'curl is required.'
command -v tar >/dev/null 2>&1 || fail 'tar is required.'
if command -v sha256sum >/dev/null 2>&1; then
    hash_command=sha256sum
elif command -v shasum >/dev/null 2>&1; then
    hash_command=shasum
else
    fail 'sha256sum or shasum is required to verify the download.'
fi

case "$(uname -s)/$(uname -m)" in
    Darwin/arm64) platform=darwin_arm64 ;;
    Darwin/x86_64) platform=darwin_amd64 ;;
    Linux/x86_64) platform=linux_amd64 ;;
    *) fail 'Prebuilt releases support macOS (Apple Silicon/Intel) and Linux x86-64. On Windows, use WSL.' ;;
esac

version=${BLINDSPOT_VERSION:-latest}
base_url=https://github.com/kitanovicd/blindspot/releases
if [ "$version" = latest ]; then
    download_url=$base_url/latest/download
else
    printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || fail 'BLINDSPOT_VERSION must look like v0.1.0.'
    download_url=$base_url/download/$version
fi

install_dir=${BLINDSPOT_INSTALL_DIR:-"$HOME/.local/bin"}
archive=blindspot_$platform.tar.gz
temp_dir=$(mktemp -d)
staged_binary=
cleanup() {
    rm -rf "$temp_dir"
    if [ -n "$staged_binary" ]; then rm -f "$staged_binary"; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

download() {
    curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
        --retry 3 --connect-timeout 15 --max-time 180 "$download_url/$1" -o "$temp_dir/$1" \
        || fail "Could not download $1. Check https://github.com/kitanovicd/blindspot/releases."
}

printf 'Downloading Blindspot (%s, %s)…\n' "$version" "$platform"
download "$archive"
download checksums.txt
expected=$(awk -v file="$archive" '$2 == file { print $1 }' "$temp_dir/checksums.txt")
printf '%s\n' "$expected" | grep -Eq '^[0-9a-f]{64}$' || fail "Missing or invalid checksum for $archive."
if [ "$hash_command" = shasum ]; then
    actual=$(shasum -a 256 "$temp_dir/$archive" | awk '{ print $1 }')
else
    actual=$(sha256sum "$temp_dir/$archive" | awk '{ print $1 }')
fi
[ "$actual" = "$expected" ] || fail 'Checksum mismatch; installation refused.'

tar -xzf "$temp_dir/$archive" -C "$temp_dir" blindspot
[ -f "$temp_dir/blindspot" ] && [ ! -L "$temp_dir/blindspot" ] || fail 'Release does not contain a regular blindspot binary.'
mkdir -p "$install_dir"
install_dir=$(cd "$install_dir" && pwd)
[ ! -d "$install_dir/blindspot" ] || fail "$install_dir/blindspot is a directory."
staged_binary=$(mktemp "$install_dir/.blindspot-XXXXXX")
cat "$temp_dir/blindspot" > "$staged_binary"
chmod 755 "$staged_binary"
installed_version=$("$staged_binary" --version) || fail 'The downloaded binary could not run on this machine.'
mv -f "$staged_binary" "$install_dir/blindspot"
staged_binary=

printf '\nInstalled %s\n  %s/blindspot\n' "$installed_version" "$install_dir"
case ":$PATH:" in
    *":$install_dir:"*) ;;
    *)
        if [ "$install_dir" = "$HOME/.local/bin" ]; then
            printf '\nAdd this line to your shell configuration:\n  export PATH="$HOME/.local/bin:$PATH"\n'
        else
            printf '\nAdd this directory to PATH in your shell configuration:\n  %s\n' "$install_dir"
        fi
        ;;
esac
printf '\nNext, from a Foundry project:\n  blindspot setup\n  blindspot run --limit 5 --open\n'
