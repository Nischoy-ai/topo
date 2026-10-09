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


def verify(expected, directory):
    if len(expected) > 4096:
        raise ValueError('manifest exceeds limit')
    # Compare exact manifest bytes before accepting any downloaded pathname.
    if read_regular(directory / 'SHA256SUMS', 4096) != expected:
        raise ValueError('downloaded manifest differs from reviewed manifest')
    entries = {}
    for line in expected.decode('ascii').splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  ([A-Za-z0-9_.-]+)', line)
        if not match or match[2] not in FILES or match[2] in entries:
            raise ValueError('invalid manifest entry')
        entries[match[2]] = match[1]
    if set(entries) != FILES:
        raise ValueError('missing signing input')
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
        if len(sys.argv) != 2:
            raise ValueError('expected one directory')
        root = Path(__file__).resolve().parent.parent
        expected = read_regular(root / 'release/servicenow-0.4.6-beta.SHA256SUMS', 4096)
        verify(expected, Path(sys.argv[1]))
    except Exception:
        print('ServiceNow signing inputs rejected.', file=sys.stderr)
        return 1
    print('Existing ServiceNow release bytes match the reviewed manifest.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
