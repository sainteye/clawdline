// tools/scratch.sh, driven for real: however a snapshot run ends, it leaves nothing behind.
//
// **Why this file exists.** The recipes AGENTS.md used to print made a repository snapshot and a
// private TMPDIR with `mktemp -d` and removed neither; a root has no `work/`, so every run left both
// behind. The tool that replaced them promises the absence of a directory, which is the one result
// an exit code cannot show — so every run below is followed by a look at the root it was pointed at.
//
// **Nothing here touches this machine's real scratch.** The tool is only ever pointed at a root
// inside a `mkdtemp` sandbox that is deleted on the way out. `toolDoor` refuses any other root or
// working directory, and the one deliberate refusal is counted at the end: a containment check
// nobody has seen fire is not one. Repositories are fresh `git init`s in the same sandbox, with HOME
// and git's global and system configuration pointed away from the person's. The single path outside
// the sandbox the tool is shown is `/usr`, as a root that belongs to somebody else, and the tool has
// to refuse it before it writes anything.
//
// **Run under zh_TW on purpose.** The tool pins LC_ALL=C and TZ=UTC for every `ps` and `date` it
// parses. This file sets LANG=zh_TW.UTF-8 and no LC_ALL, so a tool that forgot would read an owner's
// start time out of a Chinese date, and the owner checks below would say so.

import { spawn, spawnSync } from "node:child_process";
import {
    existsSync, lstatSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, realpathSync, rmSync,
    statSync, symlinkSync, unlinkSync, utimesSync, writeFileSync,
} from "node:fs";
import { createHash } from "node:crypto";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

let checks = 0;
let failures = 0;
const check = (what, ok, detail = "") => {
    checks += 1;
    if (!ok) failures += 1;
    console.log(`  ${ok ? "✓" : "✗"} ${what}${ok || !detail ? "" : ` — ${detail}`}`);
};

const here = dirname(fileURLToPath(import.meta.url));
const TOOL = join(here, "..", "tools", "scratch.sh");
if (!existsSync(TOOL)) {
    console.log(`  ✗ ${TOOL} does not exist, so there is nothing to test`);
    process.exit(1);
}

// Cleanup runs from an exit handler, because `stop()` exits outright and would skip a `finally`.
const lingering = new Set();
let sandbox = null;
process.on("exit", () => {
    for (const pid of lingering) {
        try { process.kill(pid, "SIGKILL"); } catch { /* already gone */ }
    }
    if (sandbox) {
        try { rmSync(sandbox, { recursive: true, force: true }); } catch { /* already gone */ }
    }
});
const stop = (why) => {
    console.log(`  ✗ ${why}`);
    console.log(`scratch tool: stopped after ${checks} checks — ${why}`);
    process.exit(1);
};

sandbox = realpathSync(mkdtempSync(join(tmpdir(), "clawdline-scratch-tool-")));
const home = join(sandbox, "home");
mkdirSync(home);
const gitconfig = join(sandbox, "gitconfig");
writeFileSync(gitconfig, "");
const root = join(sandbox, "scratch");
const repo = join(sandbox, "repo");

const env = (extra = {}) => ({
    PATH: process.env.PATH,
    HOME: home,
    TMPDIR: sandbox,
    LANG: "zh_TW.UTF-8",
    GIT_CONFIG_GLOBAL: gitconfig,
    GIT_CONFIG_SYSTEM: "/dev/null",
    GIT_CEILING_DIRECTORIES: dirname(sandbox),
    GIT_AUTHOR_NAME: "scratch test",
    GIT_AUTHOR_EMAIL: "scratch@test.invalid",
    GIT_COMMITTER_NAME: "scratch test",
    GIT_COMMITTER_EMAIL: "scratch@test.invalid",
    ...extra,
});

const within = (path) => {
    const resolved = resolve(path);
    return resolved === sandbox || resolved.startsWith(`${sandbox}/`);
};
// The only door to the tool. A root or working directory outside the sandbox is a bug in this file,
// not a result, so it throws instead of running anything.
let refusedOutside = 0;
const toolDoor = (scratchRoot, cwd, foreign = false) => {
    if (!foreign && !within(scratchRoot)) {
        refusedOutside += 1;
        throw new Error(`refusing to point tools/scratch.sh at ${scratchRoot}: outside the sandbox`);
    }
    if (!within(cwd)) {
        refusedOutside += 1;
        throw new Error(`refusing to run tools/scratch.sh in ${cwd}: outside the sandbox`);
    }
};
const runTool = (args, { scratchRoot = root, cwd = repo, input, foreign = false } = {}) => {
    toolDoor(scratchRoot, cwd, foreign);
    return spawnSync("/bin/bash", [TOOL, ...args], {
        cwd,
        input,
        encoding: "utf8",
        timeout: 60_000,
        env: env({ CLAWDLINE_SCRATCH_ROOT: scratchRoot }),
    });
};
const git = (cwd, args) => {
    if (!within(cwd)) stop(`refusing to run git in ${cwd}: outside the sandbox`);
    const result = spawnSync("git", args, { cwd, encoding: "utf8", env: env() });
    if (result.status !== 0) stop(`git ${args.join(" ")} failed in ${cwd}: ${result.stderr}`);
    return result.stdout;
};
const entries = (dir = root) => (existsSync(dir) ? readdirSync(dir) : []);
const leavesNothing = (what) => {
    const left = entries();
    check(what, left.length === 0, `left an entry behind: ${left.join(", ")}`);
};
const readMarker = (entry) => {
    try { return JSON.parse(readFileSync(join(entry, ".clawdline-scratch.json"), "utf8")); } catch { return null; }
};
const said = (result) => `status ${result.status}, signal ${result.signal}, stderr: ${(result.stderr || "").trim()}`;
const nap = (ms) => new Promise((wake) => setTimeout(wake, ms));
const waitFor = async (ready, ms) => {
    const deadline = Date.now() + ms;
    while (Date.now() < deadline) {
        if (ready()) return true;
        await nap(50);
    }
    return ready();
};
const running = (pid) => {
    try { process.kill(pid, 0); return true; } catch (error) { return error.code === "EPERM"; }
};

// ---- A repository in every state the worktree subject has to carry ------------------------------

mkdirSync(repo);
git(repo, ["init", "-q"]);
writeFileSync(join(repo, "a.txt"), "base\n");
writeFileSync(join(repo, "gone.txt"), "gone\n");
writeFileSync(join(repo, "stat.txt"), "same\n");
writeFileSync(join(repo, "bin.dat"), Buffer.from([0, 1, 2, 255, 0]));
git(repo, ["add", "a.txt", "gone.txt", "stat.txt", "bin.dat"]);
git(repo, ["commit", "-qm", "base"]);
writeFileSync(join(repo, "a.txt"), "edited\n");
rmSync(join(repo, "gone.txt"));
writeFileSync(join(repo, "bin.dat"), Buffer.from([0, 9, 2, 255, 0]));
writeFileSync(join(repo, "staged.txt"), "staged\n");
git(repo, ["add", "staged.txt"]);
writeFileSync(join(repo, "new.txt"), "untracked\n");
// The same bytes with a new mtime: the index's stat cache for this file is now stale, which is what
// makes a plain `git diff HEAD` write back the index it read. Last, so no git command above refreshes it.
const past = new Date("2020-01-01T00:00:00Z");
utimesSync(join(repo, "stat.txt"), past, past);

const indexDigest = () => createHash("sha256").update(readFileSync(join(repo, ".git", "index"))).digest("hex");
const objectCount = () => {
    let count = 0;
    const walk = (dir) => {
        for (const entry of readdirSync(dir, { withFileTypes: true })) {
            if (entry.isDirectory()) walk(join(dir, entry.name));
            else count += 1;
        }
    };
    walk(join(repo, ".git", "objects"));
    return count;
};

const probe = [
    'printf "a=%s\\n" "$(cat a.txt)"',
    '[ -e gone.txt ] && echo gone=present || echo gone=absent',
    'printf "staged=%s\\n" "$(cat staged.txt 2>/dev/null || echo missing)"',
    'printf "new=%s\\n" "$(cat new.txt 2>/dev/null || echo missing)"',
    'printf "bin=%s\\n" "$(od -An -tx1 bin.dat | tr -d " \\n")"',
    'printf "tracked=%s\\n" "$(git ls-files | wc -l | tr -d " ")"',
    'printf "tmpdir=%s\\n" "$TMPDIR"',
    'printf "cwd=%s\\n" "$(pwd -P)"',
].join("; ");
const parse = (text) => Object.fromEntries(text.split("\n").filter((line) => line.includes("="))
    .map((line) => [line.slice(0, line.indexOf("=")), line.slice(line.indexOf("=") + 1)]));
const escape = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

// ---- The harness can refuse -----------------------------------------------------------------------

let refused = false;
try {
    runTool(["new", "probe", "--ttl-hours", "1"], { scratchRoot: "/tmp/clawdline-scratch" });
} catch {
    refused = true;
}
check("the harness refuses to point the tool at this machine's real owned root", refused);

// ---- snapshot-run: success, and what the copy is ------------------------------------------------

console.log("snapshot-run");
const indexBefore = indexDigest();
const objectsBefore = objectCount();
const worktree = runTool(["snapshot-run", "--subject", "worktree", "--", "/bin/sh", "-c", probe]);
const seen = parse(worktree.stdout);
check("a worktree run whose command succeeds exits 0", worktree.status === 0, said(worktree));
check("the copy carries the unstaged edit, the deletion and the binary change",
    seen.a === "edited" && seen.gone === "absent" && seen.bin === "000902ff00", JSON.stringify(seen));
check("the copy carries the staged new file and the untracked one",
    seen.staged === "staged" && seen.new === "untracked", JSON.stringify(seen));
check("the copy is a repository with all five files staged, so a version scan has files to read",
    seen.tracked === "5", JSON.stringify(seen));
const rootReal = existsSync(root) ? realpathSync(root) : root;
const entryPattern = `^${escape(rootReal)}/snapshot-worktree\\.[A-Za-z0-9_]+`;
check("the command runs at the top of the copy, with TMPDIR inside the same entry",
    new RegExp(`${entryPattern}/tree$`).test(seen.cwd || "") && new RegExp(`${entryPattern}/tmp$`).test(seen.tmpdir || ""),
    JSON.stringify(seen));
check("the owned root is created mode 0700", existsSync(root) && (statSync(root).mode & 0o777) === 0o700);
leavesNothing("success leaves nothing under the root");
check("the worktree subject did not write the shared index", indexDigest() === indexBefore);
check("the worktree subject created no object in the shared .git", objectCount() === objectsBefore);
// The control, on the same repository at the same moment: a plain `git diff HEAD` does rewrite this
// index, so the two checks above were able to fail.
git(repo, ["diff", "--binary", "--full-index", "--no-ext-diff", "HEAD"]);
check("control: a plain git diff HEAD in this repository rewrites its index", indexDigest() !== indexBefore);

const index = runTool(["snapshot-run", "--subject", "index", "--", "/bin/sh", "-c", probe]);
const staged = parse(index.stdout);
check("an index run exits 0", index.status === 0, said(index));
check("the index subject is what is staged, and nothing that is not",
    staged.a === "base" && staged.gone === "present" && staged.staged === "staged" && staged.new === "missing",
    JSON.stringify(staged));
leavesNothing("an index run leaves nothing");

const piped = runTool(["snapshot-run", "--subject", "worktree", "--", "cat"], { input: "through standard input\n" });
check("standard input reaches the command", piped.stdout === "through standard input\n", JSON.stringify(piped.stdout));
leavesNothing("a run reading standard input leaves nothing");

// ---- Failure --------------------------------------------------------------------------------------

console.log("failure");
for (const status of [75, 3]) {
    const failed = runTool(["snapshot-run", "--subject", "worktree", "--", "/bin/sh", "-c", `exit ${status}`]);
    check(`a command that exits ${status} makes the run exit ${status}`, failed.status === status, said(failed));
    leavesNothing(`a command that exits ${status} leaves nothing`);
}

const broken = join(sandbox, "broken-repo");
mkdirSync(broken);
git(broken, ["init", "-q"]);
writeFileSync(join(broken, "f.txt"), "f\n");
git(broken, ["add", "f.txt"]);
git(broken, ["commit", "-qm", "f"]);
const blob = git(broken, ["rev-parse", "HEAD:f.txt"]).trim();
rmSync(join(broken, ".git", "objects", blob.slice(0, 2), blob.slice(2)));
const corrupt = runTool(["snapshot-run", "--subject", "worktree", "--", "true"], { cwd: broken });
check("a copy that cannot be made is refused as scratch_snapshot_failed, and the command never runs",
    corrupt.status === 70 && corrupt.stderr.includes("scratch_snapshot_failed"), said(corrupt));
leavesNothing("a failure while copying leaves nothing");

const notRepo = join(sandbox, "not-a-repo");
mkdirSync(notRepo);
const outsideGit = runTool(["snapshot-run", "--subject", "worktree", "--", "true"], { cwd: notRepo });
check("outside a repository the run is refused as scratch_not_in_git",
    outsideGit.status === 70 && outsideGit.stderr.includes("scratch_not_in_git"), said(outsideGit));

// ---- Signals --------------------------------------------------------------------------------------

console.log("signals");
const signalled = (signal) => new Promise((done) => {
    toolDoor(root, repo);
    const spawnedAt = Math.floor(Date.now() / 1000);
    const child = spawn("/bin/bash", [TOOL, "snapshot-run", "--subject", "worktree", "--",
        "/bin/sh", "-c", 'echo "command=$$"; exec sleep 60'], {
        cwd: repo, env: env({ CLAWDLINE_SCRATCH_ROOT: root }), stdio: ["ignore", "pipe", "pipe"],
    });
    lingering.add(child.pid);
    let out = "";
    let commandPid = null;
    let marker = null;
    const timer = setTimeout(() => { try { child.kill("SIGKILL"); } catch { /* gone */ } }, 30_000);
    child.stdout.on("data", (chunk) => {
        out += chunk;
        const match = /command=(\d+)/.exec(out);
        if (match && commandPid === null) {
            commandPid = Number(match[1]);
            lingering.add(commandPid);
            const [name] = entries();
            marker = name ? readMarker(join(root, name)) : null;
            child.kill(signal);
        }
    });
    child.stderr.resume();
    child.on("exit", (code, sig) => {
        clearTimeout(timer);
        lingering.delete(child.pid);
        done({ code, sig, commandPid, marker, spawnedAt, pid: child.pid });
    });
});
for (const [signal, number] of [["SIGINT", 2], ["SIGTERM", 15], ["SIGHUP", 1]]) {
    const run = await signalled(signal);
    check(`${signal}: the command was running when it arrived`, run.commandPid !== null, "the command never started");
    check(`${signal}: while it ran, the marker named the tool process as owner, started when it was spawned`,
        run.marker?.owner?.pid === run.pid && Math.abs(run.marker.owner.process_start - run.spawnedAt) <= 2
            && run.marker.keep_until === null,
        JSON.stringify(run.marker));
    check(`${signal} ends the run by that same signal`, run.sig === signal || run.code === 128 + number,
        `code ${run.code}, signal ${run.sig}`);
    leavesNothing(`${signal} leaves nothing`);
    const still = run.commandPid !== null && await waitFor(() => !running(run.commandPid), 2_000) === false;
    check(`${signal} does not leave the command running`, !still, `pid ${run.commandPid} is still running`);
    if (!still && run.commandPid !== null) lingering.delete(run.commandPid);
}

// ---- --keep, new, and the marker ------------------------------------------------------------------

console.log("kept entries");
const contractKeys = ["clawdline_scratch", "created_at", "purpose", "owner", "keep_until"];
const markerShape = (marker, purpose) => marker !== null
    && JSON.stringify(Object.keys(marker)) === JSON.stringify(contractKeys)
    && marker.clawdline_scratch === 1 && marker.purpose === purpose
    && Number.isInteger(marker.created_at) && marker.created_at <= Math.floor(Date.now() / 1000)
    && (marker.owner === null || (Number.isInteger(marker.owner.pid) && Number.isInteger(marker.owner.process_start)
        && typeof marker.owner.command === "string"));
const modeOf = (path) => (existsSync(path) ? statSync(path).mode & 0o777 : null);

const kept = runTool(["snapshot-run", "--subject", "worktree", "--keep", "--ttl-hours", "2", "--", "true"]);
const keptNames = entries();
const keptPath = keptNames.length === 1 ? join(rootReal, keptNames[0]) : null;
const keptMarker = keptPath ? readMarker(keptPath) : null;
const keptAt = Math.floor(Date.now() / 1000);
check("--keep exits with the command's status", kept.status === 0, said(kept));
check("--keep leaves exactly one entry, named <purpose>.<random>, mode 0700",
    keptPath !== null && /^snapshot-worktree\.[A-Za-z0-9_]+$/.test(keptNames[0]) && modeOf(keptPath) === 0o700,
    `entries: ${keptNames.join(", ")}`);
check("--keep prints the kept path", keptPath !== null && kept.stderr.includes(keptPath), kept.stderr.trim());
check("the kept marker is version 1 with exactly the contract's keys", markerShape(keptMarker, "snapshot-worktree"),
    JSON.stringify(keptMarker));
check("the kept marker records keep_until --ttl-hours from now",
    Number.isInteger(keptMarker?.keep_until) && Math.abs(keptMarker.keep_until - (keptAt + 7200)) <= 60,
    JSON.stringify(keptMarker));
check("a kept entry is owned by the assistant above the run, or by nobody",
    keptMarker !== null && (keptMarker.owner === null || ["claude", "codex"].includes(keptMarker.owner.command)),
    JSON.stringify(keptMarker));
if (keptPath !== null) {
    const removed = runTool(["remove", keptPath]);
    check("remove takes a kept entry away", removed.status === 0, said(removed));
}
leavesNothing("nothing is left once the kept entry is removed");

const badPurpose = runTool(["new", "Not_A_Slug", "--ttl-hours", "1"]);
check("new refuses a purpose that is not a slug, and makes nothing",
    badPurpose.status === 64 && badPurpose.stderr.includes("scratch_usage") && entries().length === 0, said(badPurpose));
const badTtl = runTool(["new", "deploy", "--ttl-hours", "25"]);
check("new refuses a time to live outside 1-24 hours", badTtl.status === 64 && entries().length === 0, said(badTtl));

const deploy = runTool(["new", "deploy", "--ttl-hours", "3"]);
const deployPath = deploy.stdout.trim();
const deployMarker = readMarker(deployPath);
check("new prints the path of a mode-0700 entry directly under the root",
    deploy.status === 0 && dirname(deployPath) === rootReal && modeOf(deployPath) === 0o700, said(deploy));
check("its marker is version 1 with keep_until three hours out",
    markerShape(deployMarker, "deploy") && Math.abs(deployMarker.keep_until - (Math.floor(Date.now() / 1000) + 10800)) <= 60,
    JSON.stringify(deployMarker));
const deployRemoved = runTool(["remove", deployPath]);
check("remove takes it away", deployRemoved.status === 0 && !existsSync(deployPath), said(deployRemoved));

// An owner is the nearest process started as `claude` or `codex`. Starting a shell under that name
// makes one without depending on which assistant, if any, is running this suite.
toolDoor(root, repo);
const owned = spawnSync("/bin/bash", ["-c", '/bin/bash "$1" new creds; exit $?', "claude", TOOL], {
    argv0: "claude", cwd: repo, encoding: "utf8", env: env({ CLAWDLINE_SCRATCH_ROOT: root }),
});
const credsPath = owned.stdout.trim();
const credsMarker = readMarker(credsPath);
check("under a claude process, new needs no time to live and records that process as the owner",
    owned.status === 0 && markerShape(credsMarker, "creds") && credsMarker.owner?.pid === owned.pid
        && credsMarker.owner?.command === "claude" && credsMarker.keep_until === null,
    `${said(owned)} marker ${JSON.stringify(credsMarker)}`);
if (existsSync(credsPath)) runTool(["remove", credsPath]);

// With no assistant anywhere above it — a process whose parent has exited and been reparented to
// launchd — nothing can own an entry, and a time to live is mandatory.
const orphan = async (label, extra) => {
    const state = join(sandbox, `orphan-${label}`);
    toolDoor(root, repo);
    spawnSync("/bin/sh", ["-c", `/bin/sh -c '
        i=0
        while [ "$(ps -o ppid= -p $$ | tr -d " ")" != 1 ] && [ $i -lt 400 ]; do sleep 0.05; i=$((i+1)); done
        /bin/bash "$1" new orphan ${extra} > "$2.out" 2> "$2.err"
        echo $? > "$2.rc"
    ' orphan "$1" "$2" &`, "outer", TOOL, state], {
        cwd: repo, env: env({ CLAWDLINE_SCRATCH_ROOT: root }), stdio: "ignore",
    });
    const finished = await waitFor(() => existsSync(`${state}.rc`) && readFileSync(`${state}.rc`, "utf8").endsWith("\n"), 30_000);
    const read = (suffix) => (existsSync(`${state}.${suffix}`) ? readFileSync(`${state}.${suffix}`, "utf8").trim() : "");
    return { finished, rc: read("rc"), out: read("out"), err: read("err") };
};
const unowned = await orphan("bare", "");
check("with no assistant above it, new without --ttl-hours is refused as scratch_ttl_required and makes nothing",
    unowned.finished && unowned.rc === "64" && unowned.err.includes("scratch_ttl_required") && entries().length === 0,
    JSON.stringify(unowned));
const timed = await orphan("timed", "--ttl-hours 1");
const timedMarker = readMarker(timed.out);
check("with no assistant above it and --ttl-hours 1, the owner is null and keep_until an hour out",
    timed.finished && timed.rc === "0" && markerShape(timedMarker, "orphan") && timedMarker.owner === null
        && Math.abs(timedMarker.keep_until - (Math.floor(Date.now() / 1000) + 3600)) <= 60,
    `${JSON.stringify(timed)} marker ${JSON.stringify(timedMarker)}`);
if (existsSync(timed.out)) runTool(["remove", timed.out]);

// ---- Roots that are refused -----------------------------------------------------------------------

console.log("refused roots");
const realRoot = join(sandbox, "real-root");
mkdirSync(realRoot);
const linkedRoot = join(sandbox, "linked-root");
symlinkSync(realRoot, linkedRoot);
const viaLink = runTool(["snapshot-run", "--subject", "worktree", "--", "true"], { scratchRoot: linkedRoot });
check("a root that is a symbolic link is refused as scratch_root_symlink",
    viaLink.status === 73 && viaLink.stderr.includes("scratch_root_symlink"), said(viaLink));
check("and nothing was made where the link points", readdirSync(realRoot).length === 0);

const fileRoot = join(sandbox, "file-root");
writeFileSync(fileRoot, "");
const onFile = runTool(["snapshot-run", "--subject", "worktree", "--", "true"], { scratchRoot: fileRoot });
check("a root that is not a directory is refused as scratch_root_not_directory",
    onFile.status === 73 && onFile.stderr.includes("scratch_root_not_directory"), said(onFile));

const foreign = "/usr";
const foreignStat = lstatSync(foreign);
if (!foreignStat.isDirectory() || foreignStat.isSymbolicLink() || foreignStat.uid === process.getuid()) {
    check("a root owned by somebody else can be put in front of the tool", false,
        `${foreign} is not a real directory owned by another uid here, so this case cannot be judged`);
} else {
    const notMine = runTool(["snapshot-run", "--subject", "worktree", "--", "true"], { scratchRoot: foreign, foreign: true });
    check("a root owned by somebody else is refused as scratch_root_not_owned",
        notMine.status === 73 && notMine.stderr.includes("scratch_root_not_owned"), said(notMine));
}

// ---- remove refuses what is not an owned entry under the root -------------------------------------

console.log("remove");
const goodMarker = `{"clawdline_scratch":1,"created_at":${Math.floor(Date.now() / 1000)},"purpose":"deploy","owner":null,"keep_until":null}\n`;
const elsewhere = join(sandbox, "elsewhere", "deploy.abcdEFGH");
mkdirSync(elsewhere, { recursive: true, mode: 0o700 });
writeFileSync(join(elsewhere, ".clawdline-scratch.json"), goodMarker);
const outside = runTool(["remove", elsewhere]);
check("remove refuses a well-formed entry that is not under the root",
    outside.status === 77 && outside.stderr.includes("scratch_not_under_root") && existsSync(elsewhere), said(outside));

const bare = join(root, "bare.abcdEFGH");
mkdirSync(bare, { mode: 0o700 });
const noMarker = runTool(["remove", bare]);
check("remove refuses an entry with no marker, and leaves it",
    noMarker.status === 77 && noMarker.stderr.includes("scratch_marker_missing") && existsSync(bare), said(noMarker));
rmSync(bare, { recursive: true, force: true });

const future = join(root, "future.abcdEFGH");
mkdirSync(future, { mode: 0o700 });
writeFileSync(join(future, ".clawdline-scratch.json"), goodMarker.replace('"clawdline_scratch":1', '"clawdline_scratch":2'));
const otherVersion = runTool(["remove", future]);
check("remove refuses an entry whose marker is another version, and leaves it",
    otherVersion.status === 77 && otherVersion.stderr.includes("scratch_marker_unknown") && existsSync(future), said(otherVersion));
rmSync(future, { recursive: true, force: true });

const victim = join(sandbox, "victim");
mkdirSync(victim);
writeFileSync(join(victim, "keep.txt"), "not scratch\n");
const disguised = join(root, "deploy.linkEFGH");
symlinkSync(victim, disguised);
const throughLink = runTool(["remove", disguised]);
check("remove refuses a link named like an entry, and what it points at is untouched",
    throughLink.status === 77 && throughLink.stderr.includes("scratch_not_an_entry") && existsSync(join(victim, "keep.txt")),
    said(throughLink));
unlinkSync(disguised);

// An entry whose owner is still running and is not above the caller belongs to another session.
toolDoor(root, repo);
const holdOut = join(sandbox, "held.out");
const holder = spawn("/bin/bash", ["-c", '/bin/bash "$1" new held > "$2"; exec sleep 60', "claude", TOOL, holdOut], {
    argv0: "claude", cwd: repo, env: env({ CLAWDLINE_SCRATCH_ROOT: root }), stdio: "ignore",
});
lingering.add(holder.pid);
const holderExited = new Promise((done) => holder.on("exit", done));
const heldReady = await waitFor(() => existsSync(holdOut) && readFileSync(holdOut, "utf8").endsWith("\n"), 15_000);
const heldPath = heldReady ? readFileSync(holdOut, "utf8").trim() : "";
const whileHeld = heldReady ? runTool(["remove", heldPath]) : null;
check("remove refuses an entry whose owner is running and is not above the caller",
    whileHeld !== null && whileHeld.status === 77 && whileHeld.stderr.includes("scratch_owner_live") && existsSync(heldPath),
    whileHeld ? said(whileHeld) : "the holder never made its entry");
holder.kill("SIGKILL");
await holderExited;
lingering.delete(holder.pid);
const afterHolder = heldReady ? runTool(["remove", heldPath]) : null;
check("once that owner has exited, remove takes the entry away",
    afterHolder !== null && afterHolder.status === 0 && !existsSync(heldPath), afterHolder ? said(afterHolder) : "no entry");
leavesNothing("nothing is left under the root at the end");

// ---- The shape of the tool itself -----------------------------------------------------------------

console.log("the tool");
const toolText = readFileSync(TOOL, "utf8");
const rmLines = toolText.split("\n").filter((line) => !/^\s*#/.test(line) && /(^|[\s;&|({])rm\s+-/.test(line));
const unguarded = rmLines.filter((line) => /\$(?!\{[A-Za-z_][A-Za-z0-9_]*:\?)/.test(line.slice(line.search(/rm\s+-/))));
check(`every rm in the tool expands nothing but \${var:?} (${rmLines.length} found)`,
    rmLines.length > 0 && unguarded.length === 0, unguarded.join(" | "));
const help = runTool(["--help"]);
check("--help says the index subject is for a root only",
    help.status === 0 && /--subject index[\s\S]*ROOT ONLY/.test(help.stdout), said(help));
check("the one refusal made for being outside the sandbox was the deliberate one", refusedOutside === 1,
    `${refusedOutside} refusals`);

if (failures > 0) {
    console.log(`scratch tool: ${failures} of ${checks} checks failed`);
    process.exit(1);
}
console.log(`scratch tool: ${checks} checks passed`);
