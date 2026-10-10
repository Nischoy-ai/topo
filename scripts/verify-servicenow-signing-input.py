#!/usr/bin/env python3
"""Authenticate signing inputs against a reviewed manifest, without publication.

This check is not build provenance or ServiceNow acceptance. Signing the
manifest authenticates the existing manual package bytes, not their origin.
"""
import hashlib
import os
from pathlib import Path
import re
import stat
import sys

MAX_BYTES = 32 * 1024 * 1024
FILES = {'INDEXES.md', 'INSTALLATION.md', 'START-HERE.txt', 'manifest.json',
         'nischoy-topo-0.4.6-combined.xml', 'nischoy-topo-0.4.6-beta.zip'}
REVIEWED_RELEASES = {'servicenow-0.4.6-beta', 'servicenow-0.4.6-beta.2'}


def read_regular(path, maximum):
    if not stat.S_ISREG(path.lstat().st_mode):
        raise ValueError('not a regular file')
    descriptor = os.open(path, os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0))
    with os.fdopen(descriptor, 'rb') as stream:
        if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
            raise ValueError('not a regular file')
        body = stream.read(maximum + 1)
    if len(body) > maximum:
        raise ValueError('file exceeds signing-input limit')
    return body


def parse_manifest(expected):
    if len(expected) > 4096:
        raise ValueError('manifest exceeds limit')
    entries = {}
    for line in expected.decode('ascii').splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  ([A-Za-z0-9_.-]+)', line)
        if not match or match[2] not in FILES or match[2] in entries:
            raise ValueError('invalid manifest entry')
        entries[match[2]] = match[1]
    if set(entries) != FILES:
        raise ValueError('missing signing input')
    return entries


def verify(expected, directory):
    # Compare exact manifest bytes before accepting any downloaded pathname.
    if read_regular(directory / 'SHA256SUMS', 4096) != expected:
        raise ValueError('downloaded manifest differs from reviewed manifest')
    entries = parse_manifest(expected)
    if {entry.name for entry in directory.iterdir()} != FILES | {'SHA256SUMS'}:
        raise ValueError('unexpected signing input')
    total = 0
    for name, digest in entries.items():
        body = read_regular(directory / name, MAX_BYTES - total)
        total += len(body)
        if hashlib.sha256(body).hexdigest() != digest:
            raise ValueError('signing-input checksum mismatch')


def main():
    try:
        if len(sys.argv) not in (2, 3, 4):
            raise ValueError('expected directory and optional reviewed release tag')
        release_tag = sys.argv[2] if len(sys.argv) >= 3 else 'servicenow-0.4.6-beta'
        if release_tag not in REVIEWED_RELEASES:
            raise ValueError('release tag has no reviewed signing manifest')
        manifest_only = len(sys.argv) == 4
        if manifest_only and (sys.argv[3] != '--manifest-only' or release_tag != 'servicenow-0.4.6-beta.2'):
            raise ValueError('unsupported signing-input mode')
        root = Path(__file__).resolve().parent.parent
        expected = read_regular(root / 'release' / (release_tag + '.SHA256SUMS'), 4096)
        directory = Path(sys.argv[1])
        if manifest_only:
            if {entry.name for entry in directory.iterdir()} != {'SHA256SUMS'} or read_regular(directory / 'SHA256SUMS', 4096) != expected:
                raise ValueError('staged manifest differs from reviewed manifest')
            parse_manifest(expected)
        else:
            verify(expected, directory)
    except Exception:
        print('ServiceNow signing inputs rejected.', file=sys.stderr)
        return 1
    if manifest_only:
        print('Reviewed checksum manifest validated; package payloads not inspected.')
    else:
        print('ServiceNow release bytes match the reviewed manifest.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
