#!/usr/bin/env node

import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const source = resolve(process.env.CLAWDLINE_ONBOARDING_SOURCE || "Sources/Onboarding.swift");
const installer = resolve(process.env.CLAWDLINE_INSTALL_SOURCE || "install.sh");
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
check(checks == 17, "expected focused check count")
print("18 checks passed")
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
if (run.status !== 0 || !run.stdout.includes("18 checks passed")) {
  process.exit(run.status || 2);
}

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
console.log("20 checks passed");
