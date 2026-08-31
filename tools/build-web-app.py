#!/usr/bin/env python3
"""Build the hosted console bundle for app.clawdline.com.

One command, no CI, no network, and the same bytes every time it runs — because the thing this
produces is uploaded by a person following `docs/cloud.md`, and "is what I am about to deploy
the reviewed tree?" has to be answerable by comparing a hash rather than by trusting a build
log nobody kept.

What it does is the static half of what `RemoteServer.page` does at request time:

  * stamps every `/app/` URL with a content hash, so the assets under it can be immutable;
  * fills `<!-- clawdline:modules -->` with a `modulepreload` for every module but the entry;
  * fills `<!-- clawdline:cloud -->` with the origins this build is for, which is the whole of
    how `net/cloud-boot.js` knows it is a cloud console;
  * drops the `apple-touch-startup-image` links, because the Mac draws those on demand and a
    static host has nothing behind those twenty URLs;
  * writes the manifest and the icons the Mac would otherwise render, taking the icon bytes
    straight out of `Resources/Clawdline.icns` so they are copies rather than a second
    renderer's approximation;
  * writes `_headers` with the immutability the stamp earns and a CSP whose inline-script
    hashes are computed from the exact scripts emitted.

Determinism is the acceptance condition, so nothing here reads the clock, the environment, or
the filesystem's ordering.
"""
import argparse
import base64
import hashlib
import json
import re
import shutil
import struct
import sys
from pathlib import Path
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parent.parent
WEB = ROOT / "Resources" / "web"
APP = WEB / "app"
INDEX = WEB / "index.html"
ICNS = ROOT / "Resources" / "Clawdline.icns"

CLOUD_SLOT = "<!-- clawdline:cloud -->"
MODULES_SLOT = "<!-- clawdline:modules -->"
STARTUP_IMAGE = re.compile(
    r"[ \t]*<link rel=\"apple-touch-startup-image\"[^>]*>\n", re.MULTILINE)
INLINE_SCRIPT = re.compile(r"<script>(.*?)</script>", re.DOTALL)

# Which ICNS entries are PNG streams, and the square they are. `ic10`/`ic14` are the same
# 1024 image under two names; only one of each size is emitted.
ICNS_PNG_SIZES = {"ic07": 128, "ic08": 256, "ic09": 512, "ic10": 1024, "ic12": 64, "ic13": 512,
                  "ic14": 1024}


def fail(message):
    print(f"build-web-app: {message}", file=sys.stderr)
    return 2


def sha256_hex(data):
    return hashlib.sha256(data).hexdigest()


def sha256_b64(data):
    return base64.b64encode(hashlib.sha256(data).digest()).decode("ascii")


def collect_assets():
    """Every file under `Resources/web/app`, relative and sorted. The directory is the list."""
    files = sorted(
        path for path in APP.rglob("*")
        if path.is_file() and path.suffix in {".css", ".js", ".txt"}
        and not path.name.endswith(".test.mjs")
    )
    return [(path.relative_to(APP).as_posix(), path.read_bytes()) for path in files]


def icns_pngs(data):
    """The PNG streams already inside the icon file, by square. Copies, not conversions."""
    if data[:4] != b"icns":
        raise ValueError("Resources/Clawdline.icns is not an ICNS container")
    total = struct.unpack(">I", data[4:8])[0]
    if total > len(data):
        raise ValueError("the ICNS header claims more bytes than the file has")
    found = {}
    offset = 8
    while offset + 8 <= total:
        kind = data[offset:offset + 4].decode("latin1")
        length = struct.unpack(">I", data[offset + 4:offset + 8])[0]
        if length < 8 or offset + length > total:
            raise ValueError("an ICNS entry runs past the end of the container")
        body = data[offset + 8:offset + length]
        size = ICNS_PNG_SIZES.get(kind)
        if size is not None and body[:8] == b"\x89PNG\r\n\x1a\n" and size not in found:
            found[size] = body
        offset += length
    return dict(sorted(found.items()))


def cloud_declaration(app_origin, api_origin, relay_url, stamp):
    body = json.dumps({
        "v": 1,
        "app_origin": app_origin,
        "api_origin": api_origin,
        "relay_url": relay_url,
        "build": stamp,
    }, sort_keys=True, separators=(",", ":"))
    # `<` escaped throughout for the reason `RemoteServer.stringsScript` gives: a `</script>`
    # anywhere inside would end the element early, and `<` is legal in both languages.
    return "<script>window.__clawdlineCloud = " + body.replace("<", "\\u003c") + ";</script>"


def module_preloads(assets, stamp):
    names = sorted(
        name[len("js/"):] for name, _ in assets
        if name.startswith("js/") and name != "js/main.js"
    )
    return "\n".join(
        f'<link rel="modulepreload" href="/app/{stamp}/js/{name}">' for name in names)


def build(out, app_origin, api_origin, relay_url):
    assets = collect_assets()
    if not assets:
        return fail("no console assets were found under Resources/web/app")
    index_source = INDEX.read_text()
    for slot in (CLOUD_SLOT, MODULES_SLOT):
        if index_source.count(slot) != 1:
            return fail(f"index.html must contain exactly one {slot}")

    # The stamp names the asset bytes and the declaration, and nothing else — no clock, no
    # path, no build counter. Two runs of the same tree therefore produce the same URLs.
    digest = hashlib.sha256()
    for name, body in assets:
        digest.update(name.encode("utf-8"))
        digest.update(b"\0")
        digest.update(sha256_hex(body).encode("ascii"))
        digest.update(b"\0")
    digest.update("|".join([app_origin, api_origin, relay_url]).encode("utf-8"))
    stamp = "b" + digest.hexdigest()[:24]

    declaration = cloud_declaration(app_origin, api_origin, relay_url, stamp)
    html = index_source.replace('"/app/', f'"/app/{stamp}/')
    html = html.replace(MODULES_SLOT, module_preloads(assets, stamp))
    html = html.replace(CLOUD_SLOT, declaration)
    html, removed = STARTUP_IMAGE.subn("", html)

    inline_hashes = [
        "'sha256-" + sha256_b64(script.encode("utf-8")) + "'"
        for script in INLINE_SCRIPT.findall(html)
    ]
    if not inline_hashes:
        return fail("no inline script was found to hash for the CSP")

    if out.exists():
        shutil.rmtree(out)
    written = {}

    def write(relative, body):
        target = out / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(body)
        written[relative] = sha256_hex(body)

    for name, body in assets:
        write(f"app/{stamp}/{name}", body)

    icons = icns_pngs(ICNS.read_bytes())
    for size, body in icons.items():
        write(f"icon-{size}.png", body)

    manifest = {
        "id": "/",
        "name": "Clawdline",
        "short_name": "Clawdline",
        "display": "standalone",
        "background_color": "#0e0e11",
        "theme_color": "#0e0e11",
        "start_url": "/",
        "scope": "/",
        "icons": [
            {"src": f"/icon-{size}.png", "sizes": f"{size}x{size}", "type": "image/png",
             "purpose": "any maskable"}
            for size in icons
        ],
    }
    write("manifest.webmanifest",
          (json.dumps(manifest, indent=2, sort_keys=True) + "\n").encode("utf-8"))

    api_host = urlsplit(api_origin)
    relay_host = urlsplit(relay_url)
    policy = "; ".join([
        "default-src 'none'",
        "script-src 'self' " + " ".join(inline_hashes),
        "style-src 'self'",
        "img-src 'self' data:",
        f"connect-src 'self' {api_host.scheme}://{api_host.netloc} "
        f"{relay_host.scheme}://{relay_host.netloc}",
        "manifest-src 'self'",
        "base-uri 'none'",
        "form-action 'none'",
        "frame-ancestors 'none'",
    ])
    headers = "\n".join([
        "/*",
        "  X-Content-Type-Options: nosniff",
        "  Referrer-Policy: no-referrer",
        "  Cross-Origin-Opener-Policy: same-origin",
        "  Content-Security-Policy: " + policy,
        "",
        "/app/*",
        "  Cache-Control: public, max-age=31536000, immutable",
        "",
        "/",
        "  Cache-Control: no-store",
        "",
        "/index.html",
        "  Cache-Control: no-store",
        "",
        "/manifest.webmanifest",
        "  Cache-Control: public, max-age=300",
        "",
    ])
    write("_headers", headers.encode("utf-8"))
    write("index.html", html.encode("utf-8"))

    write("BUILD.json", (json.dumps({
        "v": 1,
        "stamp": stamp,
        "app_origin": app_origin,
        "api_origin": api_origin,
        "relay_url": relay_url,
        "assets": len(assets),
        "startup_images_removed": removed,
    }, indent=2, sort_keys=True) + "\n").encode("utf-8"))

    lines = [f"{written[name]}  {name}\n" for name in sorted(written) if name != "SHA256SUMS"]
    (out / "SHA256SUMS").write_text("".join(lines))

    print(f"build-web-app: {len(written) + 1} files, stamp {stamp}, "
          f"{len(assets)} assets, {len(icons)} icons, {removed} startup images removed")
    print("build-web-app: " + sha256_hex((out / "SHA256SUMS").read_bytes()) + "  SHA256SUMS")
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", default=str(ROOT / "dist" / "app-console"))
    parser.add_argument("--app-origin", default="https://app.clawdline.com")
    parser.add_argument("--api-origin", default="https://api.clawdline.com")
    parser.add_argument("--relay-url", default="wss://relay.clawdline.com/v1/connect")
    args = parser.parse_args()
    for name, value in (("--app-origin", args.app_origin), ("--api-origin", args.api_origin)):
        if not value.startswith("https://"):
            return fail(f"{name} must be an https origin")
    if not args.relay_url.startswith("wss://"):
        return fail("--relay-url must be a wss URL")
    return build(Path(args.out).resolve(), args.app_origin, args.api_origin, args.relay_url)


if __name__ == "__main__":
    sys.exit(main())
