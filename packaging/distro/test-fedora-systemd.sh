#!/usr/bin/env bash
# Disposable hosted CI only: a real systemd container with loopback networking.
set -euo pipefail
test "${GITHUB_ACTIONS:-}" = true
test "${RUNNER_ENVIRONMENT:-}" = github-hosted
test "${GITHUB_JOB:-}" = fedora-systemd
test "${TOPO_SETUP_DISPOSABLE_CI:-}" = yes
test -n "${TOPO_FEDORA_IMAGE:-}"
artifact_dir=$(realpath "$1")
fixture_dir=$(mktemp -d "$RUNNER_TEMP/topo-fedora-systemd.XXXXXX")
container_name="topo-fedora-systemd-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
image_name="topo-fedora-systemd:${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
cleanup() {
  docker logs "$container_name" > "$fixture_dir/systemd-boot.log" 2>&1 || true
  docker rm --force "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cat > "$fixture_dir/Dockerfile" <<'EOF'
ARG FEDORA_IMAGE
FROM ${FEDORA_IMAGE}
RUN dnf -y --setopt=install_weak_deps=False install systemd python3 ca-certificates shadow-utils util-linux gnupg2 rpm-sign && dnf clean all
STOPSIGNAL SIGRTMIN+3
CMD ["/usr/sbin/init"]
EOF
docker build --build-arg "FEDORA_IMAGE=$TOPO_FEDORA_IMAGE" --tag "$image_name" "$fixture_dir"
docker run --detach --name "$container_name" --privileged --cgroupns=private --network none \
  --tmpfs /run --tmpfs /run/lock \
  --mount "type=bind,src=$GITHUB_WORKSPACE,dst=/workspace,readonly" \
  --mount "type=bind,src=$artifact_dir,dst=/artifacts,readonly" \
  --env GITHUB_ACTIONS=true --env RUNNER_ENVIRONMENT=github-hosted --env RUNNER_OS=Linux \
  --env GITHUB_JOB=fedora-systemd --env TOPO_SETUP_DISPOSABLE_CI=yes \
  --workdir /workspace "$image_name"
for attempt in {1..60}; do
  if docker exec "$container_name" systemctl show --property=Version --value >/dev/null 2>&1; then break; fi
  sleep 1
done
docker exec -i "$container_name" bash -seuo pipefail <<'TOPO_FEDORA_FIXTURE' | tee "$fixture_dir/lifecycle.log"
  if test -f /usr/lib/systemd/system/service.d/10-timeout-abort.conf; then
    cat /usr/lib/systemd/system/service.d/10-timeout-abort.conf
    sha256sum /usr/lib/systemd/system/service.d/10-timeout-abort.conf
  fi
  arch=$(rpm --eval "%{_arch}")
  baseline=(/artifacts/mock/topo-0.4.7-0.1.*."$arch".rpm)
  upgrade=(/artifacts/upgrade/topo-0.4.7-0.2.*."$arch".rpm)
  test "${#baseline[@]}" = 1
  test "${#upgrade[@]}" = 1
  # Rawhide enforces signatures. Sign only disposable copies with a one-day CI key.
  mkdir -m 0700 /var/tmp/topo-ci-packages
  cp "${baseline[0]}" /var/tmp/topo-ci-packages/baseline.rpm
  cp "${upgrade[0]}" /var/tmp/topo-ci-packages/upgrade.rpm
  baseline_digest=$(rpm2cpio "${baseline[0]}" | sha256sum | cut -d" " -f1)
  upgrade_digest=$(rpm2cpio "${upgrade[0]}" | sha256sum | cut -d" " -f1)
  export GNUPGHOME=$(mktemp -d /var/tmp/topo-ci-key.XXXXXX)
  gpg --batch --pinentry-mode loopback --passphrase "" --quick-generate-key "Topo disposable Fedora CI <ci@nischoy.invalid>" rsa2048 sign 1d
  fingerprint=$(gpg --batch --with-colons --fingerprint | awk -F: '$1 == "fpr" { print $10; exit }')
  bash scripts/sign-rpms.sh /var/tmp/topo-ci-packages "$fingerprint"
  gpg --batch --armor --export "$fingerprint" > /var/tmp/topo-ci-public.asc
  rpm --import /var/tmp/topo-ci-public.asc
  rm -rf "$GNUPGHOME"
  unset GNUPGHOME
  test "$(rpm2cpio /var/tmp/topo-ci-packages/baseline.rpm | sha256sum | cut -d" " -f1)" = "$baseline_digest"
  test "$(rpm2cpio /var/tmp/topo-ci-packages/upgrade.rpm | sha256sum | cut -d" " -f1)" = "$upgrade_digest"
  rpm --checksig /var/tmp/topo-ci-packages/*.rpm
  echo "One-day CI signatures verified; original review RPMs and binary payloads unchanged"
  install -m 0755 /artifacts/topo-setup-controlsim /usr/local/bin/topo-setup-controlsim
  python3 scripts/test-worker-setup-systemd.py --rpm /var/tmp/topo-ci-packages/baseline.rpm \
    --upgrade-rpm /var/tmp/topo-ci-packages/upgrade.rpm --simulator /usr/local/bin/topo-setup-controlsim
TOPO_FEDORA_FIXTURE
