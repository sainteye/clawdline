#!/usr/bin/env python3
"""Say so when a delivery has landed and nobody wrote its landing record.

**The whole system has one entrance to `landed`, and a person is standing in it.** The broker
verifies a landing properly — `OrchestratorDraft.verifyTargetLanding` runs
`merge-base --is-ancestor <commit> refs/heads/<target>` inside the task's own repository, and that
check is right — but it runs only when somebody calls
`POST /v1/orchestrator/tasks/:id/landing`. Nobody calls it, nobody knows.

On the night of 2026-09-05/06 that gap produced, in one repository and one evening: 22 landing
records written by hand at the end, 14 of them for work that had been sitting in `main` for days;
21 delivery branches nobody had merged; two roots re-running the same suite to re-discover the same
red; and two roots landing the same batch without either knowing about the other. Five of the
hand-written records were for a *different* repository — the gap is the machine's, not this
checkout's, which is why this reads the registry rather than this tree.

**And it is the second, more expensive half of a family.** The other half is a check that runs and
cannot see its own subject; this half is a check that was never written, and the absence of a
mechanism looks exactly like a mechanism that found nothing.

## What this can decide, and what it refuses to guess

Three answers, kept apart on purpose, because collapsing them is how a number stops meaning
anything:

- **`unrecorded landing`** — the delivery's head is an ancestor of the target branch and the
  landing record is open. Git has already answered the question the broker would have asked. Each
  row is certain; the *count* is a **lower bound**, because a delivery that reached `main` by
  cherry-pick, squash or rebase has no commit in common with it and is invisible here.
- **`outstanding delivery`** — terminal, has commits, head is not an ancestor, record open. As a
  count this is an **upper bound** on work that genuinely has not landed, for the same reason from
  the other side: a squashed landing looks exactly like this.
- **`undecidable`** — a terminal task in a shared checkout that declared write paths and has no
  record. It has no branch, so git cannot be asked at all. Counted separately and never folded into
  either number above.

`git cherry` is deliberately not used. Its patch-id equality reports re-done work as unlanded — 9
of one evening's 45 were that — and the title scan people reach for next cannot survive a reworded
commit. Ancestry is the same predicate the broker itself trusts.

## Who this is for, and why it fails at all

A landing record can only be closed as `landed` with **this machine's orchestrator token**, never
with a task secret (`docs/api.md`). So the obligation was never really the root's: a root that has
gone home cannot have taken the debt with it, because the credential that settles it belongs to the
machine. Whoever runs this suite in this repository is standing in front of the one door there is.

It fails only on landings that happened **after the cutoff below**. Debt older than that is printed
in full on every run — loudly, individually, with the command that settles each one — and does not
fail the run: this exists to stop the *next* silent landing, not to hold a tree hostage to a
backlog somebody is already draining. `--strict` fails on all of it, which is what a root doing a
sweep wants.
"""
import argparse
import json
import os
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

# The moment this guard landed. Everything already unrecorded on this machine at that point is
# inherited debt: named on every run, not fatal. Move it forward only in a commit that says why —
# a cutoff that creeps forward silently is a mute button with a date on it.
CUTOFF = "2026-09-06"

# **A landing is not late the moment it happens.** `docs/landing.md` puts the record *after* the
# integrated-tree run — integrate, test the exact tree, then mark the same record `landed` with the
# resulting commit — and this guard runs inside that test. With no grace it would fail the one run
# the documented order places before the record, which is not a mechanism, it is a trap. Six hours
# is long enough for the whole land/verify/record cycle (the suite itself is measured at 288 s plus
# a compile) and short enough that a landing forgotten overnight is red by morning.
GRACE_HOURS = 6

# `Orchestrator.State.isTerminal`, spelled out. A task still running owes nothing yet.
TERMINAL = {"success", "failure", "timeout", "cancelled", "spawn_failed"}
# `Orchestrator.Landing.State` minus `pending`. A pending record is a declared obligation, not a
# settled one, so a pending landing whose commits are already in the target is still unrecorded —
# it is the closing half that never happened.
CLOSED = {"landed", "abandoned", "nothing_to_land"}

DELIVERY_REF_PREFIX = "refs/heads/clawdline/task/"


def registry_path():
    """`RemoteAuth.directory`, in Python. The override is the app's own, not a new one."""
    override = os.environ.get("CLAWDLINE_REMOTE_DIR", "")
    base = Path(os.path.expanduser(override)) if override else Path.home() / ".config" / "clawdline"
    return base / "orchestrator.json"


def git(args, cwd):
    """git, or None. A failure is never read as an answer — see `contained_commits`."""
    try:
        done = subprocess.run(["git"] + args, cwd=cwd, capture_output=True, text=True)
    except OSError:
        return None
    return done.stdout if done.returncode == 0 else None


def toplevel(path, cache):
    """The repository one directory belongs to, or None when it is not in one any more."""
    if path in cache:
        return cache[path]
    answer = None
    if path and os.path.isdir(path):
        out = git(["rev-parse", "--show-toplevel"], path)
        if out:
            answer = os.path.realpath(out.strip())
    cache[path] = answer
    return answer


def target_branch(repository):
    """`main` where there is one, otherwise whatever HEAD is on. Named in the output either way."""
    if git(["rev-parse", "--verify", "--quiet", "refs/heads/main"], repository) is not None:
        return "main"
    out = git(["symbolic-ref", "--short", "HEAD"], repository)
    return out.strip() if out and out.strip() else None


def contained_commits(repository, target):
    """Every commit the target branch contains, as full object ids.

    One subprocess for a whole repository — 1,196 commits in 9 ms here — instead of a
    `merge-base --is-ancestor` per task, which was 215 of them in this checkout alone. `None` means
    git did not answer, and the caller reports the repository as unreadable rather than treating an
    empty set as "nothing has landed", which would turn every delivery into an outstanding one.
    """
    out = git(["rev-list", "refs/heads/" + target], repository)
    if out is None:
        return None
    return {line.strip() for line in out.splitlines() if line.strip()}


def live_heads(repository):
    """Branch name -> the commit it points at now.

    The registry's `worktree.head` is what the app last recorded; this is what the branch says
    today. `Orchestrator.inflightRow` prefers the live one for the same reason — the difference
    between reporting a delivery and reporting a memory of one — and falls back to the stored head,
    which is the only thing left once a merged branch has been deleted. That fallback is not a
    detail: it is the case this whole file exists for.
    """
    out = git(["for-each-ref", "--format=%(refname:short) %(objectname)", DELIVERY_REF_PREFIX],
              repository)
    heads = {}
    for line in (out or "").splitlines():
        parts = line.split(" ")
        if len(parts) == 2:
            heads[parts[0]] = parts[1]
    return heads


def entered_target(repository, head, target):
    """When this delivery became part of the target branch, as a unix timestamp.

    The oldest commit on the ancestry path from `head` to the target is the merge (or the commit
    that followed a fast-forward), so its committer date is when the work entered. The head's own
    date is the wrong answer and quietly so: a branch cut in July and merged today would be filed
    under July and excused by any cutoff.
    """
    out = git(["rev-list", "--ancestry-path", "--reverse", "--format=%ct",
               head + ".." + "refs/heads/" + target], repository)
    for line in (out or "").splitlines():
        line = line.strip()
        if line and not line.startswith("commit "):
            try:
                return int(line)
            except ValueError:
                break
    # Nothing on the path means the delivery *is* the tip: fast-forwarded, with nothing landed
    # after it. Its own commit is then the moment it entered, and this fallback is why no finding
    # here carries an unknown date — an undated row is one a grace period cannot age, which is a
    # silent way to be excused.
    out = git(["show", "-s", "--format=%ct", head + "^{commit}"], repository)
    try:
        return int((out or "").strip())
    except ValueError:
        return None


def commits_beyond_base(repository, base, head):
    """How many commits this delivery added. Only asked about rows that are already findings."""
    out = git(["rev-list", "--count", base + ".." + head], repository)
    try:
        return int((out or "").strip())
    except ValueError:
        return None


def delivery_of(task):
    worktree = task.get("worktree") or {}
    if not isinstance(worktree, dict):
        return None, None, None
    return worktree.get("branch"), worktree.get("base"), worktree.get("head")


def home_of(task, by_worktree_path):
    """Which repository this task's work belongs to.

    An isolated task says so itself. A task dispatched *into somebody else's* isolated checkout —
    every review of a delivery is one — carries only that checkout's path as its `project_dir`,
    and that path is gone the moment the delivery's worktree is reclaimed. So the second lookup
    asks the task that owned the checkout, which is still in this registry with its repository on
    it. Without it, 23 terminal tasks on this machine resolved to nowhere.
    """
    worktree = task.get("worktree") or {}
    home = worktree.get("repository") if isinstance(worktree, dict) else None
    if home:
        return home
    project = task.get("project_dir")
    return by_worktree_path.get(project, project)


def owes_anything(task):
    """Whether an unresolvable task could have owed a record at all.

    A read-only review dispatched into a checkout that no longer exists owes nothing and never
    did; counting it as "cannot be judged" inflates the one number here that is supposed to mean
    *this guard cannot see*.
    """
    branch, base, head = delivery_of(task)
    return bool((branch and (base or head)) or (task.get("claims") or []))


def scan(tasks, only_repository=None):
    """Group every terminal task by the repository it belongs to and ask git about each one."""
    cache = {}
    by_repository = {}
    by_worktree_path = {}
    for task in tasks:
        worktree = task.get("worktree") if isinstance(task, dict) else None
        if isinstance(worktree, dict) and worktree.get("path") and worktree.get("repository"):
            by_worktree_path[worktree["path"]] = worktree["repository"]
    unreachable = 0
    for task in tasks:
        if not isinstance(task, dict) or task.get("state") not in TERMINAL:
            continue
        repository = toplevel(home_of(task, by_worktree_path), cache)
        if repository is None:
            unreachable += owes_anything(task)
            continue
        if only_repository and repository != only_repository:
            continue
        by_repository.setdefault(repository, []).append(task)

    findings = []
    repositories = []
    for repository in sorted(by_repository):
        rows = by_repository[repository]
        target = target_branch(repository)
        contained = contained_commits(repository, target) if target else None
        if contained is None:
            repositories.append({"repository": repository, "target": target, "tasks": len(rows),
                                 "readable": False})
            continue
        heads = live_heads(repository)
        counts = {"unrecorded": 0, "outstanding": 0, "undecidable": 0,
                  "nothing_to_land": 0, "shared_tree": 0, "refs_gone": 0, "closed": 0}
        for task in rows:
            branch, base, stored = delivery_of(task)
            landing = task.get("landing") if isinstance(task.get("landing"), dict) else {}
            settled = landing.get("state") in CLOSED
            head = heads.get(branch) or stored
            if not head or not base:
                counts["shared_tree"] += 1
                if settled or not (task.get("claims") or []):
                    counts["closed" if settled else "nothing_to_land"] += 1
                    continue
                counts["undecidable"] += 1
                findings.append({"kind": "undecidable", "repository": repository, "target": target,
                                 "task": task.get("id", ""), "title": task.get("title", ""),
                                 "state": task.get("state", ""), "branch": None, "head": None,
                                 "commits": None, "entered": None, "after_cutoff": False,
                                 "landing_state": landing.get("state"),
                                 "root_label": task.get("root_label"),
                                 "root_session": task.get("root_session")})
                continue
            if head == base:
                counts["nothing_to_land"] += 1
                continue
            if head not in contained and git(["cat-file", "-e", head + "^{commit}"],
                                             repository) is None:
                counts["refs_gone"] += 1
                continue
            if settled:
                counts["closed"] += 1
                continue
            landed = head in contained
            entered = entered_target(repository, head, target) if landed else None
            counts["unrecorded" if landed else "outstanding"] += 1
            findings.append({
                "kind": "unrecorded" if landed else "outstanding",
                "repository": repository, "target": target,
                "task": task.get("id", ""), "title": task.get("title", ""),
                "state": task.get("state", ""), "branch": branch, "head": head,
                "commits": commits_beyond_base(repository, base, head),
                "entered": entered, "after_cutoff": False,
                "landing_state": landing.get("state"),
                "root_label": task.get("root_label"), "root_session": task.get("root_session")})
        repositories.append({"repository": repository, "target": target, "tasks": len(rows),
                             "readable": True, "counts": counts})
    return findings, repositories, unreachable


def cutoff_seconds(text):
    try:
        return datetime.strptime(text, "%Y-%m-%d").replace(tzinfo=timezone.utc).timestamp()
    except ValueError:
        return None


def when(seconds):
    if not seconds:
        return "date unknown"
    return datetime.fromtimestamp(seconds, timezone.utc).strftime("%Y-%m-%d %H:%M UTC")


def describe(finding):
    who = finding.get("root_label") or "root not named"
    session = finding.get("root_session")
    who = who + (" / " + session[:8] if session else "")
    head = (finding.get("head") or "")[:8] or "no delivery"
    commits = finding.get("commits")
    size = "+%d" % commits if isinstance(commits, int) else "+?"
    state = finding.get("landing_state") or "no record at all"
    return ("    %s %s %s  %s  [%s]  %s\n        %s"
            % (finding["task"][:8], head, size, when(finding.get("entered")), state, who,
               (finding.get("title") or "")[:96]))


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--since", default=CUTOFF, metavar="YYYY-MM-DD",
                        help="landings after this date fail the run (default: %s)" % CUTOFF)
    parser.add_argument("--grace-hours", type=float, default=GRACE_HOURS, metavar="H",
                        help="how long a landing may go unrecorded before it counts "
                             "(default: %g)" % GRACE_HOURS)
    parser.add_argument("--strict", action="store_true",
                        help="fail on every unrecorded landing, however old and however recent")
    parser.add_argument("--repository", metavar="PATH",
                        help="only this repository, instead of every one the registry names")
    parser.add_argument("--json", action="store_true", help="the same answer, for a machine")
    args = parser.parse_args()

    cutoff = cutoff_seconds(args.since)
    if cutoff is None:
        print("landing records: --since wants YYYY-MM-DD, not %r" % args.since, file=sys.stderr)
        return 2

    path = registry_path()
    if not path.exists():
        # A clone on a machine that has never run Clawdline has no subject here, and saying so is
        # not the same as saying there is nothing wrong. CI is always this branch.
        print("landing records: no Clawdline task registry at %s — this machine has no landings "
              "to check, so this guard has no subject here." % path)
        return 0
    try:
        registry = json.loads(path.read_text())
    except (OSError, ValueError) as problem:
        print("landing records: %s could not be read (%s) — the finder, not the tree."
              % (path, problem), file=sys.stderr)
        return 2
    tasks = registry.get("tasks")
    if not isinstance(tasks, list):
        print("landing records: %s holds no `tasks` list — the finder, not the tree." % path,
              file=sys.stderr)
        return 2
    if not tasks:
        # The empty-scan refusal every guard here owes. A registry file the app wrote always has
        # rows in it; a scan of zero is this script having lost its subject, and passing because
        # there was nothing to look at is the exact failure this whole file is about.
        print("landing records: %s holds an empty `tasks` list — the finder, not the tree." % path,
              file=sys.stderr)
        return 2

    only = None
    if args.repository:
        only = toplevel(os.path.abspath(os.path.expanduser(args.repository)), {})
        if only is None:
            print("landing records: %s is not inside a Git repository." % args.repository,
                  file=sys.stderr)
            return 2

    findings, repositories, unreachable = scan(tasks, only_repository=only)
    ripe = datetime.now(timezone.utc).timestamp() - args.grace_hours * 3600
    for finding in findings:
        entered = finding.get("entered")
        finding["after_cutoff"] = finding["kind"] == "unrecorded" and (
            entered is None or entered >= cutoff)
        finding["within_grace"] = bool(entered and entered > ripe)

    if args.json:
        print(json.dumps({"cutoff": args.since, "strict": bool(args.strict),
                          "registry": str(path), "repositories": repositories,
                          "unreachable_task_directories": unreachable,
                          "findings": findings}, indent=2, sort_keys=True))

    unrecorded = [f for f in findings if f["kind"] == "unrecorded"]
    outstanding = [f for f in findings if f["kind"] == "outstanding"]
    undecidable = [f for f in findings if f["kind"] == "undecidable"]
    new = [f for f in unrecorded if f["after_cutoff"] and not f["within_grace"]]
    fresh = [f for f in unrecorded if f["after_cutoff"] and f["within_grace"]]
    inherited = [f for f in unrecorded if not f["after_cutoff"]]

    unreadable = [r for r in repositories if not r.get("readable")]
    for row in unreadable:
        print("landing records: git would not answer for %s (target %s), so %d terminal task(s) "
              "there were not checked." % (row["repository"], row["target"], row["tasks"]),
              file=sys.stderr)

    if not args.json:
        print("landing records: %d repositories, %d terminal tasks, registry %s"
              % (len(repositories), sum(r["tasks"] for r in repositories), path))
        for row in repositories:
            if not row.get("readable"):
                continue
            counts = row["counts"]
            print("  %s (target %s): %d unrecorded, %d outstanding, %d undecidable, %d closed"
                  % (row["repository"], row["target"], counts["unrecorded"],
                     counts["outstanding"], counts["undecidable"], counts["closed"]))

        if new:
            print("\n  LANDED, AND NO RECORD WAS WRITTEN — after the %s cutoff:" % args.since)
            for finding in new:
                print(describe(finding))
        if fresh:
            print("\n  Landed within the last %g hours with the record still open. The documented "
                  "order writes the record after this suite, so these are not late yet:"
                  % args.grace_hours)
            for finding in fresh:
                print(describe(finding))
        if inherited:
            print("\n  Unrecorded landings inherited from before the %s cutoff — every one of "
                  "these is a real obligation, and none of them fails this run:" % args.since)
            for finding in inherited:
                print(describe(finding))
        if outstanding:
            print("\n  Delivered and not in the target branch (an upper bound: a squashed or "
                  "cherry-picked landing looks exactly like this):")
            for finding in outstanding:
                print(describe(finding))
        if undecidable:
            print("\n  Shared-checkout deliveries with no record and no branch — git cannot be "
                  "asked about these, so they are neither of the two numbers above:")
            for finding in undecidable:
                print(describe(finding))
        if unreachable:
            print("\n  %d terminal task(s) that could have owed a record name a directory which "
                  "is not in a repository any more; nothing can be said about them." % unreachable)
        if findings:
            print("\n  A record is closed with this machine's orchestrator token, never with a "
                  "task secret (docs/api.md), so the debt does not leave with the root that made "
                  "it — it belongs to the machine, and to whoever is standing here:")
            print("    curl --fail-with-body -sS -X POST \\\n"
                  "      http://127.0.0.1:$PORT/v1/orchestrator/tasks/<id>/landing \\\n"
                  "      -H \"X-Clawdline-Orchestrator-Token: $(cat "
                  "~/.config/clawdline/orchestrator-token)\" \\\n"
                  "      -H 'Content-Type: application/json' \\\n"
                  "      -d '{\"state\":\"landed\",\"commit\":\"<head>\",\"target\":\"<branch>\","
                  "\"note\":\"<what was verified>\"}'")

    fatal = unrecorded if args.strict else new
    if fatal:
        print("\nlanding records: %d landing(s) happened and no record was written. %s"
              % (len(fatal),
                 "Every unrecorded landing counts under --strict."
                 if args.strict else "These are after the %s cutoff." % args.since),
              file=sys.stderr)
        return 1

    if args.json:
        # stdout is the document and nothing else; the exit code and the failing sentence on
        # stderr are still there for a caller that wants both.
        return 0
    print("\nlanding records: nothing has landed unrecorded since %s, beyond the %g-hour grace. "
          "%d unrecorded landing(s) inherited from before it, %d still inside the grace, "
          "%d outstanding, %d undecidable — and the unrecorded count is a lower bound, because a "
          "squashed or cherry-picked landing shares no commit with the branch it came from."
          % (args.since, args.grace_hours, len(inherited), len(fresh), len(outstanding),
             len(undecidable)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
