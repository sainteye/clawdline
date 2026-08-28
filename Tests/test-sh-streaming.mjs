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

// Locate by content and take the whole guarded block, so the extraction breaks loudly if the shape
// changes rather than silently testing three unrelated lines.
const first = code.findIndex(l => /\|\s*tee\s+"\$LOG"/.test(l));
const opened = first < 0 ? -1 : code.slice(0, first).map(l => l.trim()).lastIndexOf("set +e");
const closed = first < 0 ? -1 : code.findIndex((l, i) => i > first && l.trim() === "set -e");
check("test.sh still runs the suite through tee inside a guarded block",
      first >= 0 && opened >= 0 && closed > first);
check("and no live line captures the whole run into a variable first",
      !code.some(l => /\bout=\$\(/.test(l)));

if (first < 0 || opened < 0 || closed <= first) {
    console.log("test.sh streaming: cannot find the block to run; the rest is not checked");
    process.exit(1);
}
const block = code.slice(opened, closed + 1).join("\n");

const dir = mkdtempSync(join(tmpdir(), "clawdline-streaming-"));
try {
    const crasher = join(dir, "crasher.sh");
    writeFileSync(crasher, "#!/bin/bash\necho \"line before the crash\"\necho \"second line\"\nkill -TRAP $$\n");
    chmodSync(crasher, 0o755);
    const log = join(dir, "suite.log");
    // The block refers to $BIN, $STORE and $LOG; give it a binary that dies the way one did.
    const harness = join(dir, "harness.sh");
    writeFileSync(harness, [
        "#!/bin/bash",
        "set -euo pipefail",
        "set +m",                      // no job-control "Trace/BPT trap" line in the suite's output
        `BIN=${JSON.stringify(crasher)}`,
        `STORE=${JSON.stringify(dir)}`,
        `LOG=${JSON.stringify(log)}`,
        `CLAWDLINE_REMOTE_DIR=""`,
        block.replace(/"\$BIN" Resources\/mascots/, '"$BIN"'),
        'echo "status=$status tee_status=$tee_status"',
        "",
    ].join("\n"));
    chmodSync(harness, 0o755);
    const said = execFileSync("/bin/bash", [harness], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] });

    const kept = existsSync(log) ? readFileSync(log, "utf8") : "";
    check("what the run printed before it died is on disk",
          kept.includes("line before the crash") && kept.includes("second line"));
    // 128 + SIGTRAP(5). The number has to be the crashed command's and not tee's 0.
    check("the reported status is the binary's, not tee's", said.includes("status=133"));
    check("and tee's own status is read separately", said.includes("tee_status=0"));
} finally {
    rmSync(dir, { recursive: true, force: true });
}

console.log(failures === 0
    ? "test.sh streaming: all checks passed"
    : `test.sh streaming: ${failures} checks failed`);
process.exit(failures === 0 ? 0 : 1);
