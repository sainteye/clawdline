// What `./test.sh` prints has to survive the run that dies.
//
// The suite used to be captured into a shell variable and echoed after the process returned, so a
// crash mid-suite — measured on this machine as `exit 133`, SIGTRAP, under concurrent runs — took
// every line with it. The run that needed reading was the only one that arrived empty, which is
// why the trap went months without a location: nobody was ever looking at its output, because
// there wasn't any.
//
// Two things are checked here, and the first is the one a future edit would undo.

import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, chmodSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

let failures = 0;
const check = (what, ok) => {
    console.log(`  ${ok ? "✓" : "✗"} ${what}`);
    if (!ok) failures += 1;
};

const script = readFileSync(new URL("../test.sh", import.meta.url), "utf8");

// 1. The shape itself. `out=$(…)` is the construct that loses a crashed run's output, and reading
//    the pipeline's members one at a time is the one that reads a `PIPESTATUS` some earlier
//    assignment has already replaced — both were live defects here, both look fine in review.
check("the suite is streamed rather than captured and echoed at the end",
      !/\bout=\$\(/.test(script) && /\|\s*tee\s+"\$LOG"/.test(script));
check("the pipeline's statuses are copied in one assignment",
      /pipe=\("\$\{PIPESTATUS\[@\]\}"\)/.test(script));

// 2. The behaviour, run for real: a command that prints and then dies of SIGTRAP, through the same
//    construct, must leave its output on disk and report the *command's* status rather than tee's.
const dir = mkdtempSync(join(tmpdir(), "clawdline-streaming-"));
const crasher = join(dir, "crasher.sh");
writeFileSync(crasher, `#!/bin/bash\necho "line before the crash"\necho "second line"\nkill -TRAP $$\n`);
chmodSync(crasher, 0o755);

const harness = join(dir, "harness.sh");
const log = join(dir, "suite.log");
writeFileSync(harness, `#!/bin/bash
set -euo pipefail
set +e
"${crasher}" 2>&1 | tee "${log}"
pipe=("\${PIPESTATUS[@]}")
status=\${pipe[0]}
tee_status=\${pipe[1]}
set -e
echo "status=$status tee_status=$tee_status"
`);
chmodSync(harness, 0o755);

const said = execFileSync("/bin/bash", [harness], { encoding: "utf8" });

check("the log holds what was printed before the crash",
      existsSync(log)
        && readFileSync(log, "utf8").includes("line before the crash")
        && readFileSync(log, "utf8").includes("second line"));
// 128 + SIGTRAP(5). The point is that it is the crashed command's number and not tee's 0.
check("the reported status is the command's, not tee's", said.includes("status=133"));
check("and tee's own status is read separately", said.includes("tee_status=0"));

console.log(failures === 0
    ? "test.sh streaming: all checks passed"
    : `test.sh streaming: ${failures} checks failed`);
process.exit(failures === 0 ? 0 : 1);
