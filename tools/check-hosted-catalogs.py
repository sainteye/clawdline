#!/usr/bin/env python3
"""Verify every shipped product catalog on the served Cloud origin."""

import json
from pathlib import Path
import subprocess
import sys
from tempfile import TemporaryDirectory
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import Request, urlopen


LANGUAGES = ("en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de")
VALIDATOR = Path(__file__).resolve().parents[1] / "web/console/tools/validate-catalog.mjs"


def catalog(origin: str, tag: str, destination: Path) -> None:
    url = origin.rstrip("/") + "/catalogs/" + quote(tag) + ".json"
    try:
        # Cloudflare rejects urllib's default Python user agent even when the
        # same public catalog is available to browsers and named clients.
        request = Request(url, headers={"User-Agent": "clawdline-catalog-check/1"})
        with urlopen(request, timeout=20) as response:
            content = response.read()
        data = json.loads(content)
    except (HTTPError, URLError, TimeoutError, ValueError) as exc:
        raise ValueError(f"{tag}: could not read {url}: {exc}") from exc
    if not isinstance(data, dict):
        raise ValueError(f"{tag}: expected a JSON object at {url}")
    destination.write_bytes(content)


def validate(reference: Path, target: Path, tag: str, strict: bool) -> dict:
    command = ["node", str(VALIDATOR), "--reference", str(reference), "--target", str(target), "--tag", tag]
    if strict:
        command.append("--initial")
    try:
        result = subprocess.run(command, capture_output=True, text=True, check=False)
    except OSError as exc:
        raise ValueError(f"{tag}: catalog validator could not run: {exc}") from exc
    if result.returncode:
        details = "; ".join(result.stderr.splitlines()[:5]) or result.stdout.strip()
        raise ValueError(f"{tag}: catalog validation failed: {details}")
    try:
        report = json.loads(result.stdout)
    except ValueError as exc:
        raise ValueError(f"{tag}: invalid catalog validator report") from exc
    if not isinstance(report, dict) or any(not isinstance(report.get(key), (int, float)) for key in ("keys", "coverage", "missing", "untranslated")):
        raise ValueError(f"{tag}: incomplete catalog validator report")
    return report


def check(origin: str, strict: bool = False) -> None:
    if not VALIDATOR.is_file():
        raise ValueError(f"catalog validator missing: {VALIDATOR}")
    with TemporaryDirectory(prefix="clawdline-hosted-catalogs-") as temporary:
        directory = Path(temporary)
        for tag in LANGUAGES:
            catalog(origin, tag, directory / f"{tag}.json")
        reference = directory / "en.json"
        coverage = []
        for tag in LANGUAGES:
            report = validate(reference, directory / f"{tag}.json", tag, strict)
            coverage.append(f"{tag} {report['keys']} keys, {report['coverage']}% ({report['missing']} missing, {report['untranslated']} English)")
    print(f"hosted catalogs: {len(LANGUAGES)} languages; " + ", ".join(coverage))


if __name__ == "__main__":
    if len(sys.argv) not in (2, 3) or not sys.argv[1].startswith(("https://", "http://")) or (len(sys.argv) == 3 and sys.argv[2] != "--strict"):
        print("usage: tools/check-hosted-catalogs.py <origin> [--strict]", file=sys.stderr)
        sys.exit(2)
    try:
        check(sys.argv[1], strict=len(sys.argv) == 3)
    except ValueError as exc:
        print(f"FAILED: hosted catalogs: {exc}", file=sys.stderr)
        sys.exit(1)
