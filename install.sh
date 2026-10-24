#!/bin/sh
# Install Clawdline on macOS or Linux, as this user, with no sudo:
#
#   curl -fsSL https://raw.githubusercontent.com/sainteye/clawdline/main/install.sh | sh
#   curl -fsSL …/install.sh | sh -s -- --headless --port 7800
#
# This script verifies a release's Ed25519 manifest and archive with trusted
# local Python 3 and OpenSSL 3 before unpacking or running downloaded code.
# It unpacks into ~/.local/share/clawdline-next/releases/<version>/. Everything
# else — another signature check, tmux, the service, the app, the health check — is
# `clawdline setup` from inside that release, which this ends by running with
# every option it was given (`clawdline setup --help` lists them). Running it
# again repairs or upgrades; `--uninstall` removes it, and `--uninstall
# --purge` the state directory too, even after the binary is gone.
#
#   --version vX.Y.Z             that release instead of the latest
#   --channel beta               the newest release including pre-releases
#   --verbose                    where each file came from and went
#   CLAWDLINE_INSTALL_BASE_URL   where signed manifests and archives are (tests)
#   CLAWDLINE_INSTALL_OPENSSL    a trusted local OpenSSL 3 executable
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
  # Without a tmux of its own the machine ran the one the release carried,
  # which is gone with the release: nothing here can ask that server, so a
  # socket that is there is kept rather than taken from under its terminals.
  if [ -S "$sock" ] && { ! have tmux || [ -n "$(tmux -S "$sock" ls 2>/dev/null)" ]; }; then
    find "$state" -mindepth 1 -maxdepth 1 ! -path "$state/tmux" -exec rm -rf {} +
    say "removed $state (--purge), all but $state/tmux"
    say "kept $state/tmux: the terminals the daemon started may still be open; they end when you exit them, and then it can go"
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
  have python3 || die "verification_tool_missing: install trusted Python 3 and OpenSSL 3, then retry. / 請先安裝受信任的 Python 3 與 OpenSSL 3 再重試。"
  umask 077
  mkdir -p "$root/staging"
  work=$(mktemp -d "$root/staging/install.XXXXXX")
  # The staging and root directories go too when nothing else is in them.
  trap 'rm -rf "$work"; rmdir "$root/staging" "$root" 2>/dev/null || true' EXIT
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

  say "Downloading Clawdline for ${os}/${arch}…"
  detail "from $base"
  # The verifier below is shipped in this script, never fetched from a release.
  python3 -I - "$base" "$work" "$os" "$arch" "$version" <<'CLAWDLINE_VERIFY'
# This verifier is part of the reviewed installer, not a downloaded program.
# Python supplies JSON and bounded HTTPS reads; local OpenSSL supplies Ed25519.
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time
import urllib.parse
import urllib.request

# Keep in step with internal/adapters/release/keys.go. Never accept a key from
# the download host or an environment variable. Rotation ships both keys here.
TRUSTED_KEYS = ("2Du3yZ4jZF8zoMcZ2ANHFNbxZuz0wgAZdR0nZJ/g/O0=",)
# Copies of the existing release bounds, held to their register by installer tests.
MANIFEST_BYTES = 64 << 10
SIGNATURE_BYTES = 16 << 10
ARTIFACT_BYTES = 512 << 20
FETCH_SECONDS = 30
DOWNLOAD_SECONDS = 900

base, directory, system, arch, requested = sys.argv[1:]
work = Path(directory)
locale = os.environ.get("CLAWDLINE_LANG") or os.environ.get("LC_ALL") or os.environ.get("LC_MESSAGES") or os.environ.get("LANG", "en")
chinese = locale.startswith(("zh-Hant", "zh_TW", "zh-TW"))


def refuse(code, english, traditional):
    print("clawdline install: " + code + ": " + (traditional if chinese else english), file=sys.stderr)
    sys.exit(1)


def openssl():
    selected = os.environ.get("CLAWDLINE_INSTALL_OPENSSL")
    choices = [selected] if selected else [shutil.which("openssl"), "/opt/homebrew/opt/openssl@3/bin/openssl", "/usr/local/opt/openssl@3/bin/openssl"]
    for candidate in choices:
        if not candidate:
            continue
        try:
            result = subprocess.run([candidate, "version"], check=True, capture_output=True, text=True)
        except (OSError, subprocess.CalledProcessError):
            continue
        match = re.match(r"OpenSSL ([0-9]+)\.", result.stdout)
        if match and int(match[1]) >= 3:
            return candidate
    refuse("verification_tool_missing", "Install trusted OpenSSL 3 or newer, then retry. Set CLAWDLINE_INSTALL_OPENSSL to its local executable if needed. Nothing was installed.", "請先安裝受信任的 OpenSSL 3 以上版本再重試；必要時以 CLAWDLINE_INSTALL_OPENSSL 指定本機執行檔。尚未安裝任何程式。")


def secure_url(url):
    parsed = urllib.parse.urlsplit(url)
    if parsed.username is not None or parsed.password is not None:
        raise ValueError("credentials in download URL")
    # Plain HTTP is only for an explicit loopback fixture or local mirror.
    if parsed.scheme != "https" and not (parsed.scheme == "http" and parsed.hostname in ("127.0.0.1", "::1", "localhost")):
        raise ValueError("download requires HTTPS")
    return url


class SecureRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, url):
        secure_url(url)
        if urllib.parse.urlsplit(request.full_url).scheme == "https" and urllib.parse.urlsplit(url).scheme != "https":
            raise ValueError("HTTPS downgrade")
        return super().redirect_request(request, fp, code, message, headers, url)


opener = urllib.request.build_opener(SecureRedirect())


def download(name, maximum, timeout):
    deadline = time.monotonic() + timeout
    target = work / name
    with opener.open(secure_url(base + "/" + name), timeout=FETCH_SECONDS) as response, target.open("wb") as output:
        total = 0
        while True:
            if time.monotonic() >= deadline:
                raise TimeoutError("download deadline")
            part = response.read(min(io.DEFAULT_BUFFER_SIZE, maximum - total + 1))
            if not part:
                break
            total += len(part)
            if total > maximum:
                raise ValueError("download exceeds signed or registered size")
            output.write(part)
    return target


def unique_object(pairs):
    result = {}
    for name, value in pairs:
        if name in result:
            raise ValueError("duplicate JSON field")
        result[name] = value
    return result


def read_json(raw):
    return json.loads(raw, object_pairs_hook=unique_object)


try:
    verifier = openssl()
    manifest_path = download("manifest.json", MANIFEST_BYTES, FETCH_SECONDS)
    signature_path = download("manifest.sig.json", SIGNATURE_BYTES, FETCH_SECONDS)
    manifest_raw = manifest_path.read_bytes()
    signatures = read_json(signature_path.read_bytes())
    if not isinstance(signatures, list):
        raise ValueError("signature list")
    verified = False
    for public in TRUSTED_KEYS:
        key = base64.b64decode(public, validate=True)
        key_id = hashlib.sha256(key).hexdigest()[:16]
        # RFC 8410 SubjectPublicKeyInfo for an Ed25519 public key.
        public_path = work / "release-key.der"
        public_path.write_bytes(bytes.fromhex("302a300506032b6570032100") + key)
        for signature in signatures:
            if not isinstance(signature, dict) or signature.get("key_id") != key_id:
                continue
            try:
                raw = base64.b64decode(signature["sig"], validate=True)
            except (ValueError, KeyError, TypeError):
                continue
            signature_file = work / "release-signature.bin"
            signature_file.write_bytes(raw)
            check = subprocess.run([verifier, "pkeyutl", "-verify", "-rawin", "-pubin", "-keyform", "DER", "-inkey", str(public_path), "-sigfile", str(signature_file), "-in", str(manifest_path)], capture_output=True)
            if check.returncode == 0:
                verified = True
                break
        if verified:
            break
    if not verified:
        refuse("manifest_signature_invalid", "No signature matches the installer's trusted release keys. Nothing was unpacked or executed. Download the installer from the official repository and retry.", "簽章不符合安裝腳本信任的發布公鑰，未解壓縮或執行任何下載程式。請從官方程式庫重新取得安裝腳本再重試。")
    # Parse and follow the manifest only after OpenSSL verifies its exact bytes.
    manifest = read_json(manifest_raw)
    version = manifest["version"]
    if not isinstance(version, str) or not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9]+(?:[.-][A-Za-z0-9]+)*)?", version):
        raise ValueError("release version")
    if requested and requested != version:
        refuse("release_version_mismatch", "The signed release differs from --version. Nothing was installed. Choose the intended published release and retry.", "簽署的發行版與 --version 不符，尚未安裝。請確認要安裝的正式版本再重試。")
    name = "clawdline_" + version + "_" + system + "_" + arch + ".tar.gz"
    artifacts = [item for item in manifest["artifacts"] if item.get("os") == system and item.get("arch") == arch and item.get("kind") == "daemon"]
    if len(artifacts) != 1:
        raise ValueError("one daemon artifact required")
    artifact = artifacts[0]
    size, digest = artifact["size"], artifact["sha256"]
    if artifact["name"] != name or type(size) is not int or not 0 < size <= ARTIFACT_BYTES or not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest):
        raise ValueError("signed artifact fields")
    archive = download(name, size, DOWNLOAD_SECONDS)
    with archive.open("rb") as source:
        actual = hashlib.file_digest(source, "sha256").hexdigest() if hasattr(hashlib, "file_digest") else None
        if actual is None:
            hasher = hashlib.sha256()
            for part in iter(lambda: source.read(io.DEFAULT_BUFFER_SIZE), b""):
                hasher.update(part)
            actual = hasher.hexdigest()
    if archive.stat().st_size != size or actual != digest:
        refuse("artifact_mismatch", "The archive does not match the signed size and SHA-256. Nothing was unpacked or executed. Retry the official release download.", "壓縮檔不符合已簽署的大小與 SHA-256，未解壓縮或執行。請重新下載官方發行版。")
    (work / "verified-version").write_text(version, encoding="ascii")
    (work / "verified-name").write_text(name, encoding="ascii")
    print("簽章與壓縮檔已驗證；接著才解壓縮並執行。" if chinese else "Release signature and archive verified before extraction and execution.")
except Exception:
    refuse("release_verification_failed", "Release download or verification failed. Nothing was unpacked or executed. Check the official release and your trusted Python 3 / OpenSSL 3 installation, then retry.", "發行版下載或驗證失敗，未解壓縮或執行。請檢查官方發行版及受信任的 Python 3、OpenSSL 3 安裝後重試。")
CLAWDLINE_VERIFY
  v=$(cat "$work/verified-version")
  name=$(cat "$work/verified-name")

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
  # A first install that setup refused before `current` named it (a bad
  # signature; no tmux in a build that carries none) takes the unpacked
  # release away again, so "Nothing was installed" is true.
  if [ "$status" != 0 ] && [ -e "$work/previous" ]; then
    mv "$dest" "$work/refused" && mv "$work/previous" "$dest"
  elif [ "$status" != 0 ] && [ "$(readlink "$root/current" 2>/dev/null)" != "releases/$v" ]; then
    mv "$dest" "$work/refused"
    rmdir "$root/releases" 2>/dev/null || true
  fi
  exit "$status"
}

main "$@"
