#!/usr/bin/env python3
"""Prepare the independent MSAL dependency update required by Azure Identity1.6."""
import argparse,base64,gzip,hashlib,io,json,subprocess,tarfile,urllib.request,zipfile
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__);p.add_argument('--output',type=Path,required=True);a=p.parse_args()
out=a.output.resolve();out.mkdir(parents=True,exist_ok=False)
module='github.com/AzureAD/microsoft-authentication-library-for-go';v='v1.2.2';name='golang-github-azuread-microsoft-authentication-library-for-go';version='1.2.2-1~topo1'
url='https://proxy.golang.org/github.com/!azure!a!d/microsoft-authentication-library-for-go/@v/'+v+'.zip'
data=urllib.request.urlopen(url,timeout=60).read();z=zipfile.ZipFile(io.BytesIO(data));lines=''.join(hashlib.sha256(z.read(n)).hexdigest()+'  '+n+'\n' for n in sorted(z.namelist()));h='h1:'+base64.b64encode(hashlib.sha256(lines.encode()).digest()).decode();assert h=='h1:XHOnouVk1mxXfQidrMEnLlPk9UMeRtyBTnEFtxkV0kU='
tree=out/(name+'-1.2.2');tree.mkdir();prefix=module+'@'+v+'/'
for n in z.namelist():
 assert n.startswith(prefix);r=Path(n[len(prefix):]);assert '..' not in r.parts
 f=tree/r;f.parent.mkdir(parents=True,exist_ok=True);f.write_bytes(z.read(n))
with (out/(name+'_1.2.2.orig.tar.gz')).open('xb') as f,gzip.GzipFile(fileobj=f,mode='wb',filename='',mtime=0) as g,tarfile.open(fileobj=g,mode='w') as t:
 for file in sorted(tree.rglob('*')):
  i=t.gettarinfo(str(file),arcname=tree.name+'/'+str(file.relative_to(tree)));i.uid=i.gid=0;i.uname=i.gname='';i.mtime=0
  if file.is_file():
   with file.open('rb') as src:t.addfile(i,src)
  else:t.addfile(i)
d=tree/'debian';(d/'source').mkdir(parents=True);(d/'source/format').write_text('3.0 (quilt)\n')
(d/'control').write_text(f'''Source: {name}
Section: golang
Priority: optional
Maintainer: Soumik Sarkar <soumiks@gmail.com>
Build-Depends: debhelper-compat (= 13), dh-go (>= 1.69), golang-1.26-go (>= 1.26.9) | golang-1.27-go (>= 1.27.2), golang-github-pkg-browser-dev, golang-github-golang-jwt-jwt-v5-dev, golang-github-google-uuid-dev, golang-github-kylelemons-godebug-dev, golang-github-montanaflynn-stats-dev, golang-golang-x-sys-dev, golang-github-stretchr-testify-dev
Standards-Version: 4.7.4
Homepage: https://github.com/AzureAD/microsoft-authentication-library-for-go
Rules-Requires-Root: no
XS-Go-Import-Path: {module}

Package: {name}-dev
Architecture: all
Multi-Arch: foreign
Depends: golang-github-pkg-browser-dev, golang-github-golang-jwt-jwt-v5-dev, golang-github-google-uuid-dev, golang-github-kylelemons-godebug-dev, golang-github-montanaflynn-stats-dev, golang-golang-x-sys-dev, ${{misc:Depends}}
Description: Microsoft Authentication Library for Go (development files)
 Authentication library for Microsoft identity platforms. This separate source
 update enables the reviewed Azure Identity security update.
''')
(d/'changelog').write_text(f'''{name} ({version}) UNRELEASED; urgency=medium

  * Prepare upstream 1.2.2 for Azure Identity 1.6.0 dependency review.

 -- Soumik Sarkar <soumiks@gmail.com>  Sat, 17 Oct 2026 10:00:00 +0000
''')
(d/'rules').write_text('''#!/usr/bin/make -f
export PATH := /usr/lib/go-1.27/bin:/usr/lib/go-1.26/bin:$(PATH)
export GOTOOLCHAIN := local
export GOPROXY := off
export GOSUMDB := off
export GOMAXPROCS := 2
export DH_GOLANG_INSTALL_ALL := 1
%:
\tdh $@ --buildsystem=golang --with=golang --builddirectory=_build

override_dh_auto_test:
\tDH_GOLANG_EXCLUDES="microsoft-authentication-library-for-go/apps/tests/integration microsoft-authentication-library-for-go/apps/tests/performance" dh_auto_test -- -race -timeout 10m
''');(d/'rules').chmod(0o755)
# All upstream files carry Microsoft MIT terms; keep its complete license.
indent=lambda s:'\n'.join(' '+line if line else ' .' for line in s.splitlines())+'\n'
(d/'copyright').write_text('''Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: microsoft-authentication-library-for-go
Source: https://github.com/AzureAD/microsoft-authentication-library-for-go

Files: *
Copyright: Microsoft Corporation
License: MIT

Files: debian/*
Copyright: 2026 Soumik Sarkar <soumiks@gmail.com>
License: Apache-2.0

License: MIT
'''+indent((tree/'LICENSE').read_text())+'''
License: Apache-2.0
 On Debian systems, see /usr/share/common-licenses/Apache-2.0.
''')
root=Path(__file__).resolve().parents[2]
(d/'source/lintian-overrides').write_text((root/'packaging/debian/debian/source/lintian-overrides').read_text().replace('topo source:',name+' source:'))
with (out/'dpkg-source.log').open('w') as log:subprocess.run(['dpkg-source','--build',str(tree)],cwd=out,stdout=log,stderr=subprocess.STDOUT,check=True)
(out/'source-receipt.json').write_text(json.dumps({'module':module,'version':v,'go_h1':h,'zip_sha256':hashlib.sha256(data).hexdigest(),'source_url':url,'signed':False,'archive_accepted':False},indent=2)+'\n');print(tree)
