#!/usr/bin/env python3
"""Retain installed Debian copyright documents for actual linked Go sources."""
import hashlib
import json
import os
from pathlib import Path
import shutil
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
    retain(owner((directory / names[0]).resolve()))
(destination / "manifest.json").write_text(json.dumps(records, indent=2) + "\n")
print(f"Retained complete copyright documents for {len(records)} installed packages")
