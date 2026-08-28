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

import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, chmodSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

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

if (!block) {
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
    const rewritten = block.replace(/"\$BIN" Resources\/mascots/, '"$BIN"');
    check("the guard rewrote the suite invocation to its stand-in", rewritten !== block);
    const harness = join(dir, "harness.sh");
    writeFileSync(harness, [
        "#!/bin/bash",
        "set -euo pipefail",
        `BIN=${JSON.stringify(crasher)}`,
        `STORE=${JSON.stringify(dir)}`,
        `LOG=${JSON.stringify(log)}`,
        `CLAWDLINE_REMOTE_DIR=""`,
        rewritten,
        'echo "reached the end without exiting"',
        "",
    ].join("\n"));
    chmodSync(harness, 0o755);
    // stderr is kept **and shown**. The previous version captured it and never printed it, and the
    // check standing over that could not fail: the healthy path alone puts 34 bytes on stdout, so
    // `(err + out).length > 0` was true in every situation anyone tried. Measured cost: a harness
    // broken by an undefined variable printed three ✗ and never the words `unbound variable`,
    // which were sitting in `err` the whole time.
    let out = "", err = "", exitCode = 0;
    try {
        out = execFileSync("/bin/bash", [harness], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
    } catch (e) {
        out = e.stdout ?? ""; err = e.stderr ?? ""; exitCode = e.status ?? -1;
    }

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
    const harness2 = join(dir, "harness2.sh");
    writeFileSync(harness2, [
        "#!/bin/bash",
        "set -euo pipefail",
        `BIN=${JSON.stringify(passer)}`,
        `STORE=${JSON.stringify(dir)}`,
        `LOG=${JSON.stringify(unwritable)}`,
        `CLAWDLINE_REMOTE_DIR=""`,
        rewritten,
        'echo "reached the end without exiting"',
        "",
    ].join("\n"));
    chmodSync(harness2, 0o755);
    let code2 = 0, out2 = "", err2 = "";
    try {
        out2 = execFileSync("/bin/bash", [harness2], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
    } catch (e) { out2 = e.stdout ?? ""; err2 = e.stderr ?? ""; code2 = e.status ?? -1; }
    check("a log that cannot be written ends the run on its own number, not the suite's",
          code2 === 126 && !out2.includes("reached the end"));
    check("and it says the suite itself passed, so nobody hunts a failure that did not happen",
          /passed/.test(err2));

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
