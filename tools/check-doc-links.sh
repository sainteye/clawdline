#!/bin/bash
# Check every local link and image target in docs Markdown. External URLs,
# absolute paths and in-page anchors are deliberately outside this check.
set -uo pipefail
cd "$(dirname "$0")/.."

python3 - <<'PY'
from pathlib import Path
from urllib.parse import unquote
import re
import sys

root = Path.cwd()
docs = root / "docs"
if not docs.is_dir():
    print("cannot check: docs directory is missing", file=sys.stderr)
    raise SystemExit(2)

inline = re.compile(r"!?\[[^\]\n]*\]\(([^)\n]+)\)")
html = re.compile(r"\b(?:href|src)\s*=\s*['\"]([^'\"]+)['\"]", re.IGNORECASE)
reference = re.compile(r"^\s*\[[^\]\n]+\]:\s*(?:<([^>]+)>|(\S+))")
scheme = re.compile(r"^[A-Za-z][A-Za-z0-9+.-]*:")
code_span = re.compile(r"`[^`]*`")


def destination(raw: str) -> str:
    value = raw.strip()
    if value.startswith("<") and ">" in value:
        value = value[1:value.index(">")]
    elif " " in value or "\t" in value:
        value = value.split()[0]
    return unquote(value.split("#", 1)[0].split("?", 1)[0])


findings = []
for markdown in sorted(docs.rglob("*.md")):
    fenced = False
    marker = None
    try:
        lines = markdown.read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeError) as error:
        print(f"cannot check: {markdown.relative_to(root)}: {error}", file=sys.stderr)
        raise SystemExit(2)

    for number, original in enumerate(lines, 1):
        stripped = original.lstrip()
        fence = re.match(r"(```+|~~~+)", stripped)
        if fence:
            token = fence.group(1)[0]
            if not fenced:
                fenced = True
                marker = token
            elif token == marker:
                fenced = False
                marker = None
            continue
        if fenced:
            continue

        line = code_span.sub("", original)
        targets = [match.group(1) for match in inline.finditer(line)]
        targets.extend(match.group(1) for match in html.finditer(line))
        definition = reference.match(line)
        if definition:
            targets.append(definition.group(1) or definition.group(2))

        for raw in targets:
            target = destination(raw)
            if (not target or target.startswith(("#", "/", "//")) or
                    scheme.match(target) or "{" in target or "}" in target):
                continue
            resolved = markdown.parent / target
            if not resolved.exists():
                findings.append(
                    f"{markdown.relative_to(root)}:{number}: missing relative target: {target}"
                )

if findings:
    print("\n".join(findings))
    print(f"documentation link check failed: {len(findings)} missing target(s)", file=sys.stderr)
    raise SystemExit(1)

count = sum(1 for _ in docs.rglob("*.md"))
print(f"documentation link check passed: {count} Markdown file(s)")
PY
