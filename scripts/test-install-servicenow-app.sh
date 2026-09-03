#!/bin/sh
set -eu

script=scripts/install-servicenow-app.sh
if "$script" >/dev/null 2>&1; then
    echo "installer accepted a missing authentication alias" >&2
    exit 1
fi
if "$script" 'alias;unexpected' >/dev/null 2>&1; then
    echo "installer accepted an unsafe authentication alias" >&2
    exit 1
fi
if "$script" alias unexpected >/dev/null 2>&1; then
    echo "installer accepted an unexpected argument" >&2
    exit 1
fi
if rg -n -- '-{1,2}(password|client-secret|token)([ =]|$)' "$script"; then
    echo "installer contains a forbidden credential option" >&2
    exit 1
fi
