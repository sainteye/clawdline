// `./test.sh` and `./build.sh` have to say how far they have got, to somebody who is not looking at
// the terminal they were started in.
//
// **This runs the scripts' own lines.** The block that writes
// `~/.claude/statusline-cache/run-<key>.json` is lifted out of `test.sh` and out of `build.sh`
// between two literal marker comments and executed against a scratch directory — the same shape
// `Tests/test-sh-lock.mjs` and `Tests/test-sh-streaming.mjs` use, and for the same reason: a harness
// that writes out its own copy of the construct it means to prove is testing bash, which needs no
// test here.
//
// Nothing below compiles anything, runs either script whole, or touches the real
// `~/.claude/statusline-cache`. Every harness runs in a directory this file made, with
// `CLAWDLINE_STATUS_DIR` pointed inside it, and the last check in this file is that the real cache
// directory was left exactly as it was found.
//
// **What it is guarding.** Three things that are easy to get wrong and silent when they are:
//
//   * A `TERM` handler that writes `fail` and then *returns into the script*, which carries on from
//     where it was interrupted and finishes by declaring success. That was measured while
//     claude-bestiary's `docs/producers.md` was written, and the `exit` in the handler is the whole
//     of the repair.
//   * A finish composed into the EXIT trap and nothing else. Measured on this Mac on 2026-09-05:
//     when a bash script with no `INT`/`TERM` trap is killed, its EXIT trap still runs and `$?` in
//     it is **0** — so a killed run would be written down as a clean one.
//   * A file written straight onto its own path instead of through a rename, which a reader can
//     catch empty. The check for that is the inode: a rename replaces the file, a redirect truncates
//     the one that is already there.

import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, chmodSync, existsSync, readdirSync, realpathSync, rmSync } from "node:fs";
import { tmpdir, homedir } from "node:os";
import { join } from "node:path";
// `fileURLToPath`, not `URL.pathname`: this checkout lives under `Application Support`, and a
// percent-encoded space is a path bash cannot open — it answered 127 and the check went red for
// a reason that had nothing to do with test.sh.
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
    console.log(`run file producer: stopped after ${checks + 1} checks — ${why}`);
    process.exit(1);
};

const OPEN = "# >>> clawdline run file >>>";
const CLOSE = "# <<< clawdline run file <<<";

const script = readFileSync(new URL("../test.sh", import.meta.url), "utf8");
const buildScript = readFileSync(new URL("../build.sh", import.meta.url), "utf8");

const lift = (text, name) => {
    const lines = text.split("\n");
    const opens = lines.filter((l) => l === OPEN).length;
    const closes = lines.filter((l) => l === CLOSE).length;
    const first = lines.indexOf(OPEN);
    const last = lines.indexOf(CLOSE);
    check(`${name} carries the two run-file markers, once each`,
          opens === 1 && closes === 1 && last > first);
    if (opens !== 1 || closes !== 1 || last <= first) {
        stop(`cannot find exactly one ${OPEN} … ${CLOSE} pair in ${name}, so there is nothing to run`);
    }
    return lines.slice(first, last + 1).join("\n");
};

const block = lift(script, "test.sh");
const buildBlock = lift(buildScript, "build.sh");

// **One block, not two dialects.** The machine lock is shared between these two scripts as a record
// contract and a set of knob names, and its own comments record what that cost: seventeen fields
// against eleven, four of them written by nobody else, and a compare-and-swap that was therefore
// `"" = ""` and always true. This one is shared as text, so the two cannot drift at all — and this
// check is the whole of that promise.
check("test.sh and build.sh carry the same block, byte for byte", block === buildBlock);
if (block !== buildBlock) {
    stop("the two blocks have diverged; every behaviour below would then be a fact about test.sh only");
}
const code = block.split("\n").filter((l) => !/^\s*#/.test(l));
if (code.join("").trim().length === 0) stop("the marked block contains no code at all");

// ---------------------------------------------------------------------------------------------
// What the block promises about itself, read off the scripts rather than off this file's memory of
// them. Each predicate is a function of the text, so the mutation controls further down can ask the
// same question of a block that has had the line taken out.

const writesThroughARename = (text) => /temp="\$CLAWDLINE_RUN_FILE\.\$\$\.tmp"/.test(text)
    && /mv -f "\$temp" "\$CLAWDLINE_RUN_FILE"/.test(text)
    && !/^\s*\}\s*>\s*"\$CLAWDLINE_RUN_FILE"/m.test(text);
check("the file is written to a temporary name in the same directory and renamed into place",
      writesThroughARename(block));

// Keyed by working directory. `ghrun-` is keyed by the git remote and cannot tell two worktrees of
// one repository apart, which is the ordinary case on this machine rather than an exotic one.
const keyedByWorkingDirectory = (text) => /run-\$\(printf '%s' "\$PWD" \| tr '\/' '-'\)\.json/.test(text)
    && !/git (config|remote|rev-parse --show-toplevel)/.test(text);
// Asked of the code and not of the block, because the comment above the line says the words
// "git remote" in the course of explaining why it is not one.
check("the key is $PWD with every / turned into a -, and no part of it comes from a git remote",
      keyedByWorkingDirectory(code.join("\n")));

check("the directory is CLAWDLINE_STATUS_DIR, falling back to the shared statusline cache",
      /CLAWDLINE_RUN_DIR="\$\{CLAWDLINE_STATUS_DIR:-\$\{HOME:-\}\/\.claude\/statusline-cache\}"/.test(block));

// The contract says so in as many words: `ghrun-` needs a `producer` because two writers compete
// for it, and this file has one writer and a staleness ceiling instead.
check("nothing in the block writes a `producer` field", !/"producer"/.test(block));

// The three traps, and **no EXIT trap** — `Tests/test-sh-lock.mjs` requires test.sh to install
// exactly one, and a second one silently replaces the first rather than running beside it.
const installsSignalTraps = (text) => /^trap 'clawdline_run_file_signal "\$\?"' ERR$/m.test(text)
    && /^trap 'clawdline_run_file_signal 130' INT$/m.test(text)
    && /^trap 'clawdline_run_file_signal 143' TERM$/m.test(text);
check("the block traps ERR, INT and TERM", installsSignalTraps(block));
check("and installs no EXIT trap of its own",
      !code.some((l) => /^\s*trap\s+[^-]/.test(l) && /\bEXIT\b/.test(l)));

// The handler exits. Without it a TERM handler returns into the script it interrupted.
const handlerExits = (text) => {
    const body = /clawdline_run_file_signal\(\) \{([\s\S]*?)\n\}/.exec(text);
    return body !== null && /clawdline_run_file_finish fail/.test(body[1]) && /\n\s*exit "\$status"/.test(body[1]);
};
check("the signal handler writes fail and then exits, rather than returning into the script",
      handlerExits(block));

// `typical_seconds` says where it came from and when, beside the value. A number with no provenance
// is indistinguishable from one somebody made up.
check("typical_seconds names its measurement, its date and the page it is written on",
      /288/.test(block) && /2026-09-03/.test(block) && /docs\/suite-runtime\.md/.test(block));
check("and build.sh writes none at all, because nobody has measured it",
      /Nobody has ever measured `\.\/build\.sh`/.test(block)
        && /\*\) CLAWDLINE_RUN_TYPICAL="\$\{CLAWDLINE_RUN_TYPICAL:-\}" ;;/.test(block));

// ---------------------------------------------------------------------------------------------
// Where each script calls it. A block nothing calls writes nothing.

// The composition is lifted rather than retyped, for the reason this whole file exists: a harness
// that writes its own copy of the line proves nothing about the line in the script.
const composed = /^\s*if declare -F clawdline_run_file_exit .*$/m.exec(script);
check("test.sh composes the run file's exit into the one EXIT handler it keeps", composed !== null);
if (composed === null) {
    stop("test.sh no longer composes clawdline_run_file_exit into clawdline_suite_exit_cleanup, so nothing below can drive the way out");
}
check("and that composition is inside the suite lock's cleanup, which is the trap that actually runs",
      /clawdline_suite_exit_cleanup\(\) \{[\s\S]*?clawdline_run_file_exit "\$status"[\s\S]*?\n\}/.test(script));

const phasesOf = (text) => [...text.matchAll(/^clawdline_run_file_phase (.+)$/gm)]
    .map((m) => m[1].replace(/^'|'$/g, ""));
const testPhases = phasesOf(script);
const buildPhases = phasesOf(buildScript);
// The four `docs/suite-runtime.md` measured, in the order the script reaches them: 3 s of guards,
// 129 s of node suites, 100 s of compile, 56 s of test binary.
check("test.sh moves the phase at the four boundaries its own timings name, in order",
      testPhases.join(",") === "guards,node suites,compiling,analysing");
// Above the lock's own phase rather than below it: `Tests/test-sh-lock.mjs` requires
// `clawdline_suite_lock_phase compiling` to be the line immediately before the compiler it declares
// itself for, and a line inserted between the two would take that guard red.
check("and the compiling phase sits with the lock phase of the same name, above the swiftc",
      /\nclawdline_run_file_phase compiling\nclawdline_suite_lock_phase compiling\nswiftc/.test(script));
check("build.sh moves the phase at its own boundaries",
      buildPhases.join(",") === "preparing,checking signing,compiling,packaging,signing,installing");
// build.sh installs three EXIT traps in sequence. The one that matters is `cleanup_build`, which
// covers the compile, the signing and the install; the signing probe's own trap is **deliberately
// left alone**, because it is inside a region `Tests/keychain-rebuild-focused.mjs` lifts and runs.
// The cost of that is written down rather than hidden: a build that stops inside the signing
// selection leaves a `running` row for the reader's staleness rule to retire, which is the case
// that rule exists for.
check("build.sh composes the run file's exit into the EXIT handler that covers the build",
      /cleanup_build\(\) \{\n[\s\S]{0,400}?clawdline_run_file_exit "\$clawdline_build_status"/.test(buildScript));
check("and leaves the signing probe's own trap exactly as it was, because that one is lifted",
      /trap 'rm -f "\$signing_probe_out" "\$signing_probe_out\.timed-out"' EXIT/.test(buildScript));

// **A call inside a region another suite lifts out is a call that suite cannot make.** Three parts
// of `build.sh` are cut out between `# BEGIN keychain-rebuild-focused: …` markers and run by
// `Tests/keychain-rebuild-focused.mjs` in a shell that has none of these functions, so a phase call
// that drifts inside one ends that harness at 127 rather than testing what it was written for. This
// happened while this feature was being written; the count is printed so that a scan finding no
// regions at all is a failure rather than a clean report about nothing.
const liftedRegions = [...buildScript.matchAll(/^# BEGIN keychain-rebuild-focused: (.+)$/gm)].map((m) => {
    const from = buildScript.indexOf(m[0]);
    const to = buildScript.indexOf(`# END keychain-rebuild-focused: ${m[1]}`);
    return { label: m[1], text: to > from ? buildScript.slice(from, to) : "" };
});
const trespassing = liftedRegions.filter((r) => /clawdline_run_file_/.test(r.text)).map((r) => r.label);
check(`none of the ${liftedRegions.length} regions build.sh hands to Tests/keychain-rebuild-focused.mjs calls these functions`,
      liftedRegions.length >= 3 && liftedRegions.every((r) => r.text !== "") && trespassing.length === 0);
// The one place a call *is* inside a lifted region is the suite lock's cleanup in `test.sh`, and it
// is guarded, because `Tests/test-sh-lock.mjs` runs that block on its own.
const lockBlock = script.slice(script.indexOf("# >>> clawdline suite lock >>>"), script.indexOf("# <<< clawdline suite lock <<<"));
const lockCalls = lockBlock.split("\n").filter((l) => /clawdline_run_file_/.test(l) && !/^\s*#/.test(l));
check("and the one call inside test.sh's suite-lock block asks whether the function is there first",
      lockCalls.length === 1
        && /if declare -F clawdline_run_file_exit >\/dev\/null 2>&1; then clawdline_run_file_exit "\$status" \|\| true; fi/.test(lockCalls[0]));

check("test.sh runs this file", /^node Tests\/run-file-producer\.mjs$/m.test(script));
// The other half of the feature, which arrives on another branch. Registering it here is what makes
// a checkout that has only one of the two say so, instead of passing quietly.
check("and the web app's half of it", /^node Tests\/web-run-progress\.mjs$/m.test(script));

// ---------------------------------------------------------------------------------------------
// Running it.

// `realpathSync` because `/var` is a symlink to `/private/var` on this Mac: a harness started in
// the first reports the second as `$PWD`, and a key built from the path node handed out would look
// for a file nothing ever wrote. That is the harness's bug to avoid, not the block's.
const scratch = realpathSync(mkdtempSync(join(tmpdir(), "clawdline-run-file-")));
const cache = join(scratch, "statusline-cache");
const realCache = join(homedir(), ".claude", "statusline-cache");
const realCacheBefore = existsSync(realCache)
    ? readdirSync(realCache).filter((n) => n.startsWith("run-")).sort().join("\n")
    : "(no directory)";

const keyFor = (dir) => `run-${dir.replace(/\//g, "-")}.json`;

// The harness is the block plus test.sh's own composition, in a script named after the label it
// should produce: `test.sh` in one directory, `build.sh` in another. The label is derived from the
// script's own name, so naming the harnesses is how that derivation gets exercised end to end.
const harness = (name, scriptName, body, blockText = block) => {
    const dir = join(scratch, name);
    mkdirSync(dir, { recursive: true });
    const path = join(dir, scriptName);
    writeFileSync(path, [
        "#!/bin/bash",
        "set -euo pipefail",
        blockText,
        // Lifted from test.sh, never retyped.
        "clawdline_harness_cleanup() {",
        "  local status=$?",
        composed[0].trim(),
        '  return "$status"',
        "}",
        "trap clawdline_harness_cleanup EXIT",
        body,
        "",
    ].join("\n"));
    chmodSync(path, 0o755);
    return { dir, path, file: join(cache, keyFor(dir)) };
};

const run = (h, env = {}, args = []) => {
    const r = spawnSync("/bin/bash", [h.path, ...args], {
        encoding: "utf8",
        cwd: h.dir,
        env: { ...process.env, CLAWDLINE_STATUS_DIR: cache, ...env },
    });
    return { code: r.status, out: r.stdout ?? "", err: r.stderr ?? "", all: (r.stdout ?? "") + (r.stderr ?? "") };
};

// A row that was never written is a red result, not a stack trace.
// **`set -m` in the driver, and it is load-bearing.** Without job control, a background job of a
// non-interactive shell starts with `SIGINT` ignored, and a signal ignored on entry cannot be
// trapped — so an INT scenario written the obvious way measures bash's job control rather than this
// block's handler. With it the harness gets its own process group and the default disposition.
const driveAndKill = (h, signal) => {
    const driver = join(h.dir, `drive-${signal}.sh`);
    writeFileSync(driver, [
        "#!/bin/bash",
        "set -m",
        `"${h.path}" &`,
        "p=$!",
        `for _ in $(seq 1 100); do [ -f "${h.file}" ] && break; sleep 0.05; done`,
        "sleep 0.2",
        `kill -${signal} "$p" 2>/dev/null || true`,
        'wait "$p"',
        'echo "status=$?"',
        "",
    ].join("\n"));
    chmodSync(driver, 0o755);
    const r = spawnSync("/bin/bash", [driver], {
        encoding: "utf8", cwd: h.dir,
        env: { ...process.env, CLAWDLINE_STATUS_DIR: cache },
    });
    return `${r.stdout ?? ""}${r.stderr ?? ""}`;
};

const rowOf = (path) => {
    if (!existsSync(path)) return null;
    try {
        return JSON.parse(readFileSync(path, "utf8"));
    } catch (e) {
        return { unparseable: String(e) };
    }
};

try {
    // 1. A run that finishes writes `ok`, in the directory it was pointed at, under the key its own
    //    working directory makes, with the label its own file name makes.
    {
        const h = harness("finishes", "test.sh", "clawdline_run_file_phase guards\n");
        const r = run(h);
        const row = rowOf(h.file);
        check("a run that finishes leaves state ok under run-<cwd with slashes as dashes>.json",
              r.code === 0 && row !== null && row.state === "ok");
        check("the label is the name of the script that produced it, drawn verbatim",
              row !== null && row.label === "test");
        check("the finished row carries no phase, because there is no longer one to draw",
              row !== null && row.phase === undefined);
        check("it names the tree it ran in and the session that started it",
              row !== null && row.tree === h.dir && typeof row.holder === "string" && row.holder.length > 0);
        check("typical_seconds is the measured 288 for a script called test",
              row !== null && row.typical_seconds === 288);
        check("stale_after is written out, and it is the 900 a reader would have assumed",
              row !== null && row.stale_after === 900);
        check("started_at and updated_at are epoch seconds, not a rendered date",
              row !== null && Number.isInteger(row.started_at) && Number.isInteger(row.updated_at));
        check("and nothing was left behind beside it — the temporary name was renamed, not copied",
              readdirSync(cache).filter((n) => n.includes(".tmp")).length === 0);
    }

    // 2. The same block in a script called build.sh writes the other label and no typical_seconds.
    {
        const h = harness("build-label", "build.sh", "clawdline_run_file_phase compiling\n");
        run(h);
        const row = rowOf(h.file);
        check("the same block in build.sh writes label build", row !== null && row.label === "build");
        check("and no typical_seconds at all, rather than a number nobody measured",
              row !== null && row.typical_seconds === undefined);
    }

    // 3. Each phase boundary moves `phase` and `updated_at`, `started_at` stays where it was, and
    //    every reader sees a whole file: the inode changes on every write, which is what a rename
    //    does and what a redirect onto the same path does not.
    {
        const h = harness("phases", "test.sh", [
            'snap() { cp "$CLAWDLINE_RUN_FILE" "$PWD/snap-$1.json"; ls -i "$CLAWDLINE_RUN_FILE" | awk \'{ print $1 }\' >> "$PWD/inodes"; }',
            "clawdline_run_file_phase guards",
            "snap 1",
            "sleep 1.1",
            "clawdline_run_file_phase 'node suites'",
            "snap 2",
            "sleep 1.1",
            "clawdline_run_file_phase compiling",
            "snap 3",
        ].join("\n"));
        const r = run(h);
        const snaps = [1, 2, 3].map((n) => rowOf(join(h.dir, `snap-${n}.json`)));
        check("every sample taken while the run was going was a whole file that parses",
              r.code === 0 && snaps.every((s) => s !== null && s.unparseable === undefined));
        check("each phase boundary moves the phase",
              snaps.map((s) => (s ? s.phase : "")).join(",") === "guards,node suites,compiling");
        check("and moves updated_at with it",
              snaps[0].updated_at < snaps[1].updated_at && snaps[1].updated_at < snaps[2].updated_at);
        check("while started_at stays where the run started",
              snaps[0].started_at === snaps[1].started_at && snaps[1].started_at === snaps[2].started_at);
        check("every row says running until the run is over",
              snaps.every((s) => s.state === "running"));
        const inodes = readFileSync(join(h.dir, "inodes"), "utf8").trim().split("\n");
        check("and the file is replaced rather than rewritten: a different inode after every write",
              inodes.length === 3 && new Set(inodes).size === 3);
    }

    // 4. A run killed with TERM writes `fail` and **does not fall through into the success path**.
    //    The loop of short sleeps is deliberate: one long `sleep` would leave the question of when
    //    bash gets round to the handler mixed into the answer.
    const killed = (signal, expected) => {
        const h = harness(`killed-${signal}`, "test.sh", [
            "clawdline_run_file_phase compiling",
            'for _ in $(seq 1 12); do sleep 0.25; done',
            'echo "fell through" > "$PWD/fell-through"',
        ].join("\n"));
        const out = driveAndKill(h, signal);
        const row = rowOf(h.file);
        check(`a run killed with ${signal} writes fail`, row !== null && row.state === "fail");
        check(`and does not carry on to the line after the ${signal}`,
              !existsSync(join(h.dir, "fell-through")));
        check(`and leaves on ${expected}, so its caller can tell it was killed`,
              new RegExp(`status=${expected}`).test(out));
    };
    killed("TERM", 143);
    killed("INT", 130);

    // 5. The two ways a run ends badly without a signal: a command that fails, and a deliberate
    //    `exit`. The first is the ERR trap, the second reaches nothing but the EXIT handler.
    {
        const h = harness("fails", "test.sh", [
            "clawdline_run_file_phase compiling",
            "/usr/bin/false",
            'echo "fell through" > "$PWD/fell-through"',
        ].join("\n"));
        const r = run(h);
        const row = rowOf(h.file);
        check("a command that fails leaves state fail and the run's own status",
              row !== null && row.state === "fail" && r.code === 1);
        check("and the script does not carry on past it",
              !existsSync(join(h.dir, "fell-through")));
    }
    {
        const h = harness("exits", "test.sh", [
            "clawdline_run_file_phase analysing",
            "# The shape every guard in test.sh uses: say what is wrong, then exit on a number.",
            'echo "the suite exited 125" >&2',
            "exit 125",
        ].join("\n"));
        const r = run(h);
        const row = rowOf(h.file);
        check("a deliberate exit on a number leaves state fail, which no ERR trap would have seen",
              row !== null && row.state === "fail" && r.code === 125);
    }

    // 6. The clear, which is the other half of what the block owes: a row from a run that no longer
    //    exists can be taken away without editing the file by hand.
    {
        const h = harness("clears", "test.sh", [
            "clawdline_run_file_phase guards",
            'test -f "$CLAWDLINE_RUN_FILE" || { echo "no row was written at all" >&2; exit 1; }',
            "clawdline_run_file_clear",
        ].join("\n"));
        const r = run(h);
        check("clearing takes the row away, and the clean finish then writes it back as ok",
              r.code === 0 && rowOf(h.file) !== null && rowOf(h.file).state === "ok");
        const h2 = harness("clears-last", "test.sh", [
            "clawdline_run_file_phase guards",
            "clawdline_run_file_clear",
            // The finish has to be spent for the EXIT composition to leave the row gone.
            "CLAWDLINE_RUN_FINISHED=1",
        ].join("\n"));
        run(h2);
        check("and a producer that clears last leaves no row behind", !existsSync(h2.file));
    }

    // 7. It never fails the run it is reporting on. A cache directory that cannot be made is the
    //    ordinary shape of that: no `$HOME`, a read-only home, a full disk.
    {
        const wall = join(scratch, "wall");
        writeFileSync(wall, "not a directory\n");
        const h = harness("unwritable", "test.sh", [
            "clawdline_run_file_phase guards",
            'echo "reached the end" > "$PWD/reached"',
        ].join("\n"));
        const r = run(h, { CLAWDLINE_STATUS_DIR: join(wall, "cache") });
        check("a cache directory it cannot create costs the person the bar and nothing else",
              r.code === 0 && existsSync(join(h.dir, "reached")));
    }

    // 8. A tree path with a quote and a backslash in it. The file still has to parse, because a file
    //    that does not parse is drawn as nothing at all — which looks exactly like no run.
    {
        const awkward = 'quote"and\\slash';
        const h = harness(awkward, "test.sh", "clawdline_run_file_phase guards\n");
        run(h);
        const row = rowOf(h.file);
        check("a tree path with a quote and a backslash in it still produces a file that parses",
              row !== null && row.unparseable === undefined && row.tree === h.dir);
    }

    // ---------------------------------------------------------------------------------------------
    // Positive controls. Every check above is worth what its ability to go red is worth, so each of
    // the three that carry the feature is asked again of a block with that line taken out.

    {
        const mutant = block.replace(/\n\s*exit "\$status"\n/, "\n  return 0\n");
        check("control: the handler check can go red — a handler that returns instead of exiting fails it",
              mutant !== block && !handlerExits(mutant));
        const h = harness("mutant-no-exit", "test.sh", [
            "clawdline_run_file_phase compiling",
            'for _ in $(seq 1 12); do sleep 0.25; done',
            'echo "fell through" > "$PWD/fell-through"',
        ].join("\n"), mutant);
        const out = driveAndKill(h, "TERM");
        // This is the measurement the `exit` exists because of, reproduced here so that the check
        // above is known to be answering it: without it the handler returns into the script, which
        // runs the rest of itself and **ends by declaring success**.
        check("control: without the exit, the killed run carries on to the line after the TERM",
              existsSync(join(h.dir, "fell-through")));
        check("control: and ends by reporting success, which is the defect this feature is about",
              /status=0/.test(out));
    }
    {
        const mutant = block
            .replace('temp="$CLAWDLINE_RUN_FILE.$$.tmp"', 'temp="$CLAWDLINE_RUN_FILE"')
            .replace('  mv -f "$temp" "$CLAWDLINE_RUN_FILE" 2>/dev/null || rm -f "$temp" 2>/dev/null || true\n', "");
        check("control: the rename check can go red — a block that writes straight onto the path fails it",
              mutant !== block && !writesThroughARename(mutant));
        const h = harness("mutant-in-place", "test.sh", [
            'snap() { ls -i "$CLAWDLINE_RUN_FILE" | awk \'{ print $1 }\' >> "$PWD/inodes"; }',
            "clawdline_run_file_phase guards",
            "snap",
            "clawdline_run_file_phase compiling",
            "snap",
        ].join("\n"), mutant);
        run(h);
        const inodes = readFileSync(join(h.dir, "inodes"), "utf8").trim().split("\n");
        check("control: and the inode check can go red — writing in place keeps the same inode",
              inodes.length === 2 && new Set(inodes).size === 1);
    }
    {
        const mutant = block.replace(/^trap 'clawdline_run_file_signal 143' TERM$/m, "");
        check("control: the trap check can go red — a block that drops the TERM trap fails it",
              mutant !== block && !installsSignalTraps(mutant));
        const h = harness("mutant-no-term", "test.sh", [
            "clawdline_run_file_phase compiling",
            'for _ in $(seq 1 12); do sleep 0.25; done',
        ].join("\n"), mutant);
        driveAndKill(h, "TERM");
        // Measured on this Mac on 2026-09-05: a killed bash script still runs its EXIT trap, and
        // `$?` inside it is 0. So composing the finish into EXIT alone writes a killed run down as
        // a clean one, and the three traps are not belt and braces.
        check("control: with no TERM trap, the EXIT handler alone calls the killed run ok",
              (rowOf(h.file) || {}).state === "ok");
    }

    // The two narrow modes of `test.sh` run nothing and must therefore say nothing. This is the one
    // scenario that runs the real script rather than the lifted block — both modes exit above the
    // block, compile nothing and take no lock, so it costs a few milliseconds.
    {
        const narrow = join(scratch, "narrow-cache");
        mkdirSync(narrow, { recursive: true });
        const testSh = fileURLToPath(new URL("../test.sh", import.meta.url));
        const modes = [["--verify-suite-roster"], ["--verify-completion-receipts", "/dev/null"]];
        const statuses = modes.map((args) => spawnSync("/bin/bash", [testSh, ...args], {
            encoding: "utf8",
            env: { ...process.env, CLAWDLINE_STATUS_DIR: narrow },
        }).status);
        check("the two narrow modes of test.sh answer their question and write no run file at all",
              statuses[0] === 0 && statuses[1] === 125 && readdirSync(narrow).length === 0);
    }

    // And the containment this file promised at the top: the real cache directory was not touched.
    const realCacheAfter = existsSync(realCache)
        ? readdirSync(realCache).filter((n) => n.startsWith("run-")).sort().join("\n")
        : "(no directory)";
    check("no run file was written into the real ~/.claude/statusline-cache",
          realCacheAfter === realCacheBefore);
} finally {
    rmSync(scratch, { recursive: true, force: true });
}

console.log(failures === 0
    ? `run file producer: all ${checks} checks passed`
    : `run file producer: ${failures} of ${checks} checks failed`);
process.exit(failures === 0 ? 0 : 1);
