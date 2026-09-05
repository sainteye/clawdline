// `Resources/clawdline-progress.sh` is the traps a long run needs, written once so that nothing
// else has to rediscover them.
//
// **Why this file is a suite and not a paragraph in the documentation.** In the round that built
// the producer, a dedicated agent with a full brief got the traps wrong on its first attempt, and
// so did a second agent independently, and both had to measure their way out on this machine's
// bash 3.2.57. Three shapes, all of which report a killed run as a clean one:
//
//   * `EXIT` alone, because `$?` inside that handler is 0 when the script was killed.
//   * `ERR` alone, because `set -e` does not fire it for a failure inside a function, and no ERR
//     trap ever sees a deliberate `exit 1`.
//   * A handler that returns instead of exiting, which lets a `TERM`ed script carry on from where
//     it was interrupted and finish by declaring success.
//
// Every one of those has a positive control at the bottom of this file: the helper is copied,
// the line is taken out of the copy, and the copy is driven to show the defect happening. A check
// nobody has seen fail is a claim with nothing behind it.
//
// **Nothing here compiles anything, runs `./test.sh` or `./build.sh`, or touches the real
// `~/.claude/statusline-cache`.** Every harness runs in a directory this file made with
// `CLAWDLINE_STATUS_DIR` pointed inside it, and the last check is that the real cache directory was
// left exactly as it was found.

import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, chmodSync, existsSync, readdirSync,
         realpathSync, rmSync, statSync } from "node:fs";
import { tmpdir, homedir } from "node:os";
import { join } from "node:path";
// `fileURLToPath`, not `URL.pathname`: this checkout lives under `Application Support`, and a
// percent-encoded space is a path bash cannot open — it answers 127 and the check goes red for a
// reason that has nothing to do with the helper.
import { fileURLToPath } from "node:url";

let failures = 0;
let checks = 0;
const check = (what, ok) => {
    checks += 1;
    console.log(`  ${ok ? "✓" : "✗"} ${what}`);
    if (!ok) failures += 1;
};
// A guard that cannot find what it guards says so and stops, rather than reporting a clean scan of
// nothing. The count on the way out is the other half of that: it is what lets the next reader tell
// "clean" from "never looked".
const stop = (why) => {
    console.log(`  ✗ ${why}`);
    console.log(`progress helper: stopped after ${checks + 1} checks — ${why}`);
    process.exit(1);
};

const helperPath = fileURLToPath(new URL("../Resources/clawdline-progress.sh", import.meta.url));
if (!existsSync(helperPath)) stop(`there is no ${helperPath} to test`);
const helper = readFileSync(helperPath, "utf8");

check("the helper is executable, so `clawdline-progress run …` works without naming an interpreter",
      (statSync(helperPath).mode & 0o111) !== 0);
// bash, not sh: `trap … ERR` is not POSIX, and the whole point of this file is the traps.
check("and it is a bash script, because ERR traps are not POSIX",
      /^#!\/bin\/bash$/m.test(helper.split("\n")[0]));

// ---------------------------------------------------------------------------------------------
// What the helper promises about itself, read off the file rather than off this suite's memory of
// it. Each predicate is a function of the text, so the mutation controls at the bottom can ask the
// same question of a copy that has had the line taken out.

const armsEveryTrap = (text) => /^\s*trap 'clawdline_run_file_signal "\$\?"' ERR$/m.test(text)
    && /^\s*trap 'clawdline_run_file_signal 130' INT$/m.test(text)
    && /^\s*trap 'clawdline_run_file_signal 143' TERM$/m.test(text)
    && /^\s*trap 'clawdline_run_file_exit "\$\?"' EXIT$/m.test(text);
check("progress_start arms all four traps: ERR, INT, TERM and EXIT", armsEveryTrap(helper));

const handlerExits = (text) => {
    const body = /clawdline_run_file_signal\(\) \{([\s\S]*?)\n\}/.exec(text);
    return body !== null && /clawdline_run_file_finish fail/.test(body[1]) && /\n\s*exit "\$status"/.test(body[1]);
};
check("the signal handler writes fail and then exits, rather than returning into the script",
      handlerExits(helper));

const writesThroughARename = (text) => /temp="\$CLAWDLINE_RUN_FILE\.\$\$\.tmp"/.test(text)
    && /mv -f "\$temp" "\$CLAWDLINE_RUN_FILE"/.test(text)
    && !/^\s*\}\s*>\s*"\$CLAWDLINE_RUN_FILE"/m.test(text);
check("the file is written to a temporary name in the same directory and renamed into place",
      writesThroughARename(helper));

const code = helper.split("\n").filter((l) => !/^\s*#/.test(l)).join("\n");
// Keyed by working directory, and **nothing truncated**. A truncating key was taken out of all
// three implementations of this rule on 2026-09-05 because `[-48:]` is lossy; the check that it
// stayed out is driven further down, on a path that is longer than that.
const keyedByWorkingDirectory = (text) => /run-\$\(printf '%s' "\$PWD" \| tr '\/' '-'\)\.json/.test(text)
    && !/git (config|remote|rev-parse --show-toplevel)/.test(text);
// Asked of the code and not of the whole file, because the comment above the line says the words
// "git remote" in the course of explaining why it is not one.
check("the key is $PWD with every / turned into a -, and no part of it comes from a git remote",
      keyedByWorkingDirectory(code));
check("the directory is CLAWDLINE_STATUS_DIR, falling back to the shared statusline cache",
      /CLAWDLINE_RUN_DIR="\$\{CLAWDLINE_STATUS_DIR:-\$\{HOME:-\}\/\.claude\/statusline-cache\}"/.test(helper));
// The contract says so in as many words: `ghrun-` needs a `producer` because two writers compete
// for it, and this record has one writer and a staleness ceiling instead.
check("nothing in the helper writes a `producer` field", !/"producer"/.test(helper));

// ---------------------------------------------------------------------------------------------
// Running it.

// `realpathSync` because `/var` is a symlink to `/private/var` on this Mac: a harness started in
// the first reports the second as `$PWD`, and a key built from the path node handed out would look
// for a file nothing ever wrote.
const scratch = realpathSync(mkdtempSync(join(tmpdir(), "clawdline-progress-")));
const cache = join(scratch, "statusline-cache");
const realCache = join(homedir(), ".claude", "statusline-cache");
// The containment assertion at the end names **this suite's own keys**, not the directory's
// contents. Diffing the whole listing before and after looks stricter and is in fact unusable
// here: `~/.claude/statusline-cache` is machine-wide, several sessions run `./test.sh` on this Mac
// at once, and each of those legitimately writes its own `run-` file mid-flight. Measured on
// 2026-09-05 — this check went red while another session was in its node-suite phase, and nothing
// this file did had touched the real directory. A check that goes red for somebody else's correct
// behaviour teaches people to ignore it, which is worse than not having it.

const keyFor = (dir) => `run-${dir.replace(/\//g, "-")}.json`;
const rowOf = (path) => {
    if (!existsSync(path)) return null;
    try {
        return JSON.parse(readFileSync(path, "utf8"));
    } catch (e) {
        return { unparseable: String(e) };
    }
};

// A copy of the helper with one line taken out, for the controls. Every mutation is asserted to
// have changed something: a `.replace` that matched nothing silently tests the original again,
// which is the shape of a control that proves the opposite of what it claims.
const mutantHelper = (name, transform) => {
    const dir = join(scratch, `mutant-${name}`);
    mkdirSync(dir, { recursive: true });
    const path = join(dir, "clawdline-progress.sh");
    const text = transform(helper);
    if (text === helper) stop(`the ${name} mutation changed nothing, so its control proves nothing`);
    writeFileSync(path, text);
    chmodSync(path, 0o755);
    return { path, text };
};

// A working directory of its own for every scenario, so one run's row can never be read as
// another's — the key is the directory and nothing else.
const workdir = (name) => {
    const dir = join(scratch, name);
    mkdirSync(dir, { recursive: true });
    return { dir, file: join(cache, keyFor(dir)) };
};

const runHelper = (w, args, env = {}, path = helperPath) => {
    const r = spawnSync("/bin/bash", [path, ...args], {
        encoding: "utf8",
        cwd: w.dir,
        env: { ...process.env, CLAWDLINE_STATUS_DIR: cache, ...env },
    });
    return { code: r.status, out: r.stdout ?? "", err: r.stderr ?? "" };
};

// A script that sources the helper, for the second form.
const sourced = (w, body, path = helperPath, name = "run.sh") => {
    const script = join(w.dir, name);
    writeFileSync(script, ["#!/bin/bash", "set -euo pipefail",
                           `. ${JSON.stringify(path)}`, body, ""].join("\n"));
    chmodSync(script, 0o755);
    return script;
};
const runScript = (w, script, env = {}) => {
    const r = spawnSync("/bin/bash", [script], {
        encoding: "utf8",
        cwd: w.dir,
        env: { ...process.env, CLAWDLINE_STATUS_DIR: cache, ...env },
    });
    return { code: r.status, out: r.stdout ?? "", err: r.stderr ?? "" };
};

// **`set -m` in the driver, and it is load-bearing.** Without job control a background job of a
// non-interactive shell starts with `SIGINT` ignored, and a signal ignored on entry cannot be
// trapped — so an INT scenario written the obvious way measures bash's job control rather than the
// helper's handler. With it the job gets its own process group and the default disposition, and the
// signal goes to the group, which is what a person pressing ⌃C at a terminal actually sends.
const driveAndKill = (w, argv, signal, target = "group", settle = "0.3") => {
    const driver = join(w.dir, `drive-${signal}.sh`);
    writeFileSync(driver, [
        "#!/bin/bash",
        "set -m",
        `${argv.map((a) => JSON.stringify(a)).join(" ")} &`,
        "p=$!",
        `for _ in $(seq 1 200); do [ -f ${JSON.stringify(w.file)} ] && break; sleep 0.05; done`,
        `sleep ${settle}`,
        target === "group" ? `kill -${signal} -"$p" 2>/dev/null || true`
                           : `kill -${signal} "$p" 2>/dev/null || true`,
        'wait "$p"',
        'echo "status=$?"',
        "",
    ].join("\n"));
    chmodSync(driver, 0o755);
    const r = spawnSync("/bin/bash", [driver], {
        encoding: "utf8", cwd: w.dir,
        env: { ...process.env, CLAWDLINE_STATUS_DIR: cache },
    });
    return `${r.stdout ?? ""}${r.stderr ?? ""}`;
};

// A command that takes a while and says so if it is ever allowed to finish. **A loop of short
// sleeps rather than one long one**: bash runs a trap after the current foreground command
// returns, so a single `sleep 10` would mix "when does bash get round to the handler" into every
// answer below.
const slowCommand = (dir, seconds = 10) => {
    const path = join(dir, "slow.sh");
    writeFileSync(path, ["#!/bin/bash",
                         `for _ in $(seq 1 ${Math.round(seconds * 4)}); do sleep 0.25; done`,
                         'echo "fell through" > "$PWD/fell-through"', ""].join("\n"));
    chmodSync(path, 0o755);
    return path;
};

try {
    // ---------------------------------------------------------------------------------------
    // 1. The wrapper form. `clawdline-progress run … -- <command>` is the one to reach for,
    //    because the helper is the parent process: the status is its own, the signals are its
    //    own, and the caller installs no traps at all.
    {
        const w = workdir("wrapper-ok");
        const r = runHelper(w, ["run", "--label", "lint", "--typical", "120", "--", "/bin/echo", "hello"]);
        const row = rowOf(w.file);
        check("a wrapped command that succeeds leaves state ok, and the wrapper exits 0",
              r.code === 0 && row !== null && row.state === "ok" && r.out.includes("hello"));
        check("the label is the one that was asked for, drawn verbatim",
              row !== null && row.label === "lint");
        check("--typical is written through as typical_seconds",
              row !== null && row.typical_seconds === 120);
        check("the finished row carries no phase, because there is no longer one to draw",
              row !== null && row.phase === undefined);
        check("it names the tree it ran in and the session that started it",
              row !== null && row.tree === w.dir && typeof row.holder === "string" && row.holder.length > 0);
        check("started_at and updated_at are epoch seconds, not a rendered date",
              row !== null && Number.isInteger(row.started_at) && Number.isInteger(row.updated_at));
        check("and nothing was left beside it — the temporary name was renamed, not copied",
              readdirSync(cache).filter((n) => n.includes(".tmp")).length === 0);
    }
    {
        const w = workdir("wrapper-fail");
        const r = runHelper(w, ["run", "--label", "import", "--", "/bin/bash", "-c", "exit 3"]);
        check("a wrapped command that fails leaves state fail",
              (rowOf(w.file) || {}).state === "fail");
        // The status is the command's, not a flattened 1. A caller that wraps a step of a pipeline
        // in this needs the number it would have got without the wrapper.
        check("and the wrapper exits on the command's own status, not a flattened 1", r.code === 3);
    }
    {
        // **No `--typical`, no `typical_seconds` — the field is absent rather than invented.**
        // `./build.sh` has never been measured and this is the rule it relies on: to every reader
        // of this record an invented number is indistinguishable from a measured one.
        const w = workdir("wrapper-no-typical");
        runHelper(w, ["run", "--label", "encode", "--", "/bin/echo", "done"]);
        const row = rowOf(w.file);
        check("with no --typical the row carries no typical_seconds at all",
              row !== null && row.typical_seconds === undefined && !readFileSync(w.file, "utf8").includes("typical_seconds"));
        check("and the rest of the row is unaffected by its absence",
              row !== null && row.state === "ok" && row.label === "encode" && row.stale_after === 900);
    }
    {
        // A `--typical` that is not a number would make the row unparseable, and a row that does
        // not parse is drawn as nothing at all — which looks exactly like no run. The reader's rule
        // is that a malformed value is an absent one; the producer follows the same rule.
        const w = workdir("wrapper-bad-typical");
        runHelper(w, ["run", "--typical", "about five minutes", "--", "/bin/echo", "hi"]);
        const row = rowOf(w.file);
        check("a --typical that is not a number is absent rather than unparseable",
              row !== null && row.unparseable === undefined && row.typical_seconds === undefined);
    }
    {
        // The label a wrapped command gets by default is the command's own name. `$0` inside the
        // helper is `clawdline-progress.sh`, and a bar row reading `clawdline-progress` would say
        // nothing at all about what is taking the time.
        const w = workdir("wrapper-default-label");
        const lint = join(w.dir, "lint.sh");
        writeFileSync(lint, "#!/bin/bash\nexit 0\n");
        chmodSync(lint, 0o755);
        runHelper(w, ["run", "--", lint]);
        check("with no --label the row is named after the command, not after the helper",
              (rowOf(w.file) || {}).label === "lint");
    }
    {
        // `--` is how the brief writes it and it is the shape to teach, but a command that does not
        // begin with a dash needs no separator and a person will leave it out.
        const w = workdir("wrapper-no-separator");
        const r = runHelper(w, ["run", "--label", "batch", "/bin/echo", "ok"]);
        check("the -- separator is optional when the command is not itself a flag",
              r.code === 0 && (rowOf(w.file) || {}).label === "batch");
    }
    {
        const w = workdir("wrapper-usage");
        const r = runHelper(w, ["run", "--label", "nothing"]);
        check("run with no command answers 64 and says what is missing, rather than reporting a run",
              r.code === 64 && /needs a command/.test(r.err) && !existsSync(w.file));
        const bad = runHelper(w, ["run", "--typiacl", "9", "--", "/bin/echo", "hi"]);
        check("and a misspelt option is refused rather than silently dropped",
              bad.code === 64 && /unknown option/.test(bad.err));
    }
    {
        // **The row says `running` while the command is running.** Sampled from inside the command
        // itself, which is the only place that can see the row mid-run.
        const w = workdir("wrapper-running");
        const peek = join(w.dir, "peek.sh");
        writeFileSync(peek, ["#!/bin/bash",
                             `cp ${JSON.stringify(w.file)} "$PWD/mid-run.json"`, ""].join("\n"));
        chmodSync(peek, 0o755);
        runHelper(w, ["run", "--label", "deploy", "--", peek]);
        const mid = rowOf(join(w.dir, "mid-run.json"));
        check("the row exists and says running before the wrapped command has finished",
              mid !== null && mid.state === "running" && mid.label === "deploy");
    }

    // ---------------------------------------------------------------------------------------
    // 2. Killed. This is the class of bug the helper exists for, and in the wrapper form it
    //    cannot occur, because the traps are the helper's own and the caller wrote none.
    const killedWrapper = (signal, expected) => {
        const w = workdir(`wrapper-killed-${signal}`);
        const slow = slowCommand(w.dir);
        const out = driveAndKill(w, ["/bin/bash", helperPath, "run", "--label", "suite", "--", slow], signal);
        const row = rowOf(w.file);
        check(`a wrapped command killed with ${signal} leaves state fail`,
              row !== null && row.state === "fail");
        check(`and the wrapper leaves on ${expected}, so its caller can tell it was killed`,
              new RegExp(`status=${expected}`).test(out));
        check(`and nothing carried on past the ${signal}`,
              !existsSync(join(w.dir, "fell-through")));
    };
    killedWrapper("TERM", 143);
    killedWrapper("INT", 130);

    // ---------------------------------------------------------------------------------------
    // 3. The sourced form, which is what a script that wants its own phases uses. `test.sh` and
    //    `build.sh` are the two in this repository; `Tests/run-file-producer.mjs` is where their
    //    own use of it is checked.
    {
        const w = workdir("sourced-phases");
        const script = sourced(w, [
            'snap() { cp "$CLAWDLINE_RUN_FILE" "$PWD/snap-$1.json"; ls -i "$CLAWDLINE_RUN_FILE" | awk \'{ print $1 }\' >> "$PWD/inodes"; }',
            "progress_start --label test --typical 288",
            "progress_phase guards",
            "snap 1",
            "sleep 1.1",
            "progress_phase 'node suites'",
            "snap 2",
            "sleep 1.1",
            "progress_phase compiling",
            "snap 3",
        ].join("\n"));
        const r = runScript(w, script);
        const snaps = [1, 2, 3].map((n) => rowOf(join(w.dir, `snap-${n}.json`)));
        check("every sample taken while the run was going was a whole file that parses",
              r.code === 0 && snaps.every((s) => s !== null && s.unparseable === undefined));
        check("each phase boundary moves the phase",
              snaps.map((s) => (s ? s.phase : "")).join(",") === "guards,node suites,compiling");
        check("and moves updated_at with it",
              snaps[0].updated_at < snaps[1].updated_at && snaps[1].updated_at < snaps[2].updated_at);
        check("while started_at stays where the run started",
              snaps[0].started_at === snaps[1].started_at && snaps[1].started_at === snaps[2].started_at);
        check("every row says running until the run is over, and carries the measurement it was given",
              snaps.every((s) => s.state === "running" && s.typical_seconds === 288));
        check("and the finished row says ok", (rowOf(w.file) || {}).state === "ok");
        const inodes = readFileSync(join(w.dir, "inodes"), "utf8").trim().split("\n");
        check("the file is replaced rather than rewritten: a different inode after every write",
              inodes.length === 3 && new Set(inodes).size === 3);
    }
    {
        // **Sourcing writes nothing.** A script that sourced the helper and found a row already
        // open for it would be reporting a run that had not started — and on a `--help` path that
        // exits three lines later, a run that never will.
        const w = workdir("sourced-quiet");
        const script = sourced(w, 'echo "sourced and did nothing"');
        const r = runScript(w, script);
        check("sourcing the helper defines the functions and writes no row at all",
              r.code === 0 && !existsSync(w.file));
        const named = sourced(w, "declare -F progress_start progress_phase progress_finish progress_clear",
                              helperPath, "names.sh");
        const n = runScript(w, named);
        check("and the four names the sourced form documents are all defined",
              n.code === 0 && ["progress_start", "progress_phase", "progress_finish", "progress_clear"]
                  .every((f) => n.out.includes(f)));
    }
    {
        // The two ways a sourced run ends badly without a signal: a command that fails, which the
        // ERR trap sees, and a deliberate `exit`, which **no ERR trap ever sees**.
        const w = workdir("sourced-fails");
        const script = sourced(w, [
            "progress_start --label test",
            "progress_phase compiling",
            "/usr/bin/false",
            'echo "fell through" > "$PWD/fell-through"',
        ].join("\n"));
        const r = runScript(w, script);
        check("a command that fails leaves state fail and the run's own status",
              (rowOf(w.file) || {}).state === "fail" && r.code === 1);
        check("and the script does not carry on past it",
              !existsSync(join(w.dir, "fell-through")));
    }
    {
        const w = workdir("sourced-exits");
        const script = sourced(w, [
            "progress_start --label test",
            "progress_phase guards",
            "# The shape every guard in test.sh uses: say what is wrong, then exit on a number.",
            'echo "trailing comma before ) — Swift 6.1 syntax" >&2',
            "exit 1",
        ].join("\n"));
        const r = runScript(w, script);
        check("a deliberate exit leaves state fail, which no ERR trap would have seen",
              (rowOf(w.file) || {}).state === "fail" && r.code === 1);
    }
    {
        const w = workdir("sourced-killed");
        const script = sourced(w, [
            "progress_start --label test",
            "progress_phase compiling",
            'for _ in $(seq 1 40); do sleep 0.25; done',
            'echo "fell through" > "$PWD/fell-through"',
        ].join("\n"));
        const out = driveAndKill(w, ["/bin/bash", script], "TERM");
        check("a sourced run killed with TERM leaves state fail and stops where it was",
              (rowOf(w.file) || {}).state === "fail" && !existsSync(join(w.dir, "fell-through"))
                && /status=143/.test(out));
    }
    {
        // A script with an EXIT handler of its own **replaces** the helper's rather than joining
        // it — bash keeps exactly one — so it has to compose the helper's way out into its own.
        // This is the shape `test.sh` and `build.sh` both use, through a `declare -F` guard.
        const w = workdir("sourced-composed");
        const script = sourced(w, [
            "progress_start --label test",
            "cleanup() {",
            "  local status=$?",
            '  echo "cleaned up" > "$PWD/cleaned"',
            '  if declare -F clawdline_run_file_exit >/dev/null 2>&1; then clawdline_run_file_exit "$status" || true; fi',
            '  return "$status"',
            "}",
            "trap cleanup EXIT",
            "progress_phase compiling",
            "exit 125",
        ].join("\n"));
        const r = runScript(w, script);
        check("a script that keeps its own EXIT handler still reports, if it composes the helper's",
              r.code === 125 && (rowOf(w.file) || {}).state === "fail"
                && existsSync(join(w.dir, "cleaned")));
    }
    {
        // The clear: a row from a run that no longer exists can be taken away without editing the
        // file by hand.
        const w = workdir("sourced-clear");
        const script = sourced(w, [
            "progress_start --label test",
            "progress_phase guards",
            'test -f "$CLAWDLINE_RUN_FILE" || { echo "no row was written at all" >&2; exit 1; }',
            "progress_clear",
            "# The finish has to be spent for the EXIT composition to leave the row gone.",
            "CLAWDLINE_RUN_FINISHED=1",
        ].join("\n"));
        const r = runScript(w, script);
        check("progress_clear takes the row away, and a spent finish leaves it gone",
              r.code === 0 && !existsSync(w.file));
    }

    // ---------------------------------------------------------------------------------------
    // 4. The key. `<key>` is `$PWD` with every `/` turned into a `-` and **nothing truncated** —
    //    the rule `ProjectStatus.key(forPath:)` keeps in Swift and `path_key()` keeps in
    //    claude-bestiary. A truncating key was taken out of all three on 2026-09-05 because
    //    `[-48:]` is lossy: two worktrees of one repository differ in the part it cut off.
    {
        const deep = join(scratch, "a-working-directory-whose-name-is-long", "and-then-some-more",
                          "so-that-the-key-cannot-fit-in-forty-eight-characters");
        mkdirSync(deep, { recursive: true });
        const w = { dir: deep, file: join(cache, keyFor(deep)) };
        runHelper(w, ["run", "--label", "deep", "--", "/bin/echo", "hi"]);
        const expected = keyFor(deep);
        check(`a key of ${expected.length} characters is written whole, with nothing cut off it`,
              expected.length > 48 && existsSync(w.file));
        check("and the row it holds names the whole directory it ran in",
              (rowOf(w.file) || {}).tree === deep);
        // The control that says the check above is answering the right question: the last 48
        // characters of the two sibling directories below are identical, so a truncating key
        // would have written both runs into one file.
        const sibling = join(scratch, "another-directory-that-is-also-long", "and-then-some-more",
                             "so-that-the-key-cannot-fit-in-forty-eight-characters");
        mkdirSync(sibling, { recursive: true });
        const sw = { dir: sibling, file: join(cache, keyFor(sibling)) };
        runHelper(sw, ["run", "--label", "sibling", "--", "/bin/echo", "hi"]);
        check("two directories whose keys share their last 48 characters get two rows, not one",
              deep.replace(/\//g, "-").slice(-48) === sibling.replace(/\//g, "-").slice(-48)
                && existsSync(w.file) && existsSync(sw.file)
                && (rowOf(w.file) || {}).label === "deep" && (rowOf(sw.file) || {}).label === "sibling");
        // The control, and it is the reason the truncating key was taken out of all three
        // implementations rather than merely discouraged: with `[-48:]` back in, those same two
        // runs write into one file and the second silently overwrites the first.
        const m = mutantHelper("truncating-key", (t) => t.replace(
            `CLAWDLINE_RUN_FILE="$CLAWDLINE_RUN_DIR/run-$(printf '%s' "$PWD" | tr '/' '-').json"`,
            `clawdline_lossy_key=$(printf '%s' "$PWD" | tr '/' '-')\n`
                + `    CLAWDLINE_RUN_FILE="$CLAWDLINE_RUN_DIR/run-\${clawdline_lossy_key: -48}.json"`));
        check("control: the key check can go red — a helper that truncates to 48 fails it",
              !keyedByWorkingDirectory(m.text));
        // The name the truncating helper produces: `run-` + the last 48 characters of the
        // dashed path + `.json`. Computed from the same rule rather than pasted, so a change to
        // the mutation shows up here as a missing file rather than as a check that stopped asking.
        const lossyName = `run-${deep.replace(/\//g, "-").slice(-48)}.json`;
        runHelper({ dir: deep }, ["run", "--label", "deep", "--", "/bin/echo", "hi"], {}, m.path);
        runHelper({ dir: sibling }, ["run", "--label", "sibling", "--", "/bin/echo", "hi"], {}, m.path);
        check("control: and with it the two runs share one row, whose label is whichever ran last",
              existsSync(join(cache, lossyName))
                && (rowOf(join(cache, lossyName)) || {}).label === "sibling");
    }

    // ---------------------------------------------------------------------------------------
    // 5. It never fails the run it is reporting on. A cache directory that cannot be made is the
    //    ordinary shape of that: no `$HOME`, a read-only home, a full disk.
    {
        const wall = join(scratch, "wall");
        writeFileSync(wall, "not a directory\n");
        const w = workdir("unwritable");
        const r = runHelper(w, ["run", "--label", "lint", "--", "/bin/echo", "reached the end"],
                            { CLAWDLINE_STATUS_DIR: join(wall, "cache") });
        check("a cache directory it cannot create costs the person the bar and nothing else",
              r.code === 0 && r.out.includes("reached the end"));
    }
    {
        // A tree path with a quote and a backslash in it. The file still has to parse, because a
        // file that does not parse is drawn as nothing at all — which looks exactly like no run.
        const w = workdir('quote"and\\slash');
        runHelper(w, ["run", "--label", "awkward", "--", "/bin/echo", "hi"]);
        const row = rowOf(w.file);
        check("a tree path with a quote and a backslash in it still produces a file that parses",
              row !== null && row.unparseable === undefined && row.tree === w.dir);
    }
    {
        // `stale_after` goes into the file with `%s`, so what a person puts there decides whether
        // the row parses at all. `0` is kept — a producer that writes it means *expire
        // immediately*, and "falsy therefore default" is a language accident rather than a
        // decision.
        const ceiling = (name, value) => {
            const w = workdir(`stale-${name}`);
            runHelper(w, ["run", "--stale-after", value, "--", "/bin/echo", "hi"]);
            return rowOf(w.file) || {};
        };
        check("a stale_after that is not a number falls back to the documented default",
              ceiling("words", "abc").stale_after === 900
                && ceiling("leading-zero", "007").stale_after === 900
                && ceiling("fraction", "5.5").stale_after === 900
                && ceiling("plus", "+5").stale_after === 900);
        check("zero is kept, because a producer that writes it meant it",
              ceiling("zero", "0").stale_after === 0);
        check("and a negative one travels, because the reader has an answer for it",
              ceiling("negative", "-5").stale_after === -5);
    }


    // ---------------------------------------------------------------------------------------
    // 6. The heartbeat. `updated_at` is what the reader measures staleness against, and the
    //    producer used to move it only when it wrote the file — at the start, at a phase
    //    boundary, and at the end. The wrapper form has no phases at all, so **any wrapped
    //    command longer than `stale_after` vanished from the bar partway through while it was
    //    still running**, which is the case the wrapper form exists for: a data import, a video
    //    encode, a twenty-minute migration. Measured before the beat existed, `--stale-after 3`
    //    around a six-second command: at t=5 the row was four seconds old and drawn nowhere.
    //
    //    Every scenario here sets a short ceiling so the suite can watch a whole staleness
    //    window go past in a few seconds. The interval is a fifth of the ceiling and never less
    //    than a second, so a ceiling of 3 beats once a second and one of 900 beats every 30.

    // Samples the row **from inside the run**, with the time each sample was taken beside it —
    // the only place that can see the row while the row is the point. `cat` of a file that is
    // replaced by a rename reads one whole version of it, which is the property the format was
    // built around and the reason a sample can be parsed at all.
    const samplerLines = (file, count, gap) => [
        `for _ in $(seq 1 ${count}); do`,
        `  sleep ${gap}`,
        `  printf '%s %s\\n' "$(date +%s)" "$(cat ${JSON.stringify(file)})" >> "$PWD/samples.txt"`,
        "done",
    ];
    const samplerCommand = (w, count, gap) => {
        const path = join(w.dir, "sample.sh");
        writeFileSync(path, ["#!/bin/bash", ...samplerLines(w.file, count, gap), ""].join("\n"));
        chmodSync(path, 0o755);
        return path;
    };
    const samplesOf = (w) => (existsSync(join(w.dir, "samples.txt"))
        ? readFileSync(join(w.dir, "samples.txt"), "utf8").trim().split("\n")
        : []).map((l) => ({ at: Number(l.slice(0, l.indexOf(" "))), row: JSON.parse(l.slice(l.indexOf(" ") + 1)) }));
    // How old the row was when it was sampled — the exact quantity the reader compares against
    // `stale_after`, computed the way the reader computes it.
    const ageOf = (s) => s.at - s.row.updated_at;
    // Everything a beat is not allowed to move, which is every field but `updated_at`. A
    // heartbeat is a liveness claim and not a progress claim, so `phase` and `started_at` in
    // particular have to come back out of the file exactly as the run put them in.
    const exceptUpdatedAt = (row) => JSON.stringify({ ...row, updated_at: 0 });

    {
        // **The measurement in the brief, driven forwards.** Thirteen samples 0.4s apart around a
        // ceiling of three seconds: without a beat the row is stale from about the fourth sample
        // on, and the command has another two seconds to run.
        const w = workdir("beat-wrapper");
        const r = runHelper(w, ["run", "--label", "import", "--stale-after", "3", "--",
                                samplerCommand(w, 13, 0.4)]);
        const s = samplesOf(w);
        check("a wrapped command outlives its own staleness ceiling, so the question is a real one",
              r.code === 0 && s.length === 13 && s[s.length - 1].at - s[0].row.started_at > 3);
        check("and every sample of it was fresh: no sample was older than the ceiling it set",
              s.every((x) => ageOf(x) <= 3));
        check("because updated_at moved while the command ran and nothing else wrote the file",
              new Set(s.map((x) => x.row.updated_at)).size >= 4);
        check("every sample said running, and no beat moved anything but updated_at",
              s.every((x) => x.row.state === "running")
                && new Set(s.map((x) => exceptUpdatedAt(x.row))).size === 1);
    }
    {
        // The sourced form with **no `progress_phase` calls at all**, which is the shape a script
        // that has one long thing to do writes. Nothing but the beat can move `updated_at` here.
        const w = workdir("beat-sourced");
        const script = sourced(w, ["progress_start --label migrate --stale-after 3",
                                   ...samplerLines(w.file, 13, 0.4)].join("\n"));
        const r = runScript(w, script);
        const s = samplesOf(w);
        check("a sourced run that never calls progress_phase stays fresh for its whole length",
              r.code === 0 && s.length === 13 && s[s.length - 1].at - s[0].row.started_at > 3
                && s.every((x) => ageOf(x) <= 3 && x.row.state === "running"));
        // **The beat has to be gone before the finish is written**, or a beat still in flight
        // overwrites `ok` with `running` and leaves a finished run in the bar until the ceiling
        // retires it. The stop is a kill and a `wait`, so the finish cannot race it.
        const finished = rowOf(w.file);
        spawnSync("/bin/sleep", ["2"]);
        const later = rowOf(w.file);
        check("and the beat stops when the run does: two intervals later the ok row is untouched",
              finished !== null && finished.state === "ok" && later !== null
                && later.state === "ok" && later.updated_at === finished.updated_at);
    }
    {
        // **The beat belongs to nobody, and a script that waits for its own jobs is how you find
        // out.** `foo & bar & wait` is the ordinary way a shell script waits for the work it
        // started; a heartbeat left in that shell's job table is waited for too, and the script
        // stops there until the run it is part of has ended. Measured on 2026-09-05 while this was
        // being built: with the beat as a child of the sourcing shell, this script never returned.
        const w = workdir("beat-not-a-job");
        const script = sourced(w, [
            "progress_start --label waiter --stale-after 3",
            "( sleep 0.5 ) &",
            "wait",
            'echo "wait returned"',
        ].join("\n"));
        const r = spawnSync("/bin/bash", [script], {
            encoding: "utf8", cwd: w.dir, timeout: 8000,
            env: { ...process.env, CLAWDLINE_STATUS_DIR: cache },
        });
        check("a sourced script's own bare `wait` returns: the beat is not one of its jobs",
              r.status === 0 && r.signal === null && (r.stdout ?? "").includes("wait returned"));
    }
    {
        // **A beat is a liveness claim, not a progress claim.** `phase` is drawn in place of the
        // percentage, so a beat that wrote the row from its own memory would take the phase off
        // the bar every time it fired.
        const w = workdir("beat-phase");
        const script = sourced(w, ["progress_start --label test --typical 288 --stale-after 3",
                                   "progress_phase compiling",
                                   ...samplerLines(w.file, 11, 0.4)].join("\n"));
        runScript(w, script);
        const s = samplesOf(w);
        check("a beat under a phase leaves the phase exactly where the run put it",
              s.length === 11 && s.every((x) => x.row.phase === "compiling"));
        check("and started_at stays where the run started while updated_at moves under it",
              new Set(s.map((x) => x.row.started_at)).size === 1
                && new Set(s.map((x) => x.row.updated_at)).size >= 3);
        check("and every other field — label, typical_seconds, stale_after, tree, holder — is untouched",
              new Set(s.map((x) => exceptUpdatedAt(x.row))).size === 1);
    }
    {
        // **`kill -9` is the case with no trap in it**, and it is the case the staleness ceiling
        // exists for. The signal goes to the run's shell alone rather than to its process group,
        // so the beat is left an orphan and has to notice by itself — which is the whole of what
        // "the beat dies with the run" has to mean.
        const w = workdir("beat-killed-9");
        // The killed command is a short one **because only the run's shell is killed here**: its
        // child carries on holding the driver's stdout, and the driver is not read to the end until
        // it lets go. Four seconds is long enough to be killed two and a half seconds in.
        const out = driveAndKill(w, ["/bin/bash", helperPath, "run", "--label", "encode",
                                     "--stale-after", "3", "--", slowCommand(w.dir, 4)],
                                 "KILL", "pid", "2.5");
        spawnSync("/bin/sleep", ["2"]);
        const orphaned = rowOf(w.file);
        spawnSync("/bin/sleep", ["2"]);
        const later = rowOf(w.file);
        check("a run killed with -9 writes nothing on the way out, so its row still says running",
              /status=137/.test(out) && orphaned !== null && orphaned.state === "running"
                && orphaned.updated_at > orphaned.started_at);
        check("and the beat it left behind stops: two intervals later updated_at has not moved",
              later !== null && later.updated_at === orphaned.updated_at);
        check("so the row goes stale on schedule, which is what retracts a killed run from the bar",
              later !== null && Math.floor(Date.now() / 1000) - later.updated_at > later.stale_after);
    }
    {
        // A ceiling of zero means the row expires the moment it is written — a producer that
        // writes it means *expire now* — so there is nothing for a beat to keep alive and none is
        // started. A write a second for a row no reader draws is cost with nothing on the other
        // side of it.
        const w = workdir("beat-zero-ceiling");
        const script = sourced(w, ["progress_start --label expired --stale-after 0",
                                   ...samplerLines(w.file, 5, 0.4)].join("\n"));
        runScript(w, script);
        const s = samplesOf(w);
        check("a ceiling of zero starts no beat at all: updated_at never moves",
              s.length === 5 && new Set(s.map((x) => x.row.updated_at)).size === 1);
    }
    {
        // **The interval is a fifth of the ceiling, not a fixed second.** At the 900-second
        // default that is 30 seconds — capped there because past it the beat costs nothing worth
        // counting — so a run of a couple of seconds writes exactly once, as it always did.
        const w = workdir("beat-default-ceiling");
        const script = sourced(w, ["progress_start --label suite",
                                   ...samplerLines(w.file, 5, 0.4)].join("\n"));
        runScript(w, script);
        const s = samplesOf(w);
        check("at the default ceiling the beat is 30 seconds away, so a two-second run writes once",
              s.length === 5 && s.every((x) => x.row.stale_after === 900)
                && new Set(s.map((x) => x.row.updated_at)).size === 1);
    }
    {
        // **A beat keeps a row alive; it never brings one back.** `progress_clear` takes the row
        // away from a run that is still going, and a beat that rebuilt the file from its own
        // memory would put it straight back.
        const w = workdir("beat-cleared");
        const script = sourced(w, [
            "progress_start --label lint --stale-after 3",
            "sleep 1.2",
            "progress_clear",
            "sleep 2",
            "# The finish has to be spent for the EXIT composition to leave the row gone.",
            "CLAWDLINE_RUN_FINISHED=1",
        ].join("\n"));
        const r = runScript(w, script);
        check("a cleared row stays cleared: a beat writes nothing that is not already there",
              r.code === 0 && !existsSync(w.file));
    }
    check("and no beat left a temporary file behind beside the rows it wrote",
          readdirSync(cache).filter((n) => n.includes(".tmp")).length === 0);

    // ---------------------------------------------------------------------------------------
    // 7. Positive controls. Every check above is worth exactly what its ability to go red is
    //    worth, so each of the five lines that carry the feature is taken out of a copy of the
    //    helper and the defect is driven until it happens.
    {
        // **The measurement this heartbeat exists because of**, driven against a copy with the beat
        // taken out: the row is written once, and from about the fourth sample on it is older than
        // the ceiling its own producer set while the command has two seconds left to run. At the
        // 900-second default that is every wrapped command longer than fifteen minutes.
        const m = mutantHelper("no-heartbeat", (t) => t.replace(
            /\n *if \[ "\$state" = running \]; then\n *clawdline_run_file_beat_start\n *fi\n/, "\n"));
        check("control: the beat can be taken out of the helper, and the helper still runs without it",
              !/\n\s*clawdline_run_file_beat_start\n/.test(m.text)
                && spawnSync("/bin/bash", ["-n", m.path]).status === 0);
        const w = workdir("control-no-heartbeat");
        runHelper(w, ["run", "--label", "import", "--stale-after", "3", "--",
                      samplerCommand(w, 13, 0.4)], {}, m.path);
        const s = samplesOf(w);
        check("control: and without it a six-second command goes stale halfway through, while it runs",
              s.length === 13 && new Set(s.map((x) => x.row.updated_at)).size === 1
                && s.filter((x) => ageOf(x) > 3).length > 0
                && s[s.length - 1].row.state === "running");
    }
    {
        // **The measurement the `exit` exists because of.** Without it the TERM handler returns
        // into the script, which runs the rest of itself and ends by declaring success.
        const m = mutantHelper("returning-handler", (t) => t.replace(/\n(\s*)exit "\$status"\n/, "\n$1return 0\n"));
        check("control: the handler check can go red — a handler that returns instead of exiting fails it",
              !handlerExits(m.text));
        const w = workdir("control-returning-handler");
        const script = sourced(w, [
            "progress_start --label test",
            "progress_phase compiling",
            'for _ in $(seq 1 40); do sleep 0.25; done',
            'echo "fell through" > "$PWD/fell-through"',
        ].join("\n"), m.path);
        // **The signal goes to the script alone, not to its process group.** A group kill takes
        // the `sleep` with it, `set -e` then ends the script on the sleep's own status, and the
        // control would be measuring errexit rather than the missing `exit`. `kill <pid>` from
        // another terminal, and `pkill -f` on the script's name, both send exactly this.
        const out = driveAndKill(w, ["/bin/bash", script], "TERM", "pid");
        check("control: without the exit, the killed run carries on to the line after the TERM",
              existsSync(join(w.dir, "fell-through")));
        check("control: and ends by reporting success, which is the defect this helper is about",
              /status=0/.test(out));
    }
    {
        // **`EXIT` alone reads a killed run as a clean one**, because `$?` inside that handler is
        // 0. Measured on this Mac on 2026-09-05; the three signal traps are not belt and braces.
        const m = mutantHelper("no-term-trap",
                               (t) => t.replace(/^\s*trap 'clawdline_run_file_signal 143' TERM$/m, ""));
        check("control: the trap check can go red — a helper that drops the TERM trap fails it",
              !armsEveryTrap(m.text));
        const w = workdir("control-no-term-trap");
        const slow = slowCommand(w.dir);
        driveAndKill(w, ["/bin/bash", m.path, "run", "--label", "suite", "--", slow], "TERM");
        check("control: with no TERM trap, the EXIT handler alone calls the killed run ok",
              (rowOf(w.file) || {}).state === "ok");
    }
    {
        // **No ERR trap ever sees a deliberate `exit`.** Without the EXIT trap the row a guard
        // leaves behind says `running`, for a run that has already stopped — and the reader's
        // staleness ceiling does not retire it for another fifteen minutes.
        const m = mutantHelper("no-exit-trap",
                               (t) => t.replace(/^\s*trap 'clawdline_run_file_exit "\$\?"' EXIT$/m, ""));
        check("control: the trap check can go red the other way — dropping the EXIT trap fails it",
              !armsEveryTrap(m.text));
        const w = workdir("control-no-exit-trap");
        const script = sourced(w, [
            "progress_start --label test",
            "progress_phase guards",
            "exit 1",
        ].join("\n"), m.path);
        const r = runScript(w, script);
        check("control: with no EXIT trap, a guard that exits leaves a row that says running",
              r.code === 1 && (rowOf(w.file) || {}).state === "running");
    }
    {
        // A file written straight onto its own path instead of through a rename is one a reader
        // can catch empty. The inode is what says which happened: a rename replaces the file, a
        // redirect truncates the one that is already there.
        const m = mutantHelper("in-place", (t) => t
            .replace('temp="$CLAWDLINE_RUN_FILE.$$.tmp"', 'temp="$CLAWDLINE_RUN_FILE"')
            .replace('    mv -f "$temp" "$CLAWDLINE_RUN_FILE" 2>/dev/null || rm -f "$temp" 2>/dev/null || true\n', ""));
        check("control: the rename check can go red — a helper that writes straight onto the path fails it",
              !writesThroughARename(m.text));
        const w = workdir("control-in-place");
        const script = sourced(w, [
            'snap() { ls -i "$CLAWDLINE_RUN_FILE" | awk \'{ print $1 }\' >> "$PWD/inodes"; }',
            "progress_start --label test",
            "progress_phase guards",
            "snap",
            "progress_phase compiling",
            "snap",
        ].join("\n"), m.path);
        runScript(w, script);
        const inodes = readFileSync(join(w.dir, "inodes"), "utf8").trim().split("\n");
        check("control: and the inode check can go red — writing in place keeps the same inode",
              inodes.length === 2 && new Set(inodes).size === 1);
    }
    {
        // Without the number validation the value reaches `printf '%s'` and the row stops being
        // JSON at all — every field in it lost, not just that one.
        const m = mutantHelper("unvalidated-stale",
                               (t) => t.replace(/\n    case "\$digits" in\n[\s\S]*?\n    esac\n/, "\n"));
        check("control: the staleness validation can be taken out of the helper",
              !/case "\$digits" in/.test(m.text));
        const w = workdir("control-unvalidated-stale");
        runHelper(w, ["run", "--stale-after", "abc", "--", "/bin/echo", "hi"], {}, m.path);
        check("control: and without it the whole row stops parsing, not merely that one field",
              (rowOf(w.file) || {}).unparseable !== undefined);
    }

    // And the containment this file promised at the top: nothing this suite ran wrote its file
    // anywhere but the scratch cache. Every working directory here lives under `scratch`, so every
    // key this suite could produce begins with that path — a stray is a name, not a count.
    const scratchKeys = `run-${scratch.replace(/\//g, "-")}`;
    const strays = existsSync(realCache)
        ? readdirSync(realCache).filter((n) => n.startsWith(scratchKeys))
        : [];
    check("no run file from this suite was written into the real ~/.claude/statusline-cache"
          + (strays.length ? ` — found ${strays.join(", ")}` : ""),
          strays.length === 0);
} finally {
    rmSync(scratch, { recursive: true, force: true });
}

console.log(failures === 0
    ? `progress helper: all ${checks} checks passed`
    : `progress helper: ${failures} of ${checks} checks failed`);
process.exit(failures === 0 ? 0 : 1);
