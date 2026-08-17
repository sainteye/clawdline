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
        # Line comments first. They are allowed inside these literals and they contain colons,
        # which is enough to make the field split below read a whole comment as a field name.
        inner = "\n".join(re.sub(r"//.*$", "", line) for line in inner.splitlines())
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
    floors = [d["since"] for d in deps if d["since"][:1].isdigit()]
    minimum = max(floors, key=lambda v: [int(n) for n in v.split(".")]) if floors else None

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
        "## The short version",
        "",
        f"- Built and used against Claude Code **{built}**.",
        (f"- The oldest that everything here works with is **{minimum}**, and only one feature "
         "cares." if minimum else "- No feature here has a known floor."),
        "- Nothing refuses to run on an older one. What you lose is the one feature whose floor",
        "  you are under, and the second table below says which.",
        "- A **newer** Claude Code is the normal state of the world and is not warned about.",
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
        "| What | Where | Works since | If it changes |",
        "|---|---|---|---|",
    ]
    for d in deps:
        lines.append(f"| {d['what']} | `{d['where_']}` | {d['since']} | {d['symptom']} |")

    lines += [
        "",
        '"Not known to have a floor" is not a shrug. These have looked the same for a long time,',
        "nobody has gone back to find the version they started in, and putting a number there",
        'that nobody checked would make the whole column mean "probably".',
        "",
        "## Claude Code has its own dictation now",
        "",
        "`/voice` — hold space, and it is good. Where it differs is the whole reason to reach for",
        "this one instead:",
        "",
        "- It **streams your audio to Anthropic's servers**; its docs say \"audio is not processed",
        "  locally\".",
        "- It needs a **Claude.ai account** — not an API key, Bedrock, Vertex or Foundry — and is",
        "  unavailable under an organisation's HIPAA compliance setting.",
        "- It transcribes **one language at a time**.",
        "- **As of 2026-08-17 it does not support Chinese at all.** Twenty languages, Japanese",
        "  and Korean among them, and no variety of Chinese in the list; no `language` value",
        "  changed that. Checked against 2.1.233, which answered `\"Chinese\" is not a supported",
        "  dictation language; using English`. Dated rather than hedged: if it changes, this",
        "  line becomes history instead of becoming wrong.",
        "",
        "Clawdline's second pass never leaves the machine and is built for the sentence with two",
        "languages in it. See [whisper.md](whisper.md).",
    ]

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
