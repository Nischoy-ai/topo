#!/usr/bin/env python3
"""Read-only mapping of declared Go modules to a Debian package-index snapshot.

This is an availability audit, not dependency resolution or a build test.
Download Packages.xz and Contents-all.gz from the same suite before running.
"""

import argparse
import datetime
import gzip
import hashlib
import json
import lzma
import pathlib
import re


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go-mod", type=pathlib.Path, required=True)
    parser.add_argument("--packages", type=pathlib.Path, required=True)
    parser.add_argument("--contents", type=pathlib.Path, required=True)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--source-commit", required=True)
    args = parser.parse_args()
    modules = []
    in_require = False
    for line in args.go_mod.read_text().splitlines():
        line = line.split("//", 1)[0].strip()
        if line == "require (":
            in_require = True
            continue
        if line == ")":
            in_require = False
            continue
        requirement = line if in_require else line.removeprefix("require ")
        if in_require or line.startswith("require "):
            match = re.fullmatch(r"([^\s]+)\s+(v[^\s]+)", requirement)
            if not match:
                raise ValueError("unsupported require syntax: " + line)
            modules.append({"module": match[1], "required_version": match[2]})

    packages = {}
    with lzma.open(args.packages, "rt") as source:
        for paragraph in source.read().split("\n\n"):
            fields = dict(line.split(": ", 1) for line in paragraph.splitlines()
                          if ": " in line and not line.startswith(" "))
            if "Package" in fields:
                packages[fields["Package"]] = {
                    key: fields.get(key) for key in
                    ("Package", "Version", "Source", "Filename", "SHA256")
                }

    # Module-root go.mod identifies a provider without guessing Debian names.
    # A missing entry is only an index miss; it does not prove source absence.
    wanted = {"usr/share/gocode/src/" + row["module"] + "/go.mod": row
              for row in modules}
    for row in modules:
        row["providers"] = []
    with gzip.open(args.contents, "rt") as source:
        for line in source:
            fields = line.split()
            if len(fields) != 2 or fields[0] not in wanted:
                continue
            for provider in fields[1].split(","):
                name = provider.rsplit("/", 1)[-1]
                wanted[fields[0]]["providers"].append(
                    packages.get(name, {"Package": name, "Version": None}))

    # Older Debian packages may omit go.mod or strip a semantic-major path.
    # Record source-file candidates separately; stripped paths need import review.
    fallback = {}
    for row in modules:
        row["source_file_candidates"] = []
        if not row["providers"]:
            root = "usr/share/gocode/src/" + row["module"]
            fallback[root] = (row, "exact_import_root")
            stripped = re.sub(r"/v[2-9][0-9]*$", "", root)
            if stripped != root:
                fallback[stripped] = (row, "major_suffix_stripped_needs_review")
    seen = set()
    with gzip.open(args.contents, "rt") as source:
        for line in source:
            fields = line.split()
            if len(fields) != 2 or not fields[0].endswith(".go"):
                continue
            parent = fields[0].rsplit("/", 1)[0]
            while parent.startswith("usr/share/gocode/src/"):
                if parent in fallback:
                    row, kind = fallback[parent]
                    for provider in fields[1].split(","):
                        name = provider.rsplit("/", 1)[-1]
                        identity = (row["module"], name, kind)
                        if identity not in seen:
                            seen.add(identity)
                            row["source_file_candidates"].append({
                                "match_kind": kind, "example_path": fields[0],
                                "provider": packages.get(name, {"Package": name})})
                    break
                parent = parent.rsplit("/", 1)[0]

    result = {
        "generated_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "source_commit": args.source_commit,
        "go_mod_sha256": sha256(args.go_mod),
        "index_base_url": args.base_url,
        "inputs": {"Packages.xz": sha256(args.packages),
                   "Contents-all.gz": sha256(args.contents)},
        "limits": ["Unsigned HTTPS metadata snapshot; not an apt trust verification",
                   "amd64 Packages plus architecture-independent source Contents only",
                   "Indexes may be generated at different times",
                   "Provider package version is not a multi-module upstream version",
                   "No build, API compatibility, license or acceptance claim"],
        "exact_topo_in_packages": packages.get("topo"),
        "toolchains": {name: packages.get(name) for name in
                       ("golang-go", "golang-1.26-go")},
        "modules": modules,
    }
    print(json.dumps(result, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
