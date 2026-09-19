#!/bin/bash
# Fail on anything this repository would publish that belongs to a person
# rather than to the project: a real home directory, a real task or session id,
# the private cloud repository, a credential, an email address.
#
# The rules are Go (internal/domain/privacy) because they need more than a
# pattern — "is this UUID one a test made up", "is this %25 an escaped pane id"
# — and because a rule is only trusted once it has been seen to fail on the
# thing it is about, which its tests do. This script is the standalone entry,
# run like tools/check-legacy-css.sh.
#
# Three answers: 0 clean, 1 a finding (each printed as file:line: rule: match),
# 2 could not check. Arguments after -- are git pathspecs; -rules explains.
set -uo pipefail
cd "$(dirname "$0")/.."

# Built rather than `go run`, which reports every non-zero exit as 1 and would
# turn "could not check" into "found something".
bin=$(mktemp -d "${TMPDIR:-/tmp}/check-private.XXXXXX") || exit 2
trap 'rm -rf "$bin"' EXIT
go build -o "$bin/check-private" ./tools/check-private || { echo "cannot check: build failed" >&2; exit 2; }
"$bin/check-private" "$@"
