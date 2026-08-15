#!/usr/bin/env python3
"""Check a mascot pack before it ships.

Run it on your own pack while you build it, and on every pull request that adds one:

    python3 tools/validate-pack.py Resources/mascots/*.json

The app performs the same checks at load time and shows the reason in the hint row.
This script exists so a contributor finds out at review time instead, and so a
malformed pack can never land on main.
"""
import json
import re
import sys

REQUIRED_ROUTINES = {"idle"}
KNOWN_ROUTINES = {"pop", "idle", "typing", "dance", "cheer"}
HEX = re.compile(r"^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$")


def check(path):
    problems = []
    try:
        pack = json.load(open(path, encoding="utf-8"))
    except Exception as exc:
        return [f"not valid JSON: {exc}"]

    for key in ("name", "grid", "palette", "poses", "routines"):
        if key not in pack:
            problems.append(f'missing "{key}"')
    if problems:
        return problems

    cols, rows = pack["grid"].get("cols", 0), pack["grid"].get("rows", 0)
    if cols < 1 or rows < 1:
        problems.append("grid.cols and grid.rows must be positive")
    if cols > 40 or rows > 40:
        problems.append(f"grid is {cols}x{rows}; anything past 40 turns to mush at this size")

    for ch, spec in pack["palette"].items():
        if len(ch) != 1:
            problems.append(f'palette key "{ch}" must be a single character')
        if spec.lower() not in ("accent", "transparent", "none", "") and not HEX.match(spec):
            problems.append(f'palette "{ch}" is "{spec}"; expected accent, transparent or #RRGGBB')

    for name, grid in pack["poses"].items():
        if len(grid) != rows:
            problems.append(f'pose "{name}" has {len(grid)} rows, grid.rows says {rows}')
        for i, row in enumerate(grid):
            if len(row) != cols:
                problems.append(f'pose "{name}" row {i} is {len(row)} characters, grid.cols says {cols}')
            for ch in row:
                if ch not in pack["palette"]:
                    problems.append(f'pose "{name}" uses "{ch}", which is not in palette')
                    break

    for name in REQUIRED_ROUTINES - set(pack["routines"]):
        problems.append(f'routine "{name}" is required')
    for name, routine in pack["routines"].items():
        if name not in KNOWN_ROUTINES:
            problems.append(f'routine "{name}" is never triggered by the app (harmless, but nothing plays it)')
        if routine.get("duration", 0) <= 0:
            problems.append(f'routine "{name}" needs a positive duration')
        keys = routine.get("keys") or []
        if not keys:
            problems.append(f'routine "{name}" has no keys')
        for key in keys:
            t = key.get("t")
            if t is None or not 0 <= t <= 1:
                problems.append(f'routine "{name}" has a key at t={t}; t is a fraction from 0 to 1')
            if "pose" in key and key["pose"] not in pack["poses"]:
                problems.append(f'routine "{name}" refers to pose "{key["pose"]}", which does not exist')

    for ch in pack.get("eyeChars", []):
        if ch not in pack["palette"]:
            problems.append(f'eyeChars lists "{ch}", which is not in palette')
    skin = pack.get("skin")
    if skin is not None and skin not in pack["palette"]:
        problems.append(f'skin is "{skin}", which is not in palette')

    return problems


def main(paths):
    if not paths:
        print(__doc__)
        return 2
    failed = False
    for path in paths:
        problems = check(path)
        if problems:
            failed = True
            print(f"✗ {path}")
            for p in problems:
                print(f"    {p}")
        else:
            print(f"✓ {path}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
