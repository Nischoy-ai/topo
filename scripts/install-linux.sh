#!/bin/sh
# Install the beta through the signed native repository. No service is started.
# The function wrapper prevents a truncated piped download from running setup.
main() (
  set -eu
  if [ "$#" -ne 0 ]; then
    echo 'Usage: install-linux.sh (Linux amd64/arm64, APT or DNF)' >&2
    exit 2
  fi
  [ "$(uname -s)" = Linux ] || { echo 'Linux is required.' >&2; exit 2; }
  case $(uname -m) in x86_64|aarch64|arm64) ;; *) echo 'Unsupported architecture.' >&2; exit 2 ;; esac
  if command -v apt-get >/dev/null 2>&1; then
    channel=apt
  elif command -v dnf >/dev/null 2>&1 && command -v rpm >/dev/null 2>&1; then
    channel=rpm
  else
    echo 'An APT or DNF system is required.' >&2; exit 2
  fi
  for tool in curl gpg awk mktemp install; do
    command -v "$tool" >/dev/null 2>&1 || { echo "Install prerequisite: $tool" >&2; exit 2; }
  done
  elevate() { if [ "$(id -u)" = 0 ]; then "$@"; else sudo "$@"; fi; }
  if [ "$(id -u)" != 0 ]; then
    command -v sudo >/dev/null 2>&1 || { echo 'Run as root or install sudo.' >&2; exit 2; }
    sudo -v
  fi
  umask 077
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  trap 'exit 1' HUP INT TERM
  origin=https://nischoy-ai.github.io/topo-packages
  curl --proto '=https' --tlsv1.2 --fail --silent --show-error --connect-timeout 15 --max-time 60 \
    "$origin/keys/nischoy-topo-archive.asc" -o "$work/key.asc"
  gpg --homedir "$work" --batch --show-keys --with-colons "$work/key.asc" >"$work/key-info"
  fingerprint=$(awk -F: '$1=="pub" {primary=1; next} primary && $1=="fpr" {print $10; primary=0}' "$work/key-info")
  [ "$fingerprint" = 6049C01BB18CE8EC395DA16F9C64F25B652F0673 ] || {
    echo 'Package signing key mismatch; nothing installed.' >&2; exit 1
  }
  if [ "$channel" = apt ]; then
    gpg --homedir "$work" --batch --dearmor --output "$work/key.gpg" "$work/key.asc"
    cat >"$work/topo.sources" <<'SOURCES'
Types: deb
URIs: https://nischoy-ai.github.io/topo-packages/apt
Suites: beta
Components: main
Architectures: amd64 arm64
Signed-By: /etc/apt/keyrings/nischoy-topo.gpg
SOURCES
    elevate install -D -m 0644 "$work/key.gpg" /etc/apt/keyrings/nischoy-topo.gpg
    elevate install -m 0644 "$work/topo.sources" /etc/apt/sources.list.d/nischoy-topo.sources
    elevate apt-get update
    elevate apt-get -o 'Dpkg::Options::=--path-include=/usr/share/doc/topo' \
      -o 'Dpkg::Options::=--path-include=/usr/share/doc/topo/*' install -y topo
  else
    cat >"$work/topo.repo" <<'REPO'
[nischoy-topo-beta]
name=Nischoy Topo (beta)
baseurl=https://nischoy-ai.github.io/topo-packages/rpm/beta/$basearch
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=file:///etc/pki/rpm-gpg/nischoy-topo.asc
REPO
    elevate install -D -m 0644 "$work/key.asc" /etc/pki/rpm-gpg/nischoy-topo.asc
    elevate rpm --import "$work/key.asc"
    elevate install -m 0644 "$work/topo.repo" /etc/yum.repos.d/nischoy-topo.repo
    elevate dnf install -y topo
  fi
  topo version
  echo 'Topo installed. Configure the worker before starting discovery.'
)
main "$@"
