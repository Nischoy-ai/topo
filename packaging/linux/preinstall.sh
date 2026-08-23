#!/bin/sh
set -eu

if ! getent group topo-agent >/dev/null 2>&1; then
    groupadd --system topo-agent
fi

if ! getent passwd topo-agent >/dev/null 2>&1; then
    useradd --system --gid topo-agent --home-dir /var/lib/topo-agent \
        --shell /usr/sbin/nologin --comment "Topo Agent" topo-agent
fi
