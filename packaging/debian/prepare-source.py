#!/usr/bin/env python3
"""Build an unsigned Debian source package from an upstream release tag.

The version comes from debian/changelog and the tarball from tag v<version>.
TOPO_UPSTREAM_REF overrides the tag, for CI runs before a release is tagged.
"""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[2]
CHANGELOG = ROOT / "packaging/debian/debian/changelog"
VERSION = subprocess.check_output(["dpkg-parsechangelog", "-l", str(CHANGELOG), "-S", "Version"], text=True).strip().rsplit("-", 1)[0]
UPSTREAM = os.environ.get("TOPO_UPSTREAM_REF") or "v" + VERSION


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    output = args.output.resolve()
    if output.exists():
        parser.error("output must not exist; existing artifacts are preserved")
    subprocess.run(["git", "cat-file", "-e", UPSTREAM + "^{commit}"], cwd=ROOT, check=True)
    output.mkdir(parents=True)
    archive = output / ("topo_" + VERSION + ".orig.tar.gz")
    epoch = subprocess.check_output(["git", "show", "-s", "--format=%ct", UPSTREAM], cwd=ROOT, text=True).strip()
    # git archive exports only tracked public files; private symlinks/notes are ignored.
    raw = subprocess.check_output(["git", "archive", "--format=tar", "--prefix=topo-" + VERSION + "/", UPSTREAM], cwd=ROOT)
    with archive.open("xb") as dest:
        with gzip.GzipFile(fileobj=dest, mode="wb", filename="", mtime=0) as compressed:
            compressed.write(raw)
    with tarfile.open(archive) as source:
        source.extractall(output, filter="data")
    tree = output / ("topo-" + VERSION)
    if (tree / "vendor").exists() or (tree / ".maintainer").exists():
        raise ValueError("unbundled public source required")
    shutil.copytree(ROOT / "packaging/debian/debian", tree / "debian", ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
    for service in ("worker", "agent"):
        if (tree / f"debian/topo.topo-{service}.service").read_bytes() != (tree / f"packaging/systemd/topo-{service}.package.service").read_bytes():
            raise ValueError("packaged service differs from reviewed upstream unit")
    env = os.environ | {"SOURCE_DATE_EPOCH": epoch}
    with (output / "dpkg-source.log").open("w") as log:
        subprocess.run(["dpkg-source", "--build", str(tree)], cwd=output, env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
    files = {p.name: {"bytes": p.stat().st_size, "sha256": hashlib.sha256(p.read_bytes()).hexdigest()} for p in output.iterdir() if p.is_file()}
    (output / "source-manifest.json").write_text(json.dumps({
        "upstream_ref": UPSTREAM,
        "upstream_commit": subprocess.check_output(["git", "rev-parse", UPSTREAM + "^{commit}"], cwd=ROOT, text=True).strip(), "upstream_version": VERSION,
        "packaging_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
        "signed": False, "upload_ready": False, "archive_accepted": False,
        "status": "review candidate; native qualification, signing and sponsor/archive review tracked separately",
        "files": files,
    }, indent=2) + "\n")
    print(tree)


if __name__ == "__main__":
    main()
