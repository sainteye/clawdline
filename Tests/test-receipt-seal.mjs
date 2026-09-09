import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { analyzeVerificationLog, canonicalCommandDigest, canonicalEnvironmentDigest,
  canonicalRepositoryDigest }
  from "../tools/verified-test-run.mjs";

let checks = 0;
const check = (name, actual, expected = true) => {
  checks += 1;
  assert.deepEqual(actual, expected, name);
};
const directory = mkdtempSync(join(tmpdir(), "clawdline-receipt-seal-"));
const helper = fileURLToPath(new URL("../tools/apply-test-receipt-seal.sh", import.meta.url));
const runner = readFileSync(fileURLToPath(new URL("../test.sh", import.meta.url)), "utf8");
const cloud = "CLAWDLINE_CLOUD_TESTS_COMPLETE v=1 suite_count=12 suites="
  + "CloudEnvelope:1,CloudAccount:2,CloudTransport:3,CloudAppBridge:4,CloudSettings:5,"
  + "ScheduleResume:6,CloudClock:7,CloudCanonicalJSON:8,CloudCommandLedger:9,"
  + "CloudOutboundSpool:10,CloudPairing:11,CloudLifecycle:12";
const tuple = (overrides = {}) => "CLAWDLINE_TEST_SEAL " + JSON.stringify({
  version: 1, outcome: "passed", swift_receipt: "123 checks passed",
  assertion_sites: 99, cloud_receipt: cloud, ...overrides,
});
const completeLog = (seal = tuple()) => `123 checks passed\n${cloud}\n${seal}\n`;
const original = ["#!/bin/bash", "expected_cloud_receipt='old cloud'",
  "expected_swift_receipt='1 checks passed'", "expected_swift_receipt_witness=1", ""].join("\n");
const run = (name, log) => {
  const target = join(directory, `${name}.sh`);
  const logPath = join(directory, `${name}.log`);
  writeFileSync(target, original);
  writeFileSync(logPath, log);
  const result = spawnSync("bash", [helper, logPath], {
    encoding: "utf8", env: { ...process.env, CLAWDLINE_SEAL_TARGET: target },
  });
  return { ...result, target, bytes: readFileSync(target, "utf8") };
};

const good = run("good", completeLog());
check("a complete green tuple is accepted", good.status, 0);
check("the Cloud seal moves whole", good.bytes.includes(`expected_cloud_receipt='${cloud}'`));
check("the Swift seal moves from the same receipt", good.bytes.includes("expected_swift_receipt='123 checks passed'"));
check("the witness moves from the same receipt", good.bytes.includes("expected_swift_receipt_witness=99"));
check("the helper reports the retained tuple digest", /sha256=[0-9a-f]{64}/.test(good.stdout));

const incomplete = run("incomplete", "123 checks passed\n" + cloud + "\n");
check("a log without the combined receipt is refused", incomplete.status, 125);
check("an incomplete log changes no seal byte", incomplete.bytes, original);

const red = run("red", "1 of 123 checks failed:\n" + completeLog());
check("a red log cannot carry a forged green tuple", red.status, 125);
check("a red log changes no seal byte", red.bytes, original);

const missingCloudField = cloud.replace(",CloudLifecycle:12", "");
const omission = run("cloud-omission", `123 checks passed\n${missingCloudField}\n`
  + tuple({ cloud_receipt: missingCloudField }) + "\n");
check("the reproduced twelfth Cloud-field omission is refused", omission.status, 125);
check("a Cloud-field omission changes no seal byte", omission.bytes, original);

const reorderedCloud = cloud.replace("CloudEnvelope:1,CloudAccount:2",
  "CloudAccount:2,CloudEnvelope:1");
const reordered = run("cloud-reordered", `123 checks passed\n${reorderedCloud}\n`
  + tuple({ cloud_receipt: reorderedCloud }) + "\n");
check("a count-preserving Cloud roster reorder is refused", reordered.status, 125);
check("a reordered Cloud roster changes no seal byte", reordered.bytes, original);

const malformed = run("malformed", `123 checks passed\n${cloud}\nCLAWDLINE_TEST_SEAL {"version":1\n`);
check("malformed receipt JSON is refused", malformed.status, 125);
check("malformed receipt changes no seal byte", malformed.bytes, original);

const duplicate = run("duplicate", completeLog(tuple() + "\n" + tuple()));
check("two candidate tuples are ambiguous and refused", duplicate.status, 125);
check("ambiguous tuples change no seal byte", duplicate.bytes, original);

const twoSwift = run("two-swift", `456 checks passed\n${completeLog()}`);
check("two different Swift completion receipts are ambiguous and refused", twoSwift.status, 125);
check("different Swift receipts change no seal byte", twoSwift.bytes, original);

const otherCloud = cloud.replace("CloudEnvelope:1", "CloudEnvelope:99");
const twoCloud = run("two-cloud", `${otherCloud}\n${completeLog()}`);
check("two different Cloud completion receipts are ambiguous and refused", twoCloud.status, 125);
check("different Cloud receipts change no seal byte", twoCloud.bytes, original);

const badTarget = run("bad-target", completeLog());
writeFileSync(badTarget.target, original.replace("expected_swift_receipt_witness=1\n", ""));
const retry = spawnSync("bash", [helper, join(directory, "bad-target.log")], {
  encoding: "utf8", env: { ...process.env, CLAWDLINE_SEAL_TARGET: badTarget.target },
});
check("a target missing one of the three fields is refused", retry.status, 125);
check("preflight happens before the atomic write", readFileSync(badTarget.target, "utf8"),
      original.replace("expected_swift_receipt_witness=1\n", ""));

check("the runner defines unfiltered scope from the absence of focused groups",
  /is_unfiltered_test_run\(\) \{\n  \[ -z "\$\{CLAWDLINE_TEST_GROUPS:-\}" \]\n\}/.test(runner));
check("the full seal emitter and verifier share exactly two scope gates",
  [...runner.matchAll(/^if is_unfiltered_test_run; then$/gm)].length, 2);
check("the final full receipt check is inside the second scope gate",
  /if is_unfiltered_test_run; then\n  verify_test_completion_receipts "\$LOG"\nfi/.test(runner));
const emitterStart = runner.indexOf("emit_complete_test_seal_receipt() {");
const emitterEnd = runner.indexOf("\nis_unfiltered_test_run()", emitterStart);
const emitter = runner.slice(emitterStart, emitterEnd);
check("a sealed green run exposes its complete receipt after the internal log is removed",
  /printf '%s\\n' "\$seal" >> "\$log"\n  printf '%s\\n' "\$seal"/.test(emitter));
check("test.sh offers one opt-in durable preflight instead of an unwritten manual API",
  /CLAWDLINE_VERIFY_QUESTION_ID[\s\S]*exec node tools\/verified-test-run\.mjs/.test(runner));
check("repository identity uses one versioned canonical framing",
  canonicalRepositoryDigest("git@example/repo"), canonicalRepositoryDigest("git@example/repo"));
check("command digest preserves argv boundaries",
  canonicalCommandDigest(["ab", "c"]) !== canonicalCommandDigest(["a", "bc"]));
check("environment digest ignores insertion order",
  canonicalEnvironmentDigest({ b: "2", a: "1"}), canonicalEnvironmentDigest({ a: "1", b: "2"}));
check("environment digest distinguishes absent from empty",
  canonicalEnvironmentDigest({ a: undefined }) !== canonicalEnvironmentDigest({ a: "" }));
const durableOutcome = analyzeVerificationLog(Buffer.from(completeLog()), 0, 12, "full", {});
check("the outer wrapper can complete a passing full run from captured stdout",
  durableOutcome.full_suite_receipt_sha256,
  createHash("sha256").update(tuple() + "\n").digest("hex"));
check("the outer wrapper refuses a seal that disagrees with the captured completion lines",
  (() => {
    try {
      analyzeVerificationLog(Buffer.from(completeLog(tuple({ swift_receipt: "122 checks passed" }))),
        0, 12, "full", {});
      return false;
    } catch (error) { return error.message === "full_receipt_mismatch"; }
  })());
check("a zero exit without the expected scoped receipt cannot become passed evidence",
  (() => {
    try { analyzeVerificationLog(Buffer.from("no receipt\n"), 0, 12, "focused", {}); return false; }
    catch (error) { return error.message === "passing_receipt_ambiguous"; }
  })());

console.log(`${checks} receipt-seal checks passed`);
