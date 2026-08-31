#!/usr/bin/env node

import { mkdtempSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const source = resolve(process.env.CLAWDLINE_ONBOARDING_SOURCE || "Sources/Onboarding.swift");
const remoteQRSource = resolve(process.env.CLAWDLINE_REMOTE_QR_SOURCE || "Sources/RemoteQR.swift");
const sourcesDirectory = resolve(process.env.CLAWDLINE_SOURCES_DIR || "Sources");
const installer = resolve(process.env.CLAWDLINE_INSTALL_SOURCE || "install.sh");
const settings = resolve(process.env.CLAWDLINE_SETTINGS_SOURCE || "Sources/Settings.swift");
const work = mkdtempSync(join(process.env.TMPDIR || tmpdir(), "clawdline-onboarding-"));
const harness = join(work, "main.swift");
const binary = join(work, "onboarding-focused");

// Read first so a missing or empty subject cannot turn into a successful zero-check run.
if (!readFileSync(source, "utf8").includes("enum LocalBrowserPolicy")) {
  throw new Error(`onboarding policy seam not found in ${source}`);
}

writeFileSync(harness, String.raw`
import Foundation

var checks = 0
func check(_ condition: @autoclosure () -> Bool, _ name: String) {
    guard condition() else {
        FileHandle.standardError.write(Data(("FAIL: " + name + "\n").utf8))
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

let off = LocalBrowserPolicy.decide(serverEnabled: false, healthReady: false,
                                    deviceCreated: false, deviceLastSeen: false)
check(off.phase == .serverOff && off.action == .enableServer, "server-off has one enable action")
check(!off.mayMintDevice && !off.succeeded, "server-off cannot mint or succeed")

let checking = LocalBrowserPolicy.decide(serverEnabled: true, healthReady: false,
                                         deviceCreated: false, deviceLastSeen: false)
check(checking.phase == .checkingHealth && checking.action == .retryHealth,
      "config switch alone remains checking")
check(!checking.mayMintDevice && !checking.succeeded, "health failure cannot mint or succeed")

let ready = LocalBrowserPolicy.decide(serverEnabled: true, healthReady: true,
                                      deviceCreated: false, deviceLastSeen: false)
check(ready.phase == .readyToOpen && ready.action == .openBrowser,
      "health readiness offers browser open")
check(ready.mayMintDevice && !ready.succeeded, "only health readiness permits minting")

let unseen = LocalBrowserPolicy.decide(serverEnabled: true, healthReady: true,
                                       deviceCreated: true, deviceLastSeen: false)
check(unseen.phase == .awaitingDevice && unseen.action == .openBrowserAgain,
      "minted but unseen device remains pending")
check(!unseen.succeeded, "device allocation is not success")

let seen = LocalBrowserPolicy.decide(serverEnabled: true, healthReady: true,
                                     deviceCreated: true, deviceLastSeen: true)
check(seen.phase == .connected && seen.action == .finish, "exact seen device completes")
check(seen.succeeded && !seen.mayMintDevice, "connected is success without another credential")
check(AppLaunchPolicy.shouldShowHome(onboardingComplete: false), "fresh launch shows Home")
check(!AppLaunchPolicy.shouldShowHome(onboardingComplete: true), "completed launch may stay quiet")

let missingTunnel = CloudflareOnboardingPolicy.decide(
    cloudflaredInstalled: false, tunnel: .off, deviceCreated: false,
    exactDeviceIsNonLocal: false, exactDeviceLastSeen: false)
check(missingTunnel.phase == .cloudflaredMissing && missingTunnel.qrURL == nil,
      "missing cloudflared cannot offer a QR")
let tunnelOff = CloudflareOnboardingPolicy.decide(
    cloudflaredInstalled: true, tunnel: .off, deviceCreated: false,
    exactDeviceIsNonLocal: false, exactDeviceLastSeen: false)
check(tunnelOff.phase == .tunnelOff && tunnelOff.action == .openSettings,
      "tunnel-off has one settings action")
let tunnelStarting = CloudflareOnboardingPolicy.decide(
    cloudflaredInstalled: true, tunnel: .starting, deviceCreated: false,
    exactDeviceIsNonLocal: false, exactDeviceLastSeen: false)
check(tunnelStarting.phase == .starting && tunnelStarting.qrURL == nil,
      "starting tunnel cannot offer a QR")
let reason = "Set remote_hostname to the hostname you routed to that tunnel"
let tunnelFailed = CloudflareOnboardingPolicy.decide(
    cloudflaredInstalled: true, tunnel: .failed(reason: reason), deviceCreated: false,
    exactDeviceIsNonLocal: false, exactDeviceLastSeen: false)
check(tunnelFailed.phase == .failed(reason: reason), "tunnel failure preserves its exact reason")
let live = CloudflareOnboardingPolicy.decide(
    cloudflaredInstalled: true, tunnel: .up(url: "https://fresh.trycloudflare.com"),
    deviceCreated: false, exactDeviceIsNonLocal: false, exactDeviceLastSeen: false)
check(live.qrURL == "https://fresh.trycloudflare.com" && live.mayMintDevice,
      "only live tunnel URL permits QR minting")
let localSeen = CloudflareOnboardingPolicy.decide(
    cloudflaredInstalled: true, tunnel: .up(url: "https://fresh.trycloudflare.com"),
    deviceCreated: true, exactDeviceIsNonLocal: false, exactDeviceLastSeen: true)
check(!localSeen.succeeded, "local device never completes phone setup")
let exactUnseen = CloudflareOnboardingPolicy.decide(
    cloudflaredInstalled: true, tunnel: .up(url: "https://fresh.trycloudflare.com"),
    deviceCreated: true, exactDeviceIsNonLocal: true, exactDeviceLastSeen: false)
let isAwaiting: Bool
if case .awaitingDevice = exactUnseen.phase { isAwaiting = true } else { isAwaiting = false }
check(isAwaiting && !exactUnseen.succeeded,
      "minted exact phone without lastSeen remains pending")
let exactSeen = CloudflareOnboardingPolicy.decide(
    cloudflaredInstalled: true, tunnel: .up(url: "https://fresh.trycloudflare.com"),
    deviceCreated: true, exactDeviceIsNonLocal: true, exactDeviceLastSeen: true)
let isConnected: Bool
if case .connected = exactSeen.phase { isConnected = true } else { isConnected = false }
check(isConnected && exactSeen.succeeded,
      "exact non-local phone lastSeen completes tunnel route")

let accountOnly = CloudPreviewPolicy.decide(
    account: .proved("account-1"), machineCredential: .proved("machine-1"),
    relayReady: .notProved, e2eePairing: .absent, viewerReceipt: .unavailable)
check(accountOnly.relayReady == .notProved && accountOnly.viewerReceipt == .unavailable,
      "cloud account does not promote relay or viewer receipt")
check(accountOnly.action == .pairPhone && !accountOnly.succeeded,
      "credential without pairing offers exactly pairing")
let pairedPreview = CloudPreviewPolicy.decide(
    account: .proved("account-1"), machineCredential: .proved("machine-1"),
    relayReady: .notProved, e2eePairing: .proved("viewer-1"), viewerReceipt: .unavailable)
check(pairedPreview.action == .reviewPreviewStatus && !pairedPreview.succeeded,
      "E2EE pairing cannot stand in for relay readiness or viewer receipt")
let allCloudProofs = CloudPreviewPolicy.decide(
    account: .proved("account-1"), machineCredential: .proved("machine-1"),
    relayReady: .proved("ready"), e2eePairing: .proved("viewer-1"),
    viewerReceipt: .proved("received"))
check(allCloudProofs.succeeded, "cloud succeeds only when all five facts are proved")
check(checks == 29, "expected focused policy check count")
print("30 checks passed")
`, "utf8");

const compile = spawnSync("xcrun", ["swiftc", "-D", "ONBOARDING_POLICY_ONLY", source, harness, "-o", binary], {
  encoding: "utf8",
});
if (compile.status !== 0) {
  process.stderr.write(compile.stdout + compile.stderr);
  process.exit(compile.status || 1);
}

const run = spawnSync(binary, [join(work, "store")], { encoding: "utf8" });
process.stdout.write(run.stdout);
process.stderr.write(run.stderr);
if (run.status !== 0 || !run.stdout.includes("30 checks passed")) {
  process.exit(run.status || 2);
}

const remoteSource = readFileSync(remoteQRSource, "utf8");
const signInFunction = remoteSource.match(/static func signInURL[\s\S]*?^    \}/m)?.[0] || "";
if (!signInFunction.includes("guard case .up") ||
    !signInFunction.includes('else { return "" }') || signInFunction.includes("127.0.0.1")) {
  process.stderr.write("FAIL after 30 checks: phone QR accepts a non-up or loopback address\n");
  process.exit(5);
}
console.log("✓ phone QR is gated on the live tunnel URL");

// The gate above only decides what a code may contain. Settings has the other half: it mints a
// real RemoteAuth device before it asks for an address, so an ungated caller leaves a live key in
// the device list and shows an empty square. Assert the order, not just the presence of a guard.
const settingsSource = readFileSync(settings, "utf8");
const pairPhone = settingsSource.match(/private func pairPhone\(\) \{[\s\S]*?^    \}/m)?.[0] || "";
const gateAt = pairPhone.indexOf("case .up = RemoteTunnel.shared.state");
const mintAt = pairPhone.indexOf("RemoteAuth.addDevice");
if (gateAt === -1 || mintAt === -1 || gateAt > mintAt) {
  process.stderr.write("FAIL after 31 checks: Settings mints a phone key without a live tunnel\n");
  process.exit(7);
}
console.log("✓ settings refuses to mint a phone key with no live tunnel");

const copyFiles = readdirSync(sourcesDirectory).filter((name) => /^Copy\+.*\.swift$/.test(name));
const conformers = copyFiles.reduce((count, name) => {
  const text = readFileSync(join(sourcesDirectory, name), "utf8");
  return count + (text.match(/func setupCloudFacts\(/g) || []).length;
}, 0);
if (conformers !== 14) {
  process.stderr.write(`FAIL after 32 checks: expected 14 localized onboarding conformers, found ${conformers}\n`);
  process.exit(6);
}
console.log("✓ all 14 copy conformers carry the remote onboarding facts");

const installSource = readFileSync(installer, "utf8");
const launchFunction = installSource.match(/launch_installed_app\(\) \{[\s\S]*?^\}/m)?.[0];
if (!launchFunction) {
  process.stderr.write("FAIL: installer has no bounded launch_installed_app behavior\n");
  process.exit(3);
}
console.log("✓ installer has bounded launch behavior");

const launchProbe = spawnSync("/bin/bash", ["-c", `${launchFunction}
DEST='/tmp/Clawdline Install Probe'
APP='Clawdline.app'
open() { printf '%s' "$1"; }
launch_installed_app open
`], { encoding: "utf8" });
if (launchProbe.status !== 0 || launchProbe.stdout !== "/tmp/Clawdline Install Probe/Clawdline.app") {
  process.stderr.write("FAIL: installer did not open the exact installed app path\n");
  process.exit(4);
}
console.log("✓ installer opens the exact installed app path");
console.log("35 checks passed");
