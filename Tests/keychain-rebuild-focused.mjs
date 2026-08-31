#!/usr/bin/env node

import {
  chmodSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const lifecycleSource = resolve(process.env.CLAWDLINE_KEYCHAIN_LIFECYCLE_SOURCE ||
  "Sources/CloudBridgeLifecycle.swift");
const buildSource = resolve(process.env.CLAWDLINE_KEYCHAIN_BUILD_SOURCE || "build.sh");
const setupSource = resolve(process.env.CLAWDLINE_KEYCHAIN_SETUP_SOURCE ||
  "tools/setup-local-signing-identity.sh");
const work = mkdtempSync(join(process.env.TMPDIR || tmpdir(), "clawdline-keychain-focused-"));

let nodeChecks = 0;
function check(condition, name) {
  if (!condition) {
    process.stderr.write(`FAIL after ${nodeChecks} node checks: ${name}\n`);
    process.exit(1);
  }
  nodeChecks += 1;
  console.log(`✓ ${name}`);
}

function executable(path, body) {
  writeFileSync(path, body, "utf8");
  chmodSync(path, 0o755);
}

function declaration(source, name) {
  const start = source.indexOf(name);
  if (start < 0) throw new Error(`missing declaration: ${name}`);
  const opening = source.indexOf("{", start);
  let depth = 0;
  let state = "code";
  let blockDepth = 0;
  for (let index = opening; index < source.length; index += 1) {
    const here = source[index];
    const next = source[index + 1];
    if (state === "line") {
      if (here === "\n") state = "code";
      continue;
    }
    if (state === "block") {
      if (here === "/" && next === "*") { blockDepth += 1; index += 1; continue; }
      if (here === "*" && next === "/") {
        blockDepth -= 1; index += 1;
        if (blockDepth === 0) state = "code";
      }
      continue;
    }
    if (state === "string") {
      if (here === "\\") { index += 1; continue; }
      if (here === '"') state = "code";
      continue;
    }
    if (here === "/" && next === "/") { state = "line"; index += 1; continue; }
    if (here === "/" && next === "*") {
      state = "block"; blockDepth = 1; index += 1; continue;
    }
    if (here === '"') { state = "string"; continue; }
    if (here === "{") depth += 1;
    if (here === "}") {
      depth -= 1;
      if (depth === 0) return source.slice(start, index + 1);
    }
  }
  throw new Error(`unterminated declaration: ${name}`);
}

function marked(source, label) {
  const begin = `# BEGIN keychain-rebuild-focused: ${label}`;
  const end = `# END keychain-rebuild-focused: ${label}`;
  const first = source.indexOf(begin);
  const last = source.indexOf(end);
  if (first < 0 || last < first) throw new Error(`missing marked shell block: ${label}`);
  return source.slice(source.indexOf("\n", first) + 1, last);
}

function run(path, args = [], env = {}) {
  return spawnSync(path, args, {
    cwd: process.cwd(), encoding: "utf8", env: { ...process.env, ...env },
  });
}

try {
  const lifecycle = readFileSync(lifecycleSource, "utf8");
  const policy = declaration(lifecycle, "struct CloudIdentityReadPolicy");
  const reader = declaration(lifecycle, "final class CloudIdentityReader");
  const swiftHarness = join(work, "main.swift");
  const swiftBinary = join(work, "keychain-concurrency");
  writeFileSync(swiftHarness, `
import Foundation

${policy}

${reader}

final class Probe: @unchecked Sendable {
    private let lock = NSLock()
    private(set) var running = 0
    private(set) var maximum = 0
    private(set) var completions = 0
    private(set) var completionOffMain = false
    private(set) var operationOnMain = false
    func entered() {
        lock.lock(); running += 1; maximum = max(maximum, running)
        if Thread.isMainThread { operationOnMain = true }
        lock.unlock()
    }
    func left() { lock.lock(); running -= 1; lock.unlock() }
    func completed() {
        lock.lock(); completions += 1
        if !Thread.isMainThread { completionOffMain = true }
        lock.unlock()
    }
    func snapshot() -> (Int, Int, Bool, Bool) {
        lock.lock(); defer { lock.unlock() }
        return (maximum, completions, completionOffMain, operationOnMain)
    }
}

var checks = 0
func check(_ condition: @autoclosure () -> Bool, _ name: String) {
    guard condition() else {
        FileHandle.standardError.write(Data(("FAIL after \\(checks) Swift checks: " + name + "\\n").utf8))
        exit(1)
    }
    checks += 1
    print("✓ " + name)
}

var tracker = CloudIdentityReadPolicy.Tracker()
check(tracker.knowledge == .unknown, "launch begins with identity unknown, not signed out")
check(CloudIdentityReadPolicy.request(&tracker) == .start(generation: 1) &&
      tracker.knowledge == .reading, "the first request starts one read and exposes reading")
check(CloudIdentityReadPolicy.request(&tracker) == .coalesced,
      "an overlapping request is coalesced")
check(CloudIdentityReadPolicy.complete(generation: 1, tracker: &tracker) ==
      .restart(generation: 2), "a stale answer starts only the latest requested read")
check(CloudIdentityReadPolicy.complete(generation: 1, tracker: &tracker) == .discard,
      "an answer from a completed generation cannot apply twice")
check(CloudIdentityReadPolicy.complete(generation: 2, tracker: &tracker) == .accept &&
      tracker.knowledge == .resolved, "only the newest answer becomes resolved evidence")
_ = CloudIdentityReadPolicy.request(&tracker)
CloudIdentityReadPolicy.resolveWithoutRead(&tracker)
check(CloudIdentityReadPolicy.complete(generation: 3, tracker: &tracker) == .discard &&
      tracker.knowledge == .resolved, "foreground sign-out invalidates an answer already returning")

let probe = Probe()
let started = DispatchSemaphore(value: 0)
let release = DispatchSemaphore(value: 0)
let identityReader = CloudIdentityReader<Int>(label: "clawdline.test.identity")
let before = Date()
identityReader.read({
    probe.entered(); started.signal(); release.wait(); probe.left(); return 1
}, completion: { _ in probe.completed() })
let elapsed = Date().timeIntervalSince(before)
check(elapsed < 0.1, "starting a blocked read returns to the main thread immediately")
check(started.wait(timeout: .now() + 1) == .success,
      "the background reader actually starts its operation")
check(!probe.snapshot().3, "the blocking operation runs off the main thread")
identityReader.read({
    probe.entered(); probe.left(); return 2
}, completion: { _ in probe.completed() })
Thread.sleep(forTimeInterval: 0.05)
check(probe.snapshot().0 == 1, "the dedicated reader never overlaps two operations")
release.signal()
let deadline = Date().addingTimeInterval(2)
while probe.snapshot().1 < 2 && Date() < deadline {
    RunLoop.main.run(until: Date().addingTimeInterval(0.01))
}
let final = probe.snapshot()
check(final.1 == 2 && !final.2, "both results return through the main queue")
print("\\(checks) Swift checks passed")
`, "utf8");
  const compile = run("swiftc", ["-swift-version", "5", "-target", "arm64-apple-macos13.0",
    swiftHarness, "-o", swiftBinary]);
  check(compile.status === 0, `production concurrency seams compile: ${compile.stderr}`);
  const swift = run(swiftBinary);
  process.stdout.write(swift.stdout);
  process.stderr.write(swift.stderr);
  check(swift.status === 0 && /12 Swift checks passed/.test(swift.stdout),
    "compiled concurrency seam proves background, serial, main-return ordering");
  check(lifecycle.includes("restoredIdentity: @Sendable () throws") &&
        !lifecycle.includes("try services.restoredIdentity()"),
    "the lifecycle dependency is background-callable and apply has no direct credential read");
  check(lifecycle.includes("identityReader.read(restore)") &&
        lifecycle.includes("finishedIdentityRead(result, generation: generation)"),
    "the production lifecycle is wired through the compiled reader and generation policy");

  const fakeBin = join(work, "fake-bin");
  mkdirSync(fakeBin);
  const fakeState = join(work, "identity-count");
  executable(join(fakeBin, "security"), `#!/bin/bash
set -e
case "$1" in
  find-identity)
    if [ -s "$FAKE_SECURITY_STATE" ]; then
      printf '  1) ABCDEF0123456789ABCDEF0123456789ABCDEF01 "Clawdline Local Development"\\n'
    fi
    ;;
  import)
    count=0
    [ ! -s "$FAKE_SECURITY_STATE" ] || count=$(cat "$FAKE_SECURITY_STATE")
    printf '%s' $((count + 1)) > "$FAKE_SECURITY_STATE"
    ;;
  add-trusted-cert) ;;
  *) exit 2 ;;
esac
`);
  executable(join(fakeBin, "openssl"), `#!/bin/bash
while [ "$#" -gt 0 ]; do
  case "$1" in
    -keyout|-out) shift; printf fixture > "$1" ;;
  esac
  shift
done
`);
  const fakeKeychain = join(work, "login.keychain-db");
  writeFileSync(fakeKeychain, "fixture", "utf8");
  const setupEnv = {
    PATH: `${fakeBin}${delimiter}${process.env.PATH}`,
    FAKE_SECURITY_STATE: fakeState,
    CLAWDLINE_LOCAL_SIGN_KEYCHAIN: fakeKeychain,
    TMPDIR: work,
  };
  const firstSetup = run(setupSource, [], setupEnv);
  const secondSetup = run(setupSource, [], setupEnv);
  check(firstSetup.status === 0 && secondSetup.status === 0 &&
        readFileSync(fakeState, "utf8") === "1",
    "running setup twice exits zero twice and imports exactly one identity");
  check(firstSetup.stdout.includes("created local signing identity") &&
        secondSetup.stdout.includes("already exists") &&
        secondSetup.stdout.includes("Always Allow"),
    "setup reports created, unchanged, and the one-time Always Allow step");

  const build = readFileSync(buildSource, "utf8");
  const selection = marked(build, "signing identity selection");
  const branches = marked(build, "signing branches");
  const selectionScript = join(work, "select-signing.sh");
  executable(selectionScript, `#!/bin/bash
set -euo pipefail
LOCAL_SIGN_IDENTITY_NAME="Clawdline Local Development"
LOCAL_SIGNING=0
${selection}
printf 'RESULT=%s|%s\\n' "$SIGN_IDENTITY" "$LOCAL_SIGNING"
`);
  const selectEnv = { PATH: `${fakeBin}${delimiter}${process.env.PATH}`,
    FAKE_SECURITY_STATE: fakeState };
  const automaticEnv = { ...process.env, ...selectEnv };
  delete automaticEnv.CLAWDLINE_SIGN_IDENTITY;
  const automatic = spawnSync(selectionScript, { encoding: "utf8", env: automaticEnv });
  check(automatic.status === 0 &&
        automatic.stdout.includes("RESULT=ABCDEF0123456789ABCDEF0123456789ABCDEF01|1"),
    "build selection prefers the stable local identity when no override exists");
  const developer = run(selectionScript, [], {
    ...selectEnv, CLAWDLINE_SIGN_IDENTITY: "Developer ID Application: Example" });
  check(developer.status === 0 &&
        developer.stdout.includes("RESULT=Developer ID Application: Example|0"),
    "CLAWDLINE_SIGN_IDENTITY still wins exactly over local discovery");
  const explicitAdhoc = run(selectionScript, [], {
    ...selectEnv, CLAWDLINE_SIGN_IDENTITY: "-" });
  check(explicitAdhoc.status === 0 && explicitAdhoc.stdout.includes("RESULT=-|0"),
    "an explicit ad-hoc override remains ad-hoc")
  rmSync(fakeState);
  const absentEnv = { ...process.env, ...selectEnv };
  delete absentEnv.CLAWDLINE_SIGN_IDENTITY;
  const absent = spawnSync(selectionScript, { encoding: "utf8", env: absentEnv });
  check(absent.status === 0 && absent.stdout.includes("RESULT=-|0") &&
        absent.stdout.includes("tools/setup-local-signing-identity.sh"),
    "missing local identity falls back visibly with the exact repair command");

  const signingScript = join(work, "signing-branches.sh");
  const codesignLog = join(work, "codesign.log");
  executable(join(fakeBin, "codesign"), `#!/bin/bash
printf '%s\\n' "$*" >> "$FAKE_CODESIGN_LOG"
`);
  executable(signingScript, `#!/bin/bash
set -euo pipefail
BUNDLE_ID="com.tsunamiworks.clawdline"
LOCAL_SIGN_IDENTITY_NAME="Clawdline Local Development"
STAGED_APP="/tmp/Clawdline.app"
${branches}
`);
  function signingArgs(identity, local) {
    writeFileSync(codesignLog, "", "utf8");
    const result = run(signingScript, [], {
      PATH: `${fakeBin}${delimiter}${process.env.PATH}`,
      FAKE_CODESIGN_LOG: codesignLog, SIGN_IDENTITY: identity, LOCAL_SIGNING: String(local),
    });
    check(result.status === 0, `signing branch exits zero for ${identity}: ${result.stderr}`);
    return readFileSync(codesignLog, "utf8");
  }
  const adhocArgs = signingArgs("-", 0);
  check(adhocArgs.includes("--sign - --identifier com.tsunamiworks.clawdline"),
    "ad-hoc fallback signs with the bundle identifier");
  const localArgs = signingArgs("LOCALHASH", 1);
  check(localArgs.includes("--sign LOCALHASH --identifier com.tsunamiworks.clawdline") &&
        !localArgs.includes("--timestamp") && !localArgs.includes("--options runtime"),
    "stable local signing uses its identity without release-only options");
  const releaseArgs = signingArgs("Developer ID Application: Example", 0);
  check(releaseArgs.includes("--options runtime --timestamp --entitlements Resources/Clawdline.entitlements"),
    "Developer ID keeps hardened runtime, timestamp, and release entitlements");

  console.log(`${nodeChecks} node checks passed`);
} finally {
  rmSync(work, { recursive: true, force: true });
}
