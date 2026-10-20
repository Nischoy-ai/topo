#!/bin/sh
# Add the reviewed ServiceNow app package to a worker release directory, so one
# GitHub Release carries both parts of Topo.
#
# release/servicenow-package names the published ServiceNow package release to
# include. Its ZIP and XML are downloaded from that release and must match the
# reviewed checksums committed in release/<tag>.SHA256SUMS.
set -eu

if [ "$#" -ne 1 ]; then
    echo "usage: $0 <release-dist-dir>" >&2
    exit 2
fi
dist=$1
if [ ! -d "$dist" ]; then
    echo "release directory does not exist: $dist" >&2
    exit 1
fi

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
repo=${GITHUB_REPOSITORY:-Nischoy-ai/topo}
tag=$(tr -d '[:space:]' < "$root/release/servicenow-package")
case "$tag" in
    servicenow-[0-9]*) ;;
    *)
        echo "invalid ServiceNow package tag in release/servicenow-package: $tag" >&2
        exit 1
        ;;
esac
reviewed=$root/release/$tag.SHA256SUMS
if [ ! -f "$reviewed" ]; then
    echo "missing reviewed checksums: release/$tag.SHA256SUMS" >&2
    exit 1
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/topo-servicenow-package.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

# The ZIP holds the XML and installation checklists; the bare XML is for
# customers who only want the file to import.
grep -E '^[0-9a-f]{64}  nischoy-topo-[0-9A-Za-z.-]+\.(zip|xml)$' "$reviewed" > "$work/SHA256SUMS" || true
if [ "$(wc -l < "$work/SHA256SUMS")" -ne 2 ]; then
    echo "expected one ZIP and one XML in release/$tag.SHA256SUMS" >&2
    exit 1
fi

gh api "repos/$repo/releases/tags/$tag" --jq '.assets[] | "\(.id) \(.name)"' > "$work/assets"
while read -r _ name; do
    if [ -e "$dist/$name" ]; then
        echo "$name already exists in $dist" >&2
        exit 1
    fi
    id=$(awk -v n="$name" '$2 == n { print $1 }' "$work/assets")
    if [ -z "$id" ]; then
        echo "$name is not published on release $tag" >&2
        exit 1
    fi
    gh api -H "Accept: application/octet-stream" "repos/$repo/releases/assets/$id" > "$work/$name"
done < "$work/SHA256SUMS"

(cd "$work" && sha256sum -c SHA256SUMS)
while read -r _ name; do
    cp "$work/$name" "$dist/$name"
done < "$work/SHA256SUMS"
echo "Added ServiceNow package $tag to $dist"
