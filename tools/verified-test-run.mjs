#!/usr/bin/env node
import { createHash, randomUUID } from "node:crypto";
import { createWriteStream, mkdtempSync, readFileSync, realpathSync, rmSync } from "node:fs";
import { homedir, platform, arch, tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { spawn, spawnSync } from "node:child_process";
import http from "node:http";

const sha256 = (bytes) => createHash("sha256").update(bytes).digest("hex");
const frame = (parts) => {
  const hash = createHash("sha256");
  for (const part of parts) {
    const bytes = Buffer.from(part, "utf8");
    const length = Buffer.alloc(8);
    length.writeBigUInt64BE(BigInt(bytes.length));
    hash.update(length).update(bytes);
  }
  return hash.digest("hex");
};

export const canonicalRepositoryDigest = (identity) =>
  frame(["clawdline-verification-repository-v1", identity]);

export const canonicalCommandDigest = (argv) =>
  frame(["clawdline-verification-command-v1", ...argv]);

export const canonicalEnvironmentDigest = (facts) => {
  const keys = Object.keys(facts).sort();
  return frame(["clawdline-verification-environment-v1",
    ...keys.flatMap((key) => [key, facts[key] == null ? "<absent>" : String(facts[key])])]);
};

const git = (cwd, args, options = {}) => {
  const encoding = Object.prototype.hasOwnProperty.call(options, "encoding")
    ? options.encoding : "utf8";
  const result = spawnSync("git", args, { cwd, encoding,
    maxBuffer: 64 * 1024 * 1024 });
  if (result.status !== 0) throw new Error(`git_${args[0]}_failed`);
  return result.stdout;
};

const repositoryIdentity = (cwd, env) => {
  if (env.CLAWDLINE_VERIFICATION_REPOSITORY_ID) {
    return env.CLAWDLINE_VERIFICATION_REPOSITORY_ID;
  }
  const remote = spawnSync("git", ["config", "--get", "remote.origin.url"],
    { cwd, encoding: "utf8" });
  if (remote.status === 0 && remote.stdout.trim()) return remote.stdout.trim();
  const common = git(cwd, ["rev-parse", "--path-format=absolute", "--git-common-dir"]).trim();
  return realpathSync(common);
};

const overlayDigest = (cwd) => {
  const hash = createHash("sha256");
  hash.update(git(cwd, ["diff", "--binary", "--full-index", "--no-ext-diff", "HEAD"],
    { encoding: null }));
  const raw = git(cwd, ["ls-files", "--others", "--exclude-standard", "-z"],
    { encoding: null });
  const paths = raw.toString("utf8").split("\0").filter(Boolean)
    .sort((a, b) => Buffer.compare(Buffer.from(a), Buffer.from(b)));
  for (const path of paths) {
    hash.update(Buffer.from([0])).update(path).update(Buffer.from([0]));
    hash.update(readFileSync(join(cwd, path)));
  }
  return hash.digest("hex");
};

const artifactEnvironmentDigest = (cwd, env) => {
  const result = spawnSync("/bin/bash", ["-c",
    "set -euo pipefail\n. tools/swift-test-artifact.sh\nclawdline_swift_test_artifact_environment"],
  { cwd, env, encoding: "utf8", maxBuffer: 1024 * 1024 });
  if (result.status !== 0 || !/^[a-f0-9]{64}\n$/.test(result.stdout ?? "")) {
    // Never reserve, or answer reusable, with an absent/unknown inner compiler identity.
    // Do not leak paths or raw compiler output through the durable wrapper's diagnostics.
    throw new Error("artifact_preflight_identity_unavailable");
  }
  return result.stdout.trim();
};

const environmentFacts = (cwd, env) => ({
  architecture: arch(),
  node: process.version,
  platform: platform(),
  swift: spawnSync("swiftc", ["--version"], { env, encoding: "utf8" }).stdout?.trim() ?? "unavailable",
  CLAWDLINE_RESEAL: env.CLAWDLINE_RESEAL,
  CLAWDLINE_SUITE_JOBS: env.CLAWDLINE_SUITE_JOBS,
  CLAWDLINE_TEST_GROUPS: env.CLAWDLINE_TEST_GROUPS,
  ...(env.CLAWDLINE_SWIFT_TEST_ARTIFACT === "reuse"
    ? { swift_artifact_environment_sha256: artifactEnvironmentDigest(cwd, env) } : {}),
});

export const canonicalIdentity = (cwd, argv, env = process.env) => {
  const taskID = env.CLAWDLINE_VERIFICATION_TASK_ID || null;
  const forcedTree = env.CLAWDLINE_VERIFICATION_TREE_SHA;
  let subject;
  if (forcedTree) {
    if (!/^[0-9a-f]{40}$|^[0-9a-f]{64}$/.test(forcedTree)) throw new Error("invalid_tree_sha");
    subject = { kind: "commit_tree", tree_sha: forcedTree };
  } else {
    const dirty = git(cwd, ["status", "--porcelain=v1", "--untracked-files=all", "-z"],
      { encoding: null });
    if (dirty.length === 0) {
      subject = { kind: "commit_tree", tree_sha: git(cwd, ["rev-parse", "HEAD^{tree}"]).trim() };
    } else {
      if (!taskID) throw new Error("working_overlay_needs_task_id");
      subject = { kind: "working_overlay", base_commit: git(cwd, ["rev-parse", "HEAD"]).trim(),
        overlay_sha256: overlayDigest(cwd) };
    }
  }
  const kind = env.CLAWDLINE_TEST_GROUPS ? "focused" : "full";
  return {
    request_id: randomUUID(),
    repository_sha256: canonicalRepositoryDigest(repositoryIdentity(cwd, env)),
    ...(taskID ? { task_id: taskID } : {}), subject,
    question_id: env.CLAWDLINE_VERIFY_QUESTION_ID,
    verification_kind: kind, variant: "baseline",
    command_sha256: canonicalCommandDigest(argv),
    environment_sha256: canonicalEnvironmentDigest(environmentFacts(cwd, env)),
  };
};

const api = (path, token, method, object) => new Promise((resolvePromise, reject) => {
  const body = object == null ? null : Buffer.from(JSON.stringify(object));
  const request = http.request({ hostname: "127.0.0.1",
    port: Number(process.env.CLAWDLINE_PORT || 7717), path, method,
    headers: { "X-Clawdline-Orchestrator": token,
      ...(body ? { "Content-Type": "application/json", "Content-Length": body.length } : {}) } },
  (response) => {
    const chunks = [];
    response.on("data", (chunk) => chunks.push(chunk));
    response.on("end", () => {
      let value;
      try { value = JSON.parse(Buffer.concat(chunks).toString("utf8")); }
      catch { return reject(new Error(`ledger_http_${response.statusCode}_malformed`)); }
      resolvePromise({ status: response.statusCode, value });
    });
  });
  request.on("error", reject);
  if (body) request.write(body);
  request.end();
});

export const analyzeVerificationLog = (log, status, durationMS, kind, env) => {
  const lines = log.toString("utf8").split(/\r?\n/);
  const full = lines.filter((line) => /^[0-9]+ checks passed$/.test(line));
  const focused = lines.filter((line) => /^[0-9]+ focused checks passed$/.test(line));
  const failures = lines.map((line) => /^([0-9]+) of ([0-9]+) (?:focused )?checks failed:/.exec(line))
    .find(Boolean);
  const passed = status === 0
    ? Number((kind === "full" ? full[0] : focused[0])?.split(" ")[0] ?? 0)
    : failures ? Number(failures[2]) - Number(failures[1]) : 0;
  const failed = failures ? Number(failures[1]) : (status === 0 ? 0 : 1);
  const outcome = env.CLAWDLINE_VERIFICATION_INCONCLUSIVE_CODE
    ? "inconclusive_environment" : status === 0 ? "passed" : "failed";
  if (outcome === "passed") {
    const receipts = kind === "full" ? full : focused;
    const otherReceipts = kind === "full" ? focused : full;
    if (receipts.length !== 1 || otherReceipts.length !== 0 || failures
        || lines.some((line) => line.includes("Fatal error:"))) {
      throw new Error("passing_receipt_ambiguous");
    }
  }
  const result = { completion_id: randomUUID(), state: outcome, exit_status: status,
    checks_passed: passed, checks_failed: failed, duration_ms: durationMS,
    log_sha256: sha256(log) };
  if (outcome === "inconclusive_environment") {
    result.inconclusive_code = env.CLAWDLINE_VERIFICATION_INCONCLUSIVE_CODE;
  }
  if (kind === "full" && outcome === "passed") {
    const seals = lines.filter((line) => line.startsWith("CLAWDLINE_TEST_SEAL "));
    const clouds = lines.filter((line) => line.startsWith("CLAWDLINE_CLOUD_TESTS_COMPLETE "));
    if (full.length !== 1 || clouds.length !== 1 || seals.length !== 1) {
      throw new Error("full_receipt_ambiguous");
    }
    let seal;
    try { seal = JSON.parse(seals[0].slice("CLAWDLINE_TEST_SEAL ".length)); }
    catch { throw new Error("full_receipt_malformed"); }
    const keys = ["assertion_sites", "cloud_receipt", "outcome", "swift_receipt", "version"];
    if (!seal || typeof seal !== "object" || Array.isArray(seal)
        || JSON.stringify(Object.keys(seal).sort()) !== JSON.stringify(keys)
        || seal.version !== 1 || seal.outcome !== "passed"
        || seal.swift_receipt !== full[0] || seal.cloud_receipt !== clouds[0]
        || !Number.isSafeInteger(seal.assertion_sites) || seal.assertion_sites <= 0) {
      throw new Error("full_receipt_mismatch");
    }
    result.full_suite_receipt_sha256 = sha256(Buffer.from(seals[0] + "\n"));
  }
  return result;
};

async function main() {
  const cwd = process.cwd();
  const argv = [resolve(process.argv[2]), ...process.argv.slice(3)];
  if (!process.env.CLAWDLINE_VERIFY_QUESTION_ID) throw new Error("missing_question_id");
  const tokenPath = process.env.CLAWDLINE_ORCHESTRATOR_TOKEN_FILE
    || join(homedir(), ".config", "clawdline", "orchestrator-token");
  const token = readFileSync(tokenPath, "utf8").trim();
  if (!token) throw new Error("missing_orchestrator_token");
  const reservation = canonicalIdentity(cwd, argv, process.env);
  const reserved = await api("/v1/orchestrator/verification-runs/reserve", token, "POST", reservation);
  const decision = reserved.value.decision;
  if (decision === "reusable") {
    process.stdout.write(`verification receipt reusable: ${reserved.value.verificationRun.receiptId}\n`);
    return;
  }
  if (decision === "active") { process.stderr.write("verification receipt active in another Session\n"); process.exitCode = 75; return; }
  if (decision !== "run_required" || !reserved.value.verificationRun?.receiptId) {
    throw new Error(`ledger_reserve_${reserved.status}_${reserved.value.error ?? "refused"}`);
  }
  const receiptID = reserved.value.verificationRun.receiptId;
  const room = mkdtempSync(join(tmpdir(), "clawdline-verified-test-"));
  const logPath = join(room, "run.log");
  const output = createWriteStream(logPath, { flags: "wx", mode: 0o600 });
  const started = Date.now();
  const child = spawn("/bin/bash", ["-c", 'exec "$@" 2>&1', "verified-test", ...argv],
    { cwd, env: { ...process.env, CLAWDLINE_VERIFICATION_INNER: "1" }, stdio: ["inherit", "pipe", "inherit"] });
  const chunks = [];
  child.stdout.on("data", (chunk) => { chunks.push(chunk); output.write(chunk); process.stdout.write(chunk); });
  const status = await new Promise((resolveStatus, reject) => {
    child.on("error", reject); child.on("close", (code) => resolveStatus(code ?? 1));
  });
  await new Promise((resolveOutput) => output.end(resolveOutput));
  const log = Buffer.concat(chunks);
  let outcome;
  try { outcome = analyzeVerificationLog(log, status, Date.now() - started,
    reservation.verification_kind, process.env); }
  catch (error) {
    process.stderr.write(`verification receipt incomplete: ${error.message}; log kept at ${logPath}\n`);
    process.exitCode = status || 125;
    return;
  }
  const completed = await api(`/v1/orchestrator/verification-runs/${receiptID}/complete`, token,
    "POST", { reservation, outcome });
  if (![200, 201].includes(completed.status)) {
    process.stderr.write(`verification completion refused (${completed.status}); log kept at ${logPath}\n`);
    process.exitCode = status || 74;
    return;
  }
  rmSync(room, { recursive: true, force: true });
  process.stdout.write(`verification receipt completed: ${receiptID}\n`);
  process.exitCode = status;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => { process.stderr.write(`verified test run: ${error.message}\n`); process.exitCode = 74; });
}
