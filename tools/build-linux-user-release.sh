#!/bin/bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: tools/build-linux-user-release.sh <release-dir> <commit>" >&2
  exit 2
fi

release=$1
commit=$2
case "$commit" in
  *[!0-9a-f]* | "") echo "release commit must be a lowercase hexadecimal Git object" >&2; exit 2 ;;
esac
[ "${#commit}" -eq 40 ] || { echo "release commit must be a full 40-character Git object" >&2; exit 2; }

actual=$(git rev-parse HEAD)
[ "$actual" = "$commit" ] || { echo "checkout is $actual, not requested release $commit" >&2; exit 2; }

parent=$(dirname "$release")
mkdir -p "$parent"
stage=$(mktemp -d "$parent/.build-$commit.XXXXXX")
trap 'rm -rf -- "$stage"' EXIT HUP INT TERM
umask 077

# A session opened by the daemon inherits its production switches. They are
# inputs to a running service, not to its test/build process: standalone mode,
# for example, deliberately disables the forwarding exercised by tests.
while IFS='=' read -r name _; do
  case "$name" in
    CLAWDLINE_NEXT_*) unset "$name" ;;
  esac
done < <(env)
unset VITE_HOSTED_CONSOLE || true

go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=true -o "$stage/clawdline" ./cmd/clawdline
( cd web && npm ci --ignore-scripts && npm run check && npm run build )
cp -R web/console/dist "$stage/dist"
printf '{"stamp":"%s"}\n' "$commit" >"$stage/dist/BUILD.json"

main=$(find "$stage/dist/assets" -maxdepth 1 -type f -name 'main-*.js' -print)
[ "$(printf '%s\n' "$main" | sed '/^$/d' | wc -l)" -eq 1 ] || {
  echo "local console does not contain exactly one main bundle" >&2
  exit 1
}
if rg -q 'CloudGate' "$main"; then
  echo "local console contains CloudGate; refusing to install a hosted build" >&2
  exit 1
fi

if [ -e "$release" ]; then
  [ -x "$release/clawdline" ] && [ "$(sed -n 's/.*"stamp":"\([0-9a-f]*\)".*/\1/p' "$release/dist/BUILD.json")" = "$commit" ] || {
    echo "existing release is incomplete: $release" >&2
    exit 1
  }
  echo "release already built: $release"
  exit 0
fi
mv "$stage" "$release"
trap - EXIT HUP INT TERM
echo "release built: $release"
