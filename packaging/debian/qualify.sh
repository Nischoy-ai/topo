#!/bin/bash
# Clean builds on disposable hosted Debian runners only. No archive upload.
set -euo pipefail
test "${GITHUB_ACTIONS:-}" = true
test "${RUNNER_ENVIRONMENT:-}" = github-hosted
test "${GITHUB_JOB:-}" = debian-native
test -f /.dockerenv
grep -qx ID=debian /etc/os-release
out=/tmp/topo-debian-qualification
mkdir -p "$out"/{ntlm,msal,azure,topo,tests}
arch=$(dpkg --print-architecture)
cp /var/lib/apt/lists/*InRelease "$out/"
dpkg-query -W > "$out/host-packages.txt"
# Independent source update, authenticated by the original upstream Go checksum.
python3 packaging/debian/prepare-msal.py --output "$out/msal-source" > "$out/msal-source-path.txt"
python3 packaging/debian/prepare-azure.py --output "$out/azure-source" > "$out/azure-source-path.txt"
python3 packaging/debian/prepare-ntlmssp.py --output "$out/ntlm-source" > "$out/ntlm-source-path.txt"
# A fresh chroot is used for each build session. No host Go module cache enters it.
sbuild-createchroot --arch="$arch" --include=ca-certificates,iproute2,passwd,util-linux unstable /srv/chroot/unstable "http://deb.debian.org/debian" > "$out/chroot.log" 2>&1
# autopkgtest requires a genuinely ephemeral testbed. Snapshot the pristine
# runtime chroot before build dependencies or source trees enter it.
tar --one-file-system -czf "$out/autopkgtest-base.tar.gz" -C /srv/chroot/unstable .
cat > /etc/schroot/chroot.d/topo-autopkgtest <<EOF
[topo-autopkgtest]
type=file
file=$out/autopkgtest-base.tar.gz
profile=default
users=root
root-users=root
EOF
# schroot does not implement sbuild's network switch. Use an explicit build-only
# network namespace, after APT dependencies are installed, with loopback up.
cat > /srv/chroot/unstable/usr/local/bin/topo-offline-build <<'OFFLINE'
#!/bin/sh
set -eu
ip link set lo up
ip -brief link
[ "$(ip -o link | wc -l)" -eq 1 ]
# Configure the disposable builder before any instrumented Go tool starts.
for tool in /usr/lib/go-1.27/bin/go /usr/lib/go-1.26/bin/go; do
 if test -x "$tool"; then
  runuser --user topo-builder -- "$tool" -C / telemetry off
  break
 fi
done
exec runuser --user topo-builder -- "$@"
OFFLINE
chmod 0755 /srv/chroot/unstable/usr/local/bin/topo-offline-build
cat > /srv/chroot/unstable/usr/local/bin/topo-build-command <<'COMMAND'
#!/bin/sh
set -eu
test "$(id -u)" = 0
case "$PWD" in /build/reproducible-path/topo-*|/build/reproducible-path/golang-github-azure-*|/build/reproducible-path/golang-github-azuread-*) ;; *) exit 1 ;; esac
namei -l "$PWD/debian/changelog"
chown -R topo-builder:topo-builder "$PWD"
chown topo-builder:topo-builder "$(dirname "$PWD")"
exec unshare --net /usr/local/bin/topo-offline-build "$@"
COMMAND
chmod 0755 /srv/chroot/unstable/usr/local/bin/topo-build-command
# schroot's sbuild profile copies container passwd/group into each session.
# Define this disposable account there and create its matching chroot home.
useradd --system --user-group --groups sbuild --create-home --home-dir /var/lib/topo-builder --shell /bin/bash topo-builder
mkdir -p /srv/chroot/unstable/var/lib/topo-builder
chown "$(id -u topo-builder):$(id -g topo-builder)" /srv/chroot/unstable/var/lib/topo-builder
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
sbuild --nolog --verbose --chroot-mode=schroot --chroot="unstable-${arch}-sbuild" --dist=unstable --arch="$arch" \
 --arch-all --no-enable-network --no-run-lintian --jobs=2 --build-dir="$out/msal" \
 "$out/msal-source/"*.dsc > "$out/msal-sbuild.log" 2>&1
msal=("$out/msal/"*dev*.deb);test "${#msal[@]}" = 1
sbuild --nolog --verbose --chroot-mode=schroot --chroot="unstable-${arch}-sbuild" --dist=unstable --arch="$arch" \
 --arch-all --no-enable-network --no-run-lintian --jobs=2 --build-dir="$out/azure" \
 --extra-package="${msal[0]}" "$out/azure-source/"*.dsc > "$out/azure-sbuild.log" 2>&1
azure=("$out/azure/"*dev*.deb);test "${#azure[@]}" = 1
# Prepare unbundled Topo source; all other dependencies come from normal Debian.
python3 packaging/debian/prepare-source.py --output "$out/topo-source" > "$out/topo-source-path.txt"
sbuild --nolog --verbose --chroot-mode=schroot --chroot="unstable-${arch}-sbuild" --dist=unstable --arch="$arch" \
 --no-enable-network --no-run-lintian --jobs=2 --build-dir="$out/topo" \
 --purge-build=never --purge-deps=never --extra-package="${ntlm[0]}" --extra-package="${msal[0]}" --extra-package="${azure[0]}" "$out/topo-source/"*.dsc > "$out/topo-sbuild.log" 2>&1
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
# Keep scan and mount teardown outcomes distinct. A completed Go process can
# release its final cwd/file references just after the command exits; retry only
# normal session teardown, never lazy-unmount or ignore a remaining busy mount.
session=$(schroot --begin-session --chroot="unstable-${arch}-sbuild" --user=root)
scan_status=0
schroot --run-session --chroot="$session" --user=root --directory="$source_dir" -- \
 env PATH=/usr/lib/go-1.27/bin:/usr/lib/go-1.26/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
 python3 debian/native-security-review.py _build /build/topo-native-security > "$out/native-security.log" 2>&1 || scan_status=$?
cleanup_status=1
for attempt in 1 2 3 4 5; do
 if schroot --end-session --chroot="$session" >> "$out/schroot-cleanup.log" 2>&1; then
  cleanup_status=0
  break
 fi
 sleep 2
done
printf 'scan %s\ncleanup %s\n' "$scan_status" "$cleanup_status" > "$out/security-phase-status.txt"
if test "$cleanup_status" != 0; then
 python3 - "$out/schroot-processes.json" <<'PROCESSES'
import json,os,sys
from pathlib import Path
records=[]
for process in Path('/proc').iterdir():
 if not process.name.isdigit():continue
 try:
  root=os.readlink(process/'root')
  cwd=os.readlink(process/'cwd')
  if '/schroot/' not in root and '/schroot/' not in cwd:continue
  records.append({'pid':int(process.name),'comm':(process/'comm').read_text().strip(),'root':root,'cwd':cwd})
 except (FileNotFoundError,PermissionError,ProcessLookupError):continue
Path(sys.argv[1]).write_text(json.dumps(records,indent=2)+'\n')
PROCESSES
 cat /proc/self/mountinfo > "$out/schroot-mountinfo.txt"
fi
test "$scan_status" = 0
test "$cleanup_status" = 0
preserve_security
# Collect every independent lint/smoke outcome before failing the gate. A first
# lint error must not hide the other package reports or installed smoke result.
failed=0
record_check() {
 name=$1;shift
 if "$@" > "$out/$name.log" 2>&1; then
  printf '%s %s\n' "$name" 0 >> "$out/package-checks.txt"
 else
  status=$?
  printf '%s %s\n' "$name" "$status" >> "$out/package-checks.txt"
  tail -n 80 "$out/$name.log"
  failed=1
 fi
}
record_check ntlm-lintian lintian "$out/ntlm-source/"*.dsc "${ntlm[0]}"
record_check msal-lintian lintian "$out/msal-source/"*.dsc "${msal[0]}"
record_check azure-lintian lintian "$out/azure-source/"*.dsc "${azure[0]}"
record_check topo-lintian lintian "$out/topo-source/"*.dsc "$out/topo/"*.changes
# An actual installed-package autopkgtest in a disposable chroot (not extraction).
record_check autopkgtest autopkgtest "$out/topo-source/"*.dsc "$out/topo/"topo_*"_${arch}.deb" \
 --output-dir="$out/tests/autopkgtest" -- schroot topo-autopkgtest
test "$failed" = 0
python3 - "$out" "$arch" <<'PY'
import hashlib,json,sys
from pathlib import Path
p=Path(sys.argv[1]); records={}
for f in p.rglob('*'):
 if f.is_file() and (f.suffix in ('.deb','.dsc','.changes','.buildinfo') or f.name=='source-receipt.json'):
  records[str(f.relative_to(p))]={'bytes':f.stat().st_size,'sha256':hashlib.sha256(f.read_bytes()).hexdigest()}
(p/'BUILD-RESULT.json').write_text(json.dumps({'architecture':sys.argv[2], 'clean_sbuild':True,'native_vet_and_race':True,'native_actual_version_security':True,'lintian':True,'autopkgtest':True,'dependency_update':'separate NTLM0.1.1 review candidate via sbuild --extra-package; not in Debian archive','source_signed':False,'archive_accepted':False,'upload_ready':False,'remaining':['real systemd package lifecycle','owner signing and sponsor/archive review'],'artifacts':records},indent=2)+'\n')
PY
