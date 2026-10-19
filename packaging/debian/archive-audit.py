#!/usr/bin/env python3
"""Record candidates from APT's signature-verified indexes, not guessed providers."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def command(args):
    result = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    return {"exit_code": result.returncode, "output": result.stdout}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--map", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--installed", action="store_true")
    args = parser.parse_args()
    mapping = json.loads(args.map.read_text())
    rows = []
    for row in mapping["modules"]:
        result = command(["apt-cache", "policy", row["candidate_package"]])
        candidate = next((line.split(":", 1)[1].strip() for line in result["output"].splitlines() if line.strip().startswith("Candidate:")), None)
        if candidate == "(none)":
            candidate = None
        record = row | {"apt_candidate": candidate, "apt_policy": result}
        if args.installed:
            path = Path("/usr/share/gocode/src") / row["module"]
            record["exact_import_directory_exists"] = path.is_dir()
            record["exact_import_go_mod_exists"] = (path / "go.mod").is_file()
            record["installed_package"] = command(["dpkg-query", "-W", "-f=${Package} ${Version} ${source:Package} ${source:Version}\n", row["candidate_package"]])
            record["resolved_import_path"] = str(path.resolve()) if path.exists() else None
        rows.append(record)
    # APT verifies Release signatures and package hashes; save exact signed roots.
    roots = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in Path("/var/lib/apt/lists").glob("*InRelease")}
    report = {
        "os_release": Path("/etc/os-release").read_text(),
        "source_commit": mapping["source_commit"],
        "apt_inrelease_sha256": roots,
        "modules": rows,
        "toolchains": {name: command(["apt-cache", "policy", name]) for name in ("golang-go", "golang-1.26-go", "golang-1.27-go", "dh-golang", "dh-go")},
        "exact_topo_name": command(["apt-cache", "policy", "topo"]),
        "upload_ready": False,
        "limits": ["APT update must succeed without insecure/trust overrides before this script runs", "Candidate package versions do not establish nested Go module versions or API compatibility", "An import-root miss may need a documented distro module mapping, not a missing-package claim", "No acceptance, security equivalence or build qualification follows from availability"],
    }
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    if not args.installed:
        names = {r["candidate_package"] for r in rows if r["apt_candidate"]}
        for name in ("golang-1.26-go", "golang-1.27-go", "dh-go"):
            compiler = report["toolchains"][name]["output"]
            if "Candidate:" in compiler and "Candidate: (none)" not in compiler:
                names.add(name)
        for name in sorted(names):
            print(name)


if __name__ == "__main__":
    main()
