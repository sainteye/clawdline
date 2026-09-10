#!/usr/bin/env python3
"""Fixed-Git-tree source observations; no compiler, runtime or ownership inference."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys


SCHEMA_VERSION = 1
BUILD_INPUTS = (
    "Package.swift", "build.sh", "test.sh", "tools/swift-source-manifest.sh",
    "tools/check-architecture-boundaries.sh",
)
IMPORT = re.compile(
    r"^\s*(?:(?:@testable|@preconcurrency|@_exported|@_implementationOnly)\s+)*"
    r"import\s+(?:(?:typealias|struct|class|enum|protocol|let|var|func)\s+)?"
    r"([A-Za-z_][A-Za-z_0-9]*)", re.MULTILINE,
)
LOCK = re.compile(r"\b(?:NSLock|NSRecursiveLock)\s*\(|\.\s*(?:lock|unlock|withLock)\s*\(")
HELD_DOOR = re.compile(r"\bwithTransactionOnHeldLock\s*(?:\(|\{)")
IDENTIFIER = re.compile(r"\b[A-Za-z_][A-Za-z_0-9]*\b")


class InventoryError(Exception):
    def __init__(self, code, detail):
        self.code = code
        super().__init__(detail)


def fail(code, detail):
    raise InventoryError(code, detail)


def git(repo, *args, data=None):
    # Git 2.38 ignores GIT_NO_LAZY_FETCH. Refuse promisor configuration before object
    # lookup (below), and deny every transport even if local config changes afterwards.
    env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
    env.update(GIT_NO_REPLACE_OBJECTS="1", GIT_NO_LAZY_FETCH="1",
               GIT_ALLOW_PROTOCOL="",
               GIT_TERMINAL_PROMPT="0", GIT_CONFIG_NOSYSTEM="1",
               GIT_CONFIG_GLOBAL=os.devnull, LC_ALL="C")
    result = subprocess.run(["git", "-C", str(repo), *args], input=data,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env)
    if result.returncode:
        # Do not copy arbitrary Git stderr (paths/config can carry credentials) into reports.
        fail("inventory_git_unreadable", f"git {args[0]} failed (exit {result.returncode})")
    return result.stdout


def digest(value):
    return hashlib.sha256(value).hexdigest()


def canonical(value):
    return json.dumps(value, ensure_ascii=True, sort_keys=True, separators=(",", ":")).encode()


def selected(path):
    return path in BUILD_INPUTS or (
        path.startswith(("Sources/", "Tests/")) and path.endswith(".swift")
    )


def require_local_objects(repo):
    # --list includes repository/worktree config and includes, without resolving objects.
    # Reject the presence of either promisor key, including false/empty values: this is
    # deliberately conservative and independent of Git's version-specific lazy-fetch flag.
    for record in git(repo, "config", "--null", "--list").split(b"\0"):
        key = record.partition(b"\n")[0].lower()
        if key == b"extensions.partialclone" or re.fullmatch(rb"remote\..+\.promisor", key):
            fail("inventory_promisor_unsupported", "requires a local repository without promisor configuration")


def read_objects(repo, requests):
    """Read exact raw objects; verify their actual type and hash before using any bytes."""
    payload = git(repo, "cat-file", "--batch",
                  data="".join(oid + "\n" for oid, _, _ in requests).encode())
    objects = {}
    position = 0
    for oid, kinds, code in requests:
        end = payload.find(b"\n", position)
        header = payload[position:end].split()
        if (end < 0 or len(header) != 3 or header[0] != oid.encode()
                or header[1] not in kinds or not re.fullmatch(rb"[0-9]+", header[2])):
            fail(code, "missing object or unexpected object type/size")
        kind = header[1]
        size = int(header[2])
        start = end + 1
        raw = payload[start:start + size]
        if len(raw) != size or payload[start + size:start + size + 1] != b"\n":
            fail(code, "truncated object")
        algorithm = hashlib.sha1 if len(oid) == 40 else hashlib.sha256
        actual = algorithm(kind + b" " + str(size).encode() + b"\0" + raw).hexdigest()
        if actual != oid:
            fail(code, "object hash mismatch")
        objects[oid] = (kind, raw)
        position = start + size + 1
    if position != len(payload):
        fail("inventory_git_unreadable", "unconsumed batch output")
    return objects


def tree_entries(raw, oid_size):
    """Parse the already verified raw tree, so enumeration uses those same bytes."""
    position = 0
    names = set()
    while position < len(raw):
        space = raw.find(b" ", position)
        end = raw.find(b"\0", space + 1) if space >= 0 else -1
        if space < 0 or end < 0 or end + 1 + oid_size > len(raw):
            fail("inventory_tree_unreadable", "truncated tree entry")
        mode = raw[position:space].decode("ascii")
        name = raw[space + 1:end].decode("utf-8")
        if not name or name in (".", "..") or "/" in name or name in names:
            fail("inventory_invalid_entry", "invalid or duplicate tree entry name")
        names.add(name)
        if mode not in ("40000", "040000", "100644", "100755", "120000", "160000"):
            fail("inventory_invalid_entry", "unsupported tree entry mode")
        oid = raw[end + 1:end + 1 + oid_size].hex()
        position = end + 1 + oid_size
        yield mode, name, oid


def read_tree(repo, requested):
    if not re.fullmatch(r"(?:[a-f0-9]{40}|[a-f0-9]{64})", requested):
        fail("inventory_fixed_tree_required", "--tree requires a full lowercase commit/tree object ID")
    require_local_objects(repo)
    kind, raw = read_objects(repo, [(requested, (b"commit", b"tree"), "inventory_git_unreadable")])[requested]
    tree = requested
    known_trees = {}
    if kind == b"commit":
        match = re.fullmatch(rb"tree ([a-f0-9]{" + str(len(requested)).encode() + rb"})", raw.split(b"\n", 1)[0])
        if match is None:
            fail("inventory_git_unreadable", "commit has no valid root tree")
        tree = match[1].decode("ascii")
    else:
        known_trees[tree] = (kind, raw)
    entries = []
    pending = [("", tree)]
    while pending:
        needed = dict.fromkeys(oid for _, oid in pending if oid not in known_trees)
        if needed:
            known_trees.update(read_objects(repo, [(oid, (b"tree",), "inventory_tree_unreadable") for oid in needed]))
        following = []
        for prefix, tree_oid in pending:
            for mode, name, oid in tree_entries(known_trees[tree_oid][1], len(tree_oid) // 2):
                path = prefix + name
                if mode in ("40000", "040000"):
                    following.append((path + "/", oid))
                    continue
                if path.startswith(("Sources/", "Tests/")) and mode in ("120000", "160000"):
                    fail("inventory_nonregular_source", f"source subtree contains a symlink or submodule: {path}")
                if not selected(path):
                    continue
                if any(ord(c) < 32 or c in "`|\\" for c in path):
                    fail("inventory_invalid_entry", "selected path cannot be represented safely")
                if mode not in ("100644", "100755"):
                    fail("inventory_nonregular_source", f"selected path is not a regular blob: {path}")
                entries.append({"path": path, "mode": mode, "blob": oid})
        pending = following
    entries.sort(key=lambda row: row["path"])
    if not entries:
        fail("inventory_empty", "named tree has no selected source or build inventory")
    paths = {row["path"] for row in entries}
    missing = [path for path in BUILD_INPUTS if path not in paths]
    if (missing or not any(path.startswith("Sources/") for path in paths)
            or not any(path.startswith("Tests/") for path in paths)):
        fail("inventory_incomplete", "requires non-empty Sources/ and Tests/ Swift partitions and every build reference")
    blobs = read_objects(repo, [(oid, (b"blob",), "inventory_blob_unreadable")
                               for oid in dict.fromkeys(row["blob"] for row in entries)])
    for row in entries:
        blob = blobs[row["blob"]][1]
        try:
            text = blob.decode("utf-8")
        except UnicodeError:
            fail("inventory_source_unreadable", f"source is not UTF-8: {row['path']}")
        if not text.strip() or "\0" in text:
            fail("inventory_source_unreadable", f"source is empty or contains NUL: {row['path']}")
        row.update(bytes=len(blob), sha256=digest(blob), lines=len(text.splitlines()), text=text)
    return tree, entries


def occurrences(pattern, text):
    return [{"line": text.count("\n", 0, match.start()) + 1,
             "spelling": match.group(0).strip()} for match in pattern.finditer(text)]


def inventory(repo, requested):
    tree, entries = read_tree(repo, requested)
    evidence = [{k: row[k] for k in ("path", "mode", "blob", "bytes", "sha256")}
                for row in entries]
    production = [row for row in entries if row["path"].startswith("Sources/")]
    tests = [row for row in entries if row["path"].startswith("Tests/")]
    # A basename is a search cue, not a type or module identity. Keep ambiguous basenames.
    names = {}
    for row in production:
        stem = Path(row["path"]).stem
        if re.fullmatch(r"[A-Z][A-Za-z_0-9]*", stem):
            names.setdefault(stem, []).append(row["path"])
    edges = []
    imports = {}
    files = []
    for row in entries:
        text = row["text"]
        item = {k: v for k, v in row.items() if k != "text"}
        item["partition"] = ("production" if row in production else
                             "tests" if row in tests else "build_reference")
        item["imports"] = [{"module": m.group(1),
                            "line": text.count("\n", 0, m.start(1)) + 1}
                           for m in IMPORT.finditer(text)] if row["path"].endswith(".swift") else []
        item["lock_spellings"] = occurrences(LOCK, text) if row in production else []
        item["held_door_spellings"] = occurrences(HELD_DOOR, text) if row in production else []
        files.append(item)
        if row not in production:
            continue
        for module in sorted({entry["module"] for entry in item["imports"]}):
            imports.setdefault(module, []).append(row["path"])
        mentions = {}
        for match in IDENTIFIER.finditer(text):
            name = match.group(0)
            if name in names:
                mentions.setdefault(name, []).append(text.count("\n", 0, match.start()) + 1)
        for name, lines in sorted(mentions.items()):
            for target in names[name]:
                if target != row["path"]:
                    edges.append({"from": row["path"], "to": target,
                                  "basename": name, "count": len(lines), "lines": lines})
    return {
        "schema_version": SCHEMA_VERSION,
        "observed_tree": tree,
        "generator_sha256": digest(Path(__file__).read_bytes()),
        "inventory_sha256": digest(canonical(evidence)),
        "evidence_kind": "fixed_git_blobs_and_raw_lexical_candidates",
        "limits": [
            "Reads only the named Git tree; excludes worktree/index/untracked overlays.",
            "Promisor-configured repositories are refused before object lookup, even when complete locally; all Git transports are disabled.",
            "Verifies the requested commit/root, every traversed tree and every selected blob by raw object type and hash before using their bytes.",
            "Imports, basename mentions and lock spellings are raw lexical candidates; comments, strings and inactive #if branches can match.",
            "Basename edges are not resolved symbols, dependencies, call graphs or compiler target edges.",
            "No compilation, runtime reachability, lock correctness, platform support, state ownership or release acceptance is inferred.",
            "Build references are bytes, not evaluated SwiftPM/shell; resources, web, history/churn and external repositories are outside this inventory.",
        ],
        "counts": {"production_files": len(production), "test_files": len(tests),
                   "production_lines": sum(row["lines"] for row in production),
                   "test_lines": sum(row["lines"] for row in tests),
                   "build_references": len(BUILD_INPUTS), "lexical_edges": len(edges)},
        "production_imports": dict(sorted(imports.items())),
        "lexical_edges": edges,
        "files": files,
    }


def markdown(report):
    out = ["# Platform architecture inventory", "",
           "Generated evidence; do not hand-edit. This is a historical fixed-tree baseline, not a freshness claim about HEAD.", "",
           f"- Observed tree: `{report['observed_tree']}`",
           f"- Generator SHA-256: `{report['generator_sha256']}`",
           f"- Selected inventory SHA-256: `{report['inventory_sha256']}`",
           f"- Schema: `{report['schema_version']}`; evidence: `{report['evidence_kind']}`", "",
           "Reproduce from the repository root with this generator version:", "", "```sh",
           f"python3 tools/platform-architecture-inventory.py --tree {report['observed_tree']} --format markdown",
           f"python3 tools/platform-architecture-inventory.py --tree {report['observed_tree']} --format json",
           "```", "", "## Scope and limits", ""]
    out.extend("- " + limit for limit in report["limits"])
    out.extend(["", "A line is Python `str.splitlines()` length (including a final unterminated line).",
                "The inventory digest covers sorted path/mode/blob/byte-count/content-SHA-256 records.",
                "Missing build references, empty source partitions, nonregular sources, unreadable or corrupt traversed objects fail before output.",
                "JSON carries every source row, import line, lock spelling and lexical edge; Markdown summarizes edges and lists every file.",
                "Source-reading ownership map and adapter direction: [ADR](../adr/0001-platform-boundary-and-evidence.md).", "",
                "## Fixed-tree totals", "", "| Measurement | Count |", "|---|---:|"])
    out.extend(f"| {key} | {value} |" for key, value in report["counts"].items())
    out.extend(["", "## Production import spellings", "", "| Module spelling | Files |", "|---|---:|"])
    out.extend(f"| `{module}` | {len(paths)} |" for module, paths in report["production_imports"].items())
    out.extend(["", "## Largest lexical crossings (top 30)", "",
                "Counts include comments/strings; do not compare these with a compiler graph or the existing architecture ratchet.", "",
                "| From | Basename candidate target | Occurrences |", "|---|---|---:|"])
    for edge in sorted(report["lexical_edges"], key=lambda row: (-row["count"], row["from"], row["to"]))[:30]:
        out.append(f"| `{edge['from']}` | `{edge['to']}` | {edge['count']} |")
    out.extend(["", "## Complete selected file inventory", "",
                "P = production, T = tests, B = build reference. Locks/held doors are raw production spellings, not held-lock correctness.", "",
                "| File | Kind | Lines | Git blob | Imports | Locks / held doors |",
                "|---|---|---:|---|---|---:|"])
    for row in report["files"]:
        kind = {"production": "P", "tests": "T", "build_reference": "B"}[row["partition"]]
        modules = ", ".join(sorted({item["module"] for item in row["imports"]})) or "—"
        out.append(f"| `{row['path']}` | {kind} | {row['lines']} | `{row['blob']}` | {modules} | "
                   f"{len(row['lock_spellings'])} / {len(row['held_door_spellings'])} |")
    return "\n".join(out) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parent.parent)
    parser.add_argument("--tree", required=True, help="full immutable Git commit or tree object ID")
    parser.add_argument("--format", choices=("json", "markdown"), default="json")
    parser.add_argument("--check", type=Path, help="compare existing output bytes; fail on missing/stale output")
    args = parser.parse_args()
    try:
        report = inventory(args.repo, args.tree)
        output = (json.dumps(report, ensure_ascii=True, indent=2, sort_keys=True) + "\n"
                  if args.format == "json" else markdown(report)).encode()
        if args.check is not None:
            try:
                existing = args.check.read_bytes()
            except OSError:
                fail("inventory_check_unreadable", "comparison file is unreadable")
            if existing != output:
                fail("inventory_stale", "comparison file differs for this tree/generator/format")
            print("platform architecture inventory: current for named tree")
        else:
            sys.stdout.buffer.write(output)
    except InventoryError as error:
        print(json.dumps({"error": error.code, "detail": str(error)}), file=sys.stderr)
        return 2
    except (OSError, UnicodeError, ValueError) as error:
        print(json.dumps({"error": "inventory_unreadable", "detail": type(error).__name__}), file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
