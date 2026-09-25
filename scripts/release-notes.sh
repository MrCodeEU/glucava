#!/bin/sh
# Print the CHANGELOG section for a version: scripts/release-notes.sh v0.1.0
# Fails if there is none, so a release cannot go out without notes.
set -eu
ver="${1#v}"
notes=$(awk -v v="$ver" '
  $0 ~ "^## \\[" v "\\]" { on = 1; next }
  on && /^## \[/ { exit }
  on { print }
' "$(dirname "$0")/../CHANGELOG.md")
if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
	echo "no CHANGELOG.md section for $ver" >&2
	exit 1
fi
printf '%s\n' "$notes"
