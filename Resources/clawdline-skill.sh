#!/bin/sh
# Serve the Clawdline agent guide that shipped with this build.
#
# Why this exists: a SKILL.md installed into ~/.claude/skills/ is a copy, and a copy never
# updates. Routes and fields move between releases, so the guide lives beside the app instead
# and the installed stub only says how to reach it. Reading it is a local file read: no running
# app, no network, no compiler, so it answers the same over SSH and while Clawdline is closed.
#
# Guides sit next to this script, which is true both inside Contents/Resources of the app bundle
# and inside Resources/ of a checkout, so one resolution covers both.

set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
guides="$here/skill-guides"

usage() {
    echo "usage: clawdline-skill.sh get <topic> | list" >&2
    exit 64
}

[ $# -ge 1 ] || usage

case "$1" in
list)
    [ -d "$guides" ] || { echo "clawdline-skill: no guides beside $0" >&2; exit 69; }
    for f in "$guides"/*.md; do
        [ -e "$f" ] || continue
        b=${f##*/}
        echo "${b%.md}"
    done
    ;;
get)
    [ $# -eq 2 ] || usage
    f="$guides/$2.md"
    if [ ! -f "$f" ]; then
        echo "clawdline-skill: no guide named '$2'. Try: $0 list" >&2
        exit 69
    fi
    cat "$f"
    ;;
*)
    usage
    ;;
esac
