#!/bin/sh
set -eu

if [ "$#" -ne 5 ]; then
  echo "usage: $0 <artifact-dir> <version> <stable|beta> <published-at> <output-dir>" >&2
  exit 2
fi

artifact_dir=$1
version=$2
channel=$3
published_at=$4
output_dir=$5

if [ -e "$output_dir" ]; then
  echo "output already exists: $output_dir" >&2
  exit 1
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/topo-distribution.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

build() {
  destination=$1
  GOTOOLCHAIN=go1.23.12 go run ./internal/distributiontool \
    -artifacts "$artifact_dir" \
    -out "$destination" \
    -version "$version" \
    -channel "$channel" \
    -published-at "$published_at" \
    -release-base-url "https://github.com/Nischoy-ai/topo" \
    -repository-base-url "https://nischoy-ai.github.io/topo-packages"
}

build "$work/first"
build "$work/second"
diff -r "$work/first" "$work/second"
mv "$work/first" "$output_dir"
