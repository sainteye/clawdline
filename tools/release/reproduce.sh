#!/bin/bash
# Compares two builds of one release, file by file
# (docs/security/release-evidence.md, "Rebuilding a release").
#
# usage: tools/release/reproduce.sh compare <A> <B>
#        tools/release/reproduce.sh smoke
#
# compare takes two release directories (or two archives) built from the same
# commit, for example the published assets and a rebuild, and prints one line
# per difference:
#   archive-bytes   the .tar.gz files differ; tar and gzip record times and
#                   owners, so this alone says nothing about the payload
#   signature-only  a Mach-O file whose bytes differ but are identical once
#                   both signatures are removed (needs a Mac): a Developer ID
#                   signature carries a timestamp and is never reproduced
#   differs         the unsigned payload differs
#   only-in-A/B     a file one build has and the other does not
# It exits 0 when every file is identical or signature-only, and 1 otherwise.
# An identical unsigned payload is not a claim that the signed archive is
# bit-for-bit reproducible; that is what the signature-only lines count.
#
# smoke is a limited, runnable instance of the procedure: it builds the Linux
# amd64 daemon twice with the release build's flags, each with an empty Go
# build cache, and compares the two archives. It covers the Go build only,
# not the console bundle, tmux or the app. Run it through tools/heavy.sh.
set -euo pipefail
# compare's paths are the caller's; they are made absolute before the cd.
caller=$PWD
cd "$(dirname "$0")/../.."

usage() { echo "usage: tools/release/reproduce.sh compare <A> <B> | smoke" >&2; exit 2; }
[ "$#" -ge 1 ] || usage

work=$(mktemp -d)
finished=0
trap 's=$?; chmod -R u+w "$work" 2>/dev/null; rm -rf -- "$work"; if [ "$finished" != 1 ] && [ "$s" = 0 ]; then exit 1; fi' EXIT

sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
is_macho() { case "$(head -c 4 "$1" | od -An -tx1 | tr -d ' \n')" in cffaedfe|cefaedfe|cafebabe) return 0 ;; esac; return 1; }

# unsigned_sha prints the sha256 of a Mach-O file with its signature removed.
unsigned_sha() {
  local c="$work/strip.$$"
  cp "$1" "$c"
  chmod u+w "$c"
  codesign --remove-signature "$c" 2>/dev/null || { rm -f "$c"; return 1; }
  sha "$c"
  rm -f "$c"
}

compare() {
  local a=$1 b=$2 names=() n diffs=0 sigs=0 same=0
  case "$a" in /*) ;; *) a=$caller/$a ;; esac
  case "$b" in /*) ;; *) b=$caller/$b ;; esac
  if [ -f "$a" ] && [ -f "$b" ]; then
    mkdir -p "$work/a" "$work/b"
    cp "$a" "$work/a/$(basename "$a")"
    cp "$b" "$work/b/$(basename "$a")"
    a=$work/a b=$work/b
  fi
  [ -d "$a" ] && [ -d "$b" ] || usage
  while IFS= read -r n; do names+=("$n"); done < <( { (cd "$a" && ls -- *.tar.gz 2>/dev/null); (cd "$b" && ls -- *.tar.gz 2>/dev/null); } | sort -u)
  [ "${#names[@]}" -gt 0 ] || { echo "reproduce: no archives to compare" >&2; exit 1; }
  for n in ${names[@]+"${names[@]}"}; do
    if [ ! -f "$a/$n" ] || [ ! -f "$b/$n" ]; then
      echo "only-in-$([ -f "$a/$n" ] && echo A || echo B)  $n"; diffs=$((diffs + 1)); continue
    fi
    [ "$(sha "$a/$n")" = "$(sha "$b/$n")" ] || echo "archive-bytes  $n"
    local ua="$work/u/A/$n" ub="$work/u/B/$n"
    mkdir -p "$ua" "$ub"
    tar -xzf "$a/$n" -C "$ua"
    tar -xzf "$b/$n" -C "$ub"
    local f
    while IFS= read -r f; do
      if [ ! -e "$ua/$f" ] && [ ! -L "$ua/$f" ]; then echo "only-in-B  $n:$f"; diffs=$((diffs + 1)); continue; fi
      if [ ! -e "$ub/$f" ] && [ ! -L "$ub/$f" ]; then echo "only-in-A  $n:$f"; diffs=$((diffs + 1)); continue; fi
      if [ -L "$ua/$f" ] || [ -L "$ub/$f" ]; then
        if [ "$(readlink "$ua/$f")" = "$(readlink "$ub/$f")" ]; then same=$((same + 1)); else echo "differs  $n:$f (link)"; diffs=$((diffs + 1)); fi
        continue
      fi
      if cmp -s "$ua/$f" "$ub/$f"; then same=$((same + 1)); continue; fi
      if is_macho "$ua/$f" && command -v codesign >/dev/null; then
        local sa sb
        sa=$(unsigned_sha "$ua/$f" || echo a) sb=$(unsigned_sha "$ub/$f" || echo b)
        if [ "$sa" = "$sb" ]; then echo "signature-only  $n:$f"; sigs=$((sigs + 1)); continue; fi
      fi
      echo "differs  $n:$f"; diffs=$((diffs + 1))
    done < <( { (cd "$ua" && find . \( -type f -o -type l \)); (cd "$ub" && find . \( -type f -o -type l \)); } | sed 's#^\./##' | sort -u)
  done
  echo "reproduce: $same identical, $sigs signature-only, $diffs different"
  [ "$diffs" = 0 ]
}

smoke() {
  local commit v ldflags i
  commit=$(git rev-parse HEAD)
  v=v0.0.0-test.0
  ldflags="-s -w -X main.releaseVersion=$v -X main.releaseCommit=$commit"
  for i in A B; do
    mkdir -p "$work/smoke/$i/stage" "$work/smoke/$i/out" "$work/smoke/$i/gocache"
    echo "reproduce: build $i of linux/amd64 with an empty build cache…"
    GOCACHE="$work/smoke/$i/gocache" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
      go build -trimpath -buildvcs=true -ldflags "$ldflags" -o "$work/smoke/$i/stage/clawdline" ./cmd/clawdline
    COPYFILE_DISABLE=1 tar -C "$work/smoke/$i/stage" -czf "$work/smoke/$i/out/clawdline_${v}_linux_amd64.tar.gz" .
  done
  compare "$work/smoke/A/out" "$work/smoke/B/out"
}

case "$1" in
  compare) [ "$#" = 3 ] || usage; compare "$2" "$3" ;;
  smoke) [ "$#" = 1 ] || usage; smoke ;;
  *) usage ;;
esac
finished=1
