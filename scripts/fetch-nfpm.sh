#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
    echo "usage: $0 <new-nfpm-path>" >&2
    exit 2
fi

output=$1
if [ -e "$output" ]; then
    echo "nFPM output already exists: $output" >&2
    exit 1
fi

version=2.47.0
system=$(uname -s)
machine=$(uname -m)
case "$system/$machine" in
    Darwin/arm64)
        asset="nfpm_${version}_Darwin_arm64.tar.gz"
        expected=e8c9d1d9ac218eeed479375143dc46b8d51a2b8dbba8e2f9f15ecc8faa2e404b
        ;;
    Darwin/x86_64)
        asset="nfpm_${version}_Darwin_x86_64.tar.gz"
        expected=2b04108f8757313dde92ed729560845aadfb7782887eb6988a5dd96f9c146861
        ;;
    Linux/aarch64|Linux/arm64)
        asset="nfpm_${version}_Linux_arm64.tar.gz"
        expected=1c0f5f2999b9a974bfb04fdb0cc3306096de530ac5dbb25d739cc5f5219c919c
        ;;
    Linux/x86_64)
        asset="nfpm_${version}_Linux_x86_64.tar.gz"
        expected=0660ca602b2d2d2ae4781a06c692b3eeb9d437ffea05b831d76e41f4a3188783
        ;;
    *)
        echo "unsupported nFPM bootstrap platform: $system/$machine" >&2
        exit 1
        ;;
esac

work=$(mktemp -d "${TMPDIR:-/tmp}/topo-nfpm.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
archive="$work/$asset"
url="https://github.com/goreleaser/nfpm/releases/download/v${version}/${asset}"
curl --fail --location --proto '=https' --tlsv1.2 --silent --show-error \
    "$url" --output "$archive"
actual=$(shasum -a 256 "$archive" | awk '{print $1}')
if [ "$actual" != "$expected" ]; then
    echo "nFPM checksum mismatch: expected $expected, got $actual" >&2
    exit 1
fi
tar -xzf "$archive" -C "$work" nfpm
cp "$work/nfpm" "$output"
chmod 0755 "$output"
