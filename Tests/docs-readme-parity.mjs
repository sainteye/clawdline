#!/usr/bin/env node
// The two READMEs are translations of one document, and until now nothing compared their shape.
// `Tests/docs-ui-labels.mjs` pins eleven strings by hand, which catches a pinned sentence being
// deleted and nothing else. Measured on 2026-09-04 against five real drifts, four of them went
// green: a section deleted from the English side, a section deleted from the Chinese side, a whole
// section added to one side only, both sides changed together, and a download URL rewritten in one
// of them.
//
// This file closes the three that are structural. It compares the *sequence of heading levels* —
// count and order together — because the heading text is in two different languages and cannot be
// compared directly. Today both files are one-to-one: 1 `# `, 21 `## `, 6 `### `, 28 in all,
// counted with `grep -cE '^#{1,3} '`.
//
// What it deliberately does not catch, so that nobody reads more into a green than is there:
//
//   * **Both files changed the same wrong way.** A guard that compares two copies can never catch
//     an error made in both of them; `macOS 13` becoming `macOS 26` in both files is green here and
//     always will be. That needs a guard with a source outside the two documents.
//   * **A section renamed on one side.** `## Troubleshooting` becoming `## Debugging` leaves the
//     level sequence identical, so this file stays green while the two documents have drifted
//     apart in meaning. Nothing about heading *text* is comparable across languages, so this is a
//     limit of the approach and not an oversight.
//   * **Anything below `### `.** `#### ` and deeper are not compared, matching the range the
//     inventory measured.
import { readFileSync } from "node:fs";

const root = new URL("../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");

// Headings inside a fenced block are shell comments, not sections. There are none in either file
// today (measured: 0 in both), but a snippet whose first line is `# install` would otherwise be
// read as an H1 and turn this guard red for a reason that has nothing to do with drift.
function headings(text) {
  const found = [];
  let fenced = false;
  const lines = text.split("\n");
  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i];
    if (line.startsWith("```")) {
      fenced = !fenced;
      continue;
    }
    if (fenced) continue;
    const match = /^(#{1,3}) (.*)$/.exec(line);
    if (match) found.push({ level: match[1].length, text: match[2].trim(), line: i + 1 });
  }
  return found;
}

let checks = 0;
let failed = false;
function check(condition, message) {
  checks += 1;
  if (condition) return;
  failed = true;
  console.error(`FAIL: ${message}`);
}

const english = headings(read("README.md"));
const chinese = headings(read("README.zh-TW.md"));

// A guard that scanned nothing would pass every comparison below it. Zero headings means the
// parser stopped matching this document, not that the document is fine.
check(english.length > 0, "README.md: no `#`..`###` headings were found at all — the parser, not the file, is what changed");
check(chinese.length > 0, "README.zh-TW.md: no `#`..`###` headings were found at all — the parser, not the file, is what changed");

const shape = (list) => list.map((h) => "#".repeat(h.level)).join(" ");
const describe = (h) => (h ? `${"#".repeat(h.level)} ${h.text} (line ${h.line})` : "— nothing here —");

check(
  english.length === chinese.length,
  `the two READMEs have a different number of headings: README.md has ${english.length}, README.zh-TW.md has ${chinese.length}. ` +
    "One of them gained or lost a section the other did not.",
);

for (const level of [1, 2, 3]) {
  const inEnglish = english.filter((h) => h.level === level).length;
  const inChinese = chinese.filter((h) => h.level === level).length;
  check(
    inEnglish === inChinese,
    `heading level ${"#".repeat(level)} differs: README.md has ${inEnglish}, README.zh-TW.md has ${inChinese}`,
  );
}

check(
  shape(english) === shape(chinese),
  "the two READMEs no longer have the same heading sequence — same count is not the same shape",
);

// One report, naming where the drift is. A bare "they differ" costs the reader the whole of the
// search this guard was supposed to have done for them — and the first index at which the *levels*
// differ is usually not it: delete one `## ` from a run of eleven and the levels only stop lining
// up at the very last heading. So the comparison is reported over runs of equal level, which is
// where a missing or added section actually shows up, with both sides' headings for that run.
function runs(list) {
  const grouped = [];
  for (const heading of list) {
    const last = grouped[grouped.length - 1];
    if (last && last.level === heading.level) last.headings.push(heading);
    else grouped.push({ level: heading.level, headings: [heading] });
  }
  return grouped;
}

if (shape(english) !== shape(chinese) || english.length !== chinese.length) {
  const a = runs(english);
  const b = runs(chinese);
  const limit = Math.max(a.length, b.length);
  for (let i = 0; i < limit; i += 1) {
    const left = a[i];
    const right = b[i];
    if (left && right && left.level === right.level && left.headings.length === right.headings.length) continue;
    console.error(`  first run of headings that does not line up (run #${i + 1} of ${a.length}/${b.length}):`);
    for (const [label, run] of [["README.md      ", left], ["README.zh-TW.md", right]]) {
      if (!run) {
        console.error(`    ${label} — this file has no run #${i + 1} —`);
        continue;
      }
      console.error(`    ${label} ${"#".repeat(run.level)} x${run.headings.length}`);
      for (const heading of run.headings) console.error(`      ${describe(heading)}`);
    }
    break;
  }
}

console.log(`${failed ? "not ok" : "ok"}: ${checks} README structural-parity checks over ${english.length}/${chinese.length} headings`);
if (failed) process.exit(1);
