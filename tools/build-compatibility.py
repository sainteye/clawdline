#!/usr/bin/env python3
"""Write docs/compatibility.md from the table in Sources/Compat.swift.

The page and the app must not be able to disagree, and the only way to guarantee that is for one
of them to be built from the other. `./test.sh` regenerates and compares, so a release added to
the Swift table and not to the page is a failing test rather than a page that is quietly a
version behind.

Usage: tools/build-compatibility.py [--check]
"""
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / "Sources" / "Compat.swift"
PAGE = ROOT / "docs" / "compatibility.md"


def swift_string(text):
    """Join Swift's `"a" + "b"` string concatenations and unescape what little we use."""
    parts = re.findall(r'"((?:[^"\\]|\\.)*)"', text)
    return "".join(parts).replace('\\"', '"').replace("\\\\", "\\")


def structs(kind):
    """Every `Kind(...)` literal in the source, as a list of {label: value}."""
    body = SOURCE.read_text()
    out = []
    for match in re.finditer(kind + r"\(", body):
        depth, i = 0, match.end() - 1
        while i < len(body):
            if body[i] == "(":
                depth += 1
            elif body[i] == ")":
                depth -= 1
                if depth == 0:
                    break
            i += 1
        inner = body[match.end():i]
        fields, current, label = {}, [], None
        for piece in re.split(r",(?=\s*\w+:)", inner):
            name, _, value = piece.partition(":")
            fields[name.strip()] = swift_string(value)
        out.append(fields)
        del current, label
    return out


def render():
    releases = structs("Release")
    deps = structs("Dependency")
    built = next((r["claudeCode"] for r in releases if r["claudeCode"][:1].isdigit()), "?")

    lines = [
        "# Versions",
        "",
        "<!-- Generated from Sources/Compat.swift by tools/build-compatibility.py. Do not edit. -->",
        "",
        "Clawdline reads things Claude Code was never obliged to keep still: a transcript file, a",
        "spinner drawn on a terminal, a process name, a clipboard convention. That is a reasonable",
        "way to build this and an unreasonable thing to leave unwritten, because **each of those",
        "changing looks exactly like Clawdline being broken.** This page is what it was run",
        "against, and what you would see if that stopped being true.",
        "",
        "## Tested against",
        "",
        "| Clawdline | Claude Code | |",
        "|---|---|---|",
    ]
    for r in releases:
        lines.append(f"| {r['clawdline']} | {r['claudeCode']} | {r['notes']} |")

    lines += [
        "",
        "The Claude Code column is the version somebody actually had installed while using that",
        "release — not a supported range. A range nobody tried is how a compatibility table starts",
        "saying things that are not true.",
        "",
        "**A newer Claude Code is the normal state of the world.** It updates itself and this does",
        f"not, so nothing warns about it. Older than {built} does get a line in the menu bar, because",
        "then a missing feature really is missing rather than broken here.",
        "",
        "## What it depends on, and how you would know",
        "",
        "| What | Where | If it changes |",
        "|---|---|---|",
    ]
    for d in deps:
        lines.append(f"| {d['what']} | `{d['where_']}` | {d['symptom']} |")

    lines += [
        "",
        "## claude-tools",
        "",
        "No version of it is pinned, on purpose. Clawdline reads the **files**, and",
        "[docs/project-status.md](project-status.md) is the contract for them —",
        "[claude-tools](https://github.com/sainteye/claude-tools) is one producer, and a cron job",
        "or a git hook that writes the same shapes is another. Naming a version of it here would",
        "say something untrue about everything else that writes them.",
        "",
        "A missing or unreadable status file is a normal state rather than an error, so there is",
        "nothing to warn about: the footer simply has less to say. That is the whole compatibility",
        "story, and it is short because the coupling is a documented file format rather than a",
        "program's internals.",
        "",
    ]
    return "\n".join(lines)


def main():
    want = render()
    if "--check" in sys.argv:
        have = PAGE.read_text() if PAGE.exists() else ""
        if have != want:
            print("docs/compatibility.md is out of date — run tools/build-compatibility.py")
            subprocess.run(["diff", "-u", str(PAGE), "-"], input=want, text=True)
            sys.exit(1)
        return
    PAGE.write_text(want)
    print(f"wrote {PAGE.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
