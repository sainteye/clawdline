// `./test.sh` has to keep what it printed when the run dies, and report the binary's status.
//
// **This runs test.sh's own lines.** The first version of this file did not: it wrote out a fresh
// harness containing the construct it wanted to prove, and checked that. Those assertions passed
// against the parent commit, and passed with `test.sh` replaced by a comment and `exit 0` — they
// were testing bash, which needs no test here. An independent review measured that and said so.
//
// So the block is lifted out of `test.sh` by content and executed with a crashing stand-in for the
// binary. If somebody puts the capture-and-echo back, or reads `PIPESTATUS` a member at a time,
// this goes red for the reason it claims to.
//
// **And the shell it runs in has test.sh's traps in it**, which for a year it did not. The lifted
// block was executed in a bare `set -euo pipefail` shell, so the one thing the `set +e` window is
// most exposed to was the one thing this file was structurally unable to see: `set +e` turns off
// errexit and leaves the ERR trap armed, the handler ends in `exit`, and every line after the
// pipeline — the path of the log, the receipt-direction report, the whole `exit 126` branch for a
// `tee` that could not write — became unreachable on exactly the runs they exist for. A guard that
// cannot go red is worse than no guard, so the harness sources the same
// `Resources/clawdline-progress.sh` `./test.sh` does and opens the row with test.sh's own
// `progress_start` line, and therefore runs under the same four traps.

import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readdirSync, readFileSync, writeFileSync, chmodSync, existsSync, rmSync }
    from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
// `fileURLToPath`, not `URL.pathname`: this checkout lives under `Application Support`, and a
// percent-encoded space is a path bash cannot open.
import { fileURLToPath } from "node:url";

let failures = 0;
const check = (what, ok) => {
    console.log(`  ${ok ? "✓" : "✗"} ${what}`);
    if (!ok) failures += 1;
};

const script = readFileSync(new URL("../test.sh", import.meta.url), "utf8");
const lines = script.split("\n");
// Comments are not the program. The old shape is *named* in the prose above the block, as the
// thing that was tried and measured — a check that greps the whole file calls that a relapse.
const code = lines.filter(l => !/^\s*#/.test(l));

// Locate by content and take the block **through the branches that act on the status**, not just
// to the `set -e`. A review measured what stopping early cost: with only the pipeline extracted,
// deleting test.sh's `if [ "$status" -ne 0 ]; then exit "$status"` left all five checks green —
// and that test.sh reports a red suite as `exit 0`. The guard has to run the deciding lines, or it
// guards the plumbing and not the promise.
const first = code.findIndex(l => /\|\s*tee\s+"\$LOG"/.test(l));
const opened = first < 0 ? -1 : code.slice(0, first).map(l => l.trim()).lastIndexOf("set +e");
// The end is the last `fi` of the run of status branches that follows the pipeline, found by
// walking forward while the lines are still part of that decision.
let closed = -1;
if (first >= 0) {
    for (let i = first; i < code.length; i += 1) {
        if (/^\s*(expected_cloud_receipt|LOG)=/.test(code[i])) break;
        if (code[i].trim() === "fi") closed = i;
    }
}
const block = opened >= 0 && closed > first ? code.slice(opened, closed + 1).join("\n") : "";
check("test.sh runs the suite through tee and acts on the status in one block",
      first >= 0 && opened >= 0 && closed > first
        && /exit "\$status"/.test(block) && /exit 126/.test(block));
check("and no live line captures the whole run into a variable first",
      !code.some(l => /\bout=\$\(/.test(l)));

// The traps the block actually runs under. They live in `Resources/clawdline-progress.sh` now, so
// the harness sources that file and opens the row with test.sh's own line, both taken from the
// checkout rather than retyped. This is the whole of the repair to this file: without them the
// harness below is a shell in which the regression being guarded against cannot happen.
const helperPath = fileURLToPath(new URL("../Resources/clawdline-progress.sh", import.meta.url));
const startLine = (/^progress_start .*$/m.exec(script) || [""])[0];
const runFileBlock = existsSync(helperPath) && startLine !== ""
    ? `. ${JSON.stringify(helperPath)}\n${startLine}` : "";
check("the guard found the helper test.sh installs its traps through, and the line that arms it",
      runFileBlock !== "");
// Asked of the helper's text as well as driven, because "the harness has the trap in it" is the
// premise of every behavioural check below and a silently empty lift would make them all pass
// again. The traps are indented, because `progress_start` is a function.
check("and that helper arms an ERR trap, so this harness is not a shell without one",
      existsSync(helperPath)
        && /^\s*trap 'clawdline_run_file_signal "\$\?"' ERR$/m.test(readFileSync(helperPath, "utf8")));

// `set +e` and `trap - ERR` are one switch that bash does not couple. The pairing is asserted on
// the lifted block, so a disarm that drifts above `set +e` — where this file would never execute
// it — is a red check rather than a harness quietly proving nothing.
check("the block turns the ERR trap off with errexit and puts it back with it",
      /set \+e\ntrap - ERR\n/.test(block) && /\nset -e\ntrap 'clawdline_run_file_signal "\$\?"' ERR\n/.test(block));

if (!block || !runFileBlock) {
    console.log("test.sh streaming: cannot find the block to run; the rest is not checked");
    process.exit(1);
}

const dir = mkdtempSync(join(tmpdir(), "clawdline-streaming-"));
try {
    // It writes to **both** streams before dying. A crashing suite says why it died on stderr, and
    // that only reaches the log because of the `2>&1` in test.sh's pipeline — remove it and the one
    // line naming the cause is gone while every check here still passed, which is what a review
    // measured. A stand-in that only writes stdout cannot notice that.
    const crasher = join(dir, "crasher.sh");
    writeFileSync(crasher, "#!/bin/bash\necho \"line before the crash\"\n"
        + "echo \"Fatal error: the stand-in died\" >&2\necho \"second line\"\nkill -TRAP $$\n");
    chmodSync(crasher, 0o755);
    const log = join(dir, "suite.log");
    // The block refers to $BIN, $STORE and $LOG; give it a binary that dies the way one did. The
    // substitution is checked rather than assumed: if test.sh's invocation changes shape, an
    // unchecked `.replace` quietly does nothing and every assertion below then tests the real
    // binary's absence instead of the block. Silent wrong-thing-tested is the failure this whole
    // file exists to prevent.
    // The lifted block calls `report_receipt_direction`, defined far above it in test.sh. A lifted
    // block carries no definitions, so the harness supplies it the way it supplies $BIN and $LOG —
    // lifted, never retyped. Checked rather than assumed: under `set -e` a missing function ends the
    // run at 127, and the status assertion below would then read as a defect in the block instead of
    // in this harness. That is exactly how it presented when the call was first added.
    const directionStart = script.indexOf("# >>> clawdline receipt direction >>>");
    const directionEnd = script.indexOf("# <<< clawdline receipt direction <<<");
    check("the guard found the receipt-direction block the lifted block calls",
        directionStart >= 0 && directionEnd > directionStart);
    const receiptDirection = script.slice(directionStart, directionEnd)
        + "expected_swift_receipt='0 checks passed'\nexpected_cloud_receipt=''\n";
    const rewritten = block.replace(/"\$BIN" Resources\/mascots/, '"$BIN"');
    check("the guard rewrote the suite invocation to its stand-in", rewritten !== block);
    // One shape for every scenario, because the harness is now the thing under test as much as the
    // block is: the run-file block first, so the ERR, INT and TERM traps are armed exactly as they
    // are in `./test.sh` by the time the pipeline runs.
    //
    // `CLAWDLINE_STATUS_DIR` is not tidiness. Without it the lifted block resolves its directory
    // from `$HOME` and every run of this file would write a row into the real
    // `~/.claude/statusline-cache`, where a scratch harness would appear in the footer as a run of
    // this tree.
    const cache = join(dir, "statusline-cache");
    mkdirSync(cache, { recursive: true });
    const drive = (name, lines, blockText = rewritten) => {
        const path = join(dir, name);
        writeFileSync(path, ["#!/bin/bash", "set -euo pipefail", runFileBlock,
                             // The block writes nothing until something calls it, and test.sh has
                             // moved the phase four times before it reaches the pipeline.
                             "progress_phase 'running the suite'",
                             ...lines, blockText,
                             'echo "reached the end without exiting"', ""].join("\n"));
        chmodSync(path, 0o755);
        // stderr is kept **and shown**. A previous version captured it and never printed it, and
        // the check standing over that could not fail: the healthy path alone puts 34 bytes on
        // stdout, so `(err + out).length > 0` was true in every situation anyone tried. Measured
        // cost: a harness broken by an undefined variable printed three ✗ and never the words
        // `unbound variable`, which were sitting in `err` the whole time.
        try {
            return { out: execFileSync("/bin/bash", [path], {
                encoding: "utf8", stdio: ["ignore", "pipe", "pipe"],
                env: { ...process.env, CLAWDLINE_STATUS_DIR: cache },
            }), err: "", code: 0 };
        } catch (e) {
            return { out: e.stdout ?? "", err: e.stderr ?? "", code: e.status ?? -1 };
        }
    };

    const first = drive("harness.sh", [
        `BIN=${JSON.stringify(crasher)}`,
        `STORE=${JSON.stringify(dir)}`,
        `LOG=${JSON.stringify(log)}`,
        `CLAWDLINE_REMOTE_DIR=""`,
        receiptDirection,
    ]);
    const out = first.out, err = first.err, exitCode = first.code;
    // The premise, read back off the disk: the traps were live in that shell. A lift that produced
    // an empty string, or a block that stopped writing, would otherwise leave every check below
    // passing for the old reason — the shell without a trap in it.
    check("the harness really ran under test.sh's own run-file block",
          readdirSync(cache).some(n => n.startsWith("run-")));

    const kept = existsSync(log) ? readFileSync(log, "utf8") : "";
    check("what the run printed before it died is on disk",
          kept.includes("line before the crash") && kept.includes("second line"));
    // Not decoration: this is the only thing that notices `2>&1` leaving the pipeline, after which
    // a crashing suite's one line of explanation never reaches the log.
    check("and so is what it said on the way out", kept.includes("Fatal error: the stand-in died"));
    // The terminal and CI see it too. `| tee "$LOG" >/dev/null` keeps every check above green and
    // leaves both of them with nothing at all.
    check("and it reached the terminal, not only the file", out.includes("line before the crash"));
    // 128 + SIGTRAP(5), carried out of the block by test.sh's own `exit "$status"`. Delete that
    // branch and this is 0 — which is exactly how a red suite would be reported as green.
    check("the run's own status leaves the block", exitCode === 133 && !out.includes("reached the end"));
    // Not `err.length > 0`, which a review measured could only be false once `exitCode` was
    // already wrong, and which stayed green with test.sh's three `echo … >&2` lines deleted. What
    // is actually promised is that a failing run says *where its output went*, so that is what is
    // read back.
    check("and the run says where it left the record", err.includes(log));

    // Second scenario, for the branch the first cannot reach: a suite that passes, and a log that
    // cannot be written. That used to fall through to a receipt check reading the missing file and
    // report `125 missing receipt` about a green run, pointing at a path that does not exist.
    const passer = join(dir, "passer.sh");
    writeFileSync(passer, "#!/bin/bash\necho \"all good\"\n");
    chmodSync(passer, 0o755);
    const unwritable = join(dir, "no-such-directory", "suite.log");
    const second = drive("harness2.sh", [
        `BIN=${JSON.stringify(passer)}`,
        `STORE=${JSON.stringify(dir)}`,
        `LOG=${JSON.stringify(unwritable)}`,
        `CLAWDLINE_REMOTE_DIR=""`,
    ]);
    const out2 = second.out, err2 = second.err, code2 = second.code;
    check("a log that cannot be written ends the run on its own number, not the suite's",
          code2 === 126 && !out2.includes("reached the end"));
    check("and it says the suite itself passed, so nobody hunts a failure that did not happen",
          /passed/.test(err2));

    // **Positive control, and the reason this file was rewritten.** Both scenarios above are asked
    // again of a block with the `trap - ERR` taken out, which is what `56df2b6c` shipped. The ERR
    // trap is still armed inside the `set +e` window there, its handler ends in `exit "$status"`,
    // and so the pipeline's own failure leaves the block before any of the lines that act on it:
    // no path to the log, no receipt-direction report, and the `exit 126` branch unreachable.
    // Measured here rather than described, because a control that is only described is a claim.
    const mutant = rewritten.replace(/(^|\n)set \+e\ntrap - ERR\n/, "$1set +e\n");
    check("control: the guard can go red — a block that leaves the ERR trap armed fails it",
          mutant !== rewritten);
    const trapped = drive("harness-trapped.sh", [
        `BIN=${JSON.stringify(crasher)}`,
        `STORE=${JSON.stringify(dir)}`,
        `LOG=${JSON.stringify(join(dir, "trapped.log"))}`,
        `CLAWDLINE_REMOTE_DIR=""`,
        receiptDirection,
    ], mutant);
    check("control: with the trap armed, a red run never reaches the line naming its log",
          !trapped.err.includes(join(dir, "trapped.log")));
    const trapped2 = drive("harness-trapped2.sh", [
        `BIN=${JSON.stringify(passer)}`,
        `STORE=${JSON.stringify(dir)}`,
        `LOG=${JSON.stringify(unwritable)}`,
        `CLAWDLINE_REMOTE_DIR=""`,
    ], mutant);
    check("control: and a tee that cannot write reports the suite's status instead of 126",
          trapped2.code !== 126);

    // **Last, and covering both scenarios.** The previous version printed only the first
    // scenario's stderr, and printed it *before* the last check ran — so a review measured a decoy
    // that failed one check and produced no diagnosis at all. The claim in that commit message,
    // "prints it when anything fails", was true of half the file.
    if (failures > 0) {
        console.log("    crash scenario stderr:", JSON.stringify(err.slice(0, 400)));
        console.log("    tee-failure stderr:   ", JSON.stringify(err2.slice(0, 400)));
    }
} finally {
    rmSync(dir, { recursive: true, force: true });
}

console.log(failures === 0
    ? "test.sh streaming: all checks passed"
    : `test.sh streaming: ${failures} checks failed`);
process.exit(failures === 0 ? 0 : 1);
