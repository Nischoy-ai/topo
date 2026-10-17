#!/usr/bin/env python3
"""Retain installed Debian copyright documents for actual linked Go sources."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import re
import subprocess
import sys

build = Path(sys.argv[1]).resolve()
destination = Path(sys.argv[2])
destination.mkdir(parents=True, exist_ok=False)
env = os.environ | {"GOWORK": str(build / "go.work"), "GOPATH": str(build)}
raw = subprocess.check_output(["go", "list", "-deps", "-json", "github.com/Nischoy-ai/topo/cmd/topo"], cwd=build, env=env, text=True)
decoder = json.JSONDecoder()
packages = []
while raw.strip():
    item, end = decoder.raw_decode(raw.lstrip())
    packages.append(item)
    raw = raw.lstrip()[end:]
repairs = json.loads((build / "legacy-module-metadata.json").read_text())
records = {}
modules = {}

def owner(path):
    result = subprocess.check_output(["dpkg-query", "-S", str(path)], text=True)
    return result.split(": ", 1)[0]

def retain(package):
    if package in records:
        return
    identity = subprocess.check_output(["dpkg-query", "-W", "-f=${binary:Package}\t${Version}\t${source:Package}\t${source:Version}", package], text=True)
    doc = Path("/usr/share/doc") / package.split(":")[0] / "copyright"
    data = doc.read_bytes()  # Missing documents fail the build.
    target = package.replace(":", "_") + ".copyright"
    (destination / target).write_bytes(data)
    records[package] = {"identity": identity, "document": target,
                        "sha256": hashlib.sha256(data).hexdigest()}

# The standard library/runtime is statically included too.
retain(owner(Path(shutil.which("go")).resolve()))
for item in packages:
    if item.get("Standard") or item["ImportPath"].startswith("github.com/Nischoy-ai/topo/"):
        continue
    directory = Path(item["Dir"]).resolve()
    for repair in repairs:
        private = build / repair["build_copy"]
        if directory.is_relative_to(private):
            directory = Path(repair["installed_source"]) / directory.relative_to(private)
            break
    names = item.get("GoFiles", []) + item.get("CgoFiles", [])
    if not names:
        raise ValueError(f"No attributable source files for {item['ImportPath']}")
    package = owner((directory / names[0]).resolve())
    retain(package)
    module = item.get("Module", {})
    module_name = module.get("Path")
    if module_name and module_name not in modules:
        module_dir = Path(module["Dir"])
        candidates = []
        # Version constants for nested Azure/AWS modules cannot be inferred
        # from the Debian monorepo source-package version.
        for pattern in ("*version*.go", "*/*version*.go", "*/*/*version*.go", "*metadata*.go", "*/*metadata*.go", "*/*/*metadata*.go"):
            for source in module_dir.glob(pattern):
                if source.is_file() and source.stat().st_size <= 65536:
                    text = source.read_text(errors="strict")
                    matches = re.findall(r'(?:[Vv]ersion|[Mm]oduleVersion|[Ss][Dd][Kk]Version)\s*=\s*"([^"\n]+)"', text)
                    if matches:
                        candidates.append({"path":str(source.relative_to(module_dir)), "values":matches, "sha256":hashlib.sha256(source.read_bytes()).hexdigest()})
        modules[module_name] = {"requested_version":module.get("Version"), "debian_owner":package,
            "identity":records[package]["identity"], "go_mod":(module_dir/"go.mod").read_text(),
            "version_constant_candidates":candidates,
            "warning":"Requested Go versions describe requirements, not the version of replaced Debian source."}
(destination / "module-sources.json").write_text(json.dumps(modules, indent=2) + "\n")
(destination / "manifest.json").write_text(json.dumps(records, indent=2) + "\n")
print(f"Retained complete copyright documents for {len(records)} installed packages")
