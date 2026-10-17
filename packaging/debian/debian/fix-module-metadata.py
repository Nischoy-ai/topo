#!/usr/bin/env python3
"""Add module metadata for installed legacy Go sources in a private build copy.

Never modifies /usr/share/gocode or invents missing dependency source. dh-go
1.69 can emit local replacements for legacy installed directories without a
go.mod. Go modules require that file even for graph-only dependencies.
"""
import hashlib
import json
from pathlib import Path
import re
import shutil
import sys

build = Path(sys.argv[1]).resolve()
work = build / "go.work"
repairs = []

def repair(match):
    module, relative = match.groups()
    source = (build / relative).resolve()
    if (source / "go.mod").is_file():
        return match.group(0)
    if not source.is_dir() or not any(source.rglob("*.go")):
        raise ValueError(f"Missing source for local module {module}: {source}")
    destination = build / "legacy-modules" / hashlib.sha256(module.encode()).hexdigest()[:16]
    if destination.exists():
        raise ValueError(f"Refusing to replace existing directory: {destination}")
    shutil.copytree(source, destination)
    (destination / "go.mod").write_text(f"module {module}\n")
    repairs.append({"module": module, "installed_source": str(source),
                    "build_copy": str(destination.relative_to(build))})
    return f"replace {module} => ./{destination.relative_to(build)}"

text = re.sub(r"^replace (\S+) => (\./\S+)$", repair, work.read_text(), flags=re.M)
work.write_text(text)
(build / "legacy-module-metadata.json").write_text(json.dumps(repairs, indent=2) + "\n")
print(json.dumps(repairs, indent=2))
