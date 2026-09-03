#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
    echo "usage: $0 <preconfigured-now-sdk-oauth-alias>" >&2
    exit 2
fi

auth_alias=$1
case "$auth_alias" in
    ""|*[!A-Za-z0-9._-]*)
        echo "ServiceNow SDK authentication alias must use only letters, digits, dots, underscores, or hyphens" >&2
        exit 1
        ;;
esac
if [ "${#auth_alias}" -gt 128 ]; then
    echo "ServiceNow SDK authentication alias must be at most 128 characters" >&2
    exit 1
fi

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
app_dir=$root/integrations/servicenow/topo-control-plane
(
    cd "$app_dir"
    npm ci --ignore-scripts --no-audit --no-fund
    npm test
    npm run build
    npx now-sdk install --auth "$auth_alias" --demoData=false
)
