#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <artifact-dir> <40-hex signing fingerprint>" >&2
  exit 2
fi

artifact_dir=$1
fingerprint=$2
case "$fingerprint" in
  *[!0-9A-Fa-f]*|'') echo "invalid signing fingerprint" >&2; exit 2 ;;
esac
if [ "${#fingerprint}" -ne 40 ]; then
  echo "signing fingerprint must contain 40 hexadecimal characters" >&2
  exit 2
fi
if [ -z "${GNUPGHOME:-}" ] || [ ! -d "$GNUPGHOME" ]; then
  echo "GNUPGHOME must name the isolated release keyring" >&2
  exit 1
fi

gpg --batch --list-secret-keys "$fingerprint" >/dev/null
verification=$(mktemp -d "${TMPDIR:-/tmp}/topo-rpm-verification.XXXXXX")
trap 'rm -rf "$verification"' EXIT HUP INT TERM
gpg --batch --export "$fingerprint" > "$verification/public-key.gpg"
test -s "$verification/public-key.gpg"
rpm --dbpath "$verification/rpmdb" --initdb
rpm --dbpath "$verification/rpmdb" --import "$verification/public-key.gpg"
found=0
for package in "$artifact_dir"/*.rpm; do
  if [ ! -f "$package" ]; then
    continue
  fi
  found=1
  rpmsign --addsign \
    --define "_gpg_name $fingerprint" \
    --define "_gpg_path $GNUPGHOME" \
    "$package"
  result=$(rpm --dbpath "$verification/rpmdb" --checksig "$package")
  case "$result" in
    *': digests signatures OK'|*': signatures OK') ;;
    *) echo "RPM signature verification failed: $result" >&2; exit 1 ;;
  esac
done
if [ "$found" -ne 1 ]; then
  echo "no RPM artifacts found in $artifact_dir" >&2
  exit 1
fi
