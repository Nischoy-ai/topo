#!/bin/sh
set -eu

if [ "$#" -ne 3 ]; then
    echo "usage: $0 <version-tag> <raw-artifact-dir> <new-output-dir>" >&2
    exit 2
fi

version=$1
raw_dir=$2
output_dir=$3
case "$version" in
    v[0-9]*.[0-9]*.[0-9]*) ;;
    *)
        echo "version must be a semantic tag such as v1.2.3" >&2
        exit 1
        ;;
esac
if [ ! -d "$raw_dir" ]; then
    echo "raw artifact directory does not exist: $raw_dir" >&2
    exit 1
fi
if [ -e "$output_dir" ]; then
    echo "package output already exists: $output_dir" >&2
    exit 1
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/topo-package-reproducibility.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
nfpm=${NFPM_BIN:-$work/nfpm}
if [ -z "${NFPM_BIN:-}" ]; then
    scripts/fetch-nfpm.sh "$nfpm"
fi

mkdir "$work/raw-a" "$work/raw-b"
cp -R "$raw_dir/." "$work/raw-a/"
cp -R "$raw_dir/." "$work/raw-b/"

go run ./internal/packagetool \
    -mode build -root . -raw "$work/raw-a" -out "$work/output-a" \
    -version "$version" -nfpm "$nfpm"
go run ./internal/packagetool \
    -mode build -root . -raw "$work/raw-b" -out "$work/output-b" \
    -version "$version" -nfpm "$nfpm"
diff -r "$work/output-a" "$work/output-b"
cp -R "$work/output-a" "$output_dir"
echo "package artifacts reproduced byte-for-byte in $output_dir"
