#!/usr/bin/env python3
"""Check the three names a web string crosses before it reaches the page.

The browser reads ``T.name``.  ``core/i18n.js`` supplies the English fallback under that
name, and ``/v1/strings`` supplies the translated value under that name.  A miss at either
boundary is otherwise a page which quietly prints ``undefined`` or stays in English.

The hosted console is a fourth place. It cannot ask a Mac for ``/v1/strings``, so it loads
``strings/zh-Hant.json``, exported from a running build by ``tools/export-hosted-strings.py``.
Nothing compared that file with the source: on 2026-09-15 it was 25 keys and 6 values behind
``Sources/Copy+Chinese.swift`` with every check here green. So the catalog must carry every key
``/v1/strings`` sends, and the value ``TraditionalChinese`` gives it wherever that value is a
plain one-line string literal; every other key is counted as skipped and said.
"""
import json
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
CATALOG = WEB / "strings" / "zh-Hant.json"
CHINESE = ROOT / "Sources" / "Copy+Chinese.swift"
NAME = r"[A-Za-z_$][A-Za-z0-9_$]*"
SWIFT_ESCAPES = {"\\": "\\", '"': '"', "'": "'", "n": "\n", "t": "\t", "r": "\r", "0": "\0"}


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


def swift_literal(body):
    """The value of a one-line Swift string literal's body, or None when it is not a plain one."""
    out = []
    at = 0
    while at < len(body):
        char = body[at]
        if char != "\\":
            out.append(char)
            at += 1
            continue
        following = body[at + 1:at + 2]
        if following in SWIFT_ESCAPES:
            out.append(SWIFT_ESCAPES[following])
            at += 2
            continue
        unicode = re.match(r"u\{([0-9A-Fa-f]{1,8})\}", body[at + 1:])
        if unicode:
            out.append(chr(int(unicode.group(1), 16)))
            at += 1 + unicode.end()
            continue
        return None  # an interpolation, or an escape this reader does not know: not plain
    return "".join(out)


def catalog_problems(payload, sent):
    """What the hosted catalog lacks or says differently, and the counts behind the verdict."""
    try:
        catalog = json.loads(CATALOG.read_text())
        chinese = CHINESE.read_text()
    except (OSError, ValueError) as error:
        fail(f"hosted catalog or TraditionalChinese unreadable: {error}")
    if not isinstance(catalog, dict) or not catalog:
        fail(f"hosted catalog {CATALOG.name} holds no keys")
    traditional = section(chinese, "struct TraditionalChinese: Copy {", "\n}\n", "TraditionalChinese")
    literals = {}
    for member, body in re.findall(
            r'^    let ([A-Za-z_][A-Za-z0-9_]*)(?:\s*:\s*String)?\s*=\s*"((?:[^"\\\n]|\\.)*)"\s*$',
            traditional, re.MULTILINE):
        value = swift_literal(body)
        if value is not None:
            literals[member] = value
    # A key sent as exactly `t.member` has that member's value; anything else — a function call, a
    # concatenation — is not a literal this reader can hold the catalog to.
    plain = dict(re.findall(rf'^\s*"({NAME})"\s*:\s*t\.([A-Za-z_][A-Za-z0-9_]*)\s*,?\s*(?:\]\)\s*)?$',
                            payload, re.MULTILINE))
    missing = sorted(sent - set(catalog))
    different = []
    checked = 0
    for key in sorted(sent & set(catalog)):
        member = plain.get(key)
        if member is None or member not in literals:
            continue
        checked += 1
        if catalog[key] != literals[member]:
            different.append(f"{key} (catalog {catalog[key]!r}, TraditionalChinese.{member} {literals[member]!r})")
    if not checked:
        fail("no key of the hosted catalog could be held to a TraditionalChinese literal; the reader found nothing")
    # The line a person adds by hand, for every missing key whose value this reader can hold.
    additions = {key: literals[plain[key]] for key in missing if plain.get(key) in literals}
    return missing, different, checked, len(sent) - checked, additions


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

    absent, different, checked, skipped, additions = catalog_problems(payload, sent)
    if absent or different:
        shown = (CATALOG.relative_to(ROOT) if ROOT in CATALOG.parents
                 else CATALOG.relative_to(WEB.parent) if WEB.parent in CATALOG.parents else CATALOG)
        print(f"hosted catalog {shown} does not match what a Mac sends:")
        if absent:
            print(f"  sent by /v1/strings but missing from the hosted catalog: {', '.join(absent)}")
        for line in different:
            print(f"  a different value in the hosted catalog than TraditionalChinese: {line}")
        # A branch cannot re-export: the Mac answering 127.0.0.1:7717 is the installed build, whose
        # source is not this branch, and exporting from it writes its words over the branch's own.
        print(f"  on a branch, edit {shown} by hand — one key per line, in sorted position:")
        for key in absent:
            if key in additions:
                print(f"    add {json.dumps(key)}: {json.dumps(additions[key], ensure_ascii=False)},  (TraditionalChinese's literal)")
            else:
                print(f"    add {json.dumps(key)} with the value /v1/strings sends for it (not a plain literal here)")
        if different:
            print("    and change each different value to TraditionalChinese's literal named above")
        print("  or, from a running build of exactly this source, re-export it: python3 tools/export-hosted-strings.py")
        return 1

    print(
        f"web strings agree: {len(reads)} read, {len(defined)} defined in T, "
        f"{len(sent)} sent by /v1/strings; {len(required_usage_reads)} Usage Portfolio protocol reads guarded; "
        f"hosted zh-Hant catalog carries all {len(sent)} sent keys, {checked} values equal TraditionalChinese's "
        f"literals, {skipped} keys skipped as non-literal"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
