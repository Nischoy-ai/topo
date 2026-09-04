#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
    echo "usage: $0 <artifact-dir> <version-tag>" >&2
    exit 2
fi

artifact_dir=$1
version=$2
filename_version=${version#v}
deb="$artifact_dir/topo_${filename_version}_amd64.deb"
rpm="$artifact_dir/topo-${filename_version}-1.x86_64.rpm"
raw="$artifact_dir/topo_${filename_version}_linux_amd64.tar.gz"
fedora_image="fedora@sha256:99e203b80b1c3d8f7e161ec10a68fd02b081ef83a3963553e513c82846b97814"

(cd "$artifact_dir" && sha256sum --check SHA256SUMS)

work=$(mktemp -d "${TMPDIR:-/tmp}/topo-package-test.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
tar -xzf "$raw" -C "$work"
raw_binary=$(find "$work" -type f -name topo -print)
if [ -z "$raw_binary" ] || [ "$(printf '%s\n' "$raw_binary" | wc -l)" -ne 1 ]; then
    echo "raw archive must contain exactly one topo binary" >&2
    exit 1
fi
raw_digest=$(sha256sum "$raw_binary" | awk '{print $1}')

sudo dpkg --install "$deb"
test "$(/usr/bin/topo version)" = "$version"
test "$(sha256sum /usr/bin/topo | awk '{print $1}')" = "$raw_digest"
test -f /usr/lib/systemd/system/topo-agent.service
test -f /usr/lib/systemd/system/topo-worker.service
test -f /usr/share/doc/topo/topo-worker.env.example
systemd-analyze verify /usr/lib/systemd/system/topo-worker.service
if grep -q '^StateDirectory=' /usr/lib/systemd/system/topo-worker.service; then
    echo "stateless worker unit unexpectedly requests a state directory" >&2
    exit 1
fi
test ! -e /etc/topo-agent/topo-agent.env
test ! -e /etc/topo-worker/topo-worker.env
test ! -e /etc/systemd/system/multi-user.target.wants/topo-agent.service
test ! -e /etc/systemd/system/multi-user.target.wants/topo-worker.service
sudo mkdir -p /etc/topo-agent
printf '%s\n' operator-owned | sudo tee /etc/topo-agent/operator-owned >/dev/null
sudo mkdir -p /etc/topo-worker
printf '%s\n' operator-owned | sudo tee /etc/topo-worker/operator-owned >/dev/null
sudo dpkg --remove topo
test ! -e /usr/bin/topo
test -f /etc/topo-agent/operator-owned
test -f /etc/topo-worker/operator-owned
sudo dpkg --purge topo
test -f /etc/topo-agent/operator-owned
test -f /etc/topo-worker/operator-owned

docker run --rm \
    -v "$artifact_dir:/artifacts:ro" \
    "$fedora_image" \
    sh -eu -c "
        rpm --install /artifacts/$(basename "$rpm")
        test \"\$(/usr/bin/topo version)\" = '$version'
        test \"\$(sha256sum /usr/bin/topo | awk '{print \$1}')\" = '$raw_digest'
        test -f /usr/lib/systemd/system/topo-agent.service
        test -f /usr/lib/systemd/system/topo-worker.service
        test -f /usr/share/doc/topo/topo-worker.env.example
        test ! -e /etc/topo-agent/topo-agent.env
        test ! -e /etc/topo-worker/topo-worker.env
        test ! -e /etc/systemd/system/multi-user.target.wants/topo-agent.service
        test ! -e /etc/systemd/system/multi-user.target.wants/topo-worker.service
        mkdir -p /etc/topo-agent
        printf '%s\n' operator-owned > /etc/topo-agent/operator-owned
        mkdir -p /etc/topo-worker
        printf '%s\n' operator-owned > /etc/topo-worker/operator-owned
        rpm --erase topo
        test ! -e /usr/bin/topo
        test -f /etc/topo-agent/operator-owned
        test -f /etc/topo-worker/operator-owned
    "
