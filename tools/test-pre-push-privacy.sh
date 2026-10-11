#!/bin/sh
# Prove that the pre-push hook asks the privacy checker about the commits this
# push would add — not about the whole history — and that every answer other
# than clean refuses the push.
#
# The checker itself is stubbed: its rules have their own tests
# (tools/check-private), and what is tested here is the hook's question and
# what it does with the answer. The stub records every call so the test can
# say the history was read once, over a range, with no checkpoint.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-push-privacy.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

mkdir -p "$tmp/bin"
cat >"$tmp/bin/gh" <<STUB
#!/bin/sh
case "\$1 \$2" in
  "run list") echo '201 success https://example.invalid/runs/201' ;;
  "run view") : ;;
  *) echo "gh stub: unexpected \$*" >&2; exit 1 ;;
esac
STUB
chmod +x "$tmp/bin/gh"

git init --bare -q "$tmp/public.git"
git init -q -b main "$tmp/work"
mkdir -p "$tmp/work/tools/git-hooks"
cp "$root/tools/git-hooks/pre-push" "$tmp/work/tools/git-hooks/pre-push"
# The stub: every call is appended to calls.txt, and answer.txt (when it
# exists) is the exit status of a -history call. The working-tree call stays
# clean so a refusal can only come from the history one.
cat >"$tmp/work/tools/check-private.sh" <<STUB
#!/bin/sh
printf '%s\n' "\${*:-working-tree}" >>"$tmp/calls.txt"
case "\$1" in
  -history) if [ -f "$tmp/answer.txt" ]; then
              echo "docs/note.md:3: private-word — history only, not in the working tree"
              exit "\$(cat "$tmp/answer.txt")"
            fi ;;
esac
exit 0
STUB
chmod +x "$tmp/work/tools/git-hooks/pre-push" "$tmp/work/tools/check-private.sh"
git -C "$tmp/work" config core.hooksPath tools/git-hooks
git -C "$tmp/work" remote add origin "$tmp/public.git"

n=0
commit() {
  n=$((n + 1))
  printf '%s\n' "$n" >"$tmp/work/file.txt"
  git -C "$tmp/work" add file.txt
  git -C "$tmp/work" -c user.name='Guard Test' -c user.email='guard@example.invalid' \
    commit -q -m "Change $n"
}
fail() { echo "push privacy guard: $1" >&2; sed -n '1,30p' "$tmp/push.out" >&2; exit 1; }
push() {
  want=$1
  : >"$tmp/calls.txt"
  if PATH="$tmp/bin:$PATH" git -C "$tmp/work" push -q origin HEAD:refs/heads/main >"$tmp/push.out" 2>&1
  then got=ok; else got=refused; fi
  pushed=$(git --git-dir="$tmp/public.git" rev-parse -q --verify refs/heads/main || true)
  head=$(git -C "$tmp/work" rev-parse HEAD)
  [ "$got" = "$want" ] || fail "push was $got, want $want"
  if [ "$want" = ok ] && [ "$pushed" != "$head" ]; then fail "the push succeeded but main is not HEAD"; fi
  if [ "$want" = refused ] && [ "$pushed" = "$head" ]; then fail "a refused push still moved main"; fi
}
expect() { grep -Fq "$1" "$tmp/push.out" || fail "the output does not say: $1"; }
calls() { grep -c . "$tmp/calls.txt" || true; }

# 1. The first push of a ref the remote does not have publishes its whole
#    history, so that is what the hook asks about.
commit
push ok
[ "$(calls)" = 2 ] || fail "the hook made $(calls) check-private call(s), want two"
grep -Fq -- "-history -revs=$(git -C "$tmp/work" rev-parse HEAD) -checkpoint -" "$tmp/calls.txt" ||
  fail "the first push did not ask about the whole history: $(cat "$tmp/calls.txt")"

# 2. The next push asks about the commits it would add, once, and keeps no
#    checkpoint. This is the whole point: the question is a range.
before=$(git -C "$tmp/work" rev-parse HEAD)
commit
commit
push ok
[ "$(calls)" = 2 ] || fail "the hook made $(calls) check-private call(s), want two"
grep -Fq -- "-history -revs=$(git -C "$tmp/work" rev-parse HEAD) -since=$before -checkpoint -" "$tmp/calls.txt" ||
  fail "the push did not ask about its own range: $(cat "$tmp/calls.txt")"

# 3. A finding in those commits refuses the push, with the finding and where
#    to read about it.
echo 1 >"$tmp/answer.txt"
commit
push refused
expect 'the commits this push would add are not clean of private material'
expect 'docs/note.md:3: private-word'
expect 'docs/publishing.md'

# 4. Could-not-check and undetermined are not a pass either.
echo 2 >"$tmp/answer.txt"
push refused
expect 'are not clean of private material'
echo 3 >"$tmp/answer.txt"
push refused
expect 'are not clean of private material'

# 5. Clean again: it lands.
rm "$tmp/answer.txt"
push ok

echo "push privacy guard: a push asks about the commits it would add, once, and any answer but clean refuses it"
