#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <distribution-dir> <revision-epoch>" >&2
  exit 2
fi

root=$1
revision=$2
case "$revision" in
  ''|*[!0-9]*) echo "revision epoch must be an integer" >&2; exit 2 ;;
esac

: "${CREATEREPO_C_BIN:=createrepo_c}"
"$CREATEREPO_C_BIN" --version

found=0
for channel in stable beta; do
  for arch in x86_64 aarch64; do
    repository="$root/rpm/$channel/$arch"
    if [ ! -d "$repository/Packages" ]; then
      continue
    fi
    found=1
    if [ -e "$repository/repodata" ]; then
      echo "RPM metadata already exists: $repository/repodata" >&2
      exit 1
    fi
    "$CREATEREPO_C_BIN" \
      --no-database \
      --simple-md-filenames \
      --revision "$revision" \
      --set-timestamp-to-revision \
      "$repository"
  done
done

if [ "$found" -ne 1 ]; then
  echo "no RPM package repositories found under $root" >&2
  exit 1
fi
