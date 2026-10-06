#!/usr/bin/env python3
"""Verify every shipped product catalog on the served Cloud origin."""

import json
import re
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import urlopen


LANGUAGES = ("en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de")
HOLE = re.compile(r"\{[A-Za-z][A-Za-z0-9_]*\}")


def catalog(origin: str, tag: str) -> dict[str, str]:
    url = origin.rstrip("/") + "/catalogs/" + quote(tag) + ".json"
    try:
        with urlopen(url, timeout=20) as response:
            data = json.load(response)
    except (HTTPError, URLError, TimeoutError, ValueError) as exc:
        raise ValueError(f"{tag}: could not read {url}: {exc}") from exc
    if not isinstance(data, dict) or data.get("lang") != tag or data.get("dir") != "ltr":
        raise ValueError(f"{tag}: invalid catalog or language metadata at {url}")
    if any(not isinstance(key, str) or not isinstance(value, str) or not value for key, value in data.items()):
        raise ValueError(f"{tag}: empty or non-string catalog entry at {url}")
    return data


def check(origin: str) -> None:
    english = catalog(origin, "en")
    keys = set(english) - {"lang", "dir"}
    if not keys:
        raise ValueError("en: no product-copy keys")
    for tag in LANGUAGES[1:]:
        translated = catalog(origin, tag)
        actual = set(translated) - {"lang", "dir"}
        if actual != keys:
            raise ValueError(f"{tag}: missing {len(keys - actual)} and extra {len(actual - keys)} keys")
        for key in keys:
            if sorted(HOLE.findall(translated[key])) != sorted(HOLE.findall(english[key])):
                raise ValueError(f"{tag}: placeholders differ at {key}")
    print(f"hosted catalogs: {len(LANGUAGES)} languages, {len(keys)} matching keys and placeholders")


if __name__ == "__main__":
    if len(sys.argv) != 2 or not sys.argv[1].startswith(("https://", "http://")):
        print("usage: tools/check-hosted-catalogs.py <origin>", file=sys.stderr)
        sys.exit(2)
    try:
        check(sys.argv[1])
    except ValueError as exc:
        print(f"FAILED: hosted catalogs: {exc}", file=sys.stderr)
        sys.exit(1)
