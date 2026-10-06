#!/bin/sh
# Install Clawdline on macOS or Linux, as this user, with no sudo:
#
#   curl -fsSL https://raw.githubusercontent.com/sainteye/clawdline/main/install.sh | sh
#   curl -fsSL …/install.sh | sh -s -- --headless --port 7800
#
# This script only downloads a release, checks it against SHA256SUMS and
# unpacks it into ~/.local/share/clawdline-next/releases/<version>/. Everything
# else — the signature check, tmux, the service, the app, the health check — is
# `clawdline setup` from inside that release, which this ends by running with
# every option it was given (`clawdline setup --help` lists them). Running it
# again repairs or upgrades; `--uninstall` removes it.
#
#   --version vX.Y.Z             that release instead of the latest
#   --channel beta               the newest release including pre-releases
#   CLAWDLINE_INSTALL_BASE_URL   where SHA256SUMS and the archives are (tests)
set -eu

repo_url=https://github.com/sainteye/clawdline

say() { printf '%s\n' "$*"; }
die() { printf 'clawdline install: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# fetch <url> <file>: 0 when the file arrived; 22 for a 404, so "no release
# yet" can be told from a network failure.
fetch() {
  if have curl; then
    code=$(curl -sSL --proto '=https,http' --retry 2 -o "$2" -w '%{http_code}' "$1") || return 1
    [ "$code" = 200 ] && return 0
    [ "$code" = 404 ] && return 22
    return 1
  fi
  if have wget; then
    wget -q -O "$2" "$1" && return 0
    return 1
  fi
  die "neither curl nor wget is installed; install one of them and run this again"
}

sha256_of() {
  if have shasum; then shasum -a 256 "$1" | cut -d' ' -f1
  elif have sha256sum; then sha256sum "$1" | cut -d' ' -f1
  else die "neither shasum nor sha256sum is installed"
  fi
}

platform() {
  case $(uname -s) in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) die "Clawdline installs on macOS and Linux; this is $(uname -s)" ;;
  esac
  case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) die "Clawdline runs on amd64 and arm64; this machine is $(uname -m)" ;;
  esac
  # A shell under Rosetta says x86_64 on an Apple silicon Mac.
  if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
    arch=arm64
  fi
}

# newest_tag: the newest release on the beta channel, pre-releases included,
# from the Releases API (newest first).
newest_tag() {
  fetch "https://api.github.com/repos/sainteye/clawdline/releases?per_page=10" "$work/releases.json" ||
    die "could not list the releases at $repo_url/releases"
  sed -n 's/^ *"tag_name": *"\(v[0-9][^"]*\)".*/\1/p' "$work/releases.json" | head -n 1
}

main() {
  version="" channel="" uninstall=0
  n=$#
  while [ "$n" -gt 0 ]; do
    a=$1
    shift
    n=$((n - 1))
    case $a in
      --version)
        [ "$n" -gt 0 ] || die "--version needs a version, like v0.10.0"
        version=$1
        shift
        n=$((n - 1))
        continue
        ;;
      --version=*) version=${a#*=}; continue ;;
      --channel) [ "$n" -gt 0 ] && channel=$1 ;;
      --channel=*) channel=${a#*=} ;;
      --uninstall) uninstall=1 ;;
    esac
    set -- "$@" "$a"
  done

  root=${CLAWDLINE_NEXT_INSTALL_ROOT:-${XDG_DATA_HOME:-$HOME/.local/share}/clawdline-next}

  if [ "$uninstall" = 1 ]; then
    [ -x "$root/current/clawdline" ] || { say "nothing is installed at $root"; exit 0; }
    exec "$root/current/clawdline" setup "$@"
  fi

  platform
  have tar || die "tar is not installed"
  mkdir -p "$root/staging"
  work=$(mktemp -d "$root/staging/install.XXXXXX")
  trap 'rm -rf "$work"' EXIT
  trap 'exit 130' INT TERM

  if [ -n "${CLAWDLINE_INSTALL_BASE_URL:-}" ]; then
    base=${CLAWDLINE_INSTALL_BASE_URL%/}
  elif [ -n "$version" ]; then
    base=$repo_url/releases/download/$version
  elif [ "$channel" = beta ]; then
    tag=$(newest_tag)
    [ -n "$tag" ] || die "no release is published yet at $repo_url/releases"
    base=$repo_url/releases/download/$tag
  else
    base=$repo_url/releases/latest/download
  fi

  say "downloading Clawdline for $os/$arch from $base"
  for f in SHA256SUMS manifest.json manifest.sig.json; do
    status=0
    fetch "$base/$f" "$work/$f" || status=$?
    if [ "$status" = 22 ] && [ -z "$version" ] && [ -z "${CLAWDLINE_INSTALL_BASE_URL:-}" ]; then
      die "no release is published yet at $repo_url/releases (the latest one has no $f)"
    fi
    [ "$status" = 0 ] || die "could not download $base/$f"
  done

  suffix="_${os}_${arch}.tar.gz"
  line=$(awk -v s="$suffix" '$2 ~ /^clawdline_/ && substr($2, length($2) - length(s) + 1) == s { print; exit }' "$work/SHA256SUMS")
  [ -n "$line" ] || die "this release has no archive for $os/$arch"
  want=${line%% *}
  name=${line##* }
  v=${name#clawdline_}
  v=${v%"$suffix"}
  case $v in v[0-9]*) ;; *) die "SHA256SUMS names an archive with no version: $name" ;; esac
  named=$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' "$work/manifest.json" | head -n 1)
  [ "$named" = "$v" ] || die "the manifest is for ${named:-no version}, the archive for $v"

  fetch "$base/$name" "$work/$name" || die "could not download $base/$name"
  got=$(sha256_of "$work/$name")
  [ "$got" = "$want" ] || die "$name has sha256 $got, SHA256SUMS says $want; nothing was installed"

  # Unpacked beside its final place and renamed in, so a release directory is
  # whole or absent. setup proves every file matches the signed archive.
  mkdir "$work/unpacked"
  tar -xzf "$work/$name" -C "$work/unpacked"
  [ -x "$work/unpacked/clawdline" ] || die "$name holds no clawdline binary"
  mkdir -p "$root/releases"
  dest=$root/releases/$v
  if [ -e "$dest" ]; then
    mv "$dest" "$work/previous"
  fi
  mv "$work/unpacked" "$dest"

  say "unpacked $v into $dest"
  status=0
  "$dest/clawdline" setup --archive "$work/$name" --manifest-dir "$work" "$@" </dev/null || status=$?
  # A repair that failed puts back the copy of this version that was there.
  if [ "$status" != 0 ] && [ -e "$work/previous" ]; then
    mv "$dest" "$work/refused" && mv "$work/previous" "$dest"
  fi
  exit "$status"
}

main "$@"
