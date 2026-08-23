#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <distribution-dir> <40-hex signing fingerprint>" >&2
  exit 2
fi

root=$1
fingerprint=$2
case "$fingerprint" in
  *[!0-9A-Fa-f]*|'') echo "invalid signing fingerprint" >&2; exit 2 ;;
esac
if [ "${#fingerprint}" -ne 40 ]; then
  echo "signing fingerprint must contain 40 hexadecimal characters" >&2
  exit 2
fi
if [ -z "${GNUPGHOME:-}" ] || [ ! -d "$GNUPGHOME" ]; then
  echo "GNUPGHOME must name the isolated repository keyring" >&2
  exit 1
fi

gpg --batch --list-secret-keys "$fingerprint" >/dev/null
mkdir -p "$root/keys"
export_fingerprints=$fingerprint
if [ -n "${TOPO_ADDITIONAL_PUBLIC_KEY_FILE:-}" ]; then
  if [ ! -f "$TOPO_ADDITIONAL_PUBLIC_KEY_FILE" ]; then
    echo "TOPO_ADDITIONAL_PUBLIC_KEY_FILE does not exist" >&2
    exit 1
  fi
  case "${TOPO_ADDITIONAL_PUBLIC_KEY_FINGERPRINT:-}" in
    *[!0-9A-Fa-f]*|'') echo "invalid additional public-key fingerprint" >&2; exit 2 ;;
  esac
  if [ "${#TOPO_ADDITIONAL_PUBLIC_KEY_FINGERPRINT}" -ne 40 ]; then
    echo "additional public-key fingerprint must contain 40 hexadecimal characters" >&2
    exit 2
  fi
  gpg --batch --import "$TOPO_ADDITIONAL_PUBLIC_KEY_FILE"
  additional=$(gpg --batch --with-colons --fingerprint "$TOPO_ADDITIONAL_PUBLIC_KEY_FINGERPRINT" | awk -F: '$1 == "fpr" { print $10; exit }')
  if [ "$(printf '%s' "$additional" | tr '[:lower:]' '[:upper:]')" != \
       "$(printf '%s' "$TOPO_ADDITIONAL_PUBLIC_KEY_FINGERPRINT" | tr '[:lower:]' '[:upper:]')" ]; then
    echo "additional public-key fingerprint mismatch" >&2
    exit 1
  fi
  export_fingerprints="$fingerprint $TOPO_ADDITIONAL_PUBLIC_KEY_FINGERPRINT"
fi
# Word splitting is intentional: gpg receives one or two validated fingerprints.
# shellcheck disable=SC2086
gpg --batch --export $export_fingerprints > "$root/keys/nischoy-topo-archive.gpg"
if [ ! -s "$root/keys/nischoy-topo-archive.gpg" ]; then
  echo "failed to export repository public key" >&2
  exit 1
fi
# RPM requires an armored key while APT's .gpg Signed-By path requires the
# binary keyring above. Both files contain the same validated identities.
# shellcheck disable=SC2086
gpg --batch --armor --export $export_fingerprints > "$root/keys/nischoy-topo-archive.asc"
if [ ! -s "$root/keys/nischoy-topo-archive.asc" ]; then
  echo "failed to export armored repository public key" >&2
  exit 1
fi

if [ -n "${TOPO_GPG_PASSPHRASE_FILE:-}" ]; then
  if [ ! -f "$TOPO_GPG_PASSPHRASE_FILE" ]; then
    echo "TOPO_GPG_PASSPHRASE_FILE does not exist" >&2
    exit 1
  fi
fi

sign() {
  mode=$1
  output=$2
  input=$3
  if [ -n "${TOPO_GPG_PASSPHRASE_FILE:-}" ]; then
    gpg --batch --yes --pinentry-mode loopback \
      --passphrase-file "$TOPO_GPG_PASSPHRASE_FILE" \
      --local-user "$fingerprint" --digest-algo SHA256 \
      --armor "$mode" --output "$output" "$input"
  else
    gpg --batch --yes --local-user "$fingerprint" --digest-algo SHA256 \
      --armor "$mode" --output "$output" "$input"
  fi
}

found_apt=0
for release in "$root"/apt/dists/*/Release; do
  if [ ! -f "$release" ]; then
    continue
  fi
  found_apt=1
  sign --clearsign "$(dirname "$release")/InRelease" "$release"
  sign --detach-sign "$release.gpg" "$release"
  gpg --batch --verify "$(dirname "$release")/InRelease" >/dev/null
  gpg --batch --verify "$release.gpg" "$release" >/dev/null
done
if [ "$found_apt" -ne 1 ]; then
  echo "no APT Release file found" >&2
  exit 1
fi

found_rpm=0
for metadata in "$root"/rpm/*/*/repodata/repomd.xml; do
  if [ ! -f "$metadata" ]; then
    continue
  fi
  found_rpm=1
  sign --detach-sign "$metadata.asc" "$metadata"
  gpg --batch --verify "$metadata.asc" "$metadata" >/dev/null
done
if [ "$found_rpm" -ne 1 ]; then
  echo "no RPM repomd.xml file found" >&2
  exit 1
fi
