#!/usr/bin/env node
// What `./test.sh` costs is written down in three places a contributor reads before they run
// anything, and until 2026-09-04 all three were wrong with nothing to say so: `CONTRIBUTING.md`
// promised "1567 checks, a couple of seconds" against a sealed 8,660 and a measured 288 s, and both
// READMEs promised seconds. The repo already has the pattern that would have caught it —
// `tools/generate-governance-table.sh` renders six numbers into `docs/architecture-refactor.md`
// from the values the architecture guard just computed, and the guard re-renders and compares on
// every run — but that mechanism covers one document and these three were not in it.
//
// This file is the cheap half of that pattern: no rendering, just the comparison. Each number a
// contributor reads is checked against the one place it is written.
//
// **The two numbers are not the same kind of number, and are not guarded the same way.**
// `docs/architecture-refactor.md` makes the distinction under "A ratchet and a threshold are not
// the same guard": zero headroom is right for a quantity that only moves by deliberate work and a
// trap for one that moves in the ordinary course.
//
//   * **The check count is a fact about this tree.** It moves only when somebody adds or removes an
//     assertion, it has exactly one home — `expected_swift_receipt` in `test.sh`, set from a run —
//     and every copy of it must equal that home exactly. So: equality, no headroom.
//   * **The wall time is a fact about a machine on a day.** It is 288 s on the Mac
//     `docs/suite-runtime.md` names and it will be something else on yours, so no document here
//     quotes a number for it at all — the three say "minutes rather than seconds" and point at the
//     measurement. What is guarded is the claim that sentence actually makes: that the measured
//     whole-run figure is still minutes. That is a threshold, and the derivation is the sentence.
//
// What this cannot catch, said out loud so a green is not read for more than it is: if the
// measurement in `docs/suite-runtime.md` itself goes stale — the suite grows to twenty minutes and
// nobody re-measures — every check below stays green. A guard whose source is a document can only
// keep the copies honest with the source; it cannot keep the source honest with the world.
import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = new URL("../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");

let checks = 0;
let failed = false;
function check(condition, message) {
  checks += 1;
  if (condition) return;
  failed = true;
  console.error(`FAIL: ${message}`);
}

// Anchored on the assignment, not on the digits: `test.sh` also carries
// `expected_swift_receipt_witness=6827` two lines below, and a looser pattern would sooner or later
// read the wrong one and be confidently green about it.
const sealMatch = /^expected_swift_receipt='(\d+) checks passed'$/m.exec(read("test.sh"));
check(sealMatch !== null, "test.sh: no `expected_swift_receipt='<n> checks passed'` line — the seal moved or was renamed, and every comparison below it is now vacuous");

// The row rather than the prose: `288` appears four times in that file and only one of them is the
// whole-run figure.
const runtimeMatch = /^\| whole run \| \*\*(\d+) s\*\* \|/m.exec(read("docs/suite-runtime.md"));
check(runtimeMatch !== null, "docs/suite-runtime.md: no `| whole run | **<n> s** |` row — the measurement moved or was reshaped, and the magnitude claim in the three documents has nothing behind it");

if (!sealMatch || !runtimeMatch) {
  console.log(`not ok: ${checks} suite-fact checks`);
  process.exit(1);
}

const sealedChecks = Number(sealMatch[1]);
const measuredSeconds = Number(runtimeMatch[1]);

check(
  measuredSeconds >= 60,
  `the measured whole run is ${measuredSeconds} s, under a minute — three documents say "minutes rather than seconds" and that is now false`,
);
check(
  measuredSeconds < 3600,
  `the measured whole run is ${measuredSeconds} s, over an hour — "minutes rather than seconds" understates it by an order of magnitude`,
);

// The command block, not the prose: both READMEs also mention `./test.sh` in a paragraph about
// claims and serialization, and neither of those mentions is quoting a cost.
function fencedCommandLines(text, command) {
  const found = [];
  let fenced = false;
  const lines = text.split("\n");
  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i];
    if (line.startsWith("```")) {
      fenced = !fenced;
      continue;
    }
    if (fenced && line.startsWith(command)) found.push({ text: line, line: i + 1 });
  }
  return found;
}

for (const path of ["CONTRIBUTING.md", "README.md", "README.zh-TW.md"]) {
  const text = read(path);
  const quoted = fencedCommandLines(text, "./test.sh");
  check(
    quoted.length === 1,
    `${path}: expected exactly one \`./test.sh\` line inside a code block, found ${quoted.length} — this guard reads that line and cannot choose between two`,
  );
  if (quoted.length !== 1) continue;

  // Every integer on that line, so a second number smuggled into the comment is a failure rather
  // than something the guard reads past. A line with no digits at all fails here too, which is the
  // exact state all three of these were in.
  const numbers = (quoted[0].text.match(/\d[\d,]*/g) || []).map((n) => Number(n.replace(/,/g, "")));
  check(
    numbers.length === 1 && numbers[0] === sealedChecks,
    `${path}:${quoted[0].line} quotes ${numbers.length === 0 ? "no number" : numbers.join(", ")} for \`./test.sh\`; ` +
      `the seal in test.sh says ${sealedChecks}. The line reads: ${JSON.stringify(quoted[0].text.trim())}`,
  );

  check(
    text.includes("(docs/suite-runtime.md)"),
    `${path} does not link to docs/suite-runtime.md — it makes a claim about how long the suite takes and no longer says where that was measured`,
  );
}

const runtimeDoc = fileURLToPath(new URL("docs/suite-runtime.md", root));
check(existsSync(runtimeDoc), "docs/suite-runtime.md does not exist — three documents link to it");

console.log(`${failed ? "not ok" : "ok"}: ${checks} suite-fact checks against seal ${sealedChecks} and a measured ${measuredSeconds} s`);
if (failed) process.exit(1);
