#!/usr/bin/env python3
"""Say so when a delivery has landed and nobody wrote its landing record.

**`landed` had one entrance and a person was standing in it.** The broker verifies a landing
properly — `OrchestratorDraft.verifyTargetLanding` runs
`merge-base --is-ancestor <commit> refs/heads/<target>` inside the task's own repository, and that
check is right — but it ran only when somebody called
`POST /v1/orchestrator/tasks/:id/landing`. Nobody calls it, nobody knows.

**Since 2026-09-06 there is a second entrance, and no person in it.**
`Orchestrator.landingSweepPass` closes, on a timer, what the broker can prove by itself, and the
three answers below are what it acts on: it settles an `unrecorded landing` through the same
verified path this file describes, it settles the narrow half of `undecidable` — a shared-checkout
task every one of whose declared claims resolves to something git can see, is unmodified, and is
identical to the target on two readings five minutes apart — and it never touches an
`outstanding delivery`, which stays a person's. So a row this file prints may be gone by the next
run because a timer settled it, and that is the mechanism working rather than somebody having
been quick.

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

The landing **route** can be called only with **this machine's orchestrator token**, never with a
task secret (`docs/api.md`), and that is still true. So the obligation was never really the root's:
a root that has gone home cannot have taken the debt with it, because the credential that settles
it belongs to the machine. What has changed is that the route is no longer the only writer of a
`landed` record — the sweep above writes one without any credential at all, because it is the
machine — so whoever runs this suite is standing in front of the door a person uses, not in front
of the only door.

It fails only on landings that happened **after the cutoff below**. Debt older than that is printed
in full on every run — loudly, individually, with the command that settles each one — and does not
fail the run: this exists to stop the *next* silent landing, not to hold a tree hostage to a
backlog somebody is already draining. `--strict` fails on all of it, which is what a root doing a
sweep wants.

## Who is told, and whose build stops

**They are two questions, and until 2026-09-06 they were one.** Everything above reads the
machine's registry and prints all of it, because the debt really is the machine's. Failing did the
same, and it cost two runs that never reached a compiler. The first was an isolated child of
another line, which died here over `b932fa3a` — **another root's** landing, in the base repository,
nothing to do with the child's checkout — and it could not have cleared it even if it had
understood the message: settling needs this machine's orchestrator token, and `CHILD.md` forbids a
child from calling the landing route at all. The run was refused, told to run a `curl` it is not
permitted to run, and had nothing left to do but stop.

So the fatal set asks two more questions, and the output says both of them out loud:

- **Is this debt in the repository this run is standing in?** Eight repositories are in this
  registry and a build in one of them does not stop over another's.
- **May this run close the record?** A run inside a **linked worktree** is a child's, and a child
  may not call the landing route. Derived rather than guessed: `git rev-parse --git-common-dir`
  names `<repository>/.git` from every checkout of a repository, so the main worktree is that
  directory's parent — the shell half of `OrchestratorDraft.mainWorktree(containing:)` — and a
  checkout which is not that directory is a linked one.

**A run that may not fail says so at length**, printing the rows, who can settle them, and that it
is not the one. The alternative is the exact defect `tools/git-hooks/pre-commit` carries a
paragraph about: a check that goes quiet inside a worktree and is read as a check that passed. So
the green a narrowed run prints is not the green an empty machine prints: the two do not share a
closing sentence, and the narrowed one says *not this run's to fail over* where the other says
*nothing has landed unrecorded*. `--strict` ignores both narrowings, because a root doing a sweep
wants every row and holds the credential.

## What the sweep changes, which is the wording and not the set

`Orchestrator.landingSweepPass` settles some of this by itself, and which is worth saying: its
candidates are terminal tasks whose landing record **already exists and is `pending` with a
target** (`landingSweepCandidates`). A row with no record at all — most of what this finds —
declares no target, so no timer is coming for it, and the rows a timer can take are marked in the
output as such.

They stay in the fatal set, and the reason is the grace period rather than a preference. The sweep
runs every 300 s and this guard excuses anything younger than six hours, so a row that reaches the
fatal set has already been offered to about seventy passes and is there because the sweep declined
it or could not see it. The other reason is that redness would otherwise depend on whether an app
happened to be running, which is a property of neither this tree nor this registry.
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


def runner_context(cwd):
    """Where this run is standing, and whether it is allowed to close a landing record from there.

    `git rev-parse --show-toplevel` answers with whichever checkout the caller is in, and for a
    linked worktree that is a directory this app made and will delete; every task in the registry
    is filed under the repository it was cut from, so a scope taken from the toplevel would match
    none of them inside a worktree. The repository therefore comes from `--git-common-dir`, which
    names `<repository>/.git` from every checkout of it — the shell half of
    `OrchestratorDraft.mainWorktree(containing:)` — and the checkout is compared against it.

    `.git` is the ordinary shape and the only one with an answer here. A bare repository or a
    `--separate-git-dir` layout has no working tree that name belongs to, and deriving one from the
    path anyway would be the same guess in the other direction: `repository` is then `None`, which
    the caller reports rather than rounds off.
    """
    checkout = None
    out = git(["rev-parse", "--show-toplevel"], cwd)
    if out and out.strip():
        checkout = os.path.realpath(out.strip())
    common_dir = None
    repository = None
    out = git(["rev-parse", "--git-common-dir"], cwd)
    if out and out.strip():
        raw = out.strip()
        common_dir = os.path.realpath(raw if os.path.isabs(raw) else os.path.join(cwd, raw))
        if os.path.basename(common_dir) == ".git":
            main = os.path.dirname(common_dir)
            if os.path.isdir(main):
                repository = os.path.realpath(main)
    linked = bool(checkout and repository and checkout != repository)
    return {"checkout": checkout, "repository": repository, "git_common_dir": common_dir,
            "linked_worktree": linked,
            # The credential is the machine's, so what decides this is not which repository the
            # debt is in but who is standing here: a linked worktree is a child's checkout, and
            # `CHILD.md` forbids a child from calling the landing route at all.
            "may_settle": not linked}


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
                                 "landing_target": landing.get("target"),
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
                "landing_target": landing.get("target"),
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


def sweep_candidate(finding):
    """Whether `Orchestrator.landingSweepPass` could take this row off a person by itself.

    `landingSweepCandidates` takes terminal tasks whose landing record already exists, is
    `pending`, and names a target. A row with no record at all declares no target, so there is no
    timer coming for it — which is most of what this file finds, and the reason the sweep changes
    the wording here rather than the fatal set.
    """
    return finding.get("landing_state") == "pending" and bool(finding.get("landing_target"))


def describe(finding):
    who = finding.get("root_label") or "root not named"
    session = finding.get("root_session")
    who = who + (" / " + session[:8] if session else "")
    head = (finding.get("head") or "")[:8] or "no delivery"
    commits = finding.get("commits")
    size = "+%d" % commits if isinstance(commits, int) else "+?"
    state = finding.get("landing_state") or "no record at all"
    line = ("    %s %s %s  %s  [%s]  %s\n        %s"
            % (finding["task"][:8], head, size, when(finding.get("entered")), state, who,
               (finding.get("title") or "")[:96]))
    if sweep_candidate(finding):
        line += ("\n        a timer can take this one: the record is pending against %s, so "
                 "Orchestrator.landingSweepPass is allowed to settle it without anybody asking."
                 % finding["landing_target"])
    return line


def withheld_reason(finding, runner, scope):
    """Why this row, which is real debt after the cutoff, is not stopping this run.

    Both conditions are named per row rather than once at the top, because a reader arrives at one
    row — the one with their task id on it — and the sentence they need is the one under it.
    """
    reasons = []
    if not finding["in_runner_repository"]:
        reasons.append("it is filed under %s and this run is in %s"
                       % (finding["repository"], scope or "no repository it could name"))
    if not runner["may_settle"]:
        reasons.append("this checkout is a linked worktree of %s, and a child may not call the "
                       "landing route" % runner["repository"])
    return ("not failing here because %s. The suite run in %s fails on it, and --strict fails on "
            "it from anywhere." % (", and ".join(reasons), finding["repository"]))


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
    if not repositories:
        # **A filter that matched nothing looks exactly like a clean machine.** On 2026-09-06 a
        # cleanup on this Mac removed 25 worktrees, one of them a live task's, because the script's
        # keep-list was never read — an empty list and a list of everything safe are the same
        # number of lines of output. Both of these refusals are that shape: a `--repository` no
        # task belongs to, and a registry whose states this file no longer recognises, which is
        # what happens the day `Orchestrator.State` grows a spelling. Green because there was
        # nothing to look at is the failure this whole file is about.
        if only is not None:
            print("landing records: %d task(s) in the registry and not one of them belongs to %s "
                  "— the filter, not the tree." % (len(tasks), only), file=sys.stderr)
        else:
            print("landing records: %d task(s) in the registry and none of them is in a state "
                  "this guard calls terminal (%s) — the finder, not the tree."
                  % (len(tasks), ", ".join(sorted(TERMINAL))), file=sys.stderr)
        return 2
    ripe = datetime.now(timezone.utc).timestamp() - args.grace_hours * 3600
    for finding in findings:
        entered = finding.get("entered")
        finding["after_cutoff"] = finding["kind"] == "unrecorded" and (
            entered is None or entered >= cutoff)
        finding["within_grace"] = bool(entered and entered > ripe)

    # Everything above stays machine-wide; this decides only which of those rows may stop this run.
    # `--repository` is an operator naming the repository they are asking about, so it is the scope
    # when it is given; otherwise the scope is the repository this checkout belongs to, which
    # inside a linked worktree is not the checkout.
    runner = runner_context(os.getcwd())
    scope = only if only is not None else runner["repository"]
    for finding in findings:
        finding["in_runner_repository"] = bool(scope) and finding["repository"] == scope
        finding["sweep_candidate"] = sweep_candidate(finding)
        finding["fails_this_run"] = False

    unrecorded = [f for f in findings if f["kind"] == "unrecorded"]
    outstanding = [f for f in findings if f["kind"] == "outstanding"]
    undecidable = [f for f in findings if f["kind"] == "undecidable"]
    new = [f for f in unrecorded if f["after_cutoff"] and not f["within_grace"]]
    fresh = [f for f in unrecorded if f["after_cutoff"] and f["within_grace"]]
    inherited = [f for f in unrecorded if not f["after_cutoff"]]

    # The two questions, in the order they are printed: is this debt in the repository this run is
    # standing in, and may this run close the record. `--strict` asks neither.
    fatal, withheld = [], []
    for finding in (unrecorded if args.strict else new):
        stops = args.strict or (finding["in_runner_repository"] and runner["may_settle"])
        (fatal if stops else withheld).append(finding)
        finding["fails_this_run"] = stops

    if args.json:
        print(json.dumps({"cutoff": args.since, "strict": bool(args.strict),
                          "registry": str(path), "repositories": repositories,
                          "runner": runner, "fatal_scope": scope,
                          "unreachable_task_directories": unreachable,
                          "findings": findings}, indent=2, sort_keys=True))

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

        print("\n  %s Failing is narrower than reporting, and both of its conditions are said "
              "out loud:"
              % ("Everything above is the repository --repository named." if only is not None
                 else "Everything above is every repository in this machine's registry."))
        print("    in this repository — %s"
              % (scope or "this run cannot name the repository it is standing in"))
        if runner["may_settle"]:
            print("    allowed to settle — yes: %s is that repository's main worktree, so the "
                  "landing route is open to whoever is standing here."
                  % (runner["checkout"] or "this directory"))
        else:
            print("    allowed to settle — NO: this checkout is %s, a linked worktree of %s "
                  "(`git rev-parse --git-common-dir` says %s). A linked worktree is a child's, "
                  "and CHILD.md forbids a child from calling "
                  "POST /v1/orchestrator/tasks/<id>/landing at all, so nothing below can stop "
                  "this run."
                  % (runner["checkout"], runner["repository"], runner["git_common_dir"]))
        if args.strict:
            print("    --strict is set, so neither condition applies and every unrecorded "
                  "landing on this machine fails this run.")

        if fatal and not args.strict:
            print("\n  LANDED, AND NO RECORD WAS WRITTEN — after the %s cutoff, in %s, and this "
                  "run is standing where they can be settled:" % (args.since, scope))
            for finding in fatal:
                print(describe(finding))
        elif fatal:
            print("\n  LANDED, AND NO RECORD WAS WRITTEN — every one of them, because --strict:")
            for finding in fatal:
                print(describe(finding))
        if withheld:
            print("\n  LANDED, AND NO RECORD WAS WRITTEN — after the %s cutoff, and NOT failing "
                  "this run. Each row says which of the two conditions it misses:" % args.since)
            for finding in withheld:
                print(describe(finding))
                print("        %s" % withheld_reason(finding, runner, scope))
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
                  "      -H \"x-clawdline-orchestrator: $(cat "
                  "~/.config/clawdline/orchestrator-token)\" \\\n"
                  "      -H 'Content-Type: application/json' \\\n"
                  "      -d '{\"state\":\"landed\",\"commit\":\"<head>\",\"target\":\"<branch>\","
                  "\"note\":\"<what was verified>\"}'")

    if fatal:
        if args.strict:
            print("\nlanding records: %d landing(s) happened and no record was written. Every "
                  "unrecorded landing counts under --strict, in every repository and from every "
                  "checkout." % len(fatal), file=sys.stderr)
        else:
            print("\nlanding records: %d landing(s) in %s happened and no record was written, "
                  "and this run can settle them. These are after the %s cutoff."
                  % (len(fatal), scope, args.since), file=sys.stderr)
        return 1

    if scope is None and not args.strict:
        # The report above did not depend on where it was run and was printed in full. What cannot
        # be answered from here is the other half — which of those rows this run is entitled to
        # stop over — and a check that cannot see its own subject says so rather than exiting 0
        # carrying it. `--strict` needs no scope, so it never reaches this.
        print("\nlanding records: this run cannot name the repository it is standing in (%s), so "
              "it cannot decide what it is entitled to fail over. Run it inside a repository, or "
              "pass --repository, or pass --strict — the finder, not the tree."
              % (runner["git_common_dir"] or "git would not answer for --git-common-dir"),
              file=sys.stderr)
        return 2

    if args.json:
        # stdout is the document and nothing else; the exit code and the failing sentence on
        # stderr are still there for a caller that wants both.
        return 0
    if withheld:
        # **The green a narrowed run prints is not the green an empty machine prints.** They share
        # no words on purpose: `tools/git-hooks/pre-commit` carries a paragraph about the same
        # defect, and this is the sentence that keeps a run which may not act from reading as a
        # run that found nothing.
        out_of_scope = [f for f in withheld if not f["in_runner_repository"]]
        barred = [f for f in withheld if f["in_runner_repository"]]
        parts = []
        if out_of_scope:
            parts.append("%d in another repository on this machine" % len(out_of_scope))
        if barred:
            parts.append("%d in %s, which this run may not settle from a linked worktree"
                         % (len(barred), scope))
        print("\nlanding records: %d unrecorded landing(s) after the %s cutoff are real and none "
              "of them stops this run — %s. This exit 0 is *not this run's to fail over*, not "
              "*nothing was found*; --strict fails on every one of them from anywhere."
              % (len(withheld), args.since, ", and ".join(parts)))
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
