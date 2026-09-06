#!/usr/bin/env node
// **A run that exits 0 because it found nothing and a run that exits 0 because it may not act must
// not print the same thing.** `tools/check-landing-records.py` reports the whole machine and fails
// only on debt this run can settle — the row is in the repository the suite is running in, and the
// checkout is not a linked worktree, because a linked worktree is a child's and `CHILD.md` forbids
// a child from calling the landing route at all. That narrowing is what stopped it killing two
// runs on 2026-09-06 over another root's landing in the base repository.
//
// `Tests/guard-red-proofs/landing-records-scope.sh` holds the red half: the same debt goes red from
// the repository and green from a linked worktree of it. What that harness structurally cannot hold
// is what the green arm *says* — it only asserts that the clean arm does not repeat the failing
// sentence, and silence would satisfy it. Silence is the exact defect `tools/git-hooks/pre-commit`
// carries a paragraph about, and this file is where the other half is held: the worktree arm has to
// name the checkout, the repository, `--git-common-dir`, the row it is not failing over, who can
// settle it, and that this is not the green of an empty machine.
//
// **Every fixture here is a fresh `git init` under a `mkdtemp` that is removed on the way out**, and
// `HOME`, `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM` and `GIT_CEILING_DIRECTORIES` are pointed at it,
// so the person's git configuration can neither change a result nor be changed by one. The registry
// the guard reads is `CLAWDLINE_REMOTE_DIR` inside the sandbox; this machine's real
// `~/.config/clawdline/orchestrator.json` is never opened.
//
// The control is the last scenario rather than a remark: the same fixture with the landing record
// written is green with the *other* sentence. Without it every assertion below would also pass
// against a guard that had simply stopped finding anything.
import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, realpathSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const guard = fileURLToPath(new URL("../tools/check-landing-records.py", import.meta.url));

let checks = 0;
let failed = false;
function check(condition, message) {
    checks += 1;
    if (condition) return;
    failed = true;
    console.error(`FAIL: ${message}`);
}

// Resolved rather than as `mkdtemp` returned it: on macOS the temporary directory is reached
// through `/var` -> `/private/var`, every path the guard prints has been through `realpath`, and
// `GIT_CEILING_DIRECTORIES` is documented not to work through a symlinked entry.
const sandbox = realpathSync(mkdtempSync(join(tmpdir(), "clawdline-landing-scope-")));
process.on("exit", () => rmSync(sandbox, { recursive: true, force: true }));

const gitEnvironment = {
    ...process.env,
    HOME: sandbox,
    GIT_CONFIG_GLOBAL: join(sandbox, "gitconfig"),
    GIT_CONFIG_SYSTEM: join(sandbox, "gitconfig-system"),
    GIT_CEILING_DIRECTORIES: sandbox,
    GIT_AUTHOR_DATE: "2021-01-01T00:00:00Z",
    GIT_COMMITTER_DATE: "2021-01-01T00:00:00Z",
    GIT_AUTHOR_NAME: "landing scope",
    GIT_AUTHOR_EMAIL: "scope@proof.invalid",
    GIT_COMMITTER_NAME: "landing scope",
    GIT_COMMITTER_EMAIL: "scope@proof.invalid",
};

function git(args, cwd) {
    const done = spawnSync("git", args, { cwd, env: gitEnvironment, encoding: "utf8" });
    if (done.status !== 0) {
        console.error(`FAIL: git ${args.join(" ")} in ${cwd}: ${done.stderr || done.stdout}`);
        process.exit(1);
    }
    return (done.stdout || "").trim();
}

// One repository with a delivery merged into `main`, and the ids git gave it. The dates are 2021
// and every run below passes `--since 2020-01-01`, so "after the cutoff and older than the six-hour
// grace" is true on every day this ever runs rather than only on some of them.
function repositoryWithALanding(name, task) {
    const repository = join(sandbox, name);
    mkdirSync(repository);
    git(["init", "-q", "."], repository);
    git(["symbolic-ref", "HEAD", "refs/heads/main"], repository);
    writeFileSync(join(repository, "file.txt"), "base\n");
    git(["add", "file.txt"], repository);
    git(["commit", "-q", "-m", "base"], repository);
    const base = git(["rev-parse", "HEAD"], repository);
    git(["checkout", "-q", "-b", `clawdline/task/${task}`], repository);
    writeFileSync(join(repository, "file.txt"), "base\ndelivered\n");
    git(["commit", "-q", "-am", "the delivery"], repository);
    const head = git(["rev-parse", "HEAD"], repository);
    git(["checkout", "-q", "main"], repository);
    git(["merge", "-q", "--no-ff", "-m", "land the delivery", `clawdline/task/${task}`], repository);
    // `git rev-parse --show-toplevel` answers with the resolved path, and every comparison the
    // guard makes is between resolved paths; on macOS the sandbox is under `/private/tmp`.
    return { path: git(["rev-parse", "--show-toplevel"], repository), base, head, task };
}

function row(repository, { landing } = {}) {
    return {
        id: repository.task,
        state: "success",
        title: "a delivery that reached main",
        created: 1609459200,
        finished_at: 1609459200,
        claims: [],
        root_label: "the root that has since gone home",
        project_dir: repository.path,
        worktree: {
            repository: repository.path,
            path: join(sandbox, "gone", repository.task),
            branch: `clawdline/task/${repository.task}`,
            base: repository.base,
            head: repository.head,
        },
        ...(landing ? { landing } : {}),
    };
}

function registry(name, tasks) {
    const store = join(sandbox, `store-${name}`);
    mkdirSync(store, { recursive: true });
    writeFileSync(join(store, "orchestrator.json"), JSON.stringify({ version: 1, tasks }));
    return store;
}

function run(store, cwd, extra = []) {
    const done = spawnSync("python3", [guard, "--since", "2020-01-01", ...extra], {
        cwd,
        env: { ...gitEnvironment, CLAWDLINE_REMOTE_DIR: store },
        encoding: "utf8",
    });
    // Kept apart as well as joined: `--json` puts the document on stdout and nothing else, and a
    // parser handed the failing sentence too would be reading a stream the guard does not promise.
    return {
        status: done.status,
        stdout: done.stdout || "",
        out: `${done.stdout || ""}\n${done.stderr || ""}`,
    };
}

const CLEAR = "nothing has landed unrecorded since";
const FATAL = "and no record was written";

// **The task ids share no prefix with anything else the guard prints.** An id whose first eight
// characters are also the fixture's directory name would be found in the header of every run, and
// "the row was printed" would be true of a guard that had stopped printing rows at all.
const owed = repositoryWithALanding("owes-a-record", "d0ffee00-1111-2222-3333-444444444444");
const elsewhere = repositoryWithALanding("another-repository", "beefbeef-5555-6666-7777-888888888888");
const linked = join(sandbox, "linked-checkout");
git(["worktree", "add", "--quiet", "--detach", linked, "main"], owed.path);

// ---- 1. In the repository, in its main worktree: both conditions met, and it fails ------------
const debt = registry("debt", [row(owed)]);
const fromRepository = run(debt, owed.path);
check(fromRepository.status === 1, `standing in ${owed.path} with an unrecorded landing in it: expected exit 1, got ${fromRepository.status}`);
check(fromRepository.out.includes(FATAL), "the failing run does not say a record was never written");
check(fromRepository.out.includes("this run can settle them"), "the failing run does not say why it is entitled to fail — the second condition has to be said out loud, not only applied");
check(fromRepository.out.includes("allowed to settle — yes"), "the failing run does not name the condition it met");
check(fromRepository.out.includes(`in this repository — ${owed.path}`), "the failing run does not name the repository it is standing in");

// ---- 2. The same debt from a linked worktree: green, and not quietly ---------------------------
const fromWorktree = run(debt, linked);
check(fromWorktree.status === 0, `standing in a linked worktree: expected exit 0, got ${fromWorktree.status}`);
check(!fromWorktree.out.includes(FATAL), "the worktree run failed over a record it is not permitted to write");
check(fromWorktree.out.includes("allowed to settle — NO"), "the worktree run does not say that it is not failing");
check(fromWorktree.out.includes(linked), "the worktree run does not name the checkout it is standing in");
check(fromWorktree.out.includes("--git-common-dir"), "the worktree run does not show the derivation — a reader cannot check a verdict whose evidence is not printed");
check(fromWorktree.out.includes("CHILD.md"), "the worktree run does not say why a child may not settle a record");
check(fromWorktree.out.includes(owed.task.slice(0, 8)) && fromWorktree.out.includes(owed.head.slice(0, 8)), "the worktree run does not print the row it is not failing over");
check(fromWorktree.out.includes(`The suite run in ${owed.path} fails on it`), "the worktree run does not say who can settle the row");
check(fromWorktree.out.includes("--strict fails on it from anywhere"), "the worktree run does not name the flag that fails on it here");
check(!fromWorktree.out.includes(CLEAR), "the worktree run prints the all-clear of a machine with no debt on it, which is the sentence this whole narrowing must not be able to produce");

// ---- 3. `--strict` ignores both narrowings ----------------------------------------------------
const strictFromWorktree = run(debt, linked, ["--strict"]);
check(strictFromWorktree.status === 1, `--strict from a linked worktree: expected exit 1, got ${strictFromWorktree.status}`);
check(strictFromWorktree.out.includes("Every unrecorded landing counts under --strict"), "--strict does not say that it is the reason this run failed");

// ---- 4. Another repository's debt, from a repository that owes nothing -------------------------
const elsewhereDebt = registry("elsewhere", [row(elsewhere)]);
const fromOtherRepository = run(elsewhereDebt, owed.path);
check(fromOtherRepository.status === 0, `another repository's debt: expected exit 0, got ${fromOtherRepository.status}`);
check(fromOtherRepository.out.includes(elsewhere.task.slice(0, 8)), "reporting stopped being machine-wide: a row in another repository was not printed at all");
check(fromOtherRepository.out.includes(`it is filed under ${elsewhere.path} and this run is in ${owed.path}`), "the row does not say which of the two conditions it misses");
check(!fromOtherRepository.out.includes(CLEAR), "a run holding another repository's debt printed the all-clear");

// ---- 5. A row a timer will take, marked as such and still fatal --------------------------------
const pending = registry("pending", [row(owed, {
    landing: { state: "pending", target: "main", since: 1609459200 },
})]);
const withPending = run(pending, owed.path);
check(withPending.status === 1, `a pending record whose delivery is already in main: expected exit 1, got ${withPending.status}`);
check(withPending.out.includes("a timer can take this one"), "a row Orchestrator.landingSweepPass can settle by itself is not marked as one");
const noRecord = run(debt, owed.path);
check(!noRecord.out.includes("a timer can take this one"), "a row with no record at all is marked as a sweep candidate; the sweep takes pending records with a target and nothing else, so this marker would be telling a reader to wait for a timer that is not coming");

// ---- 6. The machine-readable answer carries the same two conditions ----------------------------
const json = JSON.parse(run(debt, linked, ["--json"]).stdout);
check(json.runner.linked_worktree === true, "--json from a linked worktree does not say it is one");
check(json.runner.may_settle === false, "--json from a linked worktree says the run may settle a record");
check(json.runner.repository === owed.path, `--json names ${json.runner.repository} as the repository this run belongs to, not ${owed.path}`);
check(json.fatal_scope === owed.path, "--json does not say which repository the failing scope is");
check(json.findings.length === 1 && json.findings[0].fails_this_run === false, "--json does not mark the row as one this run is not failing over");
const jsonFromRepository = JSON.parse(run(debt, owed.path, ["--json"]).stdout);
check(jsonFromRepository.findings[0].fails_this_run === true, "--json from the repository itself does not mark the row as fatal");

// ---- 7. A run that cannot name where it is standing refuses rather than passing ----------------
const nowhere = join(sandbox, "not-a-repository");
mkdirSync(nowhere);
const fromNowhere = run(debt, nowhere);
check(fromNowhere.status === 2, `outside any repository: expected exit 2 — the finder, not the tree — got ${fromNowhere.status}`);
check(fromNowhere.out.includes("cannot decide what it is entitled to fail over"), "a run that cannot name its own repository does not say so");
check(fromNowhere.out.includes(owed.task.slice(0, 8)), "the refusal swallowed the report; the machine-wide half does not depend on where the run is standing");

// ---- 8. The control: the same fixture with the record written is green, and says something else -
const settled = registry("settled", [row(owed, {
    landing: {
        state: "landed", commit: owed.head, target: "main",
        landed_at: 1609459200, since: 1609459200,
    },
})]);
const clean = run(settled, owed.path);
check(clean.status === 0, `the same fixture with the landing record written: expected exit 0, got ${clean.status}`);
check(clean.out.includes(CLEAR), "a machine with no unrecorded landing on it does not print the all-clear");
check(!clean.out.includes(FATAL), "the control arm failed, so every green above proves nothing");
check(!clean.out.includes("are real and none of them stops this run"), "the empty machine and the narrowed run print the same sentence, which is the confusion this narrowing exists to avoid");

console.log(`${failed ? "not ok" : "ok"}: ${checks} landing-record scope checks`);
if (failed) process.exit(1);
