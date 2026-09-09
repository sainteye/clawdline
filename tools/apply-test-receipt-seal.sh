#!/bin/bash
# Apply one complete CLAWDLINE_TEST_SEAL receipt from a retained green log.
set -euo pipefail

if [ "$#" -ne 1 ] || [ ! -f "$1" ] || [ ! -r "$1" ]; then
  echo "usage: tools/apply-test-receipt-seal.sh <complete-retained-log>" >&2
  exit 2
fi

script_dir=$(cd "$(dirname "$0")" && pwd)
repository_dir=$(cd "$script_dir/.." && pwd)
target=${CLAWDLINE_SEAL_TARGET:-$repository_dir/test.sh}

node - "$1" "$target" <<'NODE'
const fs = require("node:fs");
const crypto = require("node:crypto");
const path = require("node:path");

const [logPath, targetPath] = process.argv.slice(2);
const fail = (message) => { process.stderr.write(`receipt seal: ${message}\n`); process.exit(125); };
let log;
try { log = fs.readFileSync(logPath, "utf8"); } catch { fail("retained log is unreadable"); }
if (/^[0-9]+ of [0-9]+ checks failed:/m.test(log) || /^the suite exited /m.test(log)
    || /Fatal error:/.test(log)) {
  fail("the retained log is red; no seal was changed");
}
const lines = log.split(/\r?\n/).filter((line) => line.startsWith("CLAWDLINE_TEST_SEAL "));
if (lines.length !== 1) fail(`expected one complete CLAWDLINE_TEST_SEAL line, found ${lines.length}`);
let receipt;
try { receipt = JSON.parse(lines[0].slice("CLAWDLINE_TEST_SEAL ".length)); }
catch { fail("the complete seal receipt is malformed JSON"); }
const wantedKeys = ["assertion_sites", "cloud_receipt", "outcome", "swift_receipt", "version"];
if (!receipt || typeof receipt !== "object" || Array.isArray(receipt)
    || JSON.stringify(Object.keys(receipt).sort()) !== JSON.stringify(wantedKeys)) {
  fail("the complete seal receipt has missing or unknown fields");
}
if (receipt.version !== 1 || receipt.outcome !== "passed"
    || !Number.isSafeInteger(receipt.assertion_sites) || receipt.assertion_sites <= 0
    || !/^[1-9][0-9]* checks passed$/.test(receipt.swift_receipt)
    || typeof receipt.cloud_receipt !== "string") {
  fail("the complete seal receipt has an invalid value");
}
const ordinaryLines = log.split(/\r?\n/).filter((line) => !line.startsWith("CLAWDLINE_TEST_SEAL "));
const allSwiftReceipts = ordinaryLines.filter((line) => /^[0-9]+ checks passed$/.test(line));
const allCloudReceipts = ordinaryLines.filter((line) => line.startsWith("CLAWDLINE_CLOUD_TESTS_COMPLETE "));
if (allSwiftReceipts.length !== 1 || allCloudReceipts.length !== 1
    || allSwiftReceipts[0] !== receipt.swift_receipt
    || allCloudReceipts[0] !== receipt.cloud_receipt) {
  fail("the tuple is not backed by exactly one Swift and Cloud receipt in this retained log");
}
if (log.trimEnd().split(/\r?\n/).at(-1) !== lines[0]) {
  fail("the complete seal tuple is not the retained log's final line");
}
const cloud = /^CLAWDLINE_CLOUD_TESTS_COMPLETE v=1 suite_count=([0-9]+) suites=(.+)$/
  .exec(receipt.cloud_receipt);
if (!cloud) fail("the Cloud completion receipt is absent or malformed");
const expectedSuites = ["CloudEnvelope", "CloudAccount", "CloudTransport", "CloudAppBridge",
  "CloudSettings", "ScheduleResume", "CloudClock", "CloudCanonicalJSON",
  "CloudCommandLedger", "CloudOutboundSpool", "CloudPairing", "CloudLifecycle"];
const pairs = cloud[2].split(",");
const names = pairs.map((pair) => /^([A-Za-z][A-Za-z0-9]*):([1-9][0-9]*)$/.exec(pair));
if (names.some((entry) => !entry)
    || Number(cloud[1]) !== pairs.length
    || JSON.stringify(names.map((entry) => entry[1])) !== JSON.stringify(expectedSuites)) {
  fail("the Cloud completion receipt omits, duplicates, reorders, or malforms a suite field");
}

let target;
try { target = fs.readFileSync(targetPath, "utf8"); } catch { fail("seal target is unreadable"); }
const replacements = [
  [/^expected_cloud_receipt='[^'\n]*'$/gm, `expected_cloud_receipt='${receipt.cloud_receipt}'`],
  [/^expected_swift_receipt='[^'\n]*'$/gm, `expected_swift_receipt='${receipt.swift_receipt}'`],
  [/^expected_swift_receipt_witness=[0-9]+$/gm,
    `expected_swift_receipt_witness=${receipt.assertion_sites}`],
];
for (const [pattern, replacement] of replacements) {
  const matches = target.match(pattern) ?? [];
  if (matches.length !== 1) fail(`seal target has ${matches.length} matches for ${pattern.source}`);
  target = target.replace(pattern, replacement);
}
const directory = path.dirname(targetPath);
const temporary = path.join(directory, `.${path.basename(targetPath)}.seal-${process.pid}`);
try {
  const mode = fs.statSync(targetPath).mode;
  const fd = fs.openSync(temporary, "wx", mode);
  try { fs.writeFileSync(fd, target, "utf8"); fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
  fs.renameSync(temporary, targetPath);
  const directoryFD = fs.openSync(directory, "r");
  try { fs.fsyncSync(directoryFD); } finally { fs.closeSync(directoryFD); }
} catch (error) {
  try { fs.unlinkSync(temporary); } catch {}
  fail(`atomic seal write failed: ${error.code ?? error.message}`);
}
const digest = crypto.createHash("sha256").update(lines[0] + "\n").digest("hex");
process.stdout.write(`receipt seal: applied complete tuple sha256=${digest}\n`);
NODE
