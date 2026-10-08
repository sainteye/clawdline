#!/usr/bin/env python3
"""Count tracked implementation lines by a file's primary product responsibility.

Run from this repository with --cloud pointing at an optional Cloud service checkout to
include the optional service. Each eligible file belongs to exactly one bucket;
mixed-purpose files and wiring live in Shared rather than being split by guesswork.
"""

from __future__ import annotations

import argparse
from collections import defaultdict
import json
from pathlib import Path
import subprocess


ROOT = Path(__file__).resolve().parents[1]
CODE_SUFFIXES = {".go", ".ts", ".tsx", ".js", ".jsx", ".css", ".html", ".swift", ".mjs", ".py", ".sh", ".astro"}
EXCLUDED_PARTS = {"docs", "tools", "scripts", "experiments", "testdata", "fixtures", "vendor", "node_modules", "dist", "build"}
PUBLIC_ROOTS = {"internal", "cmd", "web", "shell"}
CLOUD_ROOTS = {"api", "relay", "marketing"}
CATEGORIES = (
    "roles", "coordination", "board", "workflows", "session_read", "session_action", "settings",
    "projects", "automation", "cloud", "usage", "shared", "website",
)


def tracked_code(root: Path, allowed_roots: set[str]):
    names = subprocess.check_output(["git", "ls-files", "-z"], cwd=root).split(b"\0")
    for raw in names:
        if not raw:
            continue
        relative = Path(raw.decode())
        name = relative.name.lower()
        if relative.suffix.lower() not in CODE_SUFFIXES or relative.parts[0] not in allowed_roots:
            continue
        if relative.as_posix() == "marketing/marketing-claude/public/architecture/index.html":
            continue  # The diagram is documentation, not product implementation.
        if any(part in EXCLUDED_PARTS for part in relative.parts):
            continue
        if any(marker in name for marker in (".test.", ".spec.", ".e2e.", "_test.go", "generated", "zz_")):
            continue
        if name.startswith("test_"):
            continue
        with (root / relative).open("rb") as source:
            lines = sum(1 for _ in source)
        yield relative.as_posix(), lines


def classify(path: str, cloud_repo: bool) -> str:
    p = path.lower()
    leaf = Path(p).stem
    if cloud_repo:
        return "website" if p.startswith("marketing/") else "cloud"

    if p.startswith("shell/") or p.startswith("internal/domain/capacity/") or p.startswith("internal/adapters/planner/"):
        return "shared"
    if p.startswith("internal/domain/work/board"):
        return "board"
    if p.startswith("internal/adapters/subagents/") or p.startswith("internal/adapters/whisper/"):
        return "session_read" if "/subagents/" in p else "session_action"
    if p.startswith("web/console/src/overlays/"):
        return "roles" if "role-detail" in p else "shared"

    # Folder owners are the strongest signal. Files that span several features
    # remain in Shared unless their primary owner is clear from the path.
    folder_rules = (
        ("internal/app/orchestrator/", "coordination"),
        ("internal/domain/persona/", "roles"),
        ("internal/domain/squad/", "roles"),
        ("internal/domain/squadpack/", "roles"),
        ("internal/adapters/squadfiles/", "roles"),
        ("internal/adapters/board/", "board"),
        ("internal/domain/board/", "board"),
        ("internal/domain/work/", "workflows"),
        ("internal/adapters/transcript/", "session_read"),
        ("internal/adapters/terminal/", "session_action"),
        ("internal/app/terminals/", "session_action"),
        ("internal/adapters/projects/", "projects"),
        ("internal/adapters/projectfiles/", "projects"),
        ("internal/adapters/projectlinks/", "projects"),
        ("internal/adapters/artifacts/", "projects"),
        ("internal/adapters/documents/", "projects"),
        ("internal/adapters/projectsync/", "projects"),
        ("internal/domain/projectsync/", "projects"),
        ("internal/adapters/push/", "automation"),
        ("internal/domain/schedule/", "automation"),
        ("internal/domain/schedulewebhook/", "automation"),
        ("internal/transport/cloud/", "cloud"),
        ("internal/app/cloudops/", "cloud"),
        ("internal/domain/cloud/", "cloud"),
        ("internal/adapters/cloud/", "cloud"),
        ("internal/adapters/cloudkeys/", "cloud"),
        ("web/console/src/cloud/", "cloud"),
        ("web/console/src/pages/squad/", "roles"),
        ("web/console/src/pages/work/", "workflows"),
        ("web/console/src/pages/settings/", "settings"),
        ("web/console/src/pages/projects/", "projects"),
        ("web/console/src/pages/terminal/", "session_action"),
        ("web/console/src/machine/", "settings"),
        ("web/console/src/push/", "automation"),
    )
    for prefix, category in folder_rules:
        if p.startswith(prefix):
            return category

    if p.startswith("web/console/src/session/"):
        if any(token in leaf for token in ("role", "persona", "intervention", "coordination", "clawdfather")):
            return "roles"
        if any(token in leaf for token in ("todo", "worktree", "task-read")):
            return "board"
        if any(token in leaf for token in ("terminal", "composer", "command", "send", "press", "markup", "sender", "start", "restore")):
            return "session_action"
        return "session_read"

    if p.startswith("web/console/src/legacy/js/net/"):
        if "cloud" in leaf:
            return "cloud"
        if any(token in leaf for token in ("schedule", "webhook")):
            return "automation"
        if any(token in leaf for token in ("billing", "plan", "usage")):
            return "usage"
        if any(token in leaf for token in ("document", "project")):
            return "projects"
        return "shared"

    if p.startswith("web/console/src/legacy/") or p.startswith("web/console/src/pages/"):
        if any(token in p for token in ("squad", "persona", "role")):
            return "roles"
        if any(token in p for token in ("coordinator", "agent", "handoff", "dispatch")):
            return "coordination"
        if any(token in p for token in ("board", "backlog", "todo")):
            return "board"
        if any(token in p for token in ("work", "proposal", "verify")):
            return "workflows"
        if any(token in p for token in ("schedule", "webhook", "push", "notification")):
            return "automation"
        if any(token in p for token in ("transcript", "timeline", "snippet", "archive", "screen", "waiting")):
            return "session_read"
        if any(token in p for token in ("terminal", "composer", "input", "shots", "image-markup", "voice", "start")):
            return "session_action"
        if any(token in p for token in ("setting", "device", "auth", "pairing", "capacity")):
            return "settings"
        if any(token in p for token in ("project", "document", "artifact", "git", "worktree")):
            return "projects"
        if any(token in p for token in ("cloud", "relay", "crypto")):
            return "cloud"
        if any(token in p for token in ("usage", "billing", "plan", "release", "update")):
            return "usage"
        return "shared"

    # Go app, HTTP, store and CLI files usually name their primary feature.
    # Resolve narrow names before broad ones, for example board_settings.
    if any(token in p for token in ("board", "backlog", "todo")):
        return "board"
    if any(token in p for token in ("work_v2", "work_unit", "work_gate", "proposal", "verify", "epic", "handover")):
        return "workflows"
    if any(token in p for token in ("squad", "persona", "role")):
        return "roles"
    if any(token in p for token in ("orchestrat", "coordinat", "dispatch", "handoff", "agent", "taskdir", "broker", "task.go")):
        return "coordination"
    if any(token in p for token in ("restore", "start", "voice", "whisper")):
        return "session_action"
    if any(token in p for token in ("transcript", "session", "timeline", "snippet", "archive", "screen")):
        return "session_read"
    if any(token in p for token in ("terminal", "composer", "shell", "input", "action", "send", "tmux")):
        return "session_action"
    if any(token in p for token in ("setting", "device", "auth", "pairing", "capacity", "config", "key", "localiz", "i18n")):
        return "settings"
    if any(token in p for token in ("project", "document", "artifact", "git", "memory", "image", "file")):
        return "projects"
    if any(token in p for token in ("schedule", "webhook", "push", "notice", "waiting", "notification")):
        return "automation"
    if any(token in p for token in ("cloud", "relay", "tunnel", "crypto")):
        return "cloud"
    if any(token in p for token in ("usage", "billing", "plan", "release", "update")):
        return "usage"
    return "shared"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cloud", type=Path, help="optional Cloud service checkout")
    parser.add_argument("--largest", type=int, default=0, help="include N largest files per category")
    args = parser.parse_args()
    roots = [(ROOT, PUBLIC_ROOTS, False)]
    if args.cloud:
        roots.append((args.cloud.resolve(), CLOUD_ROOTS, True))
    groups = defaultdict(list)
    revisions = {}
    for root, allowed, cloud_repo in roots:
        revisions["cloud" if cloud_repo else "public"] = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
        for path, lines in tracked_code(root, allowed):
            groups[classify(path, cloud_repo)].append((("cloud/" if cloud_repo else "") + path, lines))
    result = {
        "revisions": revisions,
        "method": "Git-tracked implementation files; physical lines including blanks and comments; tests, generated code, docs, tools and dependencies excluded; one primary category per file.",
        "total_files": sum(len(files) for files in groups.values()),
        "total_lines": sum(lines for files in groups.values() for _, lines in files),
        "categories": [
            {"id": category, "files": len(groups[category]), "lines": sum(n for _, n in groups[category]),
             **({"largest": sorted(groups[category], key=lambda item: -item[1])[:args.largest]} if args.largest else {})}
            for category in CATEGORIES
        ],
    }
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
