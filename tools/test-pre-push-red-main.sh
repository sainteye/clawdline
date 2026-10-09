#!/bin/sh
# Prove that the pre-push hook refuses a push onto a red main, lets the two
# named ways through, and refuses when it cannot read CI rather than passing.
# gh is a stub on PATH; nothing here reaches GitHub.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-red-main.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

mkdir -p "$tmp/bin"
# The stub answers `run list` from runs.txt and `run view` from jobs.txt, or
# fails the way an unauthenticated gh does when fail.txt exists.
cat >"$tmp/bin/gh" <<STUB
#!/bin/sh
if [ -f "$tmp/fail.txt" ]; then cat "$tmp/fail.txt" >&2; exit 4; fi
case "\$1 \$2" in
  "run list") cat "$tmp/runs.txt" ;;
  "run view") cat "$tmp/jobs.txt" ;;
  *) echo "gh stub: unexpected \$*" >&2; exit 1 ;;
esac
STUB
chmod +x "$tmp/bin/gh"

git init --bare -q "$tmp/public.git"
git init -q -b main "$tmp/work"
mkdir -p "$tmp/work/tools/git-hooks"
cp "$root/tools/git-hooks/pre-push" "$tmp/work/tools/git-hooks/pre-push"
# The privacy checks have their own test; here they find nothing.
printf '#!/bin/sh\nexit 0\n' >"$tmp/work/tools/check-private.sh"
chmod +x "$tmp/work/tools/git-hooks/pre-push" "$tmp/work/tools/check-private.sh"
git -C "$tmp/work" config core.hooksPath tools/git-hooks
git -C "$tmp/work" remote add origin "$tmp/public.git"

n=0
commit() {
  n=$((n + 1))
  printf '%s\n' "$n" >"$tmp/work/file.txt"
  git -C "$tmp/work" add file.txt
  git -C "$tmp/work" -c user.name='Guard Test' -c user.email='guard@example.invalid' \
    commit -q -m "Change $n" "$@"
}
fail() { echo "red main guard: $1" >&2; sed -n '1,30p' "$tmp/push.out" >&2; exit 1; }
# push <want: ok|refused> [env assignments…]
push() {
  want=$1; shift
  if env PATH="$tmp/bin:$PATH" "$@" git -C "$tmp/work" push -q origin HEAD:refs/heads/main >"$tmp/push.out" 2>&1; then got=ok; else got=refused; fi
  pushed=$(git --git-dir="$tmp/public.git" rev-parse -q --verify refs/heads/main || true)
  head=$(git -C "$tmp/work" rev-parse HEAD)
  [ "$got" = "$want" ] || fail "push was $got, want $want"
  if [ "$want" = ok ] && [ "$pushed" != "$head" ]; then fail "the push succeeded but main is not HEAD"; fi
  if [ "$want" = refused ] && [ "$pushed" = "$head" ]; then fail "a refused push still moved main"; fi
}
expect() { grep -Fq "$1" "$tmp/push.out" || fail "the output does not say: $1"; }

# 1. Green main: the push lands.
printf '101 success https://example.invalid/runs/101\n' >"$tmp/runs.txt"
: >"$tmp/jobs.txt"
commit
push ok

# 2. Red main: refused, naming the run and the failing jobs.
printf '102 failure https://example.invalid/runs/102\n' >"$tmp/runs.txt"
printf 'Go tests on Linux\nConsole type-check, builds and tests\n' >"$tmp/jobs.txt"
commit
push refused
expect 'refused a push onto a red main'
expect 'https://example.invalid/runs/102'
expect 'failing job: Go tests on Linux'
expect 'failing job: Console type-check, builds and tests'

# 3. The override lands it and says so.
push ok CLAWDLINE_PUSH_ON_RED=1
expect 'CLAWDLINE_PUSH_ON_RED=1 overrode the refusal'

# 4. A commit that says it is the fix lands it, even below a later commit.
commit --trailer 'CI-Fix: 102'
commit
push ok
expect 'CI-Fix: 102'

# 5. A commit without the trailer is refused again: the trailer of one already
#    on the remote does not count.
commit
push refused
expect 'refused a push onto a red main'

# 6. gh that cannot answer is not a pass.
printf 'To get started with GitHub CLI, please run:  gh auth login\n' >"$tmp/fail.txt"
push refused
expect 'could not read the latest CI run on main'
expect 'gh auth login'
push ok CLAWDLINE_PUSH_ON_RED=1
expect 'CLAWDLINE_PUSH_ON_RED=1 overrode the check'
rm "$tmp/fail.txt"

# 7. No gh at all is not a pass either.
commit
push refused CLAWDLINE_GH="$tmp/no-such-gh"
expect 'gh is not installed'

# 8. No completed run is not a pass.
: >"$tmp/runs.txt"
push refused
expect 'no completed CI run on main was found'

# 9. Green again: the push lands without any override.
printf '103 success https://example.invalid/runs/103\n' >"$tmp/runs.txt"
push ok

echo "red main guard: a red or unreadable main refused the push with its reason; CI-Fix and the override let it through"
