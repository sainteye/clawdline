#!/usr/bin/env python3
"""Take the service worker out of the Swift app, byte for byte.

`web/console/public/sw.js` is a copy, and `tools/check-legacy-css.sh` cannot
check it: that script compares files under `Resources/web`, and these bytes are
a Swift string literal inside `RemotePage.serviceWorker()`. So this is the
extractor, and running it with `--check` is what notices that the source moved.

Three answers, as there: 0 is "they match", 1 is "they drifted", and 2 is "this
could not be checked" — a missing source must never be reported as the green one.

    tools/extract-service-worker.py            # rewrite the copy
    tools/extract-service-worker.py --check    # compare, change nothing
"""

import hashlib
import json
import os
import pathlib
import sys

SWIFT = pathlib.Path(
    os.environ.get("CLAWDLINE_SWIFT_TREE", pathlib.Path.home() / "code" / "clawdline")
) / "Sources" / "RemotePage.swift"
HERE = pathlib.Path(__file__).resolve().parent.parent
COPY = HERE / "web" / "console" / "public" / "sw.js"
MANIFEST = HERE / "web" / "console" / "src" / "legacy" / "MANIFEST.json"


def extract(source: str) -> str:
    '''The body of the Swift raw multiline literal in `serviceWorker()`,
    dedented as Swift dedents it: by the indentation of the closing delimiter.'''
    start = source.index("static func serviceWorker()")
    opened = source.index('#"""', start) + len('#"""\n')
    closed = source.index('"""#', opened)
    line_start = source.rfind("\n", 0, closed) + 1
    indent = closed - line_start
    out = []
    for line in source[opened:closed].split("\n"):
        out.append(line[indent:] if line.startswith(" " * indent) else line)
    body = "\n".join(out)
    return body if body.endswith("\n") else body + "\n"


def main() -> int:
    check = "--check" in sys.argv[1:]
    if not SWIFT.exists():
        print(f"cannot check: no Swift source at {SWIFT}", file=sys.stderr)
        return 2
    try:
        wanted = extract(SWIFT.read_text())
    except ValueError:
        print(f"cannot check: {SWIFT} no longer spells serviceWorker() the same way",
              file=sys.stderr)
        return 2

    swift_sha = hashlib.sha256(SWIFT.read_bytes()).hexdigest()[:16]
    copy_sha = hashlib.sha256(wanted.encode()).hexdigest()[:16]

    if check:
        drift = []
        if not COPY.exists() or COPY.read_text() != wanted:
            drift.append(str(COPY.relative_to(HERE)))
        recorded = json.loads(MANIFEST.read_text()).get("inlined", {}).get(
            "web/console/public/sw.js", {})
        if recorded.get("swiftSha") != swift_sha:
            drift.append(f"MANIFEST swiftSha {recorded.get('swiftSha')} -> {swift_sha}")
        if recorded.get("copySha") != copy_sha:
            drift.append(f"MANIFEST copySha {recorded.get('copySha')} -> {copy_sha}")
        if drift:
            for d in drift:
                print(f"drifted: {d}", file=sys.stderr)
            return 1
        print(f"service worker: the copy matches {SWIFT}")
        return 0

    COPY.write_text(wanted)
    manifest = json.loads(MANIFEST.read_text())
    inlined = manifest.setdefault("inlined", {})
    # Only this file's row is touched. The other entries under `inlined` are the
    # drawn assets — the marks and the manifest — and reproducing those needs
    # AppKit, so nothing here may claim to have checked them.
    inlined["web/console/public/sw.js"] = {
        "swiftSource": "~/code/clawdline/Sources/RemotePage.swift",
        "swiftSymbol": "RemotePage.serviceWorker()",
        "swiftSha": swift_sha,
        "copySha": copy_sha,
        "extractedWith": "tools/extract-service-worker.py",
    }
    MANIFEST.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    print(f"service worker: {len(wanted)} bytes -> {COPY.relative_to(HERE)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
