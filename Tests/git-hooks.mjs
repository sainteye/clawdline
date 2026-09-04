// The shared-tree pre-commit guard, exercised through real `git commit` runs.
//
// **Nothing here touches the checkout it is run from.** Every repository this file commits into is
// a fresh `git init` under a `mkdtemp` directory that is deleted on the way out; `HOME`,
// `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_SYSTEM` are pointed at that directory too, so the person's
// own git configuration can neither change the result nor be changed by it, and
// `GIT_CEILING_DIRECTORIES` stops git walking up out of the sandbox into a real repository. The
// only thing read from the repository under test is the three files being tested: the hook, the
// installer and the document. `runGit` refuses a working directory outside the sandbox, and the
// count of refusals it made is asserted at the end — a guard that cannot fire is not a guard.
//
// **What is being proved.** The guard refuses a commit that carries a path another session is
// working on, and does not refuse anything else — not the session's own claims, not a finished
// task's, not an isolated worktree's (the broker empties those, and reinstating them would refuse
// the root's own landing commit), not another repository's. When Clawdline is not answering it
// says so loudly and lets the commit through, because a hook that refuses everything while the app
// is down is uninstalled within the hour.
//
// The last check is the one that keeps the rest honest: the same refusal scenario is replayed
// against a stubbed hook that always exits 0, and the commit has to succeed. If it succeeds
// against the real hook too, the scenario had no teeth and this file says so.
//
// **Both directions have a control group.** A refusal is only worth asserting if the same commit
// lands with the hook stubbed out, and an *allow* is only worth asserting if the hook was asked
// anything at all: `passesWith` therefore checks that the broker was consulted for every commit it
// let through, and every ownership rule that allows is paired with the same scenario, one identity
// changed, that must refuse.

import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, chmodSync, rmSync, existsSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname, basename } from "node:path";
import { fileURLToPath } from "node:url";
import { realpathSync } from "node:fs";

let failures = 0;
let checks = 0;
const check = (what, ok) => {
    checks += 1;
    console.log(`  ${ok ? "✓" : "✗"} ${what}`);
    if (!ok) failures += 1;
};
// Cleanup runs from an exit handler rather than from `finally`, because `stop()` below exits the
// process outright and `process.exit()` skips `finally`. Every early abort used to leave its
// mkdtemp sandbox on disk and its stand-in broker still running; on a machine that has been
// force-rebooted twice by concurrent compiles, a leaked node process is not free. The last section
// of this file runs this file again in abort mode and proves the sandbox and the broker are gone.
let sandbox = null;
let brokerProcess = null;
let cleaned = false;
const cleanup = () => {
    if (cleaned) return;
    cleaned = true;
    try { if (brokerProcess) brokerProcess.kill(); } catch { /* already gone */ }
    try { if (sandbox) rmSync(sandbox, { recursive: true, force: true }); } catch { /* already gone */ }
};
process.on("exit", cleanup);
const stop = (why) => {
    console.log(`  ✗ ${why}`);
    console.log(`git hooks guard: stopped after ${checks + 1} checks — ${why}`);
    process.exit(1);
};

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, "..");
const HOOK_SOURCE = join(repoRoot, "tools", "git-hooks", "pre-commit");
const INSTALLER_SOURCE = join(repoRoot, "tools", "install-git-hooks.sh");
const DOC_SOURCE = join(repoRoot, "docs", "shared-tree-guard.md");
for (const path of [HOOK_SOURCE, INSTALLER_SOURCE, DOC_SOURCE]) {
    if (!existsSync(path)) stop(`${path} does not exist, so there is nothing to test`);
}
const hookText = readFileSync(HOOK_SOURCE, "utf8");
const installerText = readFileSync(INSTALLER_SOURCE, "utf8");
const docText = readFileSync(DOC_SOURCE, "utf8");

sandbox = realpathSync(mkdtempSync(join(tmpdir(), "clawdline-git-hooks-")));
const home = join(sandbox, "home");
mkdirSync(home, { recursive: true });
writeFileSync(join(sandbox, "gitconfig"), "");
const tokenFile = join(home, "orchestrator-token");
writeFileSync(tokenFile, "test-orchestrator-token\n");

// Every git process in this file runs through here, and here is the only place that decides where
// git is allowed to run. A cwd outside the sandbox is a bug in the test, not a failing assertion.
let refusedOutsideSandbox = 0;
const runGit = (cwd, args, extraEnv = {}) => {
    if (!realpathSync(cwd).startsWith(sandbox)) {
        refusedOutsideSandbox += 1;
        throw new Error(`refusing to run git in ${cwd}: outside the sandbox`);
    }
    return spawnSync("git", args, {
        cwd,
        encoding: "utf8",
        env: {
            PATH: process.env.PATH,
            HOME: home,
            TMPDIR: sandbox,
            LC_ALL: "C",
            GIT_CONFIG_GLOBAL: join(sandbox, "gitconfig"),
            GIT_CONFIG_SYSTEM: "/dev/null",
            GIT_CEILING_DIRECTORIES: dirname(sandbox),
            GIT_AUTHOR_NAME: "guard test",
            GIT_AUTHOR_EMAIL: "guard@test",
            GIT_COMMITTER_NAME: "guard test",
            GIT_COMMITTER_EMAIL: "guard@test",
            CLAWDLINE_GUARD_TOKEN_FILE: tokenFile,
            CLAWDLINE_GUARD_TIMEOUT: "5",
            ...extraEnv,
        },
    });
};
const sh = (cwd, script, extraEnv = {}) =>
    spawnSync("/bin/sh", ["-c", script], {
        cwd,
        encoding: "utf8",
        env: {
            PATH: process.env.PATH,
            HOME: home,
            TMPDIR: sandbox,
            LC_ALL: "C",
            GIT_CONFIG_GLOBAL: join(sandbox, "gitconfig"),
            GIT_CONFIG_SYSTEM: "/dev/null",
            GIT_CEILING_DIRECTORIES: dirname(sandbox),
            ...extraEnv,
        },
    });

let repoSerial = 0;
// A repository with the hook and the installer copied in, one commit of history, and the guard
// installed. `install` is passed false where the installer itself is what is being tested.
const makeRepo = ({ install = true } = {}) => {
    repoSerial += 1;
    const repo = join(sandbox, `repo-${repoSerial}`);
    mkdirSync(join(repo, "tools", "git-hooks"), { recursive: true });
    writeFileSync(join(repo, "tools", "git-hooks", "pre-commit"), hookText);
    chmodSync(join(repo, "tools", "git-hooks", "pre-commit"), 0o755);
    writeFileSync(join(repo, "tools", "install-git-hooks.sh"), installerText);
    chmodSync(join(repo, "tools", "install-git-hooks.sh"), 0o755);
    runGit(repo, ["-c", "init.defaultBranch=main", "init", "-q"]);
    writeFileSync(join(repo, "README.md"), "base\n");
    runGit(repo, ["add", "--", "README.md", "tools"]);
    runGit(repo, ["commit", "-q", "--no-verify", "-m", "base"]);
    if (install) {
        const r = sh(repo, "sh tools/install-git-hooks.sh");
        if (r.status !== 0) stop(`installer failed in a fresh repository: ${r.stderr}`);
    }
    return repo;
};
const commits = (repo) => runGit(repo, ["rev-list", "--count", "HEAD"]).stdout.trim();
const stagedIn = (repo) =>
    runGit(repo, ["diff-index", "--cached", "--name-only", "HEAD"]).stdout.split("\n").filter(Boolean);

// ---- a stand-in for the broker -----------------------------------------------------------------
// **It has to be its own process.** Every git run below is a `spawnSync`, which blocks this node
// process outright, so a server living in this event loop would never answer and every scenario
// would quietly pass through the fail-open path instead of the one it meant to test. That is what
// the first run of this file did. The stand-in therefore reads its answer off disk on each request
// and writes what it was asked into a log the parent reads back.
//
// It can also be told to fail. All five ways the claims check can lose its answer are exercised
// below — connection refused, an unreadable token, an HTTP error status, a body that is not JSON,
// and a broker too slow to answer — because "it fails open" is a claim about five paths and only
// two of them used to be run.
const TASKS_FILE = join(sandbox, "tasks.json");
const MODE_FILE = join(sandbox, "mode.json");
const REQUEST_LOG = join(sandbox, "requests.jsonl");
writeFileSync(join(sandbox, "broker.cjs"), `
const { createServer } = require("http");
const fs = require("fs");
const server = createServer((req, res) => {
    fs.appendFileSync(${JSON.stringify(REQUEST_LOG)}, JSON.stringify({
        url: req.url, token: req.headers["x-clawdline-orchestrator"] || null }) + "\\n");
    let mode = {};
    try { mode = JSON.parse(fs.readFileSync(${JSON.stringify(MODE_FILE)}, "utf8")); } catch {}
    const send = () => {
        if (mode.status && mode.status !== 200) {
            res.writeHead(mode.status, { "content-type": "text/plain" });
            return res.end(mode.body === undefined ? "error" : mode.body);
        }
        let body = mode.body;
        if (body === undefined || body === null) {
            let tasks = [];
            try { tasks = JSON.parse(fs.readFileSync(${JSON.stringify(TASKS_FILE)}, "utf8")); } catch {}
            body = JSON.stringify({ tasks, at: Math.floor(Date.now() / 1000) });
        }
        res.writeHead(200, { "content-type": "application/json" });
        res.end(body);
    };
    if (mode.delayMs) setTimeout(send, mode.delayMs); else send();
});
server.listen(0, "127.0.0.1", () => process.stdout.write("PORT " + server.address().port + "\\n"));
`);
brokerProcess = spawn(process.execPath, [join(sandbox, "broker.cjs")],
                      { stdio: ["ignore", "pipe", "pipe"] });
const endpoint = await new Promise((resolve, reject) => {
    let seen = "";
    const timer = setTimeout(() => reject(new Error("the stand-in broker never printed a port")), 10000);
    brokerProcess.stdout.on("data", (chunk) => {
        seen += chunk;
        const match = seen.match(/PORT (\d+)/);
        if (match) {
            clearTimeout(timer);
            resolve(`http://127.0.0.1:${match[1]}`);
        }
    });
    let brokerErr = "";
    brokerProcess.stderr.on("data", (chunk) => { brokerErr += chunk; });
    brokerProcess.on("exit", (code) =>
        reject(new Error(`the stand-in broker exited with ${code}: ${brokerErr}`)));
});

const setTasks = (tasks) => writeFileSync(TASKS_FILE, JSON.stringify(tasks));
// `null` puts the stand-in back to answering normally.
const setMode = (mode) => {
    if (mode === null) rmSync(MODE_FILE, { force: true });
    else writeFileSync(MODE_FILE, JSON.stringify(mode));
};
const requests = () => (existsSync(REQUEST_LOG)
    ? readFileSync(REQUEST_LOG, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l))
    : []);
// How many times the broker has been asked so far. Reading it before and after a commit is what
// turns "the commit was allowed" into "the hook looked, and then allowed" — without it, a hook
// that had stopped running at all would pass every allow-direction check in this file.
const askedSoFar = () => requests().length;
setTasks([]);
setMode(null);

// If this file is run again with this set, it aborts on purpose immediately after the sandbox and
// the stand-in broker exist. The last section runs it that way and checks that both are gone.
const ABORT = process.env.CLAWDLINE_GUARD_TEST_ABORT === "1";
if (ABORT) {
    console.log(`ABORT-SANDBOX ${sandbox}`);
    console.log(`ABORT-BROKER ${brokerProcess.pid}`);
    stop("aborting on purpose: CLAWDLINE_GUARD_TEST_ABORT");
}

const MY_TERMINAL = "11111111-1111-4111-8111-111111111111";
const OTHER_TERMINAL = "22222222-2222-4222-8222-222222222222";
const OTHER_ROOT_TERMINAL = "33333333-3333-4333-8333-333333333333";
// The identity a Claude Code session actually carries: CLAUDE_CODE_SESSION_ID is the value
// Clawdline records as child.sessionId, and that a root passes as root.sessionId when it
// dispatches. Unlike root.terminalId it is a stored field, which is why a landing depends on it.
const MY_SESSION = "44444444-4444-4444-8444-444444444444";

const liveTask = (repo, claims, overrides = {}) => ({
    id: "task-under-test",
    title: "Refactor: extract the task-draft parsing and refusal block",
    state: "briefed",
    kind: "custom",
    assistant: "claude",
    projectDir: repo,
    claims,
    child: { sessionId: "child-session", backend: "iterm", terminalId: OTHER_TERMINAL },
    root: { sessionId: "root-session", label: "clawdline root — architecture refactor line",
            terminalId: OTHER_ROOT_TERMINAL },
    ...overrides,
});

// A commit attempt by "this" session, with the broker reachable.
const attemptCommit = (repo, args = [], extraEnv = {}) =>
    runGit(repo, ["commit", "-m", "mine", ...args], {
        CLAWDLINE_GUARD_ENDPOINT: endpoint,
        CLAWDLINE_TERMINAL_ID: MY_TERMINAL,
        ...extraEnv,
    });

try {
    // ---- the installer -------------------------------------------------------------------------
    {
        const repo = makeRepo({ install: false });
        const first = sh(repo, "sh tools/install-git-hooks.sh");
        check("a fresh install sets core.hooksPath to the tracked directory and exits 0",
              first.status === 0
                && runGit(repo, ["config", "--get", "core.hooksPath"]).stdout.trim() === "tools/git-hooks");
        const second = sh(repo, "sh tools/install-git-hooks.sh");
        check("running it a second time is a no-op that says so, and still exits 0",
              second.status === 0 && /already installed/.test(second.stdout));
        check("and the value it reports the second time is the one it set",
              /core\.hooksPath = tools\/git-hooks/.test(second.stdout));

        // The hook only runs if git can execute it. A checkout under a umask that stripped the bit
        // leaves a file that git silently ignores, which is the quietest way for a guard to stop.
        chmodSync(join(repo, "tools", "git-hooks", "pre-commit"), 0o644);
        sh(repo, "sh tools/install-git-hooks.sh");
        check("it puts back an executable bit that a checkout stripped",
              (statSync(join(repo, "tools", "git-hooks", "pre-commit")).mode & 0o111) !== 0);
    }
    {
        const repo = makeRepo({ install: false });
        runGit(repo, ["config", "core.hooksPath", "somebody/elses/hooks"]);
        const r = sh(repo, "sh tools/install-git-hooks.sh");
        check("it refuses to take over a core.hooksPath that points somewhere else",
              r.status !== 0);
        check("and leaves that value exactly as it found it",
              runGit(repo, ["config", "--get", "core.hooksPath"]).stdout.trim() === "somebody/elses/hooks");
        check("and names the value it found, rather than failing silently",
              /somebody\/elses\/hooks/.test(r.stderr));
        check("and prints the command that would hand it over deliberately",
              /git config core\.hooksPath tools\/git-hooks/.test(r.stderr));
    }
    {
        const repo = makeRepo({ install: false });
        rmSync(join(repo, "tools", "git-hooks", "pre-commit"));
        const r = sh(repo, "sh tools/install-git-hooks.sh");
        check("it refuses to install a hook that is not there",
              r.status !== 0 && /does not exist/.test(r.stderr));
        check("and does not leave core.hooksPath pointing at nothing",
              runGit(repo, ["config", "--get", "core.hooksPath"]).stdout.trim() === "");
    }

    // ---- the guard, with the broker answering --------------------------------------------------
    {
        const repo = makeRepo();
        setTasks([]);
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        runGit(repo, ["add", "--", "mine.txt"]);
        const before = commits(repo);
        const askedBefore = askedSoFar();
        const r = attemptCommit(repo);
        check("with nothing claimed anywhere, a commit goes through", r.status === 0);
        check("and it really landed", commits(repo) !== before);
        const asked = requests().slice(askedBefore);
        check("the hook asked the broker for the task list",
              asked.length === 1 && asked[0].url === "/v1/orchestrator/tasks");
        check("and sent the orchestrator token it was told to read",
              asked.length === 1 && asked[0].token === "test-orchestrator-token");
        check("a passing hook says nothing at all", r.stderr.trim() === "");
    }

    let refusal = null;
    {
        const repo = makeRepo();
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        writeFileSync(join(repo, "theirs.txt"), "theirs\n");
        runGit(repo, ["add", "--", "mine.txt", "theirs.txt"]);
        setTasks([liveTask(repo, ["theirs.txt"])]);
        const before = commits(repo);
        const r = attemptCommit(repo);
        refusal = r.stderr;
        check("a commit carrying a path another root's live task claims is refused", r.status !== 0);
        check("and nothing was committed", commits(repo) === before);
        check("the refusal names the offending path", /theirs\.txt/.test(r.stderr));
        check("and does not name the committer's own file as an offender",
              !/^\s+mine\.txt$/m.test(r.stderr));
        check("it names the task holding the claim", /extract the task-draft parsing/.test(r.stderr));
        check("and the root that owns it", /architecture refactor line/.test(r.stderr));
        check("it gives the repository's own remedy, which unstages without touching their bytes",
              /git reset -- theirs\.txt/.test(r.stderr));
        check("it warns against `git commit -- <path>` as a substitute, per AGENTS.md",
              /unstaged/.test(r.stderr) && /AGENTS\.md/.test(r.stderr));
        check("and it names the escape hatch, so somebody who knows better is never stuck",
              /git commit --no-verify/.test(r.stderr));
        check("the refusal states plainly that it cannot see `git reset --hard`",
              /reset --hard/.test(r.stderr));

        // The escape hatch has to work, or the refusal above is a trap rather than a guard.
        const escaped = runGit(repo, ["commit", "--no-verify", "-m", "mine"], {
            CLAWDLINE_GUARD_ENDPOINT: endpoint, CLAWDLINE_TERMINAL_ID: MY_TERMINAL });
        check("`git commit --no-verify` goes through the same refusal", escaped.status === 0);
    }
    {
        // The remedy the refusal recommends has to leave the other session's staging alone.
        const repo = makeRepo();
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        writeFileSync(join(repo, "theirs.txt"), "theirs\n");
        runGit(repo, ["add", "--", "mine.txt", "theirs.txt"]);
        setTasks([liveTask(repo, ["theirs.txt"])]);
        runGit(repo, ["reset", "-q", "--", "theirs.txt"]);
        const r = attemptCommit(repo);
        check("after `git reset -- <their path>` the same commit goes through", r.status === 0);
        check("and their bytes are still in the worktree",
              readFileSync(join(repo, "theirs.txt"), "utf8") === "theirs\n");
    }
    {
        // A partial commit is read through git's temporary index, so it is self-limiting: it never
        // sees the paths it did not name. This is what makes the guard cheap for ordinary work.
        const repo = makeRepo();
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        writeFileSync(join(repo, "theirs.txt"), "theirs\n");
        runGit(repo, ["add", "--", "mine.txt", "theirs.txt"]);
        setTasks([liveTask(repo, ["theirs.txt"])]);
        const r = attemptCommit(repo, ["--", "mine.txt"]);
        check("a commit that names only its own path is not refused", r.status === 0);
        check("and the other session's staged path is still staged afterwards",
              stagedIn(repo).includes("theirs.txt"));
    }

    // ---- who owns the claim --------------------------------------------------------------------
    // Every one of these asserts that a commit was *allowed*, which is what a hook that had
    // stopped running would also produce. So each one also asserts that the broker was asked
    // exactly once first: the allow has to be a decision rather than an absence.
    const passesWith = (label, taskOverrides, claims = ["theirs.txt"], env = {}) => {
        const repo = makeRepo();
        writeFileSync(join(repo, "theirs.txt"), "content\n");
        runGit(repo, ["add", "--", "theirs.txt"]);
        setTasks([liveTask(repo, claims, taskOverrides)]);
        const askedBefore = askedSoFar();
        const r = attemptCommit(repo, [], env);
        check(label, r.status === 0);
        check(`— and the broker was consulted before allowing that: ${label}`,
              askedSoFar() === askedBefore + 1);
        return r;
    };
    // The paired negative for each rule above. Without it, `if MINE & task_identity(task)`
    // rewritten to `if False` turned only 2 of 60 checks red: identity is the most intricate part
    // of this guard and had the thinnest assertions in the file.
    const refusesWith = (label, taskOverrides, claims = ["theirs.txt"], env = {}) => {
        const repo = makeRepo();
        writeFileSync(join(repo, "theirs.txt"), "content\n");
        runGit(repo, ["add", "--", "theirs.txt"]);
        setTasks([liveTask(repo, claims, taskOverrides)]);
        const r = attemptCommit(repo, [], env);
        check(label, r.status !== 0 && /another session is working on/.test(r.stderr));
        return r;
    };
    passesWith("a claim held by this very session (child.terminalId) does not refuse it",
               { child: { terminalId: MY_TERMINAL } });
    refusesWith("but one terminal id different and the same commit is refused",
                { child: { terminalId: OTHER_TERMINAL } });
    passesWith("a claim held by a task this session dispatched (root.terminalId) does not refuse it",
               { root: { terminalId: MY_TERMINAL, label: "me" } });
    refusesWith("and a task dispatched by a different root is refused",
                { root: { terminalId: OTHER_ROOT_TERMINAL, label: "other root" } });
    passesWith("a session identified by ITERM_SESSION_ID's `w0t0p0:<UUID>` form is recognised by " +
               "the half after the colon", { child: { terminalId: MY_TERMINAL } }, ["theirs.txt"],
               { CLAWDLINE_TERMINAL_ID: "", ITERM_SESSION_ID: `w0t3p0:${MY_TERMINAL}` });
    passesWith("a finished task's claim is not a live claim",
               { state: "success", finishedAt: 1788000000 });
    passesWith("finishedAt alone finishes a task, even with the state still `briefed`",
               { finishedAt: 1788000000 });
    passesWith("a task the broker calls cancelled holds nothing either",
               { state: "cancelled" });
    refusesWith("a state this hook has never heard of is not silently treated as over",
                { state: "reported" });
    // This input shape is defensive rather than observed: the broker empties an isolated task's
    // claims before it stores them (90 of 90 worktree tasks in the records carried none), so a
    // worktree task with claims does not occur in production. The branch exists so that a change
    // on the broker side cannot turn a root's landing commit into a refusal, and this pins it.
    passesWith("an isolated worktree task's claim is ignored — the broker empties those, and " +
               "honouring them would refuse the root's own landing commit",
               { isolation: "worktree" });
    {
        // The landing. `root.terminalId` is not a stored field: Orchestrator.record(of:)
        // recomputes it from the SessionWatch inventory on every response and omits the key when
        // that lookup misses — and that inventory needs ten minutes to rebind after a crash.
        // `root.sessionId` is always on the record, and CLAUDE_CODE_SESSION_ID is the same value
        // in the root's own shell. A root committing over its own child's claims is the only
        // legitimate way a claimed path is ever committed (AGENTS.md), so it is the one commit
        // this guard must never refuse.
        const repo = makeRepo();
        writeFileSync(join(repo, "theirs.txt"), "content\n");
        runGit(repo, ["add", "--", "theirs.txt"]);
        setTasks([liveTask(repo, ["theirs.txt"],
                           { root: { sessionId: MY_SESSION, label: "my root" } })]);
        const askedBefore = askedSoFar();
        const r = attemptCommit(repo, [],
                                { CLAWDLINE_TERMINAL_ID: "", CLAUDE_CODE_SESSION_ID: MY_SESSION });
        check("a root landing its own child's work is allowed with root.terminalId missing from " +
              "the record — root.sessionId is the channel that is always there", r.status === 0);
        check("and the broker was consulted before allowing it",
              askedSoFar() === askedBefore + 1);
        check("and it is not refused with a message naming the committer's own root as the owner",
              !/owned by\s+my root/.test(r.stderr));
    }
    {
        // The paired negative: the same record, a different session at the keyboard.
        const repo = makeRepo();
        writeFileSync(join(repo, "theirs.txt"), "content\n");
        runGit(repo, ["add", "--", "theirs.txt"]);
        setTasks([liveTask(repo, ["theirs.txt"],
                           { root: { sessionId: "somebody-elses-root", label: "other root" } })]);
        const r = attemptCommit(repo, [],
                                { CLAWDLINE_TERMINAL_ID: "", CLAUDE_CODE_SESSION_ID: MY_SESSION });
        check("another root's session id is still refused, so the landing rule is not a hole",
              r.status !== 0);
    }
    {
        // CLAWDLINE_TERMINAL_ID replaces the ambient identities rather than joining them. A tmux
        // pane id is `%0`, and every tmux server starts numbering at `%0` again, so a session that
        // has already said who it is must not additionally answer to a stranger's pane.
        const repo = makeRepo();
        writeFileSync(join(repo, "theirs.txt"), "content\n");
        runGit(repo, ["add", "--", "theirs.txt"]);
        setTasks([liveTask(repo, ["theirs.txt"], { child: { terminalId: "%0" } })]);
        const stated = attemptCommit(repo, [], { TMUX_PANE: "%0" });
        check("a tmux pane id does not confer ownership on a session that has stated its identity",
              stated.status !== 0);
        const ambient = attemptCommit(repo, [], { CLAWDLINE_TERMINAL_ID: "", TMUX_PANE: "%0" });
        check("but a session whose only identity is its tmux pane is still recognised by it",
              ambient.status === 0);
    }
    {
        // A session the hook cannot place at all — a script, a cron job, a terminal that exports
        // none of the identities. It cannot be told apart from the owner, so it gets the
        // broker-dependent layer's direction: allow, loudly. It used to be refused, with a message
        // that said another session was working on a path that may well have been its own.
        const repo = makeRepo();
        writeFileSync(join(repo, "theirs.txt"), "content\n");
        runGit(repo, ["add", "--", "theirs.txt"]);
        setTasks([liveTask(repo, ["theirs.txt"])]);
        const r = attemptCommit(repo, [], { CLAWDLINE_TERMINAL_ID: "" });
        check("a session this hook cannot identify is warned, not refused", r.status === 0);
        check("and the warning says outright that it could not tell whose the claim is",
              /NOT checked against other sessions/.test(r.stderr) && /cannot tell/.test(r.stderr));
        check("and it still names the claim and the task holding it",
              /theirs\.txt/.test(r.stderr) && /extract the task-draft parsing/.test(r.stderr));
        check("and it does not assert the path belongs to somebody else",
              !/another session is working on/.test(r.stderr));
        check("and it says how to be recognised next time",
              /CLAWDLINE_TERMINAL_ID/.test(r.stderr));
    }
    {
        // The other half of the same rule: a task whose record carries no identity at all cannot
        // be attributed either, so it cannot be the grounds for a refusal.
        const repo = makeRepo();
        writeFileSync(join(repo, "theirs.txt"), "content\n");
        runGit(repo, ["add", "--", "theirs.txt"]);
        setTasks([liveTask(repo, ["theirs.txt"], { child: {}, root: { label: "other root" } })]);
        const r = attemptCommit(repo);
        check("a task that records no session or terminal of its own is warned about, not " +
              "refused", r.status === 0 && /cannot tell/.test(r.stderr));
    }
    {
        // Both at once, which is the case neither half describes on its own: one claim this hook
        // can attribute to a stranger and one it cannot attribute to anybody. The attributable one
        // decides — a refusal — and the other is reported as what it is rather than folded into
        // the count of paths "another session is working on".
        const repo = makeRepo();
        writeFileSync(join(repo, "known.txt"), "k\n");
        writeFileSync(join(repo, "unknown.txt"), "u\n");
        runGit(repo, ["add", "--", "known.txt", "unknown.txt"]);
        setTasks([
            liveTask(repo, ["known.txt"]),
            liveTask(repo, ["unknown.txt"], { id: "anonymous", title: "anonymous task",
                                              child: {}, root: { label: "nameless root" } }),
        ]);
        const r = attemptCommit(repo);
        check("one attributable claim still refuses even beside one that cannot be attributed",
              r.status !== 0 && /contains 1 path\(s\) another session is working on/.test(r.stderr));
        check("and the unattributable one is reported as that, not counted as a stranger's",
              /could not attribute to anybody/.test(r.stderr) && /unknown\.txt/.test(r.stderr));
        check("and only the attributable one gets a `git reset` line",
              /git reset -- known\.txt/.test(r.stderr) && !/git reset -- unknown\.txt/.test(r.stderr));
    }
    {
        // A claim is a hand-typed string and this repository lives on a case-insensitive volume,
        // where `sources/foo.swift` *is* `Sources/Foo.swift`. A byte-for-byte comparison let that
        // typo through in silence, which is the direction that loses work.
        const repo = makeRepo();
        mkdirSync(join(repo, "Sources"), { recursive: true });
        writeFileSync(join(repo, "Sources", "Foo.swift"), "x\n");
        runGit(repo, ["add", "--", "Sources/Foo.swift"]);
        setTasks([liveTask(repo, ["sources/foo.swift"])]);
        const r = attemptCommit(repo);
        check("a claim differing from the staged path only in case is still a claim", r.status !== 0);
        check("and the refusal says which arm matched, so an over-refusal can be read as one",
              /only in case/.test(r.stderr));
    }
    {
        const repo = makeRepo();
        writeFileSync(join(repo, "theirs.txt"), "content\n");
        runGit(repo, ["add", "--", "theirs.txt"]);
        setTasks([liveTask(join(sandbox, "some-other-project"), ["theirs.txt"])]);
        check("a claim in another repository does not reach into this one",
              attemptCommit(repo).status === 0);
    }
    {
        const repo = makeRepo();
        mkdirSync(join(repo, "docs"), { recursive: true });
        writeFileSync(join(repo, "docs", "note.md"), "note\n");
        runGit(repo, ["add", "--", "docs/note.md"]);
        setTasks([liveTask(repo, ["docs"])]);
        check("a claim on a directory covers a staged file inside it",
              attemptCommit(repo).status !== 0);
    }
    {
        const repo = makeRepo();
        mkdirSync(join(repo, "docs"), { recursive: true });
        writeFileSync(join(repo, "docs", "note.md"), "note\n");
        runGit(repo, ["add", "--", "docs/note.md"]);
        setTasks([liveTask(repo, ["doc"])]);
        check("but a claim that merely shares a prefix does not — `doc` is not `docs/note.md`",
              attemptCommit(repo).status === 0);
    }

    // ---- the failure mode ----------------------------------------------------------------------
    // Fail open, loudly. The alternative was measured against the thing that actually happens: the
    // first commit refused for no reason is the last commit this hook ever sees.
    {
        const repo = makeRepo();
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        runGit(repo, ["add", "--", "mine.txt"]);
        const r = runGit(repo, ["commit", "-m", "mine"], {
            CLAWDLINE_GUARD_ENDPOINT: "http://127.0.0.1:1",
            CLAWDLINE_TERMINAL_ID: MY_TERMINAL,
        });
        check("with Clawdline not answering, the commit still goes through", r.status === 0);
        check("and the hook says loudly that nothing was checked",
              /NOT checked against other sessions/.test(r.stderr));
        check("and names what it could not vouch for", /mine\.txt/.test(r.stderr));
    }
    {
        const repo = makeRepo();
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        runGit(repo, ["add", "--", "mine.txt"]);
        const r = runGit(repo, ["commit", "-m", "mine"], {
            CLAWDLINE_GUARD_ENDPOINT: endpoint,
            CLAWDLINE_TERMINAL_ID: MY_TERMINAL,
            CLAWDLINE_GUARD_TOKEN_FILE: join(sandbox, "no-such-token"),
        });
        check("an unreadable orchestrator token fails open too, with the same warning",
              r.status === 0 && /NOT checked against other sessions/.test(r.stderr));
    }
    {
        const repo = makeRepo();
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        runGit(repo, ["add", "--", "mine.txt"]);
        setMode({ body: "{\"unexpected\": true}" });
        const r = attemptCommit(repo);
        setMode(null);
        check("an answer in a shape the hook does not understand fails open, and says so",
              r.status === 0 && /does not understand/.test(r.stderr));
    }
    // The remaining three failures. All five reach the same fail-open path, which was already
    // true; what was not true is that the message told you which one had happened. Two of them
    // used to print "Clawdline is not answering" about an app that was answering perfectly well —
    // so somebody chasing a rotated orchestrator token went and checked whether the app was
    // running instead, while every commit in between went silently unchecked.
    {
        const repo = makeRepo();
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        runGit(repo, ["add", "--", "mine.txt"]);
        setMode({ status: 500, body: "boom" });
        const r = attemptCommit(repo);
        setMode(null);
        check("an HTTP 500 fails open", r.status === 0
              && /NOT checked against other sessions/.test(r.stderr));
        check("and the warning names the status rather than claiming nobody answered",
              /HTTP 500/.test(r.stderr) && !/is not answering/.test(r.stderr));
    }
    {
        const repo = makeRepo();
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        runGit(repo, ["add", "--", "mine.txt"]);
        setMode({ status: 401, body: "nope" });
        const r = attemptCommit(repo);
        setMode(null);
        check("an HTTP 401 fails open", r.status === 0
              && /NOT checked against other sessions/.test(r.stderr));
        check("and it sends the reader to the token rather than to the app",
              /HTTP 401/.test(r.stderr) && /token/.test(r.stderr)
              && !/is not answering/.test(r.stderr));
    }
    {
        const repo = makeRepo();
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        runGit(repo, ["add", "--", "mine.txt"]);
        setMode({ body: "<html>not json" });
        const r = attemptCommit(repo);
        setMode(null);
        check("a body that is not JSON fails open, and says that is what happened",
              r.status === 0 && /could not read as JSON/.test(r.stderr)
              && !/is not answering/.test(r.stderr));
    }
    {
        const repo = makeRepo();
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        runGit(repo, ["add", "--", "mine.txt"]);
        setMode({ delayMs: 3000 });
        const r = attemptCommit(repo, [], { CLAWDLINE_GUARD_TIMEOUT: "1" });
        setMode(null);
        check("a broker too slow to answer fails open within the timeout it was given",
              r.status === 0 && /NOT checked against other sessions/.test(r.stderr));
        check("and that one really is `not answering`, so the phrase keeps its meaning",
              /is not answering/.test(r.stderr));
    }

    // ---- a merge in progress -------------------------------------------------------------------
    // A repository whose `main` and `side` both rewrote the same line, so any sequencer operation
    // between them conflicts.
    const conflicted = () => {
        const repo = makeRepo();
        setTasks([]);
        writeFileSync(join(repo, "a.txt"), "base\n");
        runGit(repo, ["add", "--", "a.txt"]);
        runGit(repo, ["commit", "-q", "--no-verify", "-m", "a"]);
        runGit(repo, ["checkout", "-q", "-b", "side"]);
        writeFileSync(join(repo, "a.txt"), "side\n");
        runGit(repo, ["commit", "-q", "--no-verify", "-am", "side"]);
        runGit(repo, ["checkout", "-q", "main"]);
        writeFileSync(join(repo, "a.txt"), "main\n");
        runGit(repo, ["commit", "-q", "--no-verify", "-am", "main"]);
        return repo;
    };
    {
        const repo = conflicted();
        const merge = runGit(repo, ["merge", "side"]);
        check("the fixture really is a conflicted merge", merge.status !== 0
              && existsSync(join(repo, ".git", "MERGE_HEAD")));
        writeFileSync(join(repo, "a.txt"), "resolved\n");
        runGit(repo, ["add", "--", "a.txt"]);
        const before = commits(repo);
        const r = attemptCommit(repo);
        check("concluding a merge somebody else may have started is refused", r.status !== 0);
        check("and nothing was committed", commits(repo) === before);
        check("the refusal says a merge is in progress", /merge is in progress/.test(r.stderr));
        check("and names the escape hatch for the person whose merge it is",
              /git commit --no-verify/.test(r.stderr));

        // Fails closed, and it needs no broker to do it: this is the check that still works when
        // Clawdline is down.
        const offline = runGit(repo, ["commit", "-m", "mine"], {
            CLAWDLINE_GUARD_ENDPOINT: "http://127.0.0.1:1",
            CLAWDLINE_TERMINAL_ID: MY_TERMINAL,
        });
        check("and it refuses with the broker unreachable too — it depends on nothing",
              offline.status !== 0 && /merge is in progress/.test(offline.stderr));
        const escaped = runGit(repo, ["commit", "--no-verify", "--no-edit"], {
            CLAWDLINE_GUARD_ENDPOINT: endpoint, CLAWDLINE_TERMINAL_ID: MY_TERMINAL });
        check("the owner of the merge gets through with --no-verify", escaped.status === 0);
    }

    // ---- the sequencer operations that are not a merge -------------------------------------------
    // A merge was the only one being looked at, and it is not the only one that stops in a shared
    // tree holding somebody else's hand-resolved conflicts. `git cherry-pick --continue` and
    // `git revert --continue` do call `pre-commit`, and so does a plain `git commit` typed while a
    // rebase is stopped.
    {
        const repo = conflicted();
        const picked = runGit(repo, ["cherry-pick", "side"]);
        check("the fixture really is a conflicted cherry-pick, and it leaves CHERRY_PICK_HEAD "
              + "rather than MERGE_HEAD", picked.status !== 0
              && existsSync(join(repo, ".git", "CHERRY_PICK_HEAD"))
              && !existsSync(join(repo, ".git", "MERGE_HEAD")));
        writeFileSync(join(repo, "a.txt"), "resolved\n");
        runGit(repo, ["add", "--", "a.txt"]);
        const r = attemptCommit(repo);
        check("concluding somebody else's cherry-pick is refused, and named as one",
              r.status !== 0 && /cherry-pick is in progress/.test(r.stderr));
        check("and that refusal names the escape hatch too",
              /git commit --no-verify/.test(r.stderr));
    }
    {
        const repo = conflicted();
        const reverted = runGit(repo, ["revert", "--no-edit", "HEAD~1"]);
        check("the fixture really is a conflicted revert", reverted.status !== 0
              && existsSync(join(repo, ".git", "REVERT_HEAD")));
        writeFileSync(join(repo, "a.txt"), "resolved\n");
        runGit(repo, ["add", "--", "a.txt"]);
        const r = attemptCommit(repo);
        check("concluding somebody else's revert is refused", r.status !== 0
              && /revert is in progress/.test(r.stderr));
    }
    {
        const repo = conflicted();
        runGit(repo, ["checkout", "-q", "side"]);
        const rebase = runGit(repo, ["rebase", "main"]);
        const stopped = existsSync(join(repo, ".git", "rebase-merge"))
                     || existsSync(join(repo, ".git", "rebase-apply"));
        check("the fixture really is a stopped rebase", rebase.status !== 0 && stopped);
        writeFileSync(join(repo, "a.txt"), "resolved\n");
        runGit(repo, ["add", "--", "a.txt"]);
        const r = attemptCommit(repo);
        check("and a plain `git commit` typed into somebody else's stopped rebase is refused",
              r.status !== 0 && /rebase is in progress/.test(r.stderr));
    }

    // ---- a sequencer operation in a linked worktree ----------------------------------------------
    // The refusal above is about a checkout five sessions share one index in. A linked worktree is
    // not that: it is created for one landing, by one session, and nobody else has a checkout of
    // it, so a `MERGE_HEAD` sitting in `.git/worktrees/<name>/` has exactly one possible owner.
    //
    // Before this narrowing the hook refused those too, and the only regular caller of this guard
    // was a root landing deliveries in exactly such a worktree — three of the four landings that
    // followed the hook's own installation on 2026-09-03 were made with `--no-verify` because of
    // it. An escape hatch typed several times a day is one nobody reads, which costs more than the
    // refusal was worth.
    //
    // Both directions are in one fixture on purpose. The allow is only worth asserting beside the
    // refusal it narrows, and the single thing that differs between them is which checkout the
    // commit is typed in.
    {
        const repo = conflicted();

        // The control, first, in the shared checkout.
        const shared = runGit(repo, ["merge", "side"]);
        check("the fixture conflicts in the shared checkout", shared.status !== 0
              && existsSync(join(repo, ".git", "MERGE_HEAD")));
        writeFileSync(join(repo, "a.txt"), "resolved\n");
        runGit(repo, ["add", "--", "a.txt"]);
        const refusedInShared = attemptCommit(repo);
        check("and concluding it there is still refused, exactly as before",
              refusedInShared.status !== 0 && /merge is in progress/.test(refusedInShared.stderr));
        runGit(repo, ["merge", "--abort"]);

        // The same merge, in a worktree linked to the same repository.
        const worktree = join(sandbox, `worktree-${repoSerial}`);
        const added = runGit(repo, ["worktree", "add", "-q", worktree, "side"]);
        if (added.status !== 0) stop(`git worktree add failed: ${added.stderr}`);
        const own = runGit(worktree, ["rev-parse", "--absolute-git-dir"]).stdout.trim();
        const common = runGit(worktree, ["rev-parse", "--path-format=absolute",
                                         "--git-common-dir"]).stdout.trim();
        check("the fixture really is a linked worktree — its git dir is not the repository's",
              own !== "" && common !== "" && own !== common);

        const merged = runGit(worktree, ["merge", "main"]);
        check("and the merge inside it conflicts and leaves MERGE_HEAD in that worktree's own "
              + "git dir", merged.status !== 0 && existsSync(join(own, "MERGE_HEAD")));
        writeFileSync(join(worktree, "a.txt"), "resolved\n");
        runGit(worktree, ["add", "--", "a.txt"]);
        const askedBefore = askedSoFar();
        const r = attemptCommit(worktree);
        check("concluding a merge in a linked worktree is allowed", r.status === 0);
        // Not a commit count: concluding a merge makes `rev-list --count` jump by the whole of the
        // side it just absorbed, so the number proves nothing about this one commit. The subject
        // line and the marker being gone do.
        check("and the commit was actually made, concluding that merge",
              runGit(worktree, ["log", "-1", "--format=%s"]).stdout.trim() === "mine"
              && !existsSync(join(own, "MERGE_HEAD")));
        check("it says why it allowed rather than passing in silence",
              /linked worktree/.test(r.stderr) && /can only be yours/.test(r.stderr));
        // The teeth for this direction. An allow proves nothing unless the hook ran at all, and
        // the narrowing is to the first check only: the staged paths still go to the broker.
        check("and the hook ran — the claims check was still asked about those paths",
              askedSoFar() > askedBefore);
    }
    {
        // Not just MERGE_HEAD: the narrowing is the whole of check 1, so one of the other three is
        // driven end to end too.
        const repo = conflicted();
        const worktree = join(sandbox, `worktree-pick-${repoSerial}`);
        const added = runGit(repo, ["worktree", "add", "-q", worktree, "side"]);
        if (added.status !== 0) stop(`git worktree add failed: ${added.stderr}`);
        const own = runGit(worktree, ["rev-parse", "--absolute-git-dir"]).stdout.trim();
        const picked = runGit(worktree, ["cherry-pick", "main"]);
        check("a cherry-pick in a linked worktree conflicts and leaves CHERRY_PICK_HEAD",
              picked.status !== 0 && existsSync(join(own, "CHERRY_PICK_HEAD")));
        writeFileSync(join(worktree, "a.txt"), "resolved\n");
        runGit(worktree, ["add", "--", "a.txt"]);
        const r = attemptCommit(worktree);
        check("and concluding it is allowed, with the same sentence", r.status === 0
              && /cherry-pick is in progress, and this is a linked worktree/.test(r.stderr));
    }
    {
        // And the refusal is not gone from the shared checkout for the other three either. The
        // pair for the cherry-pick above, one checkout changed.
        const repo = conflicted();
        const picked = runGit(repo, ["cherry-pick", "side"]);
        check("the same cherry-pick conflicts in the shared checkout", picked.status !== 0);
        writeFileSync(join(repo, "a.txt"), "resolved\n");
        runGit(repo, ["add", "--", "a.txt"]);
        const r = attemptCommit(repo);
        check("and there it is still refused", r.status !== 0
              && /cherry-pick is in progress in this checkout/.test(r.stderr));
    }

    // ---- `git commit --amend` --------------------------------------------------------------------
    // `pre-commit` is handed no arguments and runs before `prepare-commit-msg`, so the hook cannot
    // tell an amend from an ordinary commit without reading its parent process's argv — a guess
    // that is wrong in both directions. So this hole is written down rather than closed: an amend
    // is read against the commit it replaces, which means a foreign path *already in* that commit
    // is carried through. It creates no new damage — that commit already held it. What has to keep
    // holding is the other half, and these two checks are where that decision is recorded.
    {
        const repo = makeRepo();
        writeFileSync(join(repo, "theirs.txt"), "theirs\n");
        runGit(repo, ["add", "--", "theirs.txt"]);
        runGit(repo, ["commit", "-q", "--no-verify", "-m", "already carries theirs"]);
        setTasks([liveTask(repo, ["theirs.txt"])]);
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        runGit(repo, ["add", "--", "mine.txt"]);
        const r = runGit(repo, ["commit", "--amend", "--no-edit"], {
            CLAWDLINE_GUARD_ENDPOINT: endpoint, CLAWDLINE_TERMINAL_ID: MY_TERMINAL });
        check("an amend carries a foreign path already in HEAD through — documented, not checked",
              r.status === 0);
    }
    {
        const repo = makeRepo();
        writeFileSync(join(repo, "mine.txt"), "mine\n");
        runGit(repo, ["add", "--", "mine.txt"]);
        runGit(repo, ["commit", "-q", "--no-verify", "-m", "mine"]);
        writeFileSync(join(repo, "theirs.txt"), "theirs\n");
        runGit(repo, ["add", "--", "theirs.txt"]);
        setTasks([liveTask(repo, ["theirs.txt"])]);
        const r = runGit(repo, ["commit", "--amend", "--no-edit"], {
            CLAWDLINE_GUARD_ENDPOINT: endpoint, CLAWDLINE_TERMINAL_ID: MY_TERMINAL });
        check("but an amend that newly stages a foreign path is refused like any other commit",
              r.status !== 0);
    }

    // ---- does the scenario have teeth? ---------------------------------------------------------
    // The refusal scenario, replayed against a hook that always allows. If this commit is refused,
    // something other than the hook was refusing it and every check above is worthless.
    {
        const repo = makeRepo();
        writeFileSync(join(repo, "tools", "git-hooks", "pre-commit"),
                      "#!/bin/sh\n# stubbed: always allow\nexit 0\n");
        chmodSync(join(repo, "tools", "git-hooks", "pre-commit"), 0o755);
        writeFileSync(join(repo, "theirs.txt"), "theirs\n");
        runGit(repo, ["add", "--", "theirs.txt"]);
        setTasks([liveTask(repo, ["theirs.txt"])]);
        const r = attemptCommit(repo);
        check("with the guard stubbed out to always allow, the same bad commit lands — so the " +
              "refusals above came from the hook and not from git", r.status === 0);
    }

    // ---- what the documentation has to say -----------------------------------------------------
    check("the document states plainly that pre-commit does not fire on `git reset --hard`",
          /does not (run|fire) on `git reset --hard`/.test(docText));
    check("it names the escape hatch", /git commit --no-verify/.test(docText));
    check("it records which failure mode was chosen and why",
          /fails?[-\s]open/i.test(docText) && /fails?[-\s]closed/i.test(docText));
    check("it names the mechanism the guard rests on", /claims/.test(docText)
          && /MERGE_HEAD/.test(docText));
    check("and it says outright that the 2026-09-03 incident could not have been stopped here",
          /2026-09-03/.test(docText) && /could not have/i.test(docText));
    check("the hook itself carries the same scope sentence", /reset --hard/.test(hookText));
    check("the installer points at the tracked directory rather than .git/hooks",
          /core\.hooksPath/.test(installerText) && /tools\/git-hooks/.test(installerText));
    // The document used to describe a fallback chain the hook does not implement, headed by a
    // variable the app has never exported. Documentation that is wrong about the identity rule is
    // worse than none, because the rule is the part somebody debugs a refusal against.
    check("it names the identity the app actually sets, and the field a landing turns on",
          /CLAUDE_CODE_SESSION_ID/.test(docText) && /root\.sessionId/.test(docText));
    check("it says CLAWDLINE_TERMINAL_ID replaces the ambient values rather than joining them",
          /CLAWDLINE_TERMINAL_ID/.test(docText) && /replaces/.test(docText));
    check("it says what a session the hook cannot identify gets, now that it is not a refusal",
          /cannot identify|cannot tell/.test(docText));
    check("it names the sequencer markers the closed check covers, not just MERGE_HEAD",
          /CHERRY_PICK_HEAD/.test(docText) && /REVERT_HEAD/.test(docText) && /rebase/.test(docText));
    check("it records the `git commit --amend` hole rather than leaving it unsaid",
          /--amend/.test(docText));
    check("it says claims are matched case-insensitively and what that costs",
          /case-insensitive/.test(docText));
    check("it distinguishes the broker failures instead of calling them all an outage",
          /HTTP/.test(docText) && /JSON/.test(docText));

    // ---- can anybody find it? ---------------------------------------------------------------------
    // `core.hooksPath` is unset in a fresh checkout, so this guard does nothing at all until
    // somebody runs the installer. Landing it with no pointer anywhere is landing nothing: the
    // document was linked from no file in the repository, and neither README mentioned it.
    for (const name of ["AGENTS.md", "README.md", "README.zh-TW.md"]) {
        const text = readFileSync(join(repoRoot, name), "utf8");
        check(`${name} points at the guard and at the command that switches it on`,
              /docs\/shared-tree-guard\.md/.test(text) && /install-git-hooks\.sh/.test(text));
    }

    // ---- and the file's own cleanup ---------------------------------------------------------------
    // `stop()` exits the process, and `process.exit()` skips `finally`. Every early abort used to
    // leave a mkdtemp sandbox on disk and the stand-in broker still running. This runs this very
    // file in abort mode and checks both are gone — the only way to prove it is to abort for real.
    if (!ABORT) {
        const aborted = spawnSync(process.execPath, [fileURLToPath(import.meta.url)], {
            encoding: "utf8", env: { ...process.env, CLAWDLINE_GUARD_TEST_ABORT: "1" },
        });
        const found = aborted.stdout.match(/ABORT-SANDBOX (\S+)[\s\S]*ABORT-BROKER (\d+)/);
        check("an early abort exits non-zero and says where it stopped",
              aborted.status === 1 && found !== null);
        if (found) {
            const gone = (test) => {
                for (let i = 0; i < 40; i += 1) {
                    if (test()) return true;
                    Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 50);
                }
                return test();
            };
            check("and takes its sandbox with it rather than leaving it on disk",
                  gone(() => !existsSync(found[1])));
            check("and does not leave the stand-in broker process running",
                  gone(() => {
                      try { process.kill(Number(found[2]), 0); return false; } catch { return true; }
                  }));
        }
    }

    // ---- and the test's own containment ---------------------------------------------------------
    check("every git process this file started ran inside its own temporary directory",
          refusedOutsideSandbox === 0);
    let escapedSandbox = false;
    try {
        runGit(repoRoot, ["status"]);
    } catch {
        escapedSandbox = true;
    }
    check("and the containment is real: pointing it at this checkout is refused, not run",
          escapedSandbox && refusedOutsideSandbox === 1);

    if (failures > 0 && refusal) {
        console.log("    the refusal text was:");
        console.log(refusal.split("\n").map((l) => `      ${l}`).join("\n"));
    }
} finally {
    cleanup();
}

console.log(failures === 0
    ? `git hooks guard: all ${checks} checks passed`
    : `git hooks guard: ${failures} of ${checks} checks failed`);
process.exit(failures === 0 ? 0 : 1);
