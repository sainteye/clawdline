#!/usr/bin/env python3
"""Inspect guide freshness, or pin one reviewed translation part to English.

A pin means a maintainer has checked that translated part against the current
English part. Never run --pin merely to clear a stale status: first update and
review the translation, including commands, flags, paths, and refusal codes.
"""

import argparse
import hashlib
import json
import re
from pathlib import Path

HERE = Path(__file__).resolve().parent
PARTS_FILE = HERE.parent / "sections.go"
MANIFEST = HERE / "freshness.json"
FILES = {
    "zh-Hant": "guide.zh-TW.md",
    "ja": "guide.ja.md",
    "zh-Hans": "guide.zh-Hans.md",
    "ko": "guide.ko.md",
    "es": "guide.es.md",
    "pt-BR": "guide.pt-BR.md",
    "fr": "guide.fr.md",
    "de": "guide.de.md",
}


def part_names():
    names = re.findall(r'^\s*\{"([a-z-]+)",', PARTS_FILE.read_text(), re.M)
    if len(names) < 20:
        raise ValueError(f"only {len(names)} guide parts found in {PARTS_FILE}")
    return ["intro", *names]


def parts(path, names):
    chunks = re.split(rb"(?m)^(?=## |### )", path.read_bytes())
    if len(chunks) != len(names):
        raise ValueError(f"{path} has {len(chunks) - 1} parts, expected {len(names) - 1}")
    return dict(zip(names, chunks))


def digest(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--pin", nargs=2, metavar=("LANGUAGE", "PART"))
    parser.add_argument("--require-current", action="store_true")
    args = parser.parse_args()
    names = part_names()
    english = parts(HERE / "guide.md", names)
    manifest = json.loads(MANIFEST.read_text())
    if args.pin:
        language, part = args.pin
        if language not in FILES or part not in names:
            parser.error(f"choose a language from {', '.join(FILES)} and a part from {', '.join(names)}")
        translation = parts(HERE / FILES[language], names)
        manifest.setdefault(language, {})[part] = {
            "source": digest(english[part]),
            "translation": digest(translation[part]),
        }
        MANIFEST.write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n")
        print(f"pinned {language} {part}")
        return
    stale = False
    for language, filename in FILES.items():
        try:
            translation = parts(HERE / filename, names)
        except (OSError, ValueError):
            translation = {}
        pending = [
            part for part in names
            if manifest.get(language, {}).get(part) != {
                "source": digest(english[part]),
                "translation": digest(translation.get(part, b"")),
            }
        ]
        print(f"{language}: {', '.join(pending) if pending else 'current'}")
        stale |= bool(pending)
    if args.require_current and stale:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
