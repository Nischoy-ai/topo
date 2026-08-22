#!/bin/sh
set -eu

if [ "$#" -ne 3 ]; then
	echo "usage: scripts/build-release.sh <version> <commit> <new-output-directory>" >&2
	exit 2
fi

release_version=$1
release_commit=$2
release_output=$3

if [ -e "$release_output" ]; then
	echo "release output already exists: $release_output" >&2
	exit 1
fi

release_work=$(mktemp -d "${TMPDIR:-/tmp}/topo-release-reproducibility.XXXXXX")
trap 'rm -rf "$release_work"' EXIT HUP INT TERM
mkdir "$release_work/source-a" "$release_work/source-b"

# Build the exact committed tree twice from different absolute source paths.
# The resulting archives, metadata, and checksum manifest must be byte-for-byte
# identical before either set can become release evidence.
git archive --format=tar HEAD | tar -xf - -C "$release_work/source-a"
git archive --format=tar HEAD | tar -xf - -C "$release_work/source-b"

(
	cd "$release_work/source-a"
	go run ./internal/releasetool \
		-version "$release_version" \
		-commit "$release_commit" \
		-out "$release_work/output-a"
)
(
	cd "$release_work/source-b"
	go run ./internal/releasetool \
		-version "$release_version" \
		-commit "$release_commit" \
		-out "$release_work/output-b"
)

diff -r "$release_work/output-a" "$release_work/output-b"
cp -R "$release_work/output-a" "$release_output"
echo "release artifacts reproduced byte-for-byte in $release_output"
