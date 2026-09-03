#!/usr/bin/env python3
"""Check the three names a web string crosses before it reaches the page.

The browser reads ``T.name``.  ``core/i18n.js`` supplies the English fallback under that
name, and ``/v1/strings`` supplies the translated value under that name.  A miss at either
boundary is otherwise a page which quietly prints ``undefined`` or stays in English.
"""
import re
import sys
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
WEB = Path(os.environ.get("CLAWDLINE_WEB_ROOT", ROOT / "Resources" / "web"))
JS_ROOT = WEB / "app" / "js"
I18N = JS_ROOT / "core" / "i18n.js"
USAGE = JS_ROOT / "view" / "usage.js"
# The /v1/strings payload moved from RemoteServer.swift to RemotePage.swift when the page
# assets were extracted, and a check with one path pinned into it goes red on a pure
# relocation. Read whichever of them exist, concatenated, so the next move is invisible here.
SERVER_FILES = [ROOT / "Sources" / "RemotePage.swift", ROOT / "Sources" / "RemoteServer.swift"]
INDEX = Path(os.environ.get("CLAWDLINE_WEB_INDEX", WEB / "index.html"))
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
        usage = USAGE.read_text()
        server = "\n".join(f.read_text() for f in SERVER_FILES if f.exists())
        index = INDEX.read_text()
    except OSError as error:
        fail(str(error))
    if not modules:
        fail(f"no JavaScript modules found under {JS_ROOT.relative_to(ROOT)}")

    usage_strings = set(re.findall(r"['\"]([^'\"\n]{3,})['\"]", usage))
    required_usage_reads = {
        "/v1/orchestrator/usage/analytics?",
        "/v1/orchestrator/usage/analytics.csv?",
        "/v1/orchestrator/usage/analytics.json?",
        "usage_analytics_busy",
        "scan_limit_reached",
    }
    missing_usage = required_usage_reads - usage_strings
    if missing_usage:
        fail("Usage Portfolio module lost guarded protocol reads: "
             + ", ".join(sorted(missing_usage)))
    if not re.search(r"export\s+function\s+bindUsagePortfolio\s*\(", usage):
        fail("Usage Portfolio module lost its executable bindUsagePortfolio export")

    reads = set()
    for module in modules:
        reads.update(re.findall(rf"\bT\.({NAME})", module.read_text()))

    object_body = section(i18n, "export var T = {", "\n};", "T")
    defined = set(re.findall(rf"^\s*({NAME})\s*:", object_body, re.MULTILINE))

    # Deliberately stop at stringsScript rather than scanning the whole Swift file.  Many other
    # routes build JSON dictionaries here, and names such as `ok`, `id`, and `build` are not
    # strings the page translates.  The name on the left is authoritative: some values come
    # from a differently named Copy member (for example webScheduleNext/settingsScheduleNext).
    # Bounded by the function names alone. The full signatures were pinned here once, and a
    # relocation that changed `private func … (Request)` to `static func … (RemoteServer.Request)`
    # made both boundaries vanish — a pure move read as a missing payload.
    payload = section(
        server,
        "func strings(for request:",
        "func stringsScript(for request:",
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
        f"{len(sent)} sent by /v1/strings; {len(required_usage_reads)} Usage Portfolio protocol reads guarded"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
