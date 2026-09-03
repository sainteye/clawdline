#!/bin/sh
# Point this checkout's `core.hooksPath` at the tracked hooks in `tools/git-hooks`.
#
# Run it as often as you like: installing twice is a no-op that says so. It never overwrites a
# `core.hooksPath` that points somewhere else — it reports that and stops, because the other
# value belongs to somebody and silently taking it is the same class of mistake the hook it
# installs exists to prevent.
#
# What gets installed is documented in docs/shared-tree-guard.md.

set -eu

TARGET_REL="tools/git-hooks"

top=$(git rev-parse --show-toplevel 2>/dev/null) || {
    echo "install-git-hooks: not inside a git checkout" >&2
    exit 1
}
cd "$top"

if [ ! -f "$TARGET_REL/pre-commit" ]; then
    echo "install-git-hooks: $top/$TARGET_REL/pre-commit does not exist — nothing to install" >&2
    exit 1
fi

# `git config --get` reports the effective value, wherever it was set. A hooksPath inherited from
# ~/.gitconfig is somebody else's decision just as much as a local one, so both are reported rather
# than replaced.
current=$(git config --get core.hooksPath 2>/dev/null || true)

resolve() {
    # core.hooksPath is taken relative to the top of the working tree when it is not absolute.
    case "$1" in
        /*) printf '%s\n' "$1" ;;
        *) printf '%s\n' "$top/$1" ;;
    esac
}

if [ -n "$current" ]; then
    if [ "$(resolve "$current")" = "$(resolve "$TARGET_REL")" ]; then
        echo "install-git-hooks: already installed — core.hooksPath = $current"
    else
        scope=$(git config --show-origin --get core.hooksPath 2>/dev/null | cut -f1 || true)
        echo "install-git-hooks: core.hooksPath is already set to something else. Leaving it alone." >&2
        echo "                   value:  $current" >&2
        [ -n "$scope" ] && echo "                   set in: $scope" >&2
        echo "                   That path belongs to whoever set it; this script will not take it." >&2
        echo "                   To hand it over deliberately:" >&2
        echo "                       git config core.hooksPath $TARGET_REL" >&2
        exit 1
    fi
else
    git config core.hooksPath "$TARGET_REL"
    echo "install-git-hooks: core.hooksPath = $TARGET_REL"
fi

# git tracks the executable bit, so this is normally already true. It is not true after a checkout
# with a umask that stripped it, and a hook that is not executable is a hook that does not run.
for hook in "$TARGET_REL"/*; do
    [ -f "$hook" ] || continue
    [ -x "$hook" ] || chmod +x "$hook"
done

# Once core.hooksPath moves, anything in .git/hooks stops running. Say so rather than let somebody
# discover it the next time the hook they wrote there fails to fire.
git_dir=$(git rev-parse --git-dir)
legacy=""
for hook in "$git_dir"/hooks/*; do
    case "$hook" in *.sample|*'*') continue ;; esac
    [ -x "$hook" ] || continue
    legacy="$legacy $(basename "$hook")"
done
if [ -n "$legacy" ]; then
    echo "install-git-hooks: note — these hooks in $git_dir/hooks no longer run:$legacy" >&2
fi

echo "install-git-hooks: installed$( [ -n "$current" ] && echo " (already)" ) — pre-commit now refuses a commit"
echo "                   carrying another session's staged work. It runs on \`git commit\` only:"
echo "                   \`git reset --hard\`, \`git checkout -- <path>\` and \`git stash\` are not covered."
echo "                   Escape hatch: git commit --no-verify"
