"""The scan behind tools/check-machine-words.sh.

Reads NUL-separated paths on stdin, takes the allow list as argv[1], and exits
1 when a source string literal or shipped catalog value calls the machine a
Mac. See the shell script for why it skips comments and pinned copies.
"""

import json
import os
import re
import sys

SKIP_PREFIX = (
    # Copied from the retired Swift app and compared byte for byte by
    # check-legacy-css.sh. The bridge files beside them are this build's own
    # and are read.
    "web/console/src/legacy/js/",
    "web/console/public/strings/",
    # The macOS shell: a file that exists because the platform does.
    "shell/darwin/",
)
SKIP_EXACT = {
    # A list of catalog keys, not sentences displayed to a person.
    "web/console/public/catalogs/baseline-keys.json",
    # Declared copies of Copy+Chinese.swift. Each header says so, and says that
    # a word the original does not have is a word this build does not show.
    "web/console/src/pages/settings/window/copy.ts",
    "web/console/src/bar/words.ts",
    "web/console/src/pages/settings/shell.ts",
    # Generated from api/v1 by tools/contract-gen: fix the schema, not this.
    "web/contract/src/generated.ts",
    "internal/contract/zz_generated.go",
    # This scan and its allow list name the word they are looking for.
    "tools/machine-words.py",
}

MAC = re.compile(r"\bMacs?\b")
CATALOG_PREFIX = "web/console/public/catalogs/"
CATALOG_KEY = re.compile(r'^\s*("(?:\\.|[^"\\])*")\s*:')


def skipped(path):
    if path in SKIP_EXACT or any(path.startswith(p) for p in SKIP_PREFIX):
        return True
    # A _darwin file is built for macOS alone, so a Mac in one is this machine.
    base = os.path.basename(path)
    return base.endswith("_darwin.go") or base.endswith("_darwin_test.go")


def literals(src):
    """Every string literal in Go or TypeScript source, with its line number.

    Walked as a state machine rather than matched, because the alternative is
    what a first draft here did: a pattern for a quoted span matched the two
    apostrophes in an English comment and reported the prose between them as a
    shown sentence. A comment is never a literal, and that has to be decided by
    reading the file in order, not by looking at one span at a time.
    """
    out = []
    line = 1
    i = 0
    n = len(src)
    while i < n:
        ch = src[i]
        if ch == "\n":
            line += 1
            i += 1
        elif src.startswith("//", i):
            while i < n and src[i] != "\n":
                i += 1
        elif src.startswith("/*", i):
            end = src.find("*/", i + 2)
            end = n if end < 0 else end + 2
            line += src.count("\n", i, end)
            i = end
        elif ch in "\"'`":
            start, at = i, line
            i += 1
            raw = ch == "`"
            while i < n:
                if src[i] == "\\" and not raw:
                    i += 2
                    continue
                if src[i] == ch:
                    i += 1
                    break
                if src[i] == "\n":
                    line += 1
                    if not raw:  # an unterminated quote is not a literal
                        break
                i += 1
            out.append((at, src[start:i]))
        else:
            i += 1
    return out


def allow_list(path):
    allowed = set()
    with open(path, encoding="utf-8") as handle:
        for raw in handle:
            entry = raw.split("#", 1)[0].strip()
            if not entry:
                continue
            where, _, text = entry.partition("\t")
            allowed.add((where.strip(), text.strip()))
    return allowed


def catalog_values(src):
    """Return parsed product copy with source line numbers for review."""
    catalog = json.loads(src)
    if not isinstance(catalog, dict) or any(not isinstance(value, str) for value in catalog.values()):
        raise ValueError("catalog is not an object of strings")
    lines = {}
    for number, line in enumerate(src.splitlines(), 1):
        match = CATALOG_KEY.match(line)
        if match:
            lines[json.loads(match.group(1))] = number
    return [(lines.get(key, 1), key, value) for key, value in catalog.items()]


def main():
    allowed = allow_list(sys.argv[1])
    found = []
    read = 0
    for path in sys.stdin.read().split("\0"):
        if not path or skipped(path):
            continue
        read += 1
        try:
            src = open(path, encoding="utf-8").read()
        except OSError as err:
            print("cannot check: %s: %s" % (path, err), file=sys.stderr)
            return 2
        if path.startswith(CATALOG_PREFIX) and path.endswith(".json"):
            try:
                values = catalog_values(src)
            except (ValueError, json.JSONDecodeError) as err:
                print("cannot check: %s: %s" % (path, err), file=sys.stderr)
                return 2
            for number, key, value in values:
                text = "%s = %s" % (key, json.dumps(value, ensure_ascii=False))
                if MAC.search(value) and (path, text) not in allowed:
                    found.append((path, number, text))
        else:
            for number, text in literals(src):
                if MAC.search(text) and (path, text) not in allowed:
                    found.append((path, number, text))

    if read == 0:
        print("checked nothing; a zero-file run is a failure, not a pass", file=sys.stderr)
        return 2

    for path, number, text in found:
        print("%s:%d: a shown sentence calls the machine a Mac: %s" % (path, number, text),
              file=sys.stderr)
    if found:
        print("", file=sys.stderr)
        print("%d sentence(s) across %d files call the machine a Mac." % (len(found), read),
              file=sys.stderr)
        print("Say machine, or the name that machine goes by. Where Mac is the right",
              file=sys.stderr)
        print("word, add the line to tools/machine-words.allow with the reason.",
              file=sys.stderr)
        return 1
    print("machine words: %d files clean" % read)
    return 0


if __name__ == "__main__":
    sys.exit(main())
