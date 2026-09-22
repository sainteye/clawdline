#!/bin/sh
# Prove that the helper adds a readable origin and that a push of an unsafe
# branch is stopped before the remote receives a ref.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-push-guard.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

git init --bare -q "$tmp/public.git"
git init -q "$tmp/work"
mkdir -p "$tmp/work/tools/git-hooks"
cp "$root/tools/git-hooks/pre-push" "$tmp/work/tools/git-hooks/pre-push"
chmod +x "$tmp/work/tools/git-hooks/pre-push"

printf 'private development history\n' >"$tmp/work/history.txt"
git -C "$tmp/work" add history.txt
git -C "$tmp/work" -c user.name='Guard Test' -c user.email='guard@example.invalid' \
	commit -q -m 'Private development history'

(
	cd "$tmp/work"
	CLAWDLINE_PUBLIC_REMOTE_URL="$tmp/public.git" "$root/tools/configure-public-remote.sh"
)

if git -C "$tmp/work" push origin HEAD:refs/heads/task/unsafe >"$tmp/push.out" 2>&1; then
	echo "unsafe: a push of a non-public branch succeeded" >&2
	exit 1
fi
if ! grep -Fq 'pre-push: refused refs/heads/task/unsafe.' "$tmp/push.out"; then
	echo "push failed without the guard's explanation:" >&2
	sed -n '1,20p' "$tmp/push.out" >&2
	exit 1
fi
if git --git-dir="$tmp/public.git" show-ref --verify --quiet refs/heads/task/unsafe; then
	echo "unsafe: the blocked push still created refs/heads/task/unsafe" >&2
	exit 1
fi

echo "push guard: an unsafe branch was refused with its reason; the remote stayed unchanged"
