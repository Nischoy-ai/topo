#!/bin/bash
# Public-channel acceptance, not a generated/file-mounted repository fixture.
set -euo pipefail
trap 'echo "Live channel assertion failed at line $LINENO" >&2' ERR
if [[ $# != 1 || "${GITHUB_ACTIONS:-}" != true || "${RUNNER_ENVIRONMENT:-}" != github-hosted || ! -f /.dockerenv ]]; then
  echo "usage: $0 apt|rpm (disposable GitHub-hosted Linux container only)" >&2
  exit 2
fi
channel=$1
case "$channel" in apt|rpm) ;; *) exit 2 ;; esac
version=v0.1.0-beta.1
origin=https://nischoy-ai.github.io/topo-packages
fingerprint=6049C01BB18CE8EC395DA16F9C64F25B652F0673
if [[ "$channel" == apt ]]; then
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl gnupg jq
else
  dnf install -y ca-certificates curl gnupg2 jq
fi
curl -fsS --max-time 60 "$origin/keys/nischoy-topo-archive.asc" -o /tmp/topo-key.asc
actual=$(gpg --batch --show-keys --with-colons /tmp/topo-key.asc | awk -F: '$1=="fpr" {print $10; exit}')
test "$actual" = "$fingerprint"
if [[ "$channel" == apt ]]; then
  install -d -m 0755 /etc/apt/keyrings
  gpg --batch --dearmor --output /etc/apt/keyrings/nischoy-topo.gpg /tmp/topo-key.asc
  curl -fsS --max-time 60 "$origin/apt/nischoy-topo-beta.sources" -o /etc/apt/sources.list.d/nischoy-topo.sources
  grep -Fx 'Signed-By: /etc/apt/keyrings/nischoy-topo.gpg' /etc/apt/sources.list.d/nischoy-topo.sources
  apt-get update
  apt-get install -y topo
else
  rpm --import /tmp/topo-key.asc
  curl -fsS --max-time 60 "$origin/rpm/nischoy-topo-beta.repo" -o /etc/yum.repos.d/nischoy-topo.repo
  grep -Fx 'gpgcheck=1' /etc/yum.repos.d/nischoy-topo.repo
  grep -Fx 'repo_gpgcheck=1' /etc/yum.repos.d/nischoy-topo.repo
  dnf install -y topo
fi
topo version
test "$(topo version)" = "$version"
topo discover local >/tmp/topo-observations.jsonl
jq -e '.assets | length > 0' /tmp/topo-observations.jsonl >/dev/null
echo "Version and local discovery passed"
test -f /usr/lib/systemd/system/topo-worker.service
test -f /usr/share/doc/topo/topo-worker.env.example
test ! -e /etc/topo-worker/topo-worker.env
test ! -e /etc/systemd/system/multi-user.target.wants/topo-worker.service
test ! -e /etc/systemd/system/multi-user.target.wants/topo-agent.service
if grep -q '^StateDirectory=' /usr/lib/systemd/system/topo-worker.service; then exit 1; fi
mkdir -p /etc/topo-worker
printf '%s\n' operator-owned >/etc/topo-worker/operator-owned
echo "Dormant service and operator fixture passed"
# Pins were independently verified against the published tag's signed manifest.
case "$(uname -m)" in
  x86_64) arch=amd64; digest=9157d2c0f49d3b4040dcaad6c7ae1570dc4b7cc62c17331044c4c96f5349f320 ;;
  aarch64) arch=arm64; digest=a806a9f4200d8da972bd44f33c2f577095a4a0c764f646c85e2b88e1177cd63d ;;
  *) exit 1 ;;
esac
curl -fsSL --max-time 120 "https://github.com/Nischoy-ai/topo/releases/download/$version/topo_0.1.0-beta.1_linux_$arch.tar.gz" -o /tmp/topo.tar.gz
printf '%s  /tmp/topo.tar.gz\n' "$digest" | sha256sum --check -
mkdir /tmp/topo-raw
tar -xzf /tmp/topo.tar.gz -C /tmp/topo-raw
raw=$(find /tmp/topo-raw -type f -name topo)
test -n "$raw"
cmp /usr/bin/topo "$raw"
if [[ "$channel" == apt ]]; then apt-get remove -y topo; else dnf remove -y topo; fi
test ! -e /usr/bin/topo
test -f /etc/topo-worker/operator-owned
printf 'Live %s %s: signature-checked install, exact release binary, local discovery, dormant worker, removal and operator-file preservation passed\n' "$channel" "$arch"
