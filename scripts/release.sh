#!/bin/sh
set -eu

version=${1:?Usage: scripts/release.sh v0.1.0}
printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || { printf 'Expected a version like v0.1.0.\n' >&2; exit 1; }
cd "$(dirname "$0")/.."
go_command=${GO:-go}
mkdir -p dist
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
cp LICENSE THIRD_PARTY_NOTICES.md "$stage/"
cp "$("$go_command" env GOROOT)/LICENSE" "$stage/GO_LICENSE"

for platform in darwin_arm64 darwin_amd64 linux_amd64; do
    printf 'Building %s (%s)\n' "$version" "$platform"
    CGO_ENABLED=0 GOOS=${platform%_*} GOARCH=${platform#*_} "$go_command" build \
        -trimpath -buildvcs=false \
        -ldflags "-s -w -X github.com/kitanovicd/blindspot/internal/cli.version=$version" \
        -o "$stage/blindspot" .
    # Normalize ownership so archives never include the build machine's user name.
    case "$(tar --version)" in
        *bsdtar*) COPYFILE_DISABLE=1 tar --no-xattrs --uid 0 --gid 0 --uname root --gname root -czf "dist/blindspot_$platform.tar.gz" -C "$stage" blindspot LICENSE GO_LICENSE THIRD_PARTY_NOTICES.md ;;
        *) tar --owner=0 --group=0 --numeric-owner -czf "dist/blindspot_$platform.tar.gz" -C "$stage" blindspot LICENSE GO_LICENSE THIRD_PARTY_NOTICES.md ;;
    esac
done

(
    cd dist
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum blindspot_darwin_arm64.tar.gz blindspot_darwin_amd64.tar.gz blindspot_linux_amd64.tar.gz
    else
        shasum -a 256 blindspot_darwin_arm64.tar.gz blindspot_darwin_amd64.tar.gz blindspot_linux_amd64.tar.gz
    fi
) > dist/checksums.txt
printf 'Release archives and checksums are in dist/.\n'
