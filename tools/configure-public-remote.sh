#!/bin/sh
# Give a private development checkout a readable public origin without making
# an ordinary push possible. The hook setting is local to this checkout; a
# clean publication clone does not inherit it.
set -eu

url=${CLAWDLINE_PUBLIC_REMOTE_URL:-https://github.com/sainteye/clawdline.git}

root=$(git rev-parse --show-toplevel)
hook="$root/tools/git-hooks/pre-push"
[ -x "$hook" ] || {
	echo "refusing to add origin: the tracked pre-push guard is missing or not executable at $hook" >&2
	exit 1
}

hooks=$(git config --local --get core.hooksPath || true)
if [ -n "$hooks" ] && [ "$hooks" != "tools/git-hooks" ]; then
	echo "refusing to replace core.hooksPath ($hooks); install the push guard by hand" >&2
	exit 1
fi

# Install the guard before naming the public repository. If the second command
# fails, the checkout is still in the safer of the two partial states.
git config --local core.hooksPath tools/git-hooks

if git remote get-url origin >/dev/null 2>&1; then
	current=$(git remote get-url origin)
	if [ "$current" != "$url" ]; then
		echo "refusing to replace origin ($current); expected $url" >&2
		exit 1
	fi
else
	git remote add origin "$url"
fi

echo "origin fetches from $url"
echo "pushes from this checkout are guarded by tools/git-hooks/pre-push"
