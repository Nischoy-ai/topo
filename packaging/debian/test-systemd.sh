#!/bin/bash
# Disposable hosted CI only: native Debian image, real systemd, loopback only.
set -euo pipefail
test "${GITHUB_ACTIONS:-}" = true
test "${RUNNER_ENVIRONMENT:-}" = github-hosted
test "${GITHUB_JOB:-}" = debian-systemd
artifacts=$(realpath "$1")
fixture=$(mktemp -d "$RUNNER_TEMP/topo-debian-systemd.XXXXXX")
container="topo-debian-systemd-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
image="topo-debian-systemd:${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
cleanup() {
 docker logs "$container" > "$fixture/boot.log" 2>&1 || true
 docker rm --force "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cat > "$fixture/Dockerfile" <<'DOCKER'
FROM debian:unstable@sha256:c1bc8c85a3b2395872d431404ca01b3d1a7c3e98610eb718d80725c573cc8017
RUN apt-get update && apt-get install -y --no-install-recommends systemd python3 ca-certificates adduser util-linux && rm -rf /var/lib/apt/lists/*
STOPSIGNAL SIGRTMIN+3
CMD ["/usr/lib/systemd/systemd"]
DOCKER
docker build --tag "$image" "$fixture"
docker run --detach --name "$container" --privileged --cgroupns=private --network none \
 --tmpfs /run --tmpfs /run/lock \
 --mount "type=bind,src=$GITHUB_WORKSPACE,dst=/workspace,readonly" \
 --mount "type=bind,src=$artifacts,dst=/artifacts,readonly" \
 --env GITHUB_ACTIONS=true --env RUNNER_ENVIRONMENT=github-hosted --env RUNNER_OS=Linux \
 --env GITHUB_JOB=debian-systemd --env TOPO_SETUP_DISPOSABLE_CI=yes \
 --workdir /workspace "$image"
for attempt in {1..60}; do
 if docker exec "$container" systemctl show --property=Version --value >/dev/null 2>&1; then break; fi
 sleep 1
done
docker exec -i "$container" bash -seuo pipefail <<'DEBIAN' | tee "$fixture/lifecycle.log"
 arch=$(dpkg --print-architecture)
 packages=(/artifacts/topo/topo_*"_${arch}.deb")
 test "${#packages[@]}" = 1
 # Same source/binary, higher package revision: actual dpkg transaction only.
 # This is not historical release/N-1 upgrade qualification.
 mkdir -p /var/tmp/topo-ci-revision
 dpkg-deb --raw-extract "${packages[0]}" /var/tmp/topo-ci-revision/tree
 sed -i '/^Version:/s/$/+ci1/' /var/tmp/topo-ci-revision/tree/DEBIAN/control
 dpkg-deb --build /var/tmp/topo-ci-revision/tree /var/tmp/topo-ci-revision/topo-revision.deb
 install -m 0755 /artifacts/topo-setup-controlsim /usr/local/bin/topo-setup-controlsim
 python3 packaging/debian/test-installed-systemd.py --deb "${packages[0]}" \
  --upgrade-deb /var/tmp/topo-ci-revision/topo-revision.deb --simulator /usr/local/bin/topo-setup-controlsim
DEBIAN
