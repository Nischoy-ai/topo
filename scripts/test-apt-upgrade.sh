#!/bin/bash
# Signed, synthetic package-version transitions on disposable CI only. Payload
# comes from the candidate package; these are not published release artifacts.
set -euo pipefail
if [[ $# != 1 || "${GITHUB_ACTIONS:-}" != true || "${RUNNER_ENVIRONMENT:-}" != github-hosted ]]; then
  echo "usage: $0 <candidate-amd64.deb> (disposable GitHub-hosted runner only)" >&2
  exit 2
fi
test -n "${GNUPGHOME:-}"
test -n "${fingerprint:-}"
work=$(mktemp -d "$RUNNER_TEMP/topo-apt-upgrade.XXXXXX")
trap 'rm -rf "$work"' EXIT
chmod 0755 "$work"
dpkg-deb --raw-extract "$1" "$work/payload"
for stage in beta1 beta2 stable expired; do
  case "$stage" in
    beta1) version=0.4.6~beta.1-1 ;;
    beta2) version=0.4.6~beta.2-1 ;;
    stable|expired) version=0.4.6-1 ;;
  esac
  root="$work/$stage"
  mkdir -p "$root/pool/main" "$root/dists/test/main/binary-amd64"
  sed -i "s/^Version: .*/Version: $version/" "$work/payload/DEBIAN/control"
  dpkg-deb --root-owner-group --build "$work/payload" "$root/pool/main/topo.deb"
  (cd "$root" && dpkg-scanpackages pool/main > dists/test/main/binary-amd64/Packages)
  gzip -n -c "$root/dists/test/main/binary-amd64/Packages" > "$root/dists/test/main/binary-amd64/Packages.gz"
  python3 - "$root" "$stage" <<'PY'
import datetime, hashlib, pathlib, sys
root = pathlib.Path(sys.argv[1]) / 'dists/test'
now = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)
expiry = now + datetime.timedelta(days=-1 if sys.argv[2] == 'expired' else 90)
date = lambda value: value.strftime('%a, %d %b %Y %H:%M:%S UTC')
lines = ['Origin: Nischoy CI', 'Label: Topo upgrade fixture', 'Suite: test',
         'Codename: test', 'Architectures: amd64', 'Components: main',
         'Date: ' + date(now), 'Valid-Until: ' + date(expiry), 'SHA256:']
for path in sorted(root.glob('main/binary-amd64/Packages*')):
    data = path.read_bytes()
    lines.append(f' {hashlib.sha256(data).hexdigest()} {len(data)} {path.relative_to(root)}')
(root / 'Release').write_text('\n'.join(lines) + '\n')
PY
  gpg --batch --yes --local-user "$fingerprint" --digest-algo SHA256 \
    --armor --clearsign --output "$root/dists/test/InRelease" "$root/dists/test/Release"
done
gpg --batch --export "$fingerprint" > "$work/key.gpg"
docker run --rm -v "$work:/fixtures:ro" ubuntu:24.04 bash -euc '
  install -D -m 0644 /fixtures/key.gpg /etc/apt/keyrings/topo-fixture.gpg
  echo "deb [signed-by=/etc/apt/keyrings/topo-fixture.gpg] file:/repo/current test main" > /etc/apt/sources.list.d/topo-fixture.list
  mkdir -p /etc/topo-worker
  printf "%s\n" operator-owned > /etc/topo-worker/operator-owned
  for stage in beta1 beta2 stable; do
    case "$stage" in
      beta1) expected=0.4.6~beta.1-1 ;;
      beta2) expected=0.4.6~beta.2-1 ;;
      stable) expected=0.4.6-1 ;;
    esac
    mkdir -p /repo
    rm -rf /repo/current
    cp -R "/fixtures/$stage" /repo/current
    rm -rf /var/lib/apt/lists/*
    apt-get update
    candidate=$(apt-cache policy topo | awk "/Candidate:/ { print \$2 }")
    test "$candidate" = "$expected"
    if [[ "$stage" = beta1 ]]; then
      apt-get install --yes topo
    else
      apt-get install --yes --only-upgrade topo
    fi
    test "$(dpkg-query -W -f="\${Version}" topo)" = "$expected"
    cmp /usr/bin/topo /fixtures/payload/usr/bin/topo
    test "$(cat /etc/topo-worker/operator-owned)" = operator-owned
    test ! -e /etc/systemd/system/multi-user.target.wants/topo-worker.service
    # A second install must keep the same native version and payload.
    apt-get install --yes topo
    test "$(dpkg-query -W -f="\${Version}" topo)" = "$expected"
  done
  rm -rf /repo/current
  cp -R /fixtures/expired /repo/current
  rm -rf /var/lib/apt/lists/*
  if apt-get update > /tmp/expired.log 2>&1; then
    echo "APT accepted expired signed metadata" >&2; exit 1
  fi
  grep -i "expired" /tmp/expired.log
  test "$(dpkg-query -W -f="\${Version}" topo)" = 0.4.6-1
  test "$(cat /etc/topo-worker/operator-owned)" = operator-owned
'
echo 'Signed APT beta-to-beta-to-stable upgrades, repeat installs, payload/config preservation and expiry rejection passed (synthetic versions).'
