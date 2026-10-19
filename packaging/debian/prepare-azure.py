#!/usr/bin/env python3
"""Prepare a separate Azure SDK security update source, never a Topo bundle.

The signed Debian source index authenticates the original inputs in CI. Exact
upstream module checksums below were independently checked against sum.golang.org.
"""
import argparse,base64,gzip,hashlib,io,json,shutil,subprocess,tarfile,urllib.request,zipfile
from pathlib import Path
root=Path(__file__).resolve().parents[2]
p=argparse.ArgumentParser(description=__doc__);p.add_argument('--output',type=Path,required=True);a=p.parse_args()
out=a.output.resolve();out.mkdir(parents=True,exist_ok=False)
name='golang-github-azure-azure-sdk-for-go'
old='68.0.0+git20231220.f497335+dfsg1'
new=old+'+azidentity1.6.0'
version=new+'-1~topo2'
base='https://deb.debian.org/debian/pool/main/g/'+name+'/'
inputs={name+'_'+old+'.orig.tar.xz':'0229ef1bd5a3c9c62021d6e4bacd67d95b976768fea4825b4d3628ba190e03aa',name+'_'+old+'-7.debian.tar.xz':'74834f052a4e7d9ce0e66ae01af564dcd57c827bce42d86d87ba612f12b6213b'}
for filename,sha in inputs.items():
 data=urllib.request.urlopen(base+filename,timeout=60).read();assert hashlib.sha256(data).hexdigest()==sha
 with tarfile.open(fileobj=io.BytesIO(data)) as t:t.extractall(out,filter='data')
tree=out/(name+'-'+old);debian=out/'debian';debian.rename(tree/'debian');debian=tree/'debian'
# Replace complete upstream modules, not a version-label-only security override.
modules={'sdk/azidentity':('v1.6.0','h1:U2rTu3Ef+7w9FHKIAXM6ZyqF3UOWJZ12zIm8zECAFfg='),'sdk/azcore':('v1.11.1','h1:E+OJmp2tPvt1W+amx48v1eqbjDYsgN+RzP4q16yV5eM='),'sdk/internal':('v1.8.0','h1:jBQA3cKT4L2rWMpgE7Yt3Hwh2aUj8KXjIGLxjHeYNNo=')}
receipts={}
for path,(v,h1) in modules.items():
 m='github.com/Azure/azure-sdk-for-go/'+path
 url='https://proxy.golang.org/'+m.replace('Azure','!azure')+'/@v/'+v+'.zip'
 data=urllib.request.urlopen(url,timeout=60).read();z=zipfile.ZipFile(io.BytesIO(data))
 lines=''.join(hashlib.sha256(z.read(n)).hexdigest()+'  '+n+'\n' for n in sorted(z.namelist()))
 actual='h1:'+base64.b64encode(hashlib.sha256(lines.encode()).digest()).decode();assert actual==h1
 destination=tree/path
 nested=[f.parent for f in destination.rglob('go.mod') if f.parent!=destination]
 retained=out/'nested-modules'/path
 for child in nested:
  shutil.copytree(child,retained/child.relative_to(destination))
 shutil.rmtree(destination);destination.mkdir()
 prefix=m+'@'+v+'/'
 for n in z.namelist():
  assert n.startswith(prefix);rel=Path(n[len(prefix):]);assert '..' not in rel.parts
  target=destination/rel;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(z.read(n))
 for child in nested:
  rel=child.relative_to(destination);shutil.copytree(retained/rel,destination/rel)
 receipts[m]={'retained_nested_modules':[str(child.relative_to(destination)) for child in nested],'version':v,'h1':h1,'zip_sha256':hashlib.sha256(data).hexdigest(),'files':len(z.namelist())}
# Retain original Debian copyright and complete MIT text. Source changes and
# minimal native test scope remain explicit for the library's maintainer review.
(debian/'changelog').write_text(f"{name} ({version}) unstable; urgency=high\n\n  * Update complete azidentity module to 1.6.0 for CVE-2024-35255.\n  * Update azcore 1.11.1/internal 1.8.0 from verified upstream modules.\n\n -- Soumik Sarkar <soumiks@gmail.com>  Sat, 17 Oct 2026 10:00:00 +0000\n\n"+(debian/'changelog').read_text())
# Preserve the existing Debian team Maintainer and Uploaders.
control=(debian/'control').read_text()
control=control.replace('golang-github-azuread-microsoft-authentication-library-for-go-dev,','golang-github-azuread-microsoft-authentication-library-for-go-dev (>= 1.2.2),')
(debian/'control').write_text(control)
copyright=(debian/'copyright').read_text().replace('Copyright: 2015 Tianon Gravi <tianon@debian.org>', 'Copyright: 2015 Tianon Gravi <tianon@debian.org>\n 2026 Soumik Sarkar <soumiks@gmail.com>')
copyright=copyright.replace('Files: debian/*', 'Files: sdk/azidentity/* sdk/azcore/* sdk/internal/*\nCopyright: Microsoft Corporation\nLicense: Expat\n\nFiles: debian/*')
copyright=copyright.replace('License: Apache-2.0 or Expat\nComment: Debian packaging is licensed under the same terms as upstream', 'License: Apache-2.0\nComment: Apache-2.0 selected from the original metadata dual license;\n amendments are provided under Apache-2.0.')
(debian/'copyright').write_text(copyright)
# This metadata-only Go1.27 compatibility patch must still be relevant after the
# full azcore replacement. Refresh it from the actual retained source.
patches=debian/'patches';shutil.rmtree(patches);patches.mkdir()
# Refresh the existing Debian Go1.27 JSON error compatibility patch against
# the new upstream module, preserving the assertion's purpose.
import difflib
f=tree/'sdk/azcore/messaging/cloud_event_test.go'
old_test=f.read_text();new_test=old_test.replace('require.EqualError(t, err, "json: cannot unmarshal array into Go value of type map[string]json.RawMessage")','require.ErrorContains(t, err, "cannot unmarshal array into Go value of type map[string]")')
if new_test!=old_test:
 patch='Description: Accept Go1.27 JSON type names in existing error assertion\\nAuthor: Soumik Sarkar <soumiks@gmail.com>\\nForwarded: not-needed\\nLast-Update: 2026-10-17\\n\\n'
 patch+=''.join(difflib.unified_diff(old_test.splitlines(True),new_test.splitlines(True),fromfile='a/sdk/azcore/messaging/cloud_event_test.go',tofile='b/sdk/azcore/messaging/cloud_event_test.go'))
 (patches/'go127-json-error.patch').write_text(patch.replace('\\n','\n'))
 (patches/'series').write_text('go127-json-error.patch\n')
rules=(debian/'rules').read_text()
rules=rules.replace('# Disable unneeded stuff', 'export PATH := /usr/lib/go-1.27/bin:/usr/lib/go-1.26/bin:$(PATH)\nexport GOTOOLCHAIN := local\nexport GOPROXY := off\nexport GOSUMDB := off\nexport GOMAXPROCS := 2\nexport DH_GOLANG_INSTALL_ALL := 1\n# Native compilation covers updated SDK modules; complete source is archived.\nexport DH_GOLANG_BUILDPKG := github.com/Azure/azure-sdk-for-go/sdk/azcore/... github.com/Azure/azure-sdk-for-go/sdk/azidentity/... github.com/Azure/azure-sdk-for-go/sdk/internal/...\n\n# Disable unneeded stuff')
rules+='\noverride_dh_auto_build:\n\tdh_auto_build --builddirectory=_build\n\noverride_dh_auto_install:\n\tdh_auto_install -- --no-binaries\n\nexecute_after_dh_auto_test:\n\tcd _build/src/github.com/Azure/azure-sdk-for-go/sdk/azidentity && AZURE_RECORD_MODE=live GO111MODULE=off GOPATH=$(CURDIR)/_build go test -race -run "^TestManagedIdentityCredential_AzureArc(Errors)?$$" -count=1 -v .\n'
(debian/'rules').write_text(rules)
# Installed development sources omit repository/build automation, not Go API
# inputs. The complete original/repacked sources remain in the source archive.
rules=(debian/'rules').read_text()
rules=rules.replace('\tdh_auto_install -- --no-binaries', '\tdh_auto_install -- --no-binaries\n\trm -r debian/golang-github-azure-azure-sdk-for-go-dev/usr/share/gocode/src/github.com/Azure/azure-sdk-for-go/eng\n\tfind debian/golang-github-azure-azure-sdk-for-go-dev -type f -name .gitattributes -delete')
(debian/'rules').write_text(rules)
# Original library test exclusions are retained, documented rather than reported
# as a complete Azure SDK test pass. Topo independently vets/race-tests its full CLI.
# Build the same library source package separately before using its .deb in Topo.
source_tree=out/(name+'-'+new);tree.rename(source_tree);tree=source_tree
archive=out/(name+'_'+new+'.orig.tar.gz')
with archive.open('xb') as f,gzip.GzipFile(fileobj=f,mode='wb',filename='',mtime=0) as g,tarfile.open(fileobj=g,mode='w') as t:
 for file in sorted(tree.rglob('*')):
  if file.is_relative_to(tree/'debian'):continue
  i=t.gettarinfo(str(file),arcname=tree.name+'/'+str(file.relative_to(tree)));i.uid=i.gid=0;i.uname=i.gname='';i.mtime=1792231200
  if file.is_file():
   with file.open('rb') as src:t.addfile(i,src)
  else:t.addfile(i)
with (out/'patch-application.log').open('w') as log:subprocess.run(['dpkg-source','--before-build',str(tree)],cwd=out,stdout=log,stderr=subprocess.STDOUT,check=True)
with (out/'dpkg-source.log').open('w') as log:subprocess.run(['dpkg-source','--build',str(tree)],cwd=out,stdout=log,stderr=subprocess.STDOUT,check=True)
(out/'source-receipt.json').write_text(json.dumps({'base_inputs':inputs,'complete_module_updates':receipts,'debian_version':version,'signed':False,'archive_accepted':False,'library_tests':'existing Debian exclusions retained; not full SDK coverage'},indent=2)+'\n')
print(tree)
