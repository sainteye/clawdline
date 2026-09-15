#!/usr/bin/env python3
"""Export one Mac-served locale into the hosted console's static string catalog.

The translation remains authored in Sources/Copy+*.swift. This small exporter is the mechanical
bridge for the static Pages host, which cannot execute RemotePage.strings at request time.
Run it against the exact Clawdline build whose source is being prepared for deployment.

The catalog is written one key per line, sorted, so two branches that each add a sentence touch
different lines and a hand edit on a branch — which cannot run a build of its own source — is one
added line. ``tools/check-web-strings.py`` compares parsed JSON, so the layout never changes a verdict.
"""
import argparse
import json
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def catalog_text(value):
    """The catalog's bytes: one sorted key per line, stable for the same content."""
    return json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2, separators=(",", ": ")) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://127.0.0.1:7717")
    parser.add_argument("--language", default="zh-TW")
    parser.add_argument("--out", default=str(ROOT / "Resources/web/strings/zh-Hant.json"))
    args = parser.parse_args()
    request = urllib.request.Request(args.base_url.rstrip("/") + "/v1/strings",
                                     headers={"Accept-Language": args.language})
    with urllib.request.urlopen(request, timeout=10) as response:
        value = json.load(response)
    if value.get("lang") != "zh-Hant" or value.get("dir") != "ltr":
        raise SystemExit("the source did not answer with the Traditional Chinese catalog")
    if len(value) < 600:
        raise SystemExit("the source catalog is incomplete")
    target = Path(args.out).resolve()
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(catalog_text(value))
    print(f"export-hosted-strings: {len(value)} keys -> {target}")


if __name__ == "__main__":
    main()
