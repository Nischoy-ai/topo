#!/bin/bash
# Disposable CI only. Records blockers; this audit is NOT a package acceptance gate.
set -euo pipefail
out=/tmp/topo-debian-results
mkdir -p "$out"
python3 packaging/debian/archive-audit.py --map packaging/debian/dependency-map.json --output "$out/apt-candidates.json" > "$out/available-packages.txt"
mapfile -t candidates < "$out/available-packages.txt"
set +e
apt-get install -y --no-install-recommends "${candidates[@]}" > "$out/dependency-install.log" 2>&1
install_status=$?
set -e
python3 packaging/debian/archive-audit.py --map packaging/debian/dependency-map.json --output "$out/installed-providers.json" --installed
printf '%s\n' "$install_status" > "$out/dependency-install.exit"
dpkg-query -W > "$out/installed-packages.txt"
PATH=/usr/lib/go-1.27/bin:/usr/lib/go-1.26/bin:$PATH go version > "$out/compiler.txt"
PATH=/usr/lib/go-1.27/bin:/usr/lib/go-1.26/bin:$PATH go env >> "$out/compiler.txt"
# Native packaging tools and original public upstream tree, no Fedora vendor data.
python3 packaging/debian/prepare-source.py --output "$out/source" > "$out/source-tree.txt"
tree=$(cat "$out/source-tree.txt")
cp /var/lib/apt/lists/*InRelease "$out/"
set +e
(cd "$tree" && dpkg-checkbuilddeps) > "$out/checkbuilddeps.log" 2>&1
deps_status=$?
licensecheck --recursive --copyright --machine "$tree" > "$out/source-licensecheck.log" 2>&1
license_status=$?
lintian "$out/source/"*.dsc > "$out/lintian-source.log" 2>&1
lintian_status=$?
# Network namespace denies all external traffic. GOPROXY/GOSUMDB are also off.
# -d is diagnostic only: exposes API/import failures beyond missing Build-Depends.
# It never makes an upload-ready result, even if an incidental build succeeds.
(cd "$tree" && unshare --net -- sh -ec 'ip link set lo up; exec dpkg-buildpackage --no-sign -b -d') > "$out/diagnostic-build.log" 2>&1
build_status=$?
cp "$tree/_build/legacy-module-metadata.json" "$out/" 2>/dev/null || true
cp "$tree/_build/go.work" "$out/go-work.txt" 2>/dev/null || true
# Keep the actual package/import graph, including errors, for dependency decisions.
(cd "$tree/_build" && unshare --net -- env PATH="/usr/lib/go-1.27/bin:/usr/lib/go-1.26/bin:$PATH" GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go list -deps -e -json github.com/Nischoy-ai/topo/cmd/topo) > "$out/import-graph.json" 2> "$out/import-graph.log"
set -e
printf '%s\n' "$deps_status" > "$out/checkbuilddeps.exit"
printf '%s\n' "$license_status" > "$out/source-licensecheck.exit"
printf '%s\n' "$lintian_status" > "$out/lintian-source.exit"
printf '%s\n' "$build_status" > "$out/diagnostic-build.exit"
python3 - "$out" <<'PY'
import json,sys
from pathlib import Path
p=Path(sys.argv[1]); a=json.loads((p/'installed-providers.json').read_text())
status={'upload_ready':False,'archive_accepted':False,'source_package_signed':False,
 'dependency_install_exit':int((p/'dependency-install.exit').read_text()),
 'checkbuilddeps_exit':int((p/'checkbuilddeps.exit').read_text()),
 'diagnostic_offline_build_exit':int((p/'diagnostic-build.exit').read_text()),
 'source_lintian_exit':int((p/'lintian-source.exit').read_text()),
 'source_license_scan_exit':int((p/'source-licensecheck.exit').read_text()),
 'missing_apt_candidates':[r['candidate_package'] for r in a['modules'] if not r['apt_candidate']],
 'missing_exact_import_roots':[r['module'] for r in a['modules'] if not r['exact_import_directory_exists']],
 'remaining_gates':['dependency/module-version closure','DEP-5 copyright review','clean build without -d','unit/race/vulnerability tests on archive libraries','lintian/autopkgtest/lifecycle','owner signing key and sponsor review'],
 'note':'Audit job success means evidence was collected. Diagnostic -d build is not clean build qualification.'}
(p/'STATUS.json').write_text(json.dumps(status,indent=2)+'\n'); print(json.dumps(status,indent=2))
PY
