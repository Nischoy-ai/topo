#!/bin/sh
# Rejects GitHub Actions workflow steps that interpolate an actor-influenced
# expression (workflow_dispatch inputs, event payload fields, and similar)
# directly into a `run:` script body instead of passing it through `env:`.
# Raw interpolation splices untrusted text into the generated shell/pwsh
# script before it runs, which is a script-injection vector even when a
# same-workflow validation step happens to constrain the value today —
# TSR-2026-003. `env:` indirection keeps the expression as a single
# argument/variable, which is safe regardless of its contents.
set -eu

status=0

for workflow in .github/workflows/*.yml .github/workflows/*.yaml; do
	[ -e "$workflow" ] || continue
	matches=$(awk '
		/^[[:space:]]*run:/ {
			match($0, /[^[:space:]]/)
			run_indent = RSTART
			in_run = 1
			next
		}
		in_run {
			if ($0 !~ /^[[:space:]]*$/) {
				match($0, /[^[:space:]]/)
				if (RSTART <= run_indent) {
					in_run = 0
				}
			}
		}
		in_run && /\$\{\{[[:space:]]*(inputs|github\.event)\./ {
			print FILENAME ":" FNR ": " $0
		}
	' "$workflow")
	if [ -n "$matches" ]; then
		echo "$matches" >&2
		status=1
	fi
done

if [ "$status" -ne 0 ]; then
	echo "raw inputs/event interpolation found in a run: step; route through env: instead (see TSR-2026-003 in docs/security-review.md)" >&2
fi

exit "$status"
