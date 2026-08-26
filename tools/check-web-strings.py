#!/usr/bin/env python3
"""Check the three names a web string crosses before it reaches the page.

The browser reads ``T.name``.  ``core/i18n.js`` supplies the English fallback under that
name, and ``/v1/strings`` supplies the translated value under that name.  A miss at either
boundary is otherwise a page which quietly prints ``undefined`` or stays in English.
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
JS_ROOT = ROOT / "Resources" / "web" / "app" / "js"
I18N = JS_ROOT / "core" / "i18n.js"
SERVER = ROOT / "Sources" / "RemoteServer.swift"
NAME = r"[A-Za-z_$][A-Za-z0-9_$]*"


def fail(message):
    print(f"check-web-strings: {message}")
    sys.exit(2)


def section(text, start, end, source):
    try:
        first = text.index(start)
        last = text.index(end, first + len(start))
    except ValueError:
        fail(f"could not find the expected {source} payload boundaries")
    return text[first:last]


def main():
    try:
        modules = sorted(JS_ROOT.rglob("*.js"))
        i18n = I18N.read_text()
        server = SERVER.read_text()
    except OSError as error:
        fail(str(error))
    if not modules:
        fail(f"no JavaScript modules found under {JS_ROOT.relative_to(ROOT)}")

    reads = set()
    for module in modules:
        reads.update(re.findall(rf"\bT\.({NAME})", module.read_text()))

    object_body = section(i18n, "export var T = {", "\n};", "T")
    defined = set(re.findall(rf"^\s*({NAME})\s*:", object_body, re.MULTILINE))

    # Deliberately stop at stringsScript rather than scanning the whole Swift file.  Many other
    # routes build JSON dictionaries here, and names such as `ok`, `id`, and `build` are not
    # strings the page translates.  The name on the left is authoritative: some values come
    # from a differently named Copy member (for example webScheduleNext/settingsScheduleNext).
    payload = section(
        server,
        "private func strings(for request: Request)",
        "private func stringsScript(for request: Request)",
        "/v1/strings",
    )
    sent = set(
        re.findall(rf'^\s*"({NAME})"\s*:\s*t\.', payload, re.MULTILINE)
    )

    problems = [
        ("read but not defined in T", reads - defined),
        ("defined in T but not sent by /v1/strings", defined - sent),
        ("sent by /v1/strings but not defined in T", sent - defined),
    ]
    missing = [(label, keys) for label, keys in problems if keys]
    if missing:
        print("web string contract does not agree:")
        for label, keys in missing:
            print(f"  {label}: {', '.join(sorted(keys))}")
        return 1

    print(
        f"web strings agree: {len(reads)} read, {len(defined)} defined in T, "
        f"{len(sent)} sent by /v1/strings"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
