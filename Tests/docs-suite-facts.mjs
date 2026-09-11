#!/usr/bin/env node
// Contributor docs describe the suite's cost without copying a volatile executed-check total.
// The exact count belongs to each run receipt; source-controlled copies created a second full run
// whenever an assertion changed. This guard makes that low-value workflow unable to return.
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

const runtimeMatch = /^\| whole run \| \*\*(\d+) s\*\* \|/m.exec(read("docs/suite-runtime.md"));
check(runtimeMatch !== null, "docs/suite-runtime.md must retain a dated whole-run measurement");
if (runtimeMatch) {
  const seconds = Number(runtimeMatch[1]);
  check(seconds >= 60 && seconds < 3600,
    `the documented whole run is ${seconds} s, so the contributor wording 'minutes' is inaccurate`);
}

function fencedCommandLines(text, command) {
  const found = [];
  let fenced = false;
  for (const [index, line] of text.split("\n").entries()) {
    if (line.startsWith("```")) { fenced = !fenced; continue; }
    if (fenced && line.startsWith(command)) found.push({ text: line, line: index + 1 });
  }
  return found;
}

for (const path of ["CONTRIBUTING.md", "README.md", "README.zh-TW.md"]) {
  const text = read(path);
  const commands = fencedCommandLines(text, "./test.sh");
  check(commands.length === 1,
    `${path}: expected exactly one ./test.sh quick-start command, found ${commands.length}`);
  if (commands.length === 1) {
    check(!/\b\d[\d,]*\s+(?:checks?|個檢查)\b/i.test(commands[0].text),
      `${path}:${commands[0].line} copies a volatile check total; counts belong to run receipts`);
    check(/minutes|分鐘/.test(commands[0].text),
      `${path}:${commands[0].line} must warn that the release-candidate suite takes minutes`);
  }
  check(text.includes("(docs/suite-runtime.md)"),
    `${path} must link to docs/suite-runtime.md for the dated measurement`);
}

check(!/^expected_(?:swift|cloud)_receipt=/m.test(read("test.sh")),
  "test.sh must not restore source-controlled completion totals");
check(!/^expected_swift_receipt_witness=/m.test(read("test.sh")),
  "test.sh must not restore the assertion-site reseal gate");
check(!/if \[ "\$\{CLAWDLINE_RESEAL/.test(read("tools/check-architecture-boundaries.sh")),
  "the architecture guard must not require a reseal measurement before compile");

const runtimeDoc = fileURLToPath(new URL("docs/suite-runtime.md", root));
check(existsSync(runtimeDoc), "docs/suite-runtime.md does not exist");

console.log(`${failed ? "not ok" : "ok"}: ${checks} suite-fact checks without copied totals`);
if (failed) process.exit(1);
