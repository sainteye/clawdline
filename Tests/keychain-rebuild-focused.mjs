#!/usr/bin/env node

import {
  chmodSync, mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const lifecycleSource = resolve(process.env.CLAWDLINE_KEYCHAIN_LIFECYCLE_SOURCE ||
  "Sources/CloudBridgeLifecycle.swift");
const keysSource = resolve(process.env.CLAWDLINE_KEYCHAIN_KEYS_SOURCE ||
  "Sources/CloudKeys.swift");
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

function swiftSources(directory) {
  return readdirSync(directory, { recursive: true })
    .filter((path) => path.endsWith(".swift") && path !== "main.swift")
    .map((path) => join(directory, path)).sort();
}

try {
  const lifecycle = readFileSync(lifecycleSource, "utf8");
  const keys = readFileSync(keysSource, "utf8");
  const policy = declaration(lifecycle, "struct CloudIdentityReadPolicy");
  const reader = declaration(keys, "final class CloudKeychainReader");
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
    private(set) var starts = 0
    func entered() -> Int {
        lock.lock(); running += 1; starts += 1; maximum = max(maximum, running)
        if Thread.isMainThread { operationOnMain = true }
        let value = starts
        lock.unlock()
        return value
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
let identityReader = CloudKeychainReader<Int>(label: "clawdline.test.identity") {
    let call = probe.entered()
    if call == 1 { started.signal(); release.wait() }
    probe.left()
    return call
}
let before = Date()
identityReader.read { _ in probe.completed() }
let elapsed = Date().timeIntervalSince(before)
check(elapsed < 0.1, "starting a blocked read returns to the main thread immediately")
check(started.wait(timeout: .now() + 1) == .success,
      "the background reader actually starts its operation")
check(!probe.snapshot().3, "the blocking operation runs off the main thread")
identityReader.read { _ in probe.completed() }
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

  const storeWork = join(work, "store");
  mkdirSync(storeWork);
  const storeHarness = join(storeWork, "main.swift");
  const storeBinary = join(work, "store-main");
  writeFileSync(storeHarness, `
import Foundation

do {
    _ = try CloudKeychainStore(service: "app.clawdline.focused.main-thread-probe").data(for: "missing")
    FileHandle.standardError.write(Data("FAIL: main-thread Keychain read was admitted\\n".utf8))
    exit(1)
} catch CloudKeyError.mainThreadReadForbidden {
    print("1 store-boundary check passed")
} catch {
    FileHandle.standardError.write(Data(("FAIL: wrong main-thread error: \\(error)\\n").utf8))
    exit(1)
}
`, "utf8");
  const storeCompile = run("swiftc", ["-swift-version", "5", "-target",
    "arm64-apple-macos13.0", keysSource, storeHarness, "-framework", "Security",
    "-o", storeBinary]);
  check(storeCompile.status === 0, `the production Keychain store guard compiles: ${storeCompile.stderr}`);
  const storeProbe = run(storeBinary);
  process.stdout.write(storeProbe.stdout);
  process.stderr.write(storeProbe.stderr);
  check(storeProbe.status === 0 && /1 store-boundary check passed/.test(storeProbe.stdout),
    "the production store rejects a main-thread read before Security is reached");

  const productionTypecheck = run("xcrun", ["swiftc", "-swift-version", "5", "-target",
    "arm64-apple-macos13.0", "-typecheck", ...swiftSources("Sources")]);
  check(productionTypecheck.status === 0,
    `production wiring compiles only through the asynchronous reader: ${productionTypecheck.stderr}`);

  const fakeBin = join(work, "fake-bin");
  mkdirSync(fakeBin);
  const fakeState = join(work, "identity-count");
  executable(join(fakeBin, "security"), `#!/bin/bash
set -e
case "$1" in
  find-identity)
    case "\${FAKE_SECURITY_MODE:-normal}" in
      failure) exit 45 ;;
      duplicate)
        printf '  1) ABCDEF0123456789ABCDEF0123456789ABCDEF01 "Clawdline Local Development"\\n'
        printf '  2) 2222222222222222222222222222222222222222 "Clawdline Local Development"\\n'
        ;;
      invalid)
        if [ -s "$FAKE_SECURITY_STATE" ]; then
          printf '  1) ABCDEF0123456789ABCDEF0123456789ABCDEF01 "Clawdline Local Development"\\n'
        else
          printf '     0 valid identities found\\n'
        fi
        ;;
      *)
        if [ -s "$FAKE_SECURITY_STATE" ]; then
          printf '  1) ABCDEF0123456789ABCDEF0123456789ABCDEF01 "Clawdline Local Development"\\n'
        fi
        ;;
    esac
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
        secondSetup.stdout.includes("up to three Keychain prompts"),
    "setup reports created, unchanged, and the three-item authorization boundary");
  const invalidState = join(work, "invalid-identity-count");
  const invalidSetup = run(setupSource, [], {
    ...setupEnv, FAKE_SECURITY_STATE: invalidState, FAKE_SECURITY_MODE: "invalid",
  });
  check(invalidSetup.status === 0 && readFileSync(invalidState, "utf8") === "1" &&
        invalidSetup.stdout.includes("created local signing identity"),
    "an expired or non-code-signing namesake is ignored and a valid identity is created");
  const duplicateSetup = run(setupSource, [], {
    ...setupEnv, FAKE_SECURITY_MODE: "duplicate",
  });
  check(duplicateSetup.status !== 0 &&
        (duplicateSetup.stdout + duplicateSetup.stderr).includes("multiple valid"),
    "setup refuses an ambiguous common name without changing either identity");

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
  const inaccessible = run(selectionScript, [], {
    ...selectEnv, FAKE_SECURITY_MODE: "failure",
  });
  check(inaccessible.status === 0 && inaccessible.stdout.includes("RESULT=-|0") &&
        inaccessible.stdout.includes("could not inspect code-signing identities"),
    "a failed security query reaches the loud ad-hoc fallback instead of errexit");
  const ambiguous = run(selectionScript, [], {
    ...selectEnv, FAKE_SECURITY_MODE: "duplicate",
  });
  check(ambiguous.status !== 0 &&
        (ambiguous.stdout + ambiguous.stderr).includes("multiple valid"),
    "build refuses two valid identities with the same common name");

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
    return { log: readFileSync(codesignLog, "utf8"), result };
  }
  const adhocArgs = signingArgs("-", 0);
  check(adhocArgs.log.includes("--sign - --identifier com.tsunamiworks.clawdline"),
    "ad-hoc fallback signs with the bundle identifier");
  const localArgs = signingArgs("LOCALHASH", 1);
  check(localArgs.log.includes("--sign LOCALHASH --identifier com.tsunamiworks.clawdline") &&
        !localArgs.log.includes("--timestamp") && !localArgs.log.includes("--options runtime") &&
        localArgs.result.stdout.includes("up to three Keychain prompts"),
    "stable local signing uses its identity without release-only options");
  const releaseArgs = signingArgs("Developer ID Application: Example", 0);
  check(releaseArgs.log.includes("--options runtime --timestamp --entitlements Resources/Clawdline.entitlements"),
    "Developer ID keeps hardened runtime, timestamp, and release entitlements");

  console.log(`${nodeChecks} node checks passed`);
} finally {
  rmSync(work, { recursive: true, force: true });
}
