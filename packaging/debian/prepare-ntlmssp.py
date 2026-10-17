#!/usr/bin/env python3
"""Prepare a separate, checksum-verified NTLM dependency update review source.

This is not an archive upload or a bundled Topo dependency. Source bytes are
verified against Topo's original go.sum; compilation is offline and separate.
"""
import argparse
import base64
import gzip
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import urllib.request
import zipfile

root = Path(__file__).resolve().parents[2]
module = 'github.com/Azure/go-ntlmssp'
version = 'v0.1.1'
package = 'golang-github-azure-go-ntlmssp'
debversion = '0.1.1-1~topo1'
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--output', type=Path, required=True)
a = p.parse_args()
out = a.output.resolve()
out.mkdir(parents=True, exist_ok=False)
url = 'https://proxy.golang.org/github.com/!azure/go-ntlmssp/@v/v0.1.1.zip'
data = urllib.request.urlopen(url, timeout=60).read()
z = zipfile.ZipFile(io.BytesIO(data))
lines = ''.join(hashlib.sha256(z.read(n)).hexdigest()+'  '+n+'\n' for n in sorted(z.namelist()))
h1 = 'h1:'+base64.b64encode(hashlib.sha256(lines.encode()).digest()).decode()
assert f'{module} {version} {h1}' in (root/'go.sum').read_text()
tree = out/(package+'-0.1.1'); tree.mkdir()
prefix = module+'@'+version+'/'
for n in z.namelist():
    assert n.startswith(prefix)
    relative = Path(n[len(prefix):])
    assert '..' not in relative.parts
    path = tree/relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(z.read(n))
archive = out/(package+'_0.1.1.orig.tar.gz')
with archive.open('xb') as f, gzip.GzipFile(fileobj=f, mode='wb', filename='', mtime=0) as g, tarfile.open(fileobj=g, mode='w') as tar:
    for path in sorted(tree.rglob('*')):
        info = tar.gettarinfo(str(path), arcname=tree.name+'/'+str(path.relative_to(tree)))
        info.uid=info.gid=0;info.uname=info.gname='';info.mtime=0
        if path.is_file():
            with path.open('rb') as src: tar.addfile(info,src)
        else: tar.addfile(info)
debian=tree/'debian'; (debian/'source').mkdir(parents=True)
(debian/'source/format').write_text('3.0 (quilt)\n')
(debian/'control').write_text(f'''Source: {package}
Section: golang
Priority: optional
Maintainer: Soumik Sarkar <soumiks@gmail.com>
Build-Depends: debhelper-compat (= 13), dh-go (>= 1.69), golang-1.26-go (>= 1.26.9) | golang-1.27-go (>= 1.27.2)
Standards-Version: 4.7.4
Homepage: https://github.com/Azure/go-ntlmssp
Rules-Requires-Root: no
XS-Go-Import-Path: github.com/Azure/go-ntlmssp

Package: {package}-dev
Architecture: all
Multi-Arch: foreign
Depends: ${{misc:Depends}}
Description: Go NTLM/Negotiate authentication over HTTP (development files)
 Pure-Go NTLM authentication over HTTP. This source update retains the upstream
 malformed challenge fix in version 0.1.1. It is an unsigned review candidate,
 not an accepted Debian archive update.
''')
(debian/'changelog').write_text(f'''{package} ({debversion}) UNRELEASED; urgency=medium

  * Prepare upstream 0.1.1 update, including malformed challenge bounds fix.
  * Retain MIT and copied Go MD4 BSD-3-Clause attribution in full.

 -- Soumik Sarkar <soumiks@gmail.com>  Sat, 17 Oct 2026 10:00:00 +0000
''')
(debian/'rules').write_text('''#!/usr/bin/make -f
export PATH := /usr/lib/go-1.27/bin:/usr/lib/go-1.26/bin:$(PATH)
export GOTOOLCHAIN := local
export GOPROXY := off
export GOSUMDB := off
export GOMAXPROCS := 2
export DH_GOLANG_INSTALL_ALL := 1
%:
\tdh $@ --buildsystem=golang --with=golang --builddirectory=_build

override_dh_auto_test:
\tdh_auto_test -- -race -timeout 10m
''')
(debian/'rules').chmod(0o755)
(debian/'docs').write_text('README.md\n')
# The copied MD4 files explicitly retain the Go Authors' BSD notice. Upstream
# omitted the referenced BSD LICENSE text; preserve it here in full.
bsd=(root/'packaging/debian/license-texts/Go-BSD-3-Clause').read_text()
indent=lambda s:'\n'.join(' '+line if line else ' .' for line in s.splitlines())+'\n'
(debian/'copyright').write_text('''Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: go-ntlmssp
Source: https://github.com/Azure/go-ntlmssp

Files: *
Copyright: 2016 Microsoft
License: MIT

Files: internal/md4/*
Copyright: 2009 The Go Authors
License: BSD-3-Clause
Comment: Upstream internal/md4/README.md identifies the copied x/crypto MD4
 implementation. Source headers refer to the Go BSD license; retain it here.

Files: debian/*
Copyright: 2026 Soumik Sarkar <soumiks@gmail.com>
License: Apache-2.0

License: MIT
'''+indent((tree/'LICENSE').read_text())+'\nLicense: BSD-3-Clause\n'+indent(bsd)+'''
License: Apache-2.0
 On Debian systems, see /usr/share/common-licenses/Apache-2.0.
''')
# Match the Topo source package's narrowly documented dh-go addon lint exception.
(debian/'source/lintian-overrides').write_text((root/'packaging/debian/debian/source/lintian-overrides').read_text().replace('topo source:', package+' source:'))
with (out/'dpkg-source.log').open('w') as log:
    subprocess.run(['dpkg-source','--build',str(tree)],cwd=out,stdout=log,stderr=subprocess.STDOUT,check=True)
(out/'source-receipt.json').write_text(json.dumps({'module':module,'version':version,'source_url':url,'zip_sha256':hashlib.sha256(data).hexdigest(),'original_go_sum_h1':h1,'signed':False,'archive_accepted':False,'purpose':'separate NTLM dependency-update review candidate; never bundled into Topo source'},indent=2)+'\n')
print(tree)
