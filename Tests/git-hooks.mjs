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

import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, chmodSync, rmSync, existsSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { realpathSync } from "node:fs";

let failures = 0;
let checks = 0;
const check = (what, ok) => {
    checks += 1;
    console.log(`  ${ok ? "✓" : "✗"} ${what}`);
    if (!ok) failures += 1;
};
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

const sandbox = realpathSync(mkdtempSync(join(tmpdir(), "clawdline-git-hooks-")));
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
const TASKS_FILE = join(sandbox, "tasks.json");
const BODY_FILE = join(sandbox, "body.txt");
const REQUEST_LOG = join(sandbox, "requests.jsonl");
writeFileSync(join(sandbox, "broker.cjs"), `
const { createServer } = require("http");
const fs = require("fs");
const server = createServer((req, res) => {
    fs.appendFileSync(${JSON.stringify(REQUEST_LOG)}, JSON.stringify({
        url: req.url, token: req.headers["x-clawdline-orchestrator"] || null }) + "\\n");
    let body;
    try {
        body = fs.readFileSync(${JSON.stringify(BODY_FILE)}, "utf8");
    } catch {
        let tasks = [];
        try { tasks = JSON.parse(fs.readFileSync(${JSON.stringify(TASKS_FILE)}, "utf8")); } catch {}
        body = JSON.stringify({ tasks, at: Math.floor(Date.now() / 1000) });
    }
    res.writeHead(200, { "content-type": "application/json" });
    res.end(body);
});
server.listen(0, "127.0.0.1", () => process.stdout.write("PORT " + server.address().port + "\\n"));
`);
const brokerProcess = spawn(process.execPath, [join(sandbox, "broker.cjs")],
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
const setBody = (body) => {
    if (body === null) rmSync(BODY_FILE, { force: true });
    else writeFileSync(BODY_FILE, body);
};
const requests = () => (existsSync(REQUEST_LOG)
    ? readFileSync(REQUEST_LOG, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l))
    : []);
setTasks([]);

const MY_TERMINAL = "11111111-1111-4111-8111-111111111111";
const OTHER_TERMINAL = "22222222-2222-4222-8222-222222222222";
const OTHER_ROOT_TERMINAL = "33333333-3333-4333-8333-333333333333";

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
        const r = attemptCommit(repo);
        check("with nothing claimed anywhere, a commit goes through", r.status === 0);
        check("and it really landed", commits(repo) !== before);
        const asked = requests();
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
    const passesWith = (label, taskOverrides, claims = ["theirs.txt"], env = {}) => {
        const repo = makeRepo();
        writeFileSync(join(repo, "theirs.txt"), "content\n");
        runGit(repo, ["add", "--", "theirs.txt"]);
        setTasks([liveTask(repo, claims, taskOverrides)]);
        const r = attemptCommit(repo, [], env);
        check(label, r.status === 0);
        return r;
    };
    passesWith("a claim held by this very session (child.terminalId) does not refuse it",
               { child: { terminalId: MY_TERMINAL } });
    passesWith("a claim held by a task this session dispatched (root.terminalId) does not refuse it",
               { root: { terminalId: MY_TERMINAL, label: "me" } });
    passesWith("a finished task's claim is not a live claim",
               { state: "success", finishedAt: 1788000000 });
    passesWith("a task the broker calls cancelled holds nothing either",
               { state: "cancelled" });
    passesWith("an isolated worktree task's claim is ignored — the broker empties those, and " +
               "honouring them would refuse the root's own landing commit",
               { isolation: "worktree" });
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
        setBody("{\"unexpected\": true}");
        const r = attemptCommit(repo);
        setBody(null);
        check("an answer in a shape the hook does not understand fails open, and says so",
              r.status === 0 && /does not understand/.test(r.stderr));
    }

    // ---- a merge in progress -------------------------------------------------------------------
    {
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
    brokerProcess.kill();
    rmSync(sandbox, { recursive: true, force: true });
}

console.log(failures === 0
    ? `git hooks guard: all ${checks} checks passed`
    : `git hooks guard: ${failures} of ${checks} checks failed`);
process.exit(failures === 0 ? 0 : 1);
