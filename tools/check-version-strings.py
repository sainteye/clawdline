#!/usr/bin/env python3
"""Refuse a Clawdline version typed into a file that nothing keeps in step with the app.

**What this is for.** `Sources/CodexNaming.swift` told `codex app-server` it was a release the app
had left behind two releases earlier. The string was correct on the day it was typed, nothing
compared it with anything, and it drifted in silence. That defect has a shape, and the shape is
larger than the one line: a version is a fact with exactly one home, and every other spelling of it
is either derived from that home or is a sentence about the past.

**Three states, not two.** A version literal in this tree is one of:

  bound to a source   it is not a literal at all — the site reads `CFBundleShortVersionString`, or
                      `Compat.releases`, or `appVersion()` in the test harness. Nothing to scan:
                      these sites are invisible here, which is the point of them.
  allowed, with a reason   release history, generated output, a fixture, or a true sentence about
                      an older release. Every entry in ALLOWED below carries the reason it is
                      there, because an allowlist entry with no reason guards nothing and reads
                      like one that does.
  drifting            neither of those. Red.

**What it looks for.** Not "any three numbers with dots in them" — this tree is full of Claude Code
and Codex versions that are nobody's business here. It looks for *this app's own* version numbers,
read out of `Sources/Compat.swift`'s release table and out of `build.sh`, so the set grows by itself
when a release is cut and never has to be typed here.

**And it checks the source agrees with itself**, because the whole arrangement rests on that: the
newest row of the release table is what `ClawdlineClientIdentity` falls back to in every process
without an app bundle, and it is only the right answer while it matches what `build.sh` stamps.

Exit 0 clean, 1 on drift or a stale entry, 2 when it could not scan anything — a guard that reads
no files must not report success, which is how six other checks on this machine spent a day
announcing that nothing was wrong with a directory they never opened.
"""

import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(os.environ.get("CLAWDLINE_VERSION_SCAN_ROOT",
                           Path(__file__).resolve().parent.parent)).resolve()

# Where the version is defined, and where its history is kept. Both are read rather than assumed:
# if either stops answering, this exits 2 rather than scanning for an empty set of versions and
# finding nothing wrong.
BUILD_SCRIPT = ROOT / "build.sh"
COMPAT_SOURCE = ROOT / "Sources" / "Compat.swift"

STAMPED = re.compile(r"CFBundleShortVersionString</key><string>([^<]*)</string>")
RELEASE_ROW = re.compile(r'Release\(clawdline:\s*"([^"]+)"')

# Every place this app's own version is written down as a literal, and why that is right there.
#
# `only` is a substring every allowed line in that file must contain. Where it is set, the entry
# allows a sentence rather than a file: a version stamped into `install.sh` for some new reason
# would still be refused, because the line would not be the one about older releases being ad-hoc
# signed. Where it is None, the whole file is history or fixture and there is nothing finer to say.
ALLOWED = [
    ("build.sh", "CFBundle",
     "the stamp itself — this is where the app's version is defined and everything else derives"),
    ("Sources/Compat.swift", "Release(clawdline:",
     "the release table: one row per version that shipped, and its newest row is the fallback "
     "ClawdlineClientIdentity uses in a process with no bundle to ask"),
    ("docs/compatibility.md", None,
     "generated from the table above by tools/build-compatibility.py, which test.sh re-runs with "
     "--check on every suite"),
    ("CHANGELOG.md", None,
     "the release history; naming versions that have shipped is what this document is"),
    ("README.md", "and including",
     "a true sentence about which releases are ad-hoc signed, which the install page needs"),
    ("README.zh-TW.md", "以及更早",
     "the same sentence in the other language"),
    ("install.sh", "and earlier",
     "the legacy download branch: the older releases really are ad-hoc signed, and this names "
     "them rather than stamping anything"),
    ("Tests/install-focused.mjs", None,
     "fixture release tags — the suite drives the legacy branch, so a real past tag is the fixture"),
    ("Tests/changelog-facts.mjs", None,
     "prose about which releases the CHANGELOG covers"),
    ("tools/release.sh", None,
     "the usage example, and the header recalling the release that was cut by hand"),
    ("test.sh", None,
     "a comment recalling that same hand-cut release"),
    ("docs/api.md", None,
     "recorded example responses; the version in them is whatever was installed when they were "
     "taken, and changing it would make them less true rather than more"),
    ("docs/remote.md", None,
     "the same, for the remote page's example reply"),
    ("Resources/web/app/js/net/mock.js", None,
     "a mock version deliberately unlike any real one, so a mock reading cannot be mistaken for a "
     "live one"),
    ("Resources/web/app/js/net/client.test.mjs", None,
     "recorded hello readings used as fixtures; the build stamp they assert is part of the fixture"),
    ("Sources/Controller.swift", "git log",
     "an example git command shown to a person, not a version this app reports"),
]

# Files git tracks that are not text. Read failures are counted and reported rather than ignored,
# because "it was binary" and "it was unreadable" are different answers and only one of them is
# expected.
def tracked_files():
    result = subprocess.run(["git", "-C", str(ROOT), "ls-files", "-z"],
                            capture_output=True)
    if result.returncode != 0:
        return None
    return [p for p in result.stdout.decode("utf-8", "surrogateescape").split("\0") if p]


def known_versions():
    """This app's own versions: what build.sh stamps, and every release in the table."""
    try:
        build = BUILD_SCRIPT.read_text(encoding="utf-8")
    except OSError:
        return None, None, "build.sh could not be read"
    stamped = STAMPED.findall(build)
    if len(stamped) != 1 or not stamped[0].strip():
        return None, None, ("build.sh does not carry exactly one non-empty "
                            "CFBundleShortVersionString (found %d)" % len(stamped))
    try:
        compat = COMPAT_SOURCE.read_text(encoding="utf-8")
    except OSError:
        return None, None, "Sources/Compat.swift could not be read"
    releases = RELEASE_ROW.findall(compat)
    if not releases:
        return None, None, "Sources/Compat.swift lists no releases, so there is nothing to scan for"
    return stamped[0].strip(), releases, None


def matcher(versions):
    """A version, not a fragment of a longer number.

    The left edge refuses a digit or a dot so a longer number cannot donate its tail, and lets a
    letter through so a tag written `v<version>` is still found. The right edge refuses only a
    digit: a version at the end of a sentence is followed by a full stop, and a match that stopped
    at punctuation would miss every one of those.
    """
    alternatives = "|".join(re.escape(v) for v in sorted(set(versions), key=len, reverse=True))
    return re.compile(r"(?<![\d.])(" + alternatives + r")(?!\d)")


def main():
    stamped, releases, why = known_versions()
    if why:
        sys.stderr.write("version_scan_no_source: %s\n" % why)
        return 2

    # The arrangement rests on this one line: the fallback every bundle-less process takes is the
    # newest row of the table, and it is only the right answer while it is this build's version.
    if releases[0] != stamped:
        print("version strings do not agree at their source:")
        print("  build.sh stamps            %s" % stamped)
        print("  Sources/Compat.swift's newest release is %s" % releases[0])
        print("  Everything deriving from the table would report the older of the two. Add the")
        print("  release row, or bump the stamp — but they cannot disagree.")
        return 1

    files = tracked_files()
    if files is None:
        sys.stderr.write("version_scan_no_files: git would not list the tracked files\n")
        return 2
    if not files:
        sys.stderr.write("version_scan_empty: git listed no tracked files under %s\n" % ROOT)
        return 2

    allowed_by_path = {path: (only, why) for path, only, why in ALLOWED}
    used = set()
    version = matcher([stamped] + releases)

    scanned = 0
    binary = 0
    literals = 0
    permitted = 0
    drifting = []

    for relative in files:
        path = ROOT / relative
        try:
            text = path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError):
            binary += 1
            continue
        scanned += 1
        rule = allowed_by_path.get(relative)
        for number, line in enumerate(text.split("\n"), start=1):
            found = version.search(line)
            if not found:
                continue
            literals += 1
            if rule is not None and (rule[0] is None or rule[0] in line):
                used.add(relative)
                permitted += 1
                continue
            drifting.append((relative, number, found.group(1), line.strip()[:120], rule))

    if scanned == 0:
        sys.stderr.write("version_scan_empty: %d tracked file(s) and none of them readable as "
                         "text\n" % len(files))
        return 2

    status = 0
    if drifting:
        status = 1
        print("a Clawdline version is typed into %d place(s) that nothing keeps in step:"
              % len(drifting))
        for relative, number, found, line, rule in drifting:
            print("  %s:%d  %s" % (relative, number, found))
            print("      %s" % line)
            if rule is not None:
                print("      %s is allowed only on lines containing %r — this line is not one"
                      % (relative, rule[0]))
        print()
        print("  Each of these is one of three things, and only the first two are allowed:")
        print("    derive it   Bundle.main's CFBundleShortVersionString, Compat.releases, or")
        print("                appVersion() in Tests/TestHarness.swift")
        print("    allow it    add it to ALLOWED in tools/check-version-strings.py *with the")
        print("                reason on the same entry* — history, generated output, a fixture,")
        print("                or a sentence about an older release")
        print("    or it is drift, which is what this check exists to find")

    # An entry that no longer matches anything is the same rot in the other direction: it reads as
    # a considered exception and is guarding a line that is not there any more.
    stale = [(path, why) for path, only, why in ALLOWED if path not in used]
    if stale:
        status = 1
        print()
        print("tools/check-version-strings.py allows %d place(s) that no longer carry a version:"
              % len(stale))
        for path, why in stale:
            print("  %s — %s" % (path, why))
        print("  Take the entry out; an exception nobody needs is indistinguishable from one")
        print("  nobody checked.")

    if status == 0:
        print("version strings agree: %s, in %d tracked file(s) scanned (%d binary), "
              "%d literal(s) found, all %d allowed by %d entries, 0 drifting"
              % (stamped, scanned, binary, literals, permitted, len(used)))
    return status


if __name__ == "__main__":
    sys.exit(main())
