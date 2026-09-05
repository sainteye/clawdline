// `./test.sh` and `./build.sh` have to say how far they have got, to somebody who is not looking at
// the terminal they were started in.
//
// **This file is about the two scripts, not about the record.** The traps, the rename, the key and
// the four ways a run can end badly moved into `Resources/clawdline-progress.sh` and are driven in
// both their forms by `Tests/progress-helper.mjs`. What is left here is the half that cannot move:
// that these two scripts are the helper's first callers rather than its documentation. They source
// it **from the checkout**, so a suite run never depends on Clawdline being installed; they name
// themselves and pass only measurements that exist; they arm it before anything that could exit;
// and every EXIT trap either of them installs further down is a superset of the one `progress_start`
// armed, because bash keeps exactly one and a second `trap … EXIT` silently replaces the first.
//
// **What it runs is the scripts' own lines**, lifted by content and never retyped — the same shape
// `Tests/test-sh-lock.mjs` and `Tests/test-sh-streaming.mjs` use, and for the same reason: a harness
// that writes out its own copy of the construct it means to prove is testing bash, which needs no
// test here.
//
// Nothing below compiles anything, runs either script whole, or touches the real
// `~/.claude/statusline-cache`.

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

const script = readFileSync(new URL("../test.sh", import.meta.url), "utf8");
const buildScript = readFileSync(new URL("../build.sh", import.meta.url), "utf8");
const helperPath = fileURLToPath(new URL("../Resources/clawdline-progress.sh", import.meta.url));
if (!existsSync(helperPath)) stop("there is no Resources/clawdline-progress.sh for either script to source");

// ---------------------------------------------------------------------------------------------
// **One helper, sourced from the checkout.** This used to be a marked block copied byte for byte
// into both scripts, with a check here that the two copies had not drifted. One file cannot drift
// from itself; what has to hold now is that both scripts reach the same one, and that neither of
// them reaches for the installed app — a suite that only runs on a Mac with Clawdline in
// `/Applications` is a suite CI cannot run, and `build.sh` is the thing that puts the file there.
const sourcesTheHelper = (text) => /^\. \.\/Resources\/clawdline-progress\.sh$/m.test(text)
    && !/Clawdline\.app\/Contents\/Resources\/clawdline-progress\.sh/.test(text);
for (const [name, text] of [["test.sh", script], ["build.sh", buildScript]]) {
    check(`${name} sources the checkout's copy of the helper, never an installed bundle's`,
          sourcesTheHelper(text));
}

// The arguments each script passes are its own, and they are the only thing about the record that
// still lives in these two files. Lifted rather than retyped, because the harnesses below run them.
const startLineOf = (text) => (/^progress_start .*$/m.exec(text) || [null])[0];
const testStart = startLineOf(script);
const buildStart = startLineOf(buildScript);
check("each script opens the row by naming itself", testStart !== null && buildStart !== null);
if (testStart === null || buildStart === null) {
    stop("one of the two scripts no longer calls progress_start, so nothing below can drive it");
}
// **288 seconds is measured and says where.** One green `./test.sh` on 2026-09-03, receipt
// `8353 checks passed`, in a detached worktree pinned at `d97d0afb`, written up in
// `docs/suite-runtime.md`. A number with no provenance is indistinguishable from one somebody
// made up, so the provenance is asserted beside the value.
check("test.sh calls itself test and passes the 288 seconds somebody measured",
      /^progress_start --label test --typical 288$/.test(testStart)
        && /288/.test(script) && /2026-09-03/.test(script) && /docs\/suite-runtime\.md/.test(script));
// **Nobody has ever measured `./build.sh`**, so it passes no `--typical` and no `typical_seconds`
// is written at all. The field is optional, and an invented number is indistinguishable from a
// measured one to every reader of the file.
check("build.sh calls itself build and passes no measurement, because there is none",
      /^progress_start --label build$/.test(buildStart)
        && /Nobody has ever measured `\.\/build\.sh`/.test(buildScript));

// ---------------------------------------------------------------------------------------------
// Where each script calls it. A helper nothing calls writes nothing.

// The composition is lifted rather than retyped, for the reason this whole file exists: a harness
// that writes its own copy of the line proves nothing about the line in the script.
const composed = /^\s*if declare -F clawdline_run_file_exit .*$/m.exec(script);
check("test.sh composes the run file's exit into the one EXIT handler it keeps", composed !== null);
if (composed === null) {
    stop("test.sh no longer composes clawdline_run_file_exit into clawdline_suite_exit_cleanup, so nothing below can drive the way out");
}
check("and that composition is inside the suite lock's cleanup, which is the trap that actually runs",
      /clawdline_suite_exit_cleanup\(\) \{[\s\S]*?clawdline_run_file_exit "\$status"[\s\S]*?\n\}/.test(script));

const phasesOf = (text) => [...text.matchAll(/^progress_phase (.+)$/gm)]
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
      /\nprogress_phase compiling\nclawdline_suite_lock_phase compiling\nswiftc/.test(script));
check("build.sh moves the phase at its own boundaries",
      buildPhases.join(",") === "preparing,checking signing,compiling,packaging,signing,installing");
// build.sh installs four EXIT traps in sequence, and `cleanup_build` — which covers the compile,
// the signing and the install — is the last of them.
check("build.sh composes the run file's exit into the EXIT handler that covers the build",
      /cleanup_build\(\) \{\n[\s\S]{0,400}?clawdline_run_file_exit "\$clawdline_build_status"/.test(buildScript));
// The signing probe's trap used to be `trap 'rm -f …' EXIT` and was **knowingly** left that way,
// because it sits inside a region `Tests/keychain-rebuild-focused.mjs` lifts and runs in a shell
// where these functions do not exist. The cost was written down rather than hidden — and it was
// the largest of the three windows: four of the deliberate `exit 1`s in this file are in the
// selection below it, so the ordinary way a build stops was the one way it did not report. It is
// composed now, through the same `declare -F` guard test.sh's lock block uses, which costs the
// lifted region nothing.
// Read the handler's whole body rather than a window of N characters after its name: a comment
// grown by one paragraph would otherwise take this red for a reason that is not a defect.
const probeBody = /clawdline_signing_probe_exit\(\) \{([\s\S]*?)\n\}/.exec(buildScript);
check("and the signing probe's trap removes its own file and then reports the run",
      probeBody !== null
        && /rm -f "\$signing_probe_out" "\$signing_probe_out\.timed-out"/.test(probeBody[1])
        && /if declare -F clawdline_run_file_exit >\/dev\/null 2>&1; then clawdline_run_file_exit "\$status" \|\| true; fi/.test(probeBody[1])
        && /^trap 'clawdline_signing_probe_exit "\$\?"' EXIT$/m.test(buildScript));

// ---------------------------------------------------------------------------------------------
// **The window before the first EXIT trap.** No ERR trap ever sees a deliberate `exit`, so until
// an EXIT handler is armed an `exit` in either script is caught by nothing at all and the row it
// leaves behind says `running`. test.sh had two such guards above its lock — the trailing-comma
// scan and the browser-contract roster — and build.sh had everything above `cleanup_build`.
// `progress_start` arms that first trap now, which is what closes the window by construction; what
// is asked here is that nothing below it re-opens one.
//
// Bash keeps exactly one EXIT trap, so what has to hold is that every replacement is a widening:
// each `trap … EXIT` in either script writes the run file, and nothing takes the trap off again.
const exitTrapsOf = (text) => text.split("\n")
    .filter((l) => /^\s*trap\s+[^-]/.test(l) && /\bEXIT\b/.test(l) && !/^\s*#/.test(l));
const handlerReports = (text, line) => {
    if (/clawdline_run_file_exit/.test(line)) return true;
    // A trap that names a function is only as good as that function's body, so read the body.
    const named = /trap\s+'?([A-Za-z_][A-Za-z0-9_]*)/.exec(line);
    if (!named) return false;
    const body = new RegExp(`${named[1]}\\(\\) \\{([\\s\\S]*?)\\n\\}`).exec(text);
    return body !== null && /clawdline_run_file_exit/.test(body[1]);
};
for (const [name, text] of [["test.sh", script], ["build.sh", buildScript]]) {
    const traps = exitTrapsOf(text);
    check(`every one of ${name}'s ${traps.length} EXIT traps writes the run file, directly or through its handler`,
          traps.length >= 1 && traps.every((l) => handlerReports(text, l)));
    // The row is opened before anything that could exit. Read off the text after the `cd` — which
    // is where the key becomes knowable — rather than by line number, which goes stale.
    const body = text.slice(text.indexOf('cd "$(dirname "$0")"'));
    const opened = body.indexOf("\nprogress_start ");
    const firstExit = body.search(/\n(exit [0-9$]|[a-z_]+ \|\| exit)/);
    check(`and ${name} opens the row before the first line that could exit under it`,
          opened > 0 && (firstExit < 0 || opened < firstExit));
    // A `trap - EXIT` in column one puts the script back to having no handler at all, which is the
    // same window again. build.sh had one, between the signing probe and `cleanup_build`.
    check(`and nothing in ${name} takes the EXIT trap off again at the top level`,
          !/^trap - EXIT$/m.test(text));
}

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
// One region does call one of them — the signing probe's EXIT handler, which is the only way to
// close the window that region contains — and it does it behind `declare -F`, so the lifted shell
// takes the branch that does nothing. Every *other* mention is trespass.
const guarded = 'if declare -F clawdline_run_file_exit >/dev/null 2>&1; then clawdline_run_file_exit "$status" || true; fi';
const trespassing = liftedRegions.filter((r) => r.text.split("\n")
    .some((l) => /clawdline_run_file_|progress_(start|phase|finish|clear)/.test(l) && !/^\s*#/.test(l) && l.trim() !== guarded)).map((r) => r.label);
check(`none of the ${liftedRegions.length} regions build.sh hands to Tests/keychain-rebuild-focused.mjs calls these functions unguarded`,
      liftedRegions.length >= 3 && liftedRegions.every((r) => r.text !== "") && trespassing.length === 0);
// The one place a call *is* inside a lifted region is the suite lock's cleanup in `test.sh`, and it
// is guarded, because `Tests/test-sh-lock.mjs` runs that block on its own.
const lockBlock = script.slice(script.indexOf("# >>> clawdline suite lock >>>"), script.indexOf("# <<< clawdline suite lock <<<"));
const lockCalls = lockBlock.split("\n").filter((l) => /clawdline_run_file_|progress_(start|phase|finish|clear)/.test(l) && !/^\s*#/.test(l));
check("and the one call inside test.sh's suite-lock block asks whether the function is there first",
      lockCalls.length === 1
        && /if declare -F clawdline_run_file_exit >\/dev\/null 2>&1; then clawdline_run_file_exit "\$status" \|\| true; fi/.test(lockCalls[0]));

check("test.sh runs this file", /^node Tests\/run-file-producer\.mjs$/m.test(script));
// The helper's own suite, which owns everything this file stopped asking when the block moved.
check("and the suite that drives the helper both scripts source",
      /^node Tests\/progress-helper\.mjs$/m.test(script));
// The other half of the feature, the web app's reader. Registering it here is what makes a checkout
// that has only one of the two say so, instead of passing quietly.
check("and the web app's half of it", /^node Tests\/web-run-progress\.mjs$/m.test(script));

// ---------------------------------------------------------------------------------------------
// Running the scripts' own lines. Everything below drives the exact `progress_start` call one of
// the two scripts makes, under the exact EXIT composition `test.sh` installs, against a scratch
// cache — so what is being proved is those lines and not this file's memory of them.

// `realpathSync` because `/var` is a symlink to `/private/var` on this Mac: a harness started in
// the first reports the second as `$PWD`, and a key built from the path node handed out would look
// for a file nothing ever wrote. That is the harness's bug to avoid, not the script's.
const scratch = realpathSync(mkdtempSync(join(tmpdir(), "clawdline-run-file-")));
const cache = join(scratch, "statusline-cache");
const realCache = join(homedir(), ".claude", "statusline-cache");
const realCacheBefore = existsSync(realCache)
    ? readdirSync(realCache).filter((n) => n.startsWith("run-")).sort().join("\n")
    : "(no directory)";

const keyFor = (dir) => `run-${dir.replace(/\//g, "-")}.json`;
const rowOf = (path) => {
    if (!existsSync(path)) return null;
    try {
        return JSON.parse(readFileSync(path, "utf8"));
    } catch (e) {
        return { unparseable: String(e) };
    }
};

// The harness is one script's own opening lines plus test.sh's own composition. The source line is
// the checkout's relative path in the real script and an absolute one here, because the harness
// runs somewhere else; the substitution is asserted rather than assumed, since a `.replace` that
// matched nothing would leave every check below testing the absence of a helper.
const harness = (name, startLine, body) => {
    const dir = join(scratch, name);
    mkdirSync(dir, { recursive: true });
    const path = join(dir, "harness.sh");
    writeFileSync(path, [
        "#!/bin/bash",
        "set -euo pipefail",
        `. ${JSON.stringify(helperPath)}`,
        startLine,
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

const run = (h, env = {}) => {
    const r = spawnSync("/bin/bash", [h.path], {
        encoding: "utf8",
        cwd: h.dir,
        env: { ...process.env, CLAWDLINE_STATUS_DIR: cache, ...env },
    });
    return { code: r.status, out: r.stdout ?? "", err: r.stderr ?? "" };
};

try {
    // 1. test.sh's own opening line, run: the label it gives itself and the measurement it passes
    //    reach the file, under the key its working directory makes.
    {
        const h = harness("test-sh-start", testStart, "progress_phase guards\n");
        const r = run(h);
        const row = rowOf(h.file);
        check("test.sh's own opening line leaves state ok under run-<cwd with slashes as dashes>.json",
              r.code === 0 && row !== null && row.state === "ok");
        check("and the label a reader draws is the one test.sh gave itself",
              row !== null && row.label === "test");
        check("and the 288 seconds it passed reach the file as typical_seconds",
              row !== null && row.typical_seconds === 288);
    }

    // 2. build.sh's, which passes no measurement at all.
    {
        const h = harness("build-sh-start", buildStart, "progress_phase preparing\n");
        run(h);
        const row = rowOf(h.file);
        check("build.sh's own opening line writes label build", row !== null && row.label === "build");
        check("and no typical_seconds at all, rather than a number nobody measured",
              row !== null && row.typical_seconds === undefined);
    }

    // 3. **The window that used to be open.** No ERR trap ever sees a deliberate `exit`, and both
    //    scripts have guards that end that way above the handler they compose — test.sh's
    //    trailing-comma scan and browser-contract roster, four of build.sh's `exit 1`s. Driven
    //    through test.sh's real composition rather than read off the text.
    {
        const h = harness("early-exit", testStart, [
            "progress_phase guards",
            '# The shape of both guards above test.sh\'s lock: say what is wrong, exit on a number.',
            'echo "trailing comma before ) — Swift 6.1 syntax" >&2',
            "exit 1",
        ].join("\n"));
        const r = run(h);
        check("a guard that exits on a number leaves state fail, which no ERR trap would have seen",
              r.code === 1 && (rowOf(h.file) || {}).state === "fail");
    }

    // 4. The composed cleanup is a superset and not a replacement: it still runs its own work.
    {
        const h = harness("composition", testStart, [
            "progress_phase compiling",
            "/usr/bin/false",
        ].join("\n"));
        const r = run(h);
        check("a command that fails under the composed cleanup leaves fail and the run's own status",
              r.code === 1 && (rowOf(h.file) || {}).state === "fail");
    }

    // 5. The two narrow modes of `test.sh` run nothing and must therefore say nothing. This is the
    //    one scenario that runs the real script rather than a lifted line — both modes exit above
    //    the source line, compile nothing and take no lock, so it costs a few milliseconds.
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
