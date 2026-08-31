#!/usr/bin/env node

import {
  chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const source = resolve(process.env.CLAWDLINE_ONBOARDING_SOURCE || "Sources/Onboarding.swift");
const sourcesDirectory = resolve(process.env.CLAWDLINE_SOURCES_DIR || "Sources");
const stringsSource = resolve(process.env.CLAWDLINE_STRINGS_SOURCE || "Sources/Strings.swift");
const installer = resolve(process.env.CLAWDLINE_INSTALL_SOURCE || "install.sh");
const work = mkdtempSync(join(process.env.TMPDIR || tmpdir(), "clawdline-onboarding-"));

let nodeChecks = 0;
function checkNode(condition, name, status = 2) {
  if (!condition) {
    process.stderr.write(`FAIL after ${nodeChecks} node checks: ${name}\n`);
    process.exitCode = status;
    throw new Error(name);
  }
  nodeChecks += 1;
  console.log(`✓ ${name}`);
}

function executable(path, body) {
  writeFileSync(path, body, "utf8");
  chmodSync(path, 0o755);
}

try {
  const subject = readFileSync(source, "utf8");
  checkNode(subject.includes("enum OnboardingEvidencePolicy") &&
            subject.includes("enum PhoneCredentialIssuer") &&
            subject.includes("enum CloudPreviewEvidencePolicy"),
            "focused subject contains the production evidence seams");

  checkNode(subject.includes(
              "private static let fieldOrder: [Field] = [.detected, .expected, .recovery, .nextAction]"),
            "all Home routes share detected, expected, recovery, next-action order");

  const strings = readFileSync(stringsSource, "utf8");
  const localRecovery = strings.match(
    /func setupLocalRecovery\(_ phase: LocalBrowserPhase\) -> String\? \{[\s\S]*?\n    \}/)?.[0] || "";
  const tunnelRecovery = strings.match(
    /func setupTunnelRecovery\(_ phase: CloudflareOnboardingPhase\) -> String\? \{[\s\S]*?\n    \}/)?.[0] || "";
  const cloudRecovery = strings.match(
    /func setupCloudRecovery\(_ decision: CloudPreviewDecision\) -> String\? \{[\s\S]*?\n    \}/)?.[0] || "";
  checkNode(localRecovery.length > 0 && tunnelRecovery.length > 0 && cloudRecovery.length > 0 &&
            !localRecovery.includes("setupNextAction") &&
            !tunnelRecovery.includes("setupNextAction") &&
            !cloudRecovery.includes("setupNextAction"),
            "recovery is optional guidance, never a restatement of its action label");

  const harness = join(work, "main.swift");
  const binary = join(work, "onboarding-focused");
  writeFileSync(harness, String.raw`
import Foundation

enum RemoteAuth {
    enum Capability: String, Hashable { case read, send, admin }
    struct Device { var id: String; var lastSeen: Date?; var local: Bool }
}

enum RemoteTunnel {
    enum State { case off; case starting; case up(url: String); case failed(reason: String) }
}

struct CloudMachineIdentity: Equatable { let accountID: String; let machineID: String }
struct CloudPairedDevice: Equatable { let deviceID: String }
enum CloudBridgeLifecycle {
    enum State: Equatable {
        case detached
        case attached(accountID: String, machineID: String)
        case unauthorized(accountID: String, machineID: String)
        case failed(reason: String)
    }
}
enum ProbeError: Error { case failed }

var checks = 0
func check(_ condition: @autoclosure () -> Bool, _ name: String) {
    guard condition() else {
        FileHandle.standardError.write(Data(("FAIL after \(checks) Swift checks: " + name + "\n").utf8))
        exit(1)
    }
    checks += 1
    print("✓ " + name)
}

let root = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
let file = root.appendingPathComponent("onboarding.json")
let store = OnboardingCompletionStore(fileURL: file)
check(!store.isCurrent, "missing completion is incomplete")
try! FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
try! Data("{\"completed_version\":0}".utf8).write(to: file)
check(!store.isCurrent, "older completion version is incomplete")
check(store.markCurrent(now: Date(timeIntervalSince1970: 1_700_000_000)), "current completion writes")
check(store.isCurrent, "current completion reopens quietly")
let mode = try! FileManager.default.attributesOfItem(atPath: file.path)[.posixPermissions] as! NSNumber
check(mode.intValue == 0o600, "completion file is private")
let blockedStore = OnboardingCompletionStore(fileURL: root)
check(!blockedStore.markCurrent(), "completion persistence failure is reported to the UI layer")

check(LocalHealthEvidencePolicy.interpret(statusCode: nil, bodyOK: nil,
                                           errorCode: NSURLErrorTimedOut) == .failed(.timedOut),
      "timeout stays distinguishable")
check(LocalHealthEvidencePolicy.interpret(statusCode: nil, bodyOK: nil,
                                           errorCode: NSURLErrorCannotConnectToHost) == .failed(.transport),
      "transport failure stays distinguishable")
check(LocalHealthEvidencePolicy.interpret(statusCode: 500, bodyOK: nil,
                                           errorCode: nil) == .failed(.httpStatus(500)),
      "HTTP failure keeps its status")
check(LocalHealthEvidencePolicy.interpret(statusCode: 200, bodyOK: false,
                                           errorCode: nil) == .failed(.unhealthy),
      "HTTP 200 with ok false is not healthy")
check(LocalHealthEvidencePolicy.interpret(statusCode: 200, bodyOK: nil,
                                           errorCode: nil) == .failed(.invalidResponse),
      "HTTP 200 without an ok fact is invalid")
check(LocalHealthEvidencePolicy.interpret(statusCode: 200, bodyOK: true,
                                           errorCode: nil) == .ready,
      "only HTTP 200 with ok true is ready")
check(OnboardingPollingPolicy.interval > OnboardingPollingPolicy.requestTimeout,
      "poll interval cannot invalidate a still-live request")
check(OnboardingPollingPolicy.shouldStartRequest(isInFlight: false) &&
      !OnboardingPollingPolicy.shouldStartRequest(isInFlight: true),
      "polling never overlaps an in-flight health request")
let evidenceNow = Date(timeIntervalSince1970: 100)
check(!OnboardingPollingPolicy.shouldRefreshEvidence(
        now: evidenceNow, lastRefresh: Date(timeIntervalSince1970: 95), isInFlight: false) &&
      OnboardingPollingPolicy.shouldRefreshEvidence(
        now: evidenceNow, lastRefresh: Date(timeIntervalSince1970: 80), isInFlight: false) &&
      !OnboardingPollingPolicy.shouldRefreshEvidence(
        now: evidenceNow, lastRefresh: nil, isInFlight: true),
      "Keychain, pairing-store, and PATH evidence is cached and never overlaps")

let seenDate = Date(timeIntervalSince1970: 1)
let exactUnseen = RemoteAuth.Device(id: "expected", lastSeen: nil, local: false)
let exactSeen = RemoteAuth.Device(id: "expected", lastSeen: seenDate, local: false)
let otherSeen = RemoteAuth.Device(id: "other", lastSeen: seenDate, local: false)
let serverOff = OnboardingEvidencePolicy.local(
    serverEnabled: false, health: .ready, expectedDeviceID: nil, devices: [])
check(serverOff.phase == .serverOff && !serverOff.mayMintDevice,
      "server-off cannot mint despite a stale ready health fact")
let saveFailed = OnboardingEvidencePolicy.local(
    serverEnabled: false, configurationFailed: true, health: .notChecked,
    expectedDeviceID: nil, devices: [])
check(saveFailed.phase == .configurationFailed && !saveFailed.mayMintDevice,
      "failed config save does not claim the server changed")
let healthFailed = OnboardingEvidencePolicy.local(
    serverEnabled: true, health: .failed(.httpStatus(500)), expectedDeviceID: nil, devices: [])
check(healthFailed.phase == .healthFailed(.httpStatus(500)) && !healthFailed.mayMintDevice,
      "health failure cannot mint")
let ready = OnboardingEvidencePolicy.local(
    serverEnabled: true, health: .ready, expectedDeviceID: nil, devices: [])
check(ready.phase == .readyToOpen && ready.mayMintDevice, "proved health permits one mint")
let localCredential = LocalBrowserCredentialPolicy.plan(for: ready)
check(localCredential?.remoteCapabilities() == [.read] && localCredential?.local == false,
      "local onboarding can mint only a non-local read credential")
check(LocalBrowserCredentialPolicy.plan(for: healthFailed) == nil,
      "local onboarding cannot mint before health proof")
let wrongDevice = OnboardingEvidencePolicy.local(
    serverEnabled: true, health: .ready, expectedDeviceID: "expected", devices: [otherSeen])
check(!wrongDevice.succeeded, "another device lastSeen cannot complete local setup")
let unseen = OnboardingEvidencePolicy.local(
    serverEnabled: true, health: .ready, expectedDeviceID: "expected", devices: [exactUnseen])
check(unseen.phase == .awaitingDevice && !unseen.succeeded,
      "the exact unseen device remains pending")
let connected = OnboardingEvidencePolicy.local(
    serverEnabled: true, health: .ready, expectedDeviceID: "expected", devices: [exactSeen])
check(connected.phase == .connected && connected.succeeded,
      "only the exact seen device completes local setup")
check(CredentialLifetimePolicy.shouldRevokeUnseen(
        expectedDeviceID: "expected", devices: [exactUnseen]),
      "an unseen route credential is revoked when its gate disappears")
check(!CredentialLifetimePolicy.shouldRevokeUnseen(
        expectedDeviceID: "expected", devices: [exactSeen]),
      "a credential that supplied lastSeen remains usable")
// Both checks above hold a one-device list, so they cannot tell "the exact device was seen" from
// "some device was seen". A mixed list is the only input that separates them, and getting it wrong
// leaves a live unseen key behind whenever anything else on the Mac is active.
check(CredentialLifetimePolicy.shouldRevokeUnseen(
        expectedDeviceID: "expected", devices: [otherSeen, exactUnseen]),
      "another device lastSeen does not save an unseen route credential")

var mintCount = 0
func mint(_ name: String, _ caps: Set<RemoteAuth.Capability>, _ local: Bool)
    -> (id: String, token: String) {
    mintCount += 1
    check(name == "Phone", "issuer forwards the visible device name")
    check(caps == [.read], "read-only plan maps to the actual RemoteAuth capability")
    check(!local, "phone credentials are explicitly non-local")
    return ("phone-1", "secret")
}
let refused = PhoneCredentialIssuer.issue(
    remoteEnabled: true, remoteWrite: false, tunnel: .starting, name: "Phone", mint: mint)
check(refused == nil && mintCount == 0, "issuer never mints before a live tunnel URL")
let empty = PhoneCredentialIssuer.issue(
    remoteEnabled: true, remoteWrite: false, tunnel: .up(url: ""), name: "Phone", mint: mint)
check(empty == nil && mintCount == 0, "issuer never mints for an empty live URL")
let issued = PhoneCredentialIssuer.issue(
    remoteEnabled: true, remoteWrite: false, tunnel: .up(url: "https://phone.example/"),
    name: "Phone", mint: mint)
check(issued?.signInURL == "https://phone.example/?t=secret" && mintCount == 1,
      "issuer mints after its gate and builds the public sign-in URL")
let controlPlan = PhoneCredentialPolicy.plan(
    remoteEnabled: true, remoteWrite: true, tunnel: .up(url: "https://phone.example"))
check(controlPlan?.remoteCapabilities() == [.read, .send],
      "control capability appears only when the saved control setting is on")

let startingTunnel = OnboardingEvidencePolicy.cloudflare(
    cloudflaredInstalled: true, tunnelState: .starting,
    expectedDeviceID: nil, devices: [])
check(startingTunnel.qrURL == nil && !startingTunnel.mayMintDevice,
      "starting tunnel cannot offer a QR")
let liveTunnel = OnboardingEvidencePolicy.cloudflare(
    cloudflaredInstalled: true, tunnelState: .up(url: "https://fresh.example"),
    expectedDeviceID: nil, devices: [])
check(liveTunnel.qrURL == "https://fresh.example" && liveTunnel.mayMintDevice,
      "the actual live tunnel URL permits QR minting")
let wrongPhone = OnboardingEvidencePolicy.cloudflare(
    cloudflaredInstalled: true, tunnelState: .up(url: "https://fresh.example"),
    expectedDeviceID: "expected", devices: [otherSeen])
check(!wrongPhone.succeeded, "another phone lastSeen cannot complete tunnel setup")
let exactPhone = OnboardingEvidencePolicy.cloudflare(
    cloudflaredInstalled: true, tunnelState: .up(url: "https://fresh.example"),
    expectedDeviceID: "expected", devices: [exactSeen])
check(exactPhone.succeeded, "the exact QR device lastSeen completes tunnel setup")

let identityFailure = CloudPreviewEvidencePolicy.evaluate(
    identityResult: .failure(ProbeError.failed), lifecycle: .detached,
    devicesResult: nil, pairingAttempt: nil)
check(identityFailure.decision.account == .failed(.identityRead) &&
      identityFailure.decision.machineCredential == .failed(.identityRead) &&
      identityFailure.decision.e2eePairing == .unavailable,
      "identity read failure never asserts that pairing is absent")
let noIdentity = CloudPreviewEvidencePolicy.evaluate(
    identityResult: .success(nil), lifecycle: .detached,
    devicesResult: nil, pairingAttempt: nil)
check(noIdentity.decision.account == .absent && noIdentity.decision.e2eePairing == .unavailable,
      "proved missing identity and unavailable pairing remain distinct")
let identity = CloudMachineIdentity(accountID: "account", machineID: "machine")
let beforePairing = CloudPreviewEvidencePolicy.evaluate(
    identityResult: .success(identity), lifecycle: .attached(accountID: "account", machineID: "machine"),
    devicesResult: .success([CloudPairedDevice(deviceID: "old")]), pairingAttempt: nil)
check(beforePairing.decision.relayReady == .unavailable &&
      beforePairing.decision.e2eePairing == .unavailable,
      "attached relay and pre-action devices are not promoted to proof")
let attempt = CloudPairingAttempt(baselineDeviceIDs: ["old"], boundDeviceID: nil)
let oneNew = CloudPreviewEvidencePolicy.evaluate(
    identityResult: .success(identity), lifecycle: .detached,
    devicesResult: .success([CloudPairedDevice(deviceID: "old"), CloudPairedDevice(deviceID: "new")]),
    pairingAttempt: attempt)
check(oneNew.boundDeviceID == "new" && oneNew.decision.e2eePairing == .proved("new"),
      "one identity appearing after the pairing action is bound exactly")
let held = CloudPreviewEvidencePolicy.evaluate(
    identityResult: .success(identity), lifecycle: .detached,
    devicesResult: .success([CloudPairedDevice(deviceID: "new"), CloudPairedDevice(deviceID: "later")]),
    pairingAttempt: CloudPairingAttempt(baselineDeviceIDs: ["old"], boundDeviceID: "new"))
check(held.boundDeviceID == "new" && held.decision.e2eePairing == .proved("new"),
      "a later viewer cannot replace the bound viewer")
let ambiguous = CloudPreviewEvidencePolicy.evaluate(
    identityResult: .success(identity), lifecycle: .detached,
    devicesResult: .success([CloudPairedDevice(deviceID: "new-1"), CloudPairedDevice(deviceID: "new-2")]),
    pairingAttempt: attempt)
check(ambiguous.boundDeviceID == nil &&
      ambiguous.decision.e2eePairing == .failed(.pairingAmbiguous),
      "multiple new viewers fail closed instead of choosing by wall clock")

let allCloudProofs = CloudPreviewPolicy.decide(
    account: .proved("account"), machineCredential: .proved("machine"),
    relayReady: .proved("ready"), e2eePairing: .proved("viewer"),
    viewerReceipt: .proved("received"))
check(allCloudProofs.succeeded, "cloud success still requires all five independent proofs")
check(OnboardingExitPolicy.shouldRecordCompletion(for: .routeSucceeded),
      "a proved route records completion")
check(OnboardingExitPolicy.shouldRecordCompletion(for: .homeDismissed),
      "closing Home is an independent onboarding exit")
check(AppLaunchPolicy.shouldShowHome(onboardingComplete: false) &&
      !AppLaunchPolicy.shouldShowHome(onboardingComplete: true),
      "only the completion record controls automatic Home opening")
check(HomeReopenPolicy.shouldShowHome(hasVisibleWindows: false) &&
      !HomeReopenPolicy.shouldShowHome(hasVisibleWindows: true),
      "Dock reopen preserves an already-visible Settings or prompt window")

check(checks == 49, "expected Swift behavior check count")
print("50 Swift checks passed")
`, "utf8");

  const compile = spawnSync("xcrun", [
    "swiftc", "-swift-version", "5", "-target", "arm64-apple-macos13.0",
    "-D", "ONBOARDING_POLICY_ONLY", source, harness, "-o", binary,
  ], { encoding: "utf8" });
  if (compile.status !== 0) {
    process.stderr.write(compile.stdout + compile.stderr);
    process.exitCode = compile.status || 1;
    throw new Error("focused Swift seam did not compile");
  }
  checkNode(true, "focused seam compiles under the shipping Swift and deployment target");

  const run = spawnSync(binary, [join(work, "store")], { encoding: "utf8" });
  process.stdout.write(run.stdout);
  process.stderr.write(run.stderr);
  checkNode(run.status === 0 && run.stdout.includes("50 Swift checks passed"),
            "focused seam behavior passes its asserted check count");

  const copyFiles = readdirSync(sourcesDirectory).filter((name) => /^Copy\+.*\.swift$/.test(name));
  const conformerCount = (member) => copyFiles.reduce((count, name) => {
    const text = readFileSync(join(sourcesDirectory, name), "utf8");
    return count + (text.match(new RegExp(`(?:let|func) ${member}\\b`, "g")) || []).length;
  }, 0);
  checkNode(conformerCount("setupDismissalHint") === 14 &&
            conformerCount("setupCompletionFailed") === 14 &&
            conformerCount("setupLocalReadOnlyAction") === 14 &&
            conformerCount("setupLocalHealthTransport") === 14 &&
            conformerCount("menuAbout") === 14,
            "all 14 Copy conformers carry exit, failure, and standard-menu copy");

  const fakeBin = join(work, "fake-bin");
  const installDest = join(work, "Install Destination");
  const openRecord = join(work, "open-record");
  mkdirSync(fakeBin, { recursive: true });
  executable(join(fakeBin, "curl"), `#!/bin/bash
out=""
latest=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    *releases/latest) latest=1; shift ;;
    *) shift ;;
  esac
done
if [ "$latest" = 1 ]; then
  printf '%s' '{"assets":[{"name":"Clawdline.zip","browser_download_url":"https://example.invalid/v-test/Clawdline.zip"}]}' > "$out"
  printf '200'
else
  : > "$out"
fi
`);
  executable(join(fakeBin, "ditto"), `#!/bin/bash
if [ "$1" = "-x" ]; then
  dest="$4"
  mkdir -p "$dest/Clawdline.app"
else
  mkdir -p "$2"
fi
`);
  executable(join(fakeBin, "codesign"), "#!/bin/bash\nprintf 'adhoc signature\\n'\n");
  executable(join(fakeBin, "xattr"), "#!/bin/bash\nexit 0\n");
  executable(join(fakeBin, "open-probe"), `#!/bin/bash
printf '%s' "$1" > "${openRecord}"
`);

  const installRun = spawnSync("/bin/bash", [installer, installDest], {
    encoding: "utf8",
    env: {
      ...process.env,
      PATH: `${fakeBin}:/usr/bin:/bin`,
      CLAWDLINE_OPEN_COMMAND: join(fakeBin, "open-probe"),
    },
  });
  process.stdout.write(installRun.stdout);
  process.stderr.write(installRun.stderr);
  checkNode(installRun.status === 0, "installer reaches successful completion in the isolated probe");
  checkNode(existsSync(openRecord) &&
            readFileSync(openRecord, "utf8") === join(installDest, "Clawdline.app"),
            "successful install actually calls open on the exact installed app path");

  checkNode(nodeChecks === 8, "expected Node integration check count");
  console.log("9 node checks passed");
} finally {
  rmSync(work, { recursive: true, force: true });
}
