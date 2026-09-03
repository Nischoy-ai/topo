#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
    echo "usage: $0 <existing-artifact-dir>" >&2
    exit 2
fi

artifact_dir=$1
if [ ! -d "$artifact_dir" ]; then
    echo "artifact directory does not exist: $artifact_dir" >&2
    exit 1
fi
if [ ! -f "$artifact_dir/release-metadata.json" ]; then
    echo "artifact directory omits verified release-metadata.json: $artifact_dir" >&2
    exit 1
fi

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
app_dir=$root/integrations/servicenow/topo-control-plane
artifact_name=nischoy_topo_servicenow_control_plane_0_4_4.zip
metadata_name=servicenow-app-metadata.json
if [ -e "$artifact_dir/$artifact_name" ] || [ -e "$artifact_dir/$metadata_name" ]; then
    echo "ServiceNow release artifact already exists in $artifact_dir" >&2
    exit 1
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/topo-servicenow-package.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

(
    cd "$app_dir"
    npm ci --ignore-scripts --no-audit --no-fund
    npm test
    npm run build
    npm run pack
)
cp "$app_dir/target/$artifact_name" "$work/sdk-a.zip"
(
    cd "$app_dir"
    npm run build
    npm run pack
)
cp "$app_dir/target/$artifact_name" "$work/sdk-b.zip"

(
    cd "$root"
    go run ./internal/servicenowpackagetool -in "$work/sdk-a.zip" -out "$work/app-a.zip" > "$work/metadata-a.json"
    go run ./internal/servicenowpackagetool -in "$work/sdk-b.zip" -out "$work/app-b.zip" > "$work/metadata-b.json"
)
cmp "$work/app-a.zip" "$work/app-b.zip"
cmp "$work/metadata-a.json" "$work/metadata-b.json"

cp "$work/app-a.zip" "$artifact_dir/$artifact_name"
cp "$work/metadata-a.json" "$artifact_dir/$metadata_name"
(
    cd "$root"
    go run ./internal/packagetool -mode finalize -out "$artifact_dir"
)
echo "ServiceNow app package reproduced and validated in $artifact_dir"
