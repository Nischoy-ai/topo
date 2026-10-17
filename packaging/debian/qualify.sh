#!/bin/bash
# Clean builds on disposable hosted Debian runners only. No archive upload.
set -euo pipefail
test "${GITHUB_ACTIONS:-}" = true
test "${RUNNER_ENVIRONMENT:-}" = github-hosted
test -f /.dockerenv
grep -qx ID=debian /etc/os-release
out=/tmp/topo-debian-qualification
mkdir -p "$out"/{ntlm,topo,tests}
arch=$(dpkg --print-architecture)
cp /var/lib/apt/lists/*InRelease "$out/"
dpkg-query -W > "$out/host-packages.txt"
# Independent source update, authenticated by the original upstream Go checksum.
python3 packaging/debian/prepare-ntlmssp.py --output "$out/ntlm-source" > "$out/ntlm-source-path.txt"
# A fresh chroot is used for each build session. No host Go module cache enters it.
sbuild-createchroot --arch="$arch" --include=ca-certificates,iproute2 unstable /srv/chroot/unstable "http://deb.debian.org/debian" > "$out/chroot.log" 2>&1
# schroot does not implement sbuild's network switch. Use an explicit build-only
# network namespace, after APT dependencies are installed, with loopback up.
cat > /srv/chroot/unstable/usr/local/bin/topo-offline-build <<'OFFLINE'
#!/bin/sh
set -eu
ip link set lo up
ip -brief link
[ "$(ip -o link | wc -l)" -eq 1 ]
exec "$@"
OFFLINE
chmod 0755 /srv/chroot/unstable/usr/local/bin/topo-offline-build
cat > /srv/chroot/unstable/usr/local/bin/topo-build-command <<'COMMAND'
#!/bin/sh
set -eu
exec unshare --net /usr/local/bin/topo-offline-build "$@"
COMMAND
chmod 0755 /srv/chroot/unstable/usr/local/bin/topo-build-command
cat > "$out/sbuild-config.pl" <<'SBUILD'
$build_env_cmnd = '/usr/local/bin/topo-build-command';
$mailto = '';
1;
SBUILD
export SBUILD_CONFIG="$out/sbuild-config.pl"
sbuild --nolog --verbose --chroot-mode=schroot --chroot="unstable-${arch}-sbuild" --dist=unstable --arch="$arch" \
 --arch-all --no-enable-network --no-run-lintian --jobs=2 --build-dir="$out/ntlm" \
 "$out/ntlm-source/"*.dsc > "$out/ntlm-sbuild.log" 2>&1
ntlm=("$out/ntlm/"*dev*.deb)
test "${#ntlm[@]}" = 1
# Prepare unbundled Topo source; all other dependencies come from normal Debian.
python3 packaging/debian/prepare-source.py --output "$out/topo-source" > "$out/topo-source-path.txt"
sbuild --nolog --verbose --chroot-mode=schroot --chroot="unstable-${arch}-sbuild" --dist=unstable --arch="$arch" \
 --no-enable-network --no-run-lintian --jobs=2 --build-dir="$out/topo" \
 --purge-build=never --extra-package="${ntlm[0]}" "$out/topo-source/"*.dsc > "$out/topo-sbuild.log" 2>&1
mapfile -t simulators < <(find /build -type f -path '*/_build/topo-setup-controlsim')
test "${#simulators[@]}" = 1
cp "${simulators[0]}" "$out/topo-setup-controlsim"
lintian "$out/ntlm-source/"*.dsc "${ntlm[0]}" > "$out/ntlm-lintian.log" 2>&1
lintian "$out/topo-source/"*.dsc "$out/topo/"*.changes > "$out/topo-lintian.log" 2>&1
# An actual installed-package autopkgtest in a disposable chroot (not extraction).
autopkgtest "$out/topo-source/"*.dsc "$out/topo/"topo_*"_${arch}.deb" \
 --output-dir="$out/tests/autopkgtest" -- schroot "unstable-${arch}-sbuild" > "$out/autopkgtest.log" 2>&1
python3 - "$out" "$arch" <<'PY'
import hashlib,json,sys
from pathlib import Path
p=Path(sys.argv[1]); records={}
for f in p.rglob('*'):
 if f.is_file() and (f.suffix in ('.deb','.dsc','.changes','.buildinfo') or f.name=='source-receipt.json'):
  records[str(f.relative_to(p))]={'bytes':f.stat().st_size,'sha256':hashlib.sha256(f.read_bytes()).hexdigest()}
(p/'BUILD-RESULT.json').write_text(json.dumps({'architecture':sys.argv[2], 'clean_sbuild':True,'native_vet_and_race':True,'lintian':True,'autopkgtest':True,'dependency_update':'separate NTLM0.1.1 review candidate via sbuild --extra-package; not in Debian archive','source_signed':False,'archive_accepted':False,'upload_ready':False,'remaining':['native security equivalence review','real systemd package lifecycle','owner signing and sponsor/archive review'],'artifacts':records},indent=2)+'\n')
PY
