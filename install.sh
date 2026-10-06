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
# again repairs or upgrades; `--uninstall` removes it, and `--uninstall
# --purge` the state directory too, even after the binary is gone.
#
#   --version vX.Y.Z             that release instead of the latest
#   --channel beta               the newest release including pre-releases
#   --verbose                    where each file came from and went
#   CLAWDLINE_INSTALL_BASE_URL   where SHA256SUMS and the archives are (tests)
set -eu

repo_url=https://github.com/sainteye/clawdline

say() { printf '%s\n' "$*"; }
detail() { [ "$verbose" = 0 ] || say "  $*"; }
# die says what stopped the install, and every message ends with the next step.
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
  die "neither curl nor wget is installed. Install one of them and run this again."
}

sha256_of() {
  if have shasum; then shasum -a 256 "$1" | cut -d' ' -f1
  elif have sha256sum; then sha256sum "$1" | cut -d' ' -f1
  else die "neither shasum nor sha256sum is installed. Install coreutils (sha256sum) and run this again."
  fi
}

platform() {
  case $(uname -s) in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) die "Clawdline installs on macOS and Linux; this is $(uname -s). Run this on a Mac or a Linux machine." ;;
  esac
  case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) die "Clawdline runs on amd64 and arm64; this machine is $(uname -m). Run this on an amd64 or arm64 machine." ;;
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
    die "could not list the releases at $repo_url/releases. Check your network, or pass --version vX.Y.Z."
  sed -n 's/^ *"tag_name": *"\(v[0-9][^"]*\)".*/\1/p' "$work/releases.json" | head -n 1
}

# purge_state removes the state directory (devices, sessions, settings) the
# way `setup --uninstall --purge` does: while a terminal the daemon started
# still runs, the directory its tmux server listens in stays, and is said.
purge_state() {
  state=${CLAWDLINE_NEXT_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/clawdline-next}
  if [ ! -d "$state" ]; then
    say "nothing to purge: $state does not exist"
    return
  fi
  sock=$state/tmux/term.sock
  if [ -S "$sock" ] && have tmux && [ -n "$(tmux -S "$sock" ls 2>/dev/null)" ]; then
    find "$state" -mindepth 1 -maxdepth 1 ! -path "$state/tmux" -exec rm -rf {} +
    say "removed $state (--purge), all but $state/tmux"
    say "kept $state/tmux: the terminals the daemon started are still open; they end when you exit them (tmux -S $sock ls), and then it can go"
    return
  fi
  rm -rf "$state"
  say "removed $state (--purge): devices, sessions and settings"
}

main() {
  version="" channel="" uninstall=0 purge=0 verbose=0
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
      --purge) purge=1 ;;
      --verbose) verbose=1 ;;
    esac
    set -- "$@" "$a"
  done

  root=${CLAWDLINE_NEXT_INSTALL_ROOT:-${XDG_DATA_HOME:-$HOME/.local/share}/clawdline-next}

  if [ "$uninstall" = 1 ]; then
    [ -x "$root/current/clawdline" ] && exec "$root/current/clawdline" setup "$@"
    # Nothing to run `setup --uninstall` with: an earlier uninstall took the
    # binary. --purge still owes the state directory, so it is removed here.
    say "Clawdline is not installed at $root; nothing to stop or unlink."
    [ "$purge" = 1 ] || exit 0
    purge_state
    exit 0
  fi

  platform
  have tar || die "tar is not installed. Install it and run this again."
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
    [ -n "$tag" ] || die "no release is published yet at $repo_url/releases. Try again later, or pass --version vX.Y.Z."
    base=$repo_url/releases/download/$tag
  else
    base=$repo_url/releases/latest/download
  fi

  say "Downloading Clawdline for $os/$arch…"
  detail "from $base"
  for f in SHA256SUMS manifest.json manifest.sig.json; do
    status=0
    fetch "$base/$f" "$work/$f" || status=$?
    if [ "$status" = 22 ] && [ -z "$version" ] && [ -z "${CLAWDLINE_INSTALL_BASE_URL:-}" ]; then
      die "no release is published yet at $repo_url/releases (the latest one has no $f). Try again later, or pass --version vX.Y.Z."
    fi
    [ "$status" = 0 ] || die "could not download $base/$f. Check your network and run this again, or pass --version vX.Y.Z."
  done

  suffix="_${os}_${arch}.tar.gz"
  line=$(awk -v s="$suffix" '$2 ~ /^clawdline_/ && substr($2, length($2) - length(s) + 1) == s { print; exit }' "$work/SHA256SUMS")
  [ -n "$line" ] || die "this release has no archive for $os/$arch. Pass --version with a release that has one ($repo_url/releases)."
  want=${line%% *}
  name=${line##* }
  v=${name#clawdline_}
  v=${v%"$suffix"}
  case $v in v[0-9]*) ;; *) die "SHA256SUMS names an archive with no version: $name. Nothing was installed; report it at $repo_url/issues." ;; esac
  named=$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' "$work/manifest.json" | head -n 1)
  [ "$named" = "$v" ] || die "the manifest is for ${named:-no version}, the archive for $v. Nothing was installed; run this again in a few minutes, or pass --version vX.Y.Z."

  fetch "$base/$name" "$work/$name" || die "could not download $base/$name. Check your network and run this again."
  got=$(sha256_of "$work/$name")
  [ "$got" = "$want" ] || die "$name has sha256 $got, SHA256SUMS says $want; nothing was installed. Run this again; if it repeats, report it at $repo_url/issues."

  # Unpacked beside its final place and renamed in, so a release directory is
  # whole or absent. setup proves every file matches the signed archive.
  mkdir "$work/unpacked"
  # GNU tar names every macOS extended attribute it skips; those lines say
  # nothing about the files, anything else it says is shown.
  tar -xzf "$work/$name" -C "$work/unpacked" 2>"$work/tar.err" || { cat "$work/tar.err" >&2; die "could not unpack $name. Check the disk has room and run this again."; }
  grep -v 'Ignoring unknown extended header keyword' "$work/tar.err" >&2 || true
  [ -x "$work/unpacked/clawdline" ] || die "$name holds no clawdline binary. Nothing was installed; report it at $repo_url/issues."
  mkdir -p "$root/releases"
  dest=$root/releases/$v
  if [ -e "$dest" ]; then
    mv "$dest" "$work/previous"
  fi
  mv "$work/unpacked" "$dest"

  detail "unpacked $v into $dest"
  status=0
  "$dest/clawdline" setup --archive "$work/$name" --manifest-dir "$work" "$@" </dev/null || status=$?
  # A repair that failed puts back the copy of this version that was there.
  if [ "$status" != 0 ] && [ -e "$work/previous" ]; then
    mv "$dest" "$work/refused" && mv "$work/previous" "$dest"
  fi
  exit "$status"
}

main "$@"
