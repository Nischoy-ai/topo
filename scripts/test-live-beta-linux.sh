#!/bin/bash
# Public-channel acceptance, not a generated/file-mounted repository fixture.
set -euo pipefail
trap 'echo "Live channel assertion failed at line $LINENO" >&2' ERR
if [[ $# -lt 1 || $# -gt 2 || "${GITHUB_ACTIONS:-}" != true || "${RUNNER_ENVIRONMENT:-}" != github-hosted || ! -f /.dockerenv ]]; then
  echo "usage: $0 apt|rpm [manual|helper] (disposable GitHub-hosted Linux container only)" >&2
  exit 2
fi
channel=$1
method=${2:-manual}
case "$method" in manual|helper) ;; *) exit 2 ;; esac
case "$channel" in apt|rpm) ;; *) exit 2 ;; esac
version=v0.4.7-beta.2
origin=https://nischoy-ai.github.io/topo-packages
fingerprint=6049C01BB18CE8EC395DA16F9C64F25B652F0673
if [[ "$channel" == apt ]]; then
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl gnupg jq
else
  dnf install -y ca-certificates curl gnupg2 jq
fi
if [[ "$method" == helper ]]; then
  # Exercise the exact candidate script against public signed repositories.
  test ! -e /usr/bin/topo
  sh /test/install-linux.sh
else
curl -fsS --max-time 60 "$origin/keys/nischoy-topo-archive.asc" -o /tmp/topo-key.asc
actual=$(gpg --batch --show-keys --with-colons /tmp/topo-key.asc | awk -F: '$1=="fpr" {print $10; exit}')
test "$actual" = "$fingerprint"
if [[ "$channel" == apt ]]; then
  install -d -m 0755 /etc/apt/keyrings
  gpg --batch --dearmor --output /etc/apt/keyrings/nischoy-topo.gpg /tmp/topo-key.asc
  curl -fsS --max-time 60 "$origin/apt/nischoy-topo-beta.sources" -o /etc/apt/sources.list.d/nischoy-topo.sources
  grep -Fx 'Signed-By: /etc/apt/keyrings/nischoy-topo.gpg' /etc/apt/sources.list.d/nischoy-topo.sources
  apt-get update
  # Minimal Ubuntu images filter /usr/share/doc; retain Topo's startup example.
  apt-get -o 'Dpkg::Options::=--path-include=/usr/share/doc/topo' \
    -o 'Dpkg::Options::=--path-include=/usr/share/doc/topo/*' install -y topo
else
  rpm --import /tmp/topo-key.asc
  curl -fsS --max-time 60 "$origin/rpm/nischoy-topo-beta.repo" -o /etc/yum.repos.d/nischoy-topo.repo
  grep -Fx 'gpgcheck=1' /etc/yum.repos.d/nischoy-topo.repo
  grep -Fx 'repo_gpgcheck=1' /etc/yum.repos.d/nischoy-topo.repo
  dnf install -y topo
fi
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
  x86_64) arch=amd64; digest=c0beda98fbc63708b6ed521567cf99d81a219f7a1295196f8f21b94df9bdc90f ;;
  aarch64) arch=arm64; digest=d1faa593ba6d8a30c320a7783ad8bb4c120f09d269ee52d7e59a34824da0f891 ;;
  *) exit 1 ;;
esac
curl -fsSL --max-time 120 "https://github.com/Nischoy-ai/topo/releases/download/$version/topo_0.4.7-beta.2_linux_$arch.tar.gz" -o /tmp/topo.tar.gz
printf '%s  /tmp/topo.tar.gz\n' "$digest" | sha256sum --check -
mkdir /tmp/topo-raw
tar -xzf /tmp/topo.tar.gz -C /tmp/topo-raw
raw=$(find /tmp/topo-raw -type f -name topo)
test -n "$raw"
cmp /usr/bin/topo "$raw"
if [[ "$channel" == apt ]]; then apt-get remove -y topo; else dnf remove -y topo; fi
test ! -e /usr/bin/topo
test -f /etc/topo-worker/operator-owned
printf 'Live %s %s (%s): signature-checked install, exact release binary, local discovery, dormant worker, removal and operator-file preservation passed\n' "$channel" "$arch" "$method"
