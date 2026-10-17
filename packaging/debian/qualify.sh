#!/bin/bash
# Clean builds on disposable hosted Debian runners only. No archive upload.
set -euo pipefail
test "${GITHUB_ACTIONS:-}" = true
test "${RUNNER_ENVIRONMENT:-}" = github-hosted
test "${GITHUB_JOB:-}" = debian-native
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
 --purge-build=never --purge-deps=never --extra-package="${ntlm[0]}" "$out/topo-source/"*.dsc > "$out/topo-sbuild.log" 2>&1
# A directory schroot may retain /build physically under its root, while other
# profiles bind /var/lib/sbuild/build to /build (verified schroot1.6.13-8).
# Locate the one fresh build by inode and translate that explicit mount.
mapfile -t placement < <(python3 - <<'LOCATE'
from pathlib import Path
seen={}
chroot=Path('/srv/chroot/unstable')
for base in [chroot/'build',Path('/build'),Path('/var/lib/sbuild/build')]:
 if base.exists():
  for p in base.glob('**/_build/topo-setup-controlsim'):
   key=(p.stat().st_dev,p.stat().st_ino)
   seen.setdefault(key,p)
if len(seen)!=1:raise ValueError(f'Expected one native simulator, found {list(seen.values())}')
p=next(iter(seen.values()));source=p.parent.parent
if source.is_relative_to(chroot):inside=Path('/')/source.relative_to(chroot)
elif source.is_relative_to('/var/lib/sbuild/build'):inside=Path('/build')/source.relative_to('/var/lib/sbuild/build')
else:inside=source
print(p);print(inside)
LOCATE
)
test "${#placement[@]}" = 2
cp "${placement[0]}" "$out/topo-setup-controlsim"
source_dir="${placement[1]}"
preserve_security() {
 for directory in /srv/chroot/unstable/build/topo-native-security /build/topo-native-security /var/lib/sbuild/build/topo-native-security; do
  if test -d "$directory"; then
   find "$directory" -maxdepth 1 -type f \( -name '*.json' -o -name 'govulncheck.log' \) -exec cp '{}' "$out/" \;
  fi
 done
}
trap preserve_security EXIT
# Analyze the exact retained build sources and installed dependencies. This
# subsequent analysis phase can fetch official tool/DB data; package builds cannot.
schroot --chroot="unstable-${arch}-sbuild" --user=root --directory="$source_dir" -- \
 env PATH=/usr/lib/go-1.27/bin:/usr/lib/go-1.26/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
 python3 debian/native-security-review.py _build /build/topo-native-security > "$out/native-security.log" 2>&1
preserve_security
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
(p/'BUILD-RESULT.json').write_text(json.dumps({'architecture':sys.argv[2], 'clean_sbuild':True,'native_vet_and_race':True,'native_actual_version_security':True,'lintian':True,'autopkgtest':True,'dependency_update':'separate NTLM0.1.1 review candidate via sbuild --extra-package; not in Debian archive','source_signed':False,'archive_accepted':False,'upload_ready':False,'remaining':['real systemd package lifecycle','owner signing and sponsor/archive review'],'artifacts':records},indent=2)+'\n')
PY
