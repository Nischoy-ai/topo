#!/usr/bin/env python3
"""Prepare a source-pinned, unsigned Fedora review snapshot; never upload it."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
EXPECTED_LICENSE = (
    "Apache-2.0 AND BSD-2-Clause AND BSD-3-Clause AND CC-BY-2.5 AND CC0-1.0 "
    "AND ISC AND LicenseRef-Fedora-Public-Domain AND Linux-man-pages-copyleft "
    "AND MIT AND SunPro AND bcrypt-Solar-Designer"
)


def run(argv, *, cwd=ROOT, env=None):
    return subprocess.check_output(argv, cwd=cwd, env=env, text=True).strip()


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--commit", required=True, help="Full public upstream commit")
    parser.add_argument("--output", type=Path, required=True, help="New output directory")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.commit):
        parser.error("--commit must be a full lowercase Git SHA")
    commit = run(["git", "rev-parse", "--verify", args.commit + "^{commit}"])
    if commit != args.commit:
        raise ValueError("source commit did not resolve exactly")
    output = args.output.resolve()
    if output.exists():
        parser.error("--output must not exist; existing artifacts are preserved")
    version = run(["go", "env", "GOVERSION"])
    if version not in ("go1.26.9", "go1.26.9-X:nodwarf5"):
        raise ValueError("exact Go 1.26.9 or its qualified Fedora nodwarf5 build required")
    output.mkdir(parents=True)
    sources = output / "SOURCES"
    specs = output / "SPECS"
    reports = output / "reports"
    for path in (sources, specs, reports):
        path.mkdir()
    filename = "topo-" + commit + ".tar.gz"
    archive = sources / filename
    url = "https://codeload.github.com/Nischoy-ai/topo/tar.gz/" + commit
    request = urllib.request.Request(url, headers={"User-Agent": "Topo-Fedora-source-preparation/1"})
    with urllib.request.urlopen(request, timeout=60) as response, archive.open("xb") as stream:
        total = 0
        while chunk := response.read(1024 * 1024):
            total += len(chunk)
            if total > 64 * 1024 * 1024:
                raise ValueError("source archive exceeds bound")
            stream.write(chunk)
    work = output / "source"
    work.mkdir()
    with tarfile.open(archive) as tar:
        if sum(member.size for member in tar.getmembers()) > 128 * 1024 * 1024:
            raise ValueError("expanded source exceeds bound")
        tar.extractall(work, filter="data")
    children = list(work.iterdir())
    if len(children) != 1 or children[0].name != "topo-" + commit:
        raise ValueError("unexpected upstream source layout")
    source = children[0]
    # GitHub tar bytes may differ from git archive metadata; tracked source
    # content must still match the exact public Git tree, including symlinks.
    tree = subprocess.check_output(["git", "ls-tree", "-rz", commit], cwd=ROOT)
    checked = 0
    for entry in tree.split(b"\0"):
        if not entry:
            continue
        metadata, rawpath = entry.split(b"\t", 1)
        mode, kind, blob = metadata.decode().split()
        relpath = rawpath.decode()
        if kind != "blob":
            raise ValueError("submodules are not supported source inputs")
        expected = subprocess.check_output(["git", "cat-file", "blob", blob], cwd=ROOT)
        actual = source / relpath
        content = os.readlink(actual).encode() if mode == "120000" else actual.read_bytes()
        if content != expected:
            raise ValueError("upstream source differs from Git: " + relpath)
        checked += 1
    epoch = int(run(["git", "show", "-s", "--format=%ct", commit]))
    date = datetime.datetime.fromtimestamp(epoch, datetime.timezone.utc)
    config = source / "packaging/distro/go-vendor-tools.toml"
    shutil.copyfile(config, sources / "go-vendor-tools.toml")
    shutil.copyfile(source / "packaging/distro/topo.sysusers", sources / "topo.sysusers")
    env = os.environ.copy()
    env.update(
        GOTOOLCHAIN="local",
        GOMODCACHE=str(output / "modcache"),
        GOCACHE=str(output / "gocache"),
        GOPROXY="https://proxy.golang.org",
        GOSUMDB="sum.golang.org",
        SOURCE_DATE_EPOCH=str(epoch),
    )
    original = {name: (source / name).read_bytes() for name in ("go.mod", "go.sum")}
    vendor = sources / ("topo-" + commit + "-vendor.tar.bz2")
    with (reports / "vendor.log").open("w") as log:
        subprocess.run(
            ["go_vendor_archive", "create", "--config", str(config), "--no-use-module-proxy",
             "--output", str(vendor), str(source)],
            cwd=source, env=env, stdout=log, stderr=subprocess.STDOUT, check=True,
        )
    if any((source / name).read_bytes() != content for name, content in original.items()):
        raise ValueError("module files drifted during vendor preparation")
    with (reports / "licenses.log").open("w") as log:
        subprocess.run(
            ["go_vendor_license", "-d", "askalono", "--config", str(config),
             "--directory", str(source), "report", "all", "--no-prompt",
             "--autofill", "off", "--write-json", str(reports / "licenses.json")],
            cwd=source, env=env, stdout=log, stderr=subprocess.STDOUT, check=True,
        )
    licenses = json.loads((reports / "licenses.json").read_text())
    if (licenses["license_expression"] != EXPECTED_LICENSE
            or licenses["undetected_licenses"] or licenses["unknown_license_keys"]
            or licenses["unmatched_manual_licenses"]):
        raise ValueError("license review differs from pinned scope")
    template = (source / "packaging/distro/topo.spec.in").read_text()
    for key, value in {
        "@UPSTREAM_COMMIT@": commit,
        "@SNAPSHOT_DATE@": date.strftime("%Y%m%d"),
        "@CHANGELOG_DATE@": date.strftime("%a %b %d %Y"),
        "@SHORT_COMMIT@": commit[:7],
    }.items():
        template = template.replace(key, value)
    if re.search(r"@[A-Z_]+@", template):
        raise ValueError("unresolved spec input")
    (specs / "topo.spec").write_text(template)
    manifest = {
        "status": "unsigned_review_snapshot",
        "source_commit": commit,
        "source_url": url,
        "source_date_epoch": epoch,
        "tracked_source_files_verified": checked,
        "go_version": version,
        "module_files_unchanged": True,
        "license_expression": EXPECTED_LICENSE,
        "undetected_licenses": [],
        "artifacts": {
            str(path.relative_to(output)): {"sha256": digest(path), "bytes": path.stat().st_size}
            for path in sorted([*sources.iterdir(), *specs.iterdir()])
        },
        "external_submission": "NONE",
    }
    (reports / "source-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(json.dumps(manifest, indent=2))


if __name__ == "__main__":
    main()
