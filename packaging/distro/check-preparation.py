#!/usr/bin/env python3
"""Validate preparation metadata; this does not build or submit a package."""

import hashlib
import json
import pathlib
import re


def main():
    root = pathlib.Path(__file__).resolve().parent
    debian = json.loads((root / "debian-sid-2026-10-15.json").read_text())
    fedora = json.loads((root / "fedora-rawhide-2026-10-15.json").read_text())
    fields = json.loads((root / "submission-fields.json").read_text())
    go_mod = root.parent.parent / "go.mod"
    if hashlib.sha256(go_mod.read_bytes()).hexdigest() != debian["go_mod_sha256"]:
        raise ValueError("go.mod changed: regenerate and review the dependency audit")
    modules = debian["modules"]
    if len({row["module"] for row in modules}) != len(modules):
        raise ValueError("duplicate module observations")
    if fedora["exact_topo_found"] != any(row["name"] == "topo"
                                         for row in fedora["packages"]):
        raise ValueError("inconsistent Fedora package-name observation")
    for value in debian["inputs"].values():
        if not re.fullmatch("[0-9a-f]{64}", value):
            raise ValueError("invalid snapshot digest")
    spec = (root / "topo.spec.in").read_text()
    tokens = sorted(set(re.findall(r"@[A-Z_]+@", spec)))
    if len(tokens) != 4:
        raise ValueError("review expected source snapshot/changelog tokens")
    for guard in ("golang = 1.26.9", "GOTOOLCHAIN=local", "GOPROXY=off",
                  "GOSUMDB=off", "GOFLAGS=-mod=vendor"):
        if guard not in spec:
            raise ValueError("missing build guard: " + guard)
    print(json.dumps({
        "preparation_metadata": "PASS",
        "declared_modules": len(modules),
        "go_mod_providers": sum(bool(row["providers"]) for row in modules),
        "additional_source_candidates": sum(bool(row["source_file_candidates"])
                                            for row in modules),
        "unresolved_module_roots": [row["module"] for row in modules
                                    if not row["providers"]
                                    and not row["source_file_candidates"]],
        "unresolved_spec_tokens": tokens,
        "public_maintainer_identity_complete": bool(
            fields["public_maintainer_name"] and fields["public_maintainer_email"]),
        "submission_ready": False,
        "distro_builds": "NOT RUN",
        "external_submissions": "NONE",
        "limits": "Metadata consistency only; see README for open packaging gates",
    }, indent=2))


if __name__ == "__main__":
    main()
