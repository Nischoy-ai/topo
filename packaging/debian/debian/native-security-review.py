#!/usr/bin/env python3
"""Analyze exact native sources with unmodified govulncheck and honest versions.

Local Go replacements otherwise hide module identities from govulncheck. This
private, offline analysis proxy materializes the actual compiled source files;
it is not an upstream module mirror, build input, vendor bundle or publication.
Every package and compiled Go source byte is checked against the native tree.
"""
import concurrent.futures
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import urllib.request
import zipfile

build=Path(sys.argv[1]).resolve()
out=Path(sys.argv[2]).resolve();out.mkdir(parents=True,exist_ok=False)
root=build/'src/github.com/Nischoy-ai/topo'
records=json.loads((build.parent/'debian/topo/usr/share/doc/topo/linked-licenses/module-sources.json').read_text())
index_bytes=urllib.request.urlopen('https://vuln.go.dev/index/modules.json',timeout=60).read()
index={r['path']:r for r in json.loads(index_bytes)}
actual={}
for module in sorted(records.keys() & index.keys()):
    record=records[module]
    if module.startswith('github.com/Azure/azure-sdk-for-go/'):
        candidates=[c for c in record['version_constant_candidates'] if c['path']=='version.go']
        versions={v if v.startswith('v') else 'v'+v for c in candidates for v in c['values'] if re.fullmatch(r'v?\d+\.\d+\.\d+',v)}
        if len(versions)!=1:raise ValueError(f'Unresolved actual SDK version: {module}: {candidates}')
        actual[module]=versions.pop()
    else:
        version=record['identity'].split('\t')[3].split(':')[-1]
        if '+really' in version or '~' in version:raise ValueError('Ambiguous actual Debian version: '+version)
        match=re.match(r'(\d+\.\d+\.\d+)(?:[+-]|$)',version)
        if not match:raise ValueError(f'Unresolved actual version: {module}: {version}')
        actual[module]='v'+match[1]
(out/'actual-vulnerability-module-versions.json').write_text(json.dumps(actual,indent=2)+'\n')

def packages(cwd,env):
    raw=subprocess.check_output(['go','list','-deps','-json','./...'],cwd=cwd,env=env,text=True)
    result=[];decoder=json.JSONDecoder()
    while raw.strip():
        item,end=decoder.raw_decode(raw.lstrip());result.append(item);raw=raw.lstrip()[end:]
    return result
original_env=os.environ|{'GOWORK':str(build/'go.work'),'GOPROXY':'off','GOSUMDB':'off','GOTOOLCHAIN':'local'}
original=packages(root,original_env)
main_module='github.com/Nischoy-ai/topo'
modules={r['Module']['Path']:r['Module'] for r in original if r.get('Module') and r['Module']['Path']!=main_module}
versions={m:actual.get(m,r['Version']) for m,r in modules.items()}
# For modules without any record in this frozen official DB, a requirement
# label does not affect vulnerability filtering. Do not call it an actual
# version. Any newly listed module requires an actual-version mapping above.
labels={m:{'version':v,'basis':'actual Debian source identity' if m in actual else 'requirement label; no records in this DB snapshot'} for m,v in versions.items()}
(out/'analysis-module-labels.json').write_text(json.dumps(labels,indent=2)+'\n')

def gomod(module,language):
    requirements='\n'.join('\t'+m+' '+v for m,v in sorted(versions.items()) if m!=module)
    return f'module {module}\n\ngo {language or "1.16"}\n\nrequire (\n{requirements}\n)\n'

def escaped(value):
    return ''.join('!'+c.lower() if c.isupper() else c for c in value)

files={m:{} for m in modules}
for package in original:
    module=package.get('Module',{})
    name=module.get('Path')
    if name not in files:continue
    directory=Path(package['Dir'])
    relative=directory.resolve().relative_to(Path(module['Dir']).resolve())
    for field in ['GoFiles','CgoFiles','CFiles','CXXFiles','MFiles','HFiles','FFiles','SFiles','SwigFiles','SwigCXXFiles','SysoFiles','EmbedFiles']:
        for filename in package.get(field,[]):
            files[name][str(relative/filename)]=(directory/filename).read_bytes()
proxy=out/'native-analysis-proxy'
receipts={}
for module,entries in files.items():
    version=versions[module]
    entries['go.mod']=gomod(module,modules[module].get('GoVersion')).encode()
    target=proxy/escaped(module)/'@v';target.mkdir(parents=True)
    prefix=module+'@'+version+'/'
    buffer=io.BytesIO()
    with zipfile.ZipFile(buffer,'w',compression=zipfile.ZIP_DEFLATED) as archive:
        for name,data in sorted(entries.items()):archive.writestr(prefix+name,data)
    data=buffer.getvalue()
    (target/(version+'.zip')).write_bytes(data)
    (target/(version+'.mod')).write_bytes(entries['go.mod'])
    (target/(version+'.info')).write_text(json.dumps({'Version':version,'Time':'2026-10-17T00:00:00Z'}))
    (target/'list').write_text(version+'\n')
    receipts[module]={'sha256':hashlib.sha256(data).hexdigest(),'files':len(entries),'native_source_only':True}
main=out/'source';shutil.copytree(root,main)
(main/'go.mod').write_text(gomod(main_module,'1.26.0'))
(main/'go.sum').unlink(missing_ok=True)
# Freeze the official DB so a newly added module cannot race actual-version
# classification. The analyzer itself and vulnerability records are unmodified.
database=out/'vuln-db';(database/'index').mkdir(parents=True);(database/'ID').mkdir()
(database/'index/modules.json').write_bytes(index_bytes)
for name in ['db','vulns']:
    (database/'index'/f'{name}.json').write_bytes(urllib.request.urlopen(f'https://vuln.go.dev/index/{name}.json',timeout=60).read())
ids={v['id'] for m in set(modules)|{'stdlib'} for v in index.get(m,{}).get('vulns',[])}
def fetch(identifier):
    data=urllib.request.urlopen(f'https://vuln.go.dev/ID/{identifier}.json',timeout=60).read()
    (database/'ID'/f'{identifier}.json').write_bytes(data)
    return identifier,hashlib.sha256(data).hexdigest()
with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:db_receipts=dict(pool.map(fetch,sorted(ids)))
env=os.environ|{'GOWORK':'off','GOPROXY':proxy.as_uri(),'GOSUMDB':'off','GOTOOLCHAIN':'local','GOMODCACHE':str(out/'native-modules'),'GOCACHE':str(out/'go-build'),'GOFLAGS':'-mod=mod'}
overlay=packages(main,env)
prior={r['ImportPath']:r for r in original}
if set(prior)!={r['ImportPath'] for r in overlay}:raise ValueError('Analysis changed native package closure')
count=0
for package in overlay:
    module=package.get('Module',{})
    if module.get('Replace'):raise ValueError('Local replacement would hide vulnerability identity')
    if module.get('Path') in actual and module.get('Version')!=actual[module['Path']]:raise ValueError('Actual-version label mismatch')
    old=prior[package['ImportPath']]
    for field in ['GoFiles','CgoFiles']:
        if package.get(field,[])!=old.get(field,[]):raise ValueError('Compiled Go file set changed')
        for name in package.get(field,[]):
            if (Path(package['Dir'])/name).read_bytes()!=(Path(old['Dir'])/name).read_bytes():raise ValueError('Analysis changed compiled Go source')
            count+=1
(out/'overlay-verification.json').write_text(json.dumps({'identical_go_source_files':count,'identical_package_count':len(prior),'actual_vulnerability_module_versions':actual,'module_index_sha256':hashlib.sha256(index_bytes).hexdigest(),'unmodified_official_vulnerability_records':db_receipts,'private_native_proxy_receipts':receipts,'not_upstream_checksum_equivalence':True},indent=2)+'\n')
tool_env=os.environ|{'GOWORK':'off','GOTOOLCHAIN':'local','GOBIN':str(out/'bin'),'GOMODCACHE':str(out/'tool-modules'),'GOPATH':str(out/'tool-cache'),'GOPROXY':'https://proxy.golang.org','GOSUMDB':'sum.golang.org'}
subprocess.run(['go','install','golang.org/x/vuln/cmd/govulncheck@v1.7.0'],cwd=out,env=tool_env,check=True)
with (out/'govulncheck.log').open('w') as log:
    result=subprocess.run([str(out/'bin/govulncheck'),'-db',database.as_uri(),'./...'],cwd=main,env=env,stdout=log,stderr=subprocess.STDOUT)
print((out/'govulncheck.log').read_text())
print(json.dumps({'govulncheck_exit':result.returncode,'identical_go_source_files':count,'actual_versions':actual},indent=2))
sys.exit(result.returncode)
