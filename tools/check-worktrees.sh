#!/bin/bash
# Classify linked worktrees using Git plus the broker's ownership and landing
# evidence. A path, age, or branch name is never ownership evidence.
#
# Four answers in audit mode:
#   0  every linked worktree was classified and none needs removal
#   1  every linked worktree was classified and a dry run found removals
#   2  the repository or requested evidence could not be read
#   3  at least one linked worktree could not be decided
#
# Audit mode is a dry run unless --apply is explicit. Even --apply removes only
# a broker-owned checkout that the current inventory calls droppable and Git
# calls clean again immediately before removal. It never deletes a branch.
set -uo pipefail

usage() {
  cat >&2 <<'EOF'
usage: tools/check-worktrees.sh [--apply] [--target <branch>]
       tools/check-worktrees.sh --ephemeral [--rev <revision>] -- <command> [args...]

The first form audits the current repository. It is a dry run by default.
The second creates a detached worktree in a temporary directory, runs one
command there, and pairs creation with cleanup. A dirty or moved checkout is
kept and named instead of being forced away.

Tests may set CLAWDLINE_WORKTREE_TESTING=1 and CLAWDLINE_WORKTREE_INVENTORY_FILE
to a saved inventory response.
EOF
}

script_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd) || exit 2

repository() {
  start=${CLAWDLINE_WORKTREE_REPOSITORY:-$script_root}
  common=$(git -C "$start" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || return 1
  if [ "$(basename "$common")" = .git ]; then
    dirname "$common"
  else
    git -C "$start" rev-parse --path-format=absolute --show-toplevel 2>/dev/null
  fi
}

mode=dry-run
target=main
ephemeral=0
revision=HEAD

while [ "$#" -gt 0 ]; do
  case "$1" in
    --apply)
      [ "$ephemeral" -eq 0 ] || { usage; exit 2; }
      mode=apply
      shift
      ;;
    --target)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      target=$2
      shift 2
      ;;
    --ephemeral)
      [ "$mode" = dry-run ] || { usage; exit 2; }
      ephemeral=1
      shift
      ;;
    --rev)
      [ "$ephemeral" -eq 1 ] && [ "$#" -ge 2 ] || { usage; exit 2; }
      revision=$2
      shift 2
      ;;
    --)
      shift
      break
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

if [ "$ephemeral" -eq 1 ]; then
  [ "$#" -gt 0 ] || { usage; exit 2; }
  repo=$(repository) || {
    echo "cannot create ephemeral worktree: this is not a readable Git repository" >&2
    exit 2
  }
  holder=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-worktree.XXXXXX") || exit 2
  checkout="$holder/tree"
  added=0
  command_status=0

  cleanup_ephemeral() {
    if [ "$added" -ne 1 ] || [ ! -d "$checkout" ]; then
      rmdir "$holder" 2>/dev/null || true
      return
    fi
    head=$(git -C "$checkout" rev-parse --verify HEAD 2>/dev/null) || {
      echo "ephemeral worktree kept: HEAD is unreadable: $checkout" >&2
      return
    }
    if [ -n "$(git -C "$checkout" status --porcelain --untracked-files=all 2>/dev/null)" ]; then
      echo "ephemeral worktree kept: it has uncommitted content: $checkout" >&2
      return
    fi
    if ! git -C "$repo" merge-base --is-ancestor "$head" "refs/heads/$target" 2>/dev/null; then
      echo "ephemeral worktree kept: $head is not on refs/heads/$target: $checkout" >&2
      return
    fi
    if git -C "$repo" worktree remove -- "$checkout" >/dev/null 2>&1; then
      rmdir "$holder" 2>/dev/null || true
      echo "ephemeral worktree removed: $checkout"
    else
      echo "ephemeral worktree kept: Git refused removal: $checkout" >&2
    fi
  }
  trap cleanup_ephemeral EXIT HUP INT TERM

  if ! git -C "$repo" show-ref --verify --quiet "refs/heads/$target"; then
    echo "cannot create ephemeral worktree: target refs/heads/$target does not exist" >&2
    exit 2
  fi
  if ! git -C "$repo" worktree add --detach "$checkout" "$revision" >/dev/null; then
    echo "cannot create ephemeral worktree at $checkout" >&2
    exit 2
  fi
  added=1
  echo "ephemeral worktree: $checkout"
  (cd "$checkout" && "$@") || command_status=$?
  exit "$command_status"
fi

[ "$#" -eq 0 ] || { usage; exit 2; }

repo=$(repository) || {
  echo "cannot check: this is not a readable Git repository" >&2
  exit 2
}
if ! git -C "$repo" show-ref --verify --quiet "refs/heads/$target"; then
  echo "cannot check: target refs/heads/$target does not exist" >&2
  exit 2
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/check-worktrees.XXXXXX") || exit 2
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
inventory="$tmp/inventory.json"
evidence_error=""

if [ -n "${CLAWDLINE_WORKTREE_INVENTORY_FILE:-}" ]; then
  if [ "${CLAWDLINE_WORKTREE_TESTING:-}" != 1 ]; then
    echo "cannot check: an inventory fixture is accepted only in testing mode" >&2
    exit 2
  fi
  if ! cp "$CLAWDLINE_WORKTREE_INVENTORY_FILE" "$inventory"; then
    echo "cannot check: inventory fixture is unreadable" >&2
    exit 2
  fi
else
  state_root=${CLAWDLINE_NEXT_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/clawdline-next}
  port=${CLAWDLINE_NEXT_PORT:-7727}
  token_file="$state_root/orchestrator-token"
  if [ ! -r "$token_file" ]; then
    evidence_error="orchestrator token is unreadable"
  elif ! curl --fail-with-body -sS --get \
      -H @<(printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$token_file")") \
      --data-urlencode "project=$repo" \
      "http://127.0.0.1:$port/v1/orchestrator/inventory" >"$inventory"; then
    evidence_error="broker inventory is unavailable"
  fi
  if [ -n "$evidence_error" ]; then
    printf '{"live":[],"unlanded":[],"droppable":[],"unreadable":[],"evidence_error":"%s"}\n' \
      "$evidence_error" >"$inventory"
  fi
fi

python3 - "$repo" "$inventory" "$mode" "$target" <<'PY'
import json
import os
import re
import subprocess
import sys

repo, inventory_path, mode, target = sys.argv[1:]


class CheckFailure(Exception):
    pass


def git(*args, cwd=repo, check=True):
    proc = subprocess.run(
        ["git", "-c", "core.fsmonitor=", "-c", "core.hooksPath=/dev/null", *args],
        cwd=cwd, text=True, stdout=subprocess.PIPE,
        stderr=subprocess.PIPE, env={**os.environ, "LC_ALL": "C"},
    )
    if check and proc.returncode:
        detail = proc.stderr.strip() or f"git {args[0]} exited {proc.returncode}"
        raise CheckFailure(detail)
    return proc


def branch_name(ref):
    prefix = "refs/heads/"
    return ref[len(prefix):] if ref.startswith(prefix) else ref


def norm(path):
    return os.path.normcase(os.path.realpath(path))


filter_attribute = re.compile(rb"(?:^|\s)filter(?:=|\s)")


def unsafe_shape(path):
    """Return why status cannot account for the checkout's contents."""
    unreadable = []

    def failed(exc):
        unreadable.append(str(exc))

    for root, dirs, files in os.walk(path, onerror=failed):
        if root != path and ".git" in dirs + files:
            return "a nested repository can hold content outside the checkout snapshot"
        if ".gitattributes" in files:
            try:
                with open(os.path.join(root, ".gitattributes"), "rb") as attrs:
                    if filter_attribute.search(attrs.read()):
                        return "a Git content filter can hide worktree bytes"
            except OSError as exc:
                return f"attributes are unreadable: {exc}"
    if unreadable:
        return "the checkout cannot be walked completely: " + unreadable[0]

    common = git("rev-parse", "--path-format=absolute", "--git-common-dir", cwd=path, check=False)
    if common.returncode:
        return "the Git common directory is unreadable"
    info_attrs = os.path.join(common.stdout.strip(), "info", "attributes")
    try:
        with open(info_attrs, "rb") as attrs:
            if filter_attribute.search(attrs.read()):
                return "the repository info attributes name a content filter"
    except FileNotFoundError:
        pass
    except OSError as exc:
        return f"repository attributes are unreadable: {exc}"

    configured = git("config", "--path", "--get", "core.attributesFile", cwd=path, check=False)
    if configured.returncode == 0 and configured.stdout.strip():
        try:
            with open(configured.stdout.strip(), "rb") as attrs:
                if filter_attribute.search(attrs.read()):
                    return "core.attributesFile names a content filter"
        except OSError as exc:
            return f"core.attributesFile is unreadable: {exc}"
    return ""


def parse_worktrees():
    proc = subprocess.run(
        ["git", "worktree", "list", "--porcelain", "-z"], cwd=repo,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        env={**os.environ, "LC_ALL": "C"},
    )
    if proc.returncode:
        raise CheckFailure(proc.stderr.decode(errors="replace").strip())
    rows = []
    current = None
    for raw in proc.stdout.split(b"\0"):
        if not raw:
            continue
        line = raw.decode(errors="surrogateescape")
        if line.startswith("worktree "):
            if current:
                rows.append(current)
            current = {"path": line[9:], "branch": "", "head": "", "detached": False}
        elif current is not None and line.startswith("HEAD "):
            current["head"] = line[5:]
        elif current is not None and line.startswith("branch "):
            current["branch"] = branch_name(line[7:])
        elif current is not None and line == "detached":
            current["detached"] = True
    if current:
        rows.append(current)
    return rows


try:
    with open(inventory_path, encoding="utf-8") as source:
        inventory = json.load(source)
    if not isinstance(inventory, dict):
        raise ValueError("the response is not an object")
    for key in ("live", "unlanded", "droppable", "unreadable"):
        if not isinstance(inventory.get(key), list):
            raise ValueError(f"inventory.{key} is not a list")
    reported_repo = inventory.get("repository")
    if not inventory.get("evidence_error") and norm(reported_repo or "") != norm(repo):
        raise ValueError("inventory.repository does not name this repository")
    worktrees = parse_worktrees()
except (OSError, ValueError, json.JSONDecodeError, CheckFailure) as exc:
    print(f"cannot check: {exc}", file=sys.stderr)
    sys.exit(2)

main_path = norm(repo)
live_by_branch = {
    row.get("branch"): row for row in inventory["live"]
    if isinstance(row, dict) and row.get("branch")
}
unlanded_by_branch = {
    row.get("branch"): row for row in inventory["unlanded"]
    if isinstance(row, dict) and row.get("branch")
}
droppable = {}
for row in inventory["droppable"]:
    if not isinstance(row, dict) or not row.get("path") or not row.get("branch"):
        continue
    droppable[(norm(row["path"]), row["branch"])] = row

counts = {name: 0 for name in (
    "landed_clean", "unlanded", "uncommitted", "active_task", "unknown"
)}
candidates = []
rows = []

for wt in worktrees:
    path = wt["path"]
    path_key = norm(path)
    if path_key == main_path:
        continue
    branch = wt["branch"]
    owner = "unproved"
    reason = "no broker record proves who owns this checkout"
    category = "unknown"

    live = live_by_branch.get(branch)
    pending = unlanded_by_branch.get(branch)
    disposable = droppable.get((path_key, branch))

    if live:
        category = "active_task"
        owner = "task:" + str(live.get("task", "unknown"))
        reason = "the broker inventory says the task is live"
    else:
        unsafe = unsafe_shape(path)
        status = git("status", "--porcelain", "--untracked-files=all", cwd=path, check=False) if not unsafe else None
        if unsafe:
            category = "unknown"
            reason = unsafe
        elif status.returncode:
            category = "unknown"
            reason = "Git could not read the checkout status"
        elif status.stdout:
            category = "uncommitted"
            source = pending or disposable
            if source:
                owner = "task:" + str(source.get("task", "unknown"))
            reason = "tracked or untracked content differs from HEAD"
        elif pending:
            category = "unlanded"
            owner = "task:" + str(pending.get("task", "unknown"))
            reason = "the broker inventory says its landing is unsettled"
        elif disposable:
            category = "landed_clean"
            owner = "task:" + str(disposable.get("task", "unknown"))
            reason = "the broker inventory proves it disposable and Git says it is clean"
            candidates.append(wt)
        elif wt["detached"]:
            reason = "a detached checkout has no broker ownership record"

    counts[category] += 1
    rows.append((category, owner, branch or "(detached)", path, reason))

print(f"worktrees: repository={repo} target=refs/heads/{target} mode={mode}")
print(f"primary checkout (outside cleanup scope): {repo}")
for category, owner, branch, path, reason in rows:
    print(f"{category}\t{owner}\t{branch}\t{path}")
    print(f"  reason: {reason}")

removed = 0
failed = 0
if mode == "dry-run":
    for wt in candidates:
        print(f"would remove (branch retained): {wt['path']}")
else:
    for wt in candidates:
        key = (norm(wt["path"]), wt["branch"])
        # Re-check the two local facts at the irreversible boundary. The
        # inventory evidence was fetched by this invocation, and an exact
        # path+branch match is required again here.
        if key not in droppable:
            print(f"kept: broker ownership changed: {wt['path']}", file=sys.stderr)
            failed += 1
            continue
        unsafe = unsafe_shape(wt["path"])
        if unsafe:
            print(f"kept: checkout is not safely measurable: {wt['path']}: {unsafe}", file=sys.stderr)
            failed += 1
            continue
        status = git("status", "--porcelain", "--untracked-files=all", cwd=wt["path"], check=False)
        if status.returncode or status.stdout:
            print(f"kept: checkout is no longer provably clean: {wt['path']}", file=sys.stderr)
            failed += 1
            continue
        proc = git("worktree", "remove", "--", wt["path"], check=False)
        if proc.returncode:
            detail = proc.stderr.strip() or "Git refused removal"
            print(f"kept: {wt['path']}: {detail}", file=sys.stderr)
            failed += 1
            continue
        removed += 1
        print(f"removed (branch retained): {wt['path']}")

print("summary: " + " ".join(f"{key}={value}" for key, value in counts.items()))
if inventory.get("evidence_error"):
    print(f"evidence: {inventory['evidence_error']}", file=sys.stderr)
if mode == "dry-run":
    print(f"action: none; {len(candidates)} proven removal candidate(s)")
else:
    print(f"action: removed={removed} kept_after_recheck={failed}")

# Unknown evidence has priority over a cleanup opportunity: exit 3 must never
# be mistaken for permission. In apply mode a failure at the final guard is
# also an undecidable checkout, not a successful removal.
if counts["unknown"] or inventory.get("evidence_error") or failed:
    sys.exit(3)
if mode == "dry-run" and candidates:
    sys.exit(1)
sys.exit(0)
PY
