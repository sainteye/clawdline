#!/bin/sh
# Run tools/deploy-linux-user.sh --rev against a scratch repository and stub check-worktrees.sh,
# systemctl, curl and go.
#
# `clawdline update --apply` deploys the cloud's latest stamp, which is not the shared checkout's
# HEAD (docs/updates.md). This holds that --rev builds and verifies that commit, that the checkout's
# HEAD does not move, that a name that is not a commit is refused before anything is built, and
# that a deploy without --rev still builds HEAD.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-deployrev.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
uid=$(id -u)

repo=$tmp/repo
home=$tmp/home
bin=$tmp/bin
run=$tmp/r/run/user/$uid
mkdir -p "$repo/tools" "$bin" "$home/.config/systemd/user" "$home/.config/clawdline-next" "$run"
cp "$root/tools/deploy-linux-user.sh" "$repo/tools/"
: >"$home/.config/systemd/user/clawdline-next.service"
printf 'token\n' >"$home/.config/clawdline-next/local-token"
python3 -c 'import socket,sys; socket.socket(socket.AF_UNIX).bind(sys.argv[1])' "$run/bus"
: >"$tmp/calls"

# The release the stub build "makes" answers the stamp it was asked for.
cat >"$repo/tools/check-worktrees.sh" <<EOF
#!/bin/sh
echo "check-worktrees \$*" >>"$tmp/calls"
while [ "\$1" != -- ]; do shift; done
shift
release=\$2
mkdir -p "\$release/dist"
printf '{"stamp":"%s"}\n' "\$3" >"\$release/dist/BUILD.json"
EOF
chmod +x "$repo/tools/check-worktrees.sh"

(
  cd "$repo"
  git init -q -b main
  git -c user.email=t@example.invalid -c user.name=t add tools
  git -c user.email=t@example.invalid -c user.name=t commit -q -m one
  git -c user.email=t@example.invalid -c user.name=t commit -q --allow-empty -m two
)
first=$(git -C "$repo" rev-parse HEAD~1)
head=$(git -C "$repo" rev-parse HEAD)

cat >"$bin/id" <<'EOF'
#!/bin/sh
[ "$1" = -u ] && { echo 1000; exit 0; }
exec /usr/bin/id "$@"
EOF
cat >"$bin/systemctl" <<EOF
#!/bin/sh
echo "systemctl \$*" >>"$tmp/calls"
case "\$*" in *MainPID*) echo 42 ;; esac
exit 0
EOF
cat >"$bin/curl" <<EOF
#!/bin/sh
for a; do url=\$a; done
case \$url in
  */BUILD.json) cat "$home/.local/share/clawdline-next/current/dist/BUILD.json" ;;
  *) printf 200 ;;
esac
EOF
cat >"$bin/go" <<'EOF'
#!/bin/sh
echo 5
EOF
chmod +x "$bin"/*

deploy() {
  (
    cd "$repo"
    env -u CLAWDLINE_NEXT_DIR HOME="$home" PATH="$bin:$PATH" XDG_RUNTIME_DIR="$tmp/r/run/user/$uid" \
      XDG_DATA_HOME="$home/.local/share" XDG_CONFIG_HOME="$home/.config" \
      tools/deploy-linux-user.sh "$@"
  )
}

fail() { echo "FAIL: $*" >&2; exit 1; }

out=$(deploy --rev "$first" 2>&1) || fail "--rev deploy failed: $out"
grep -q "check-worktrees --ephemeral --rev $first -- tools/build-linux-user-release.sh .*/releases/$first $first" "$tmp/calls" ||
  fail "--rev did not build $first: $(cat "$tmp/calls")"
[ "$(git -C "$repo" rev-parse HEAD)" = "$head" ] || fail "the checkout's HEAD moved"
[ "$(readlink "$home/.local/share/clawdline-next/current")" = "$home/.local/share/clawdline-next/releases/$first" ] ||
  fail "current does not point at $first"
case $out in *"deployed $first"*) ;; *) fail "no deployed line: $out" ;; esac

: >"$tmp/calls"
if out=$(deploy --rev nosuchrev 2>&1); then fail "an unknown rev was deployed"; fi
case $out in *"not a commit"*) ;; *) fail "unknown rev not explained: $out" ;; esac
[ ! -s "$tmp/calls" ] || fail "an unknown rev reached the build: $(cat "$tmp/calls")"

if deploy --rev >/dev/null 2>&1; then fail "--rev without a commit was accepted"; fi

: >"$tmp/calls"
deploy >/dev/null 2>&1 || fail "a deploy without --rev failed"
grep -q "check-worktrees --ephemeral --rev $head " "$tmp/calls" || fail "a deploy without --rev did not build HEAD"

echo "PASS: deploy-linux-user.sh --rev"
