#!/bin/sh
# Prove that the helper adds a readable origin and that an ordinary push from
# the development checkout is stopped before the remote receives a ref.
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

if git -C "$tmp/work" push origin HEAD:main >"$tmp/push.out" 2>&1; then
	echo "unsafe: an ordinary push from the development checkout succeeded" >&2
	exit 1
fi
if ! grep -Fq 'Push blocked: this checkout contains unfiltered development history.' "$tmp/push.out"; then
	echo "push failed without the guard's explanation:" >&2
	sed -n '1,20p' "$tmp/push.out" >&2
	exit 1
fi
if git --git-dir="$tmp/public.git" show-ref --verify --quiet refs/heads/main; then
	echo "unsafe: the blocked push still created refs/heads/main" >&2
	exit 1
fi

echo "push guard: an ordinary push was refused with its reason; the remote stayed unchanged"
