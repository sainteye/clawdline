#!/usr/bin/env node

// The one write that carries a phone's diagnostic onto this Mac, driven against a real disk.
//
// **Why it is here and not in the Swift suite.** `Sources/DiagnosticReport.swift` depends on
// Foundation and nothing else, so it compiles on its own in a couple of seconds beside a harness —
// the shape `Tests/app-onboarding-focused.mjs` and `Tests/keychain-rebuild-focused.mjs` already
// use. That matters for this feature in particular: the three ways it can lie are all about what
// is or is not on the disk afterwards, and proving a guard goes red for each of them costs one
// short compile here instead of a five-minute whole-module one under the machine lock.
//
// **The three failures this exists to catch**, each of them a real shape rather than a deleted
// file:
//
//   1. **Accepted, and nothing written.** The reply says success, the file it names is stale or
//      absent, and the person who pressed the button has no way to tell. Caught by reading the
//      path out of the receipt and comparing what is in it against what was sent.
//   2. **Written, and the wrong path named.** Worse than the first, because an agent told to read
//      that path reads *something* — yesterday's report — and reasons about it. Caught the same
//      way: the receipt's path must hold this report and not the one before it.
//   3. **Over the limit, and success returned.** A cap that is not enforced is a directory with no
//      bound on it and a refusal nobody can act on. Caught by asking for a body one byte over and
//      requiring both a typed refusal and an untouched disk.
//
// What it cannot say: nothing here goes through HTTP. That the route is wired to this store, and
// authenticated, is `Sources/RemoteServer.swift`'s own case and is read as source below.

import { existsSync, mkdtempSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const storeSource = resolve(process.env.CLAWDLINE_DIAGNOSTIC_SOURCE
  || "Sources/DiagnosticReport.swift");
const serverSource = resolve(process.env.CLAWDLINE_REMOTE_SERVER_SOURCE
  || "Sources/RemoteServer.swift");
const panelSource = resolve(process.env.CLAWDLINE_DIAGNOSTIC_PANEL_SOURCE
  || "Resources/web/app/js/core/layout-diagnostics.js");
const doc = resolve(process.env.CLAWDLINE_DIAGNOSTIC_DOC || "docs/diagnostics.md");
const work = mkdtempSync(join(process.env.TMPDIR || tmpdir(), "clawdline-diagnostic-"));

let checks = 0;
function check(condition, name) {
  if (!condition) {
    process.stderr.write(`FAIL after ${checks} node checks: ${name}\n`);
    process.exitCode = 2;
    throw new Error(name);
  }
  checks += 1;
  console.log(`  ✓ ${name}`);
}

const store = readFileSync(storeSource, "utf8");
const server = readFileSync(serverSource, "utf8");
const panel = readFileSync(panelSource, "utf8");
const guide = existsSync(doc) ? readFileSync(doc, "utf8") : "";

/* ---- the fixed path, in all three places that have to agree ---------------------------------
   The point of the feature is that somebody who was not in the room can open the file. That needs
   the path to be a constant, and a constant is only useful while every copy of it says the same
   thing — the code that builds it, the route's own reply, and the document an agent reads. */
check(store.includes('"Library/Logs/Clawdline/diagnostics"'),
      "the store builds its directory under ~/Library/Logs, not ~/.config");
check(/static let fileName = "report\.json"/.test(store)
      && /static let previousFileName = "previous\.json"/.test(store),
      "both file names are fixed constants, not derived from a clock or a UUID");
check(!/UUID\(\)/.test(store), "no UUID reaches a report's name");
check(guide.includes("~/Library/Logs/Clawdline/diagnostics/report.json"),
      "docs/diagnostics.md names the exact path an agent is meant to open");
check(guide.includes("~/Library/Logs/Clawdline/diagnostics/previous.json"),
      "and the press before it");

/* ---- the route, spelled twice ---------------------------------------------------------------- */
const routeInPanel = /export var REPORT_ROUTE = "([^"]+)"/.exec(panel);
check(!!routeInPanel, "the panel names the route it posts to");
check(server.includes(`case ("POST", "${routeInPanel[1]}")`),
      `the server answers that exact route (${routeInPanel[1]})`);
check(/guard case \.allowed\(let device, _\) = permission\(for: request\)/
        .test(server.slice(server.indexOf(`case ("POST", "${routeInPanel[1]}")`),
                           server.indexOf(`case ("POST", "${routeInPanel[1]}")`) + 900)),
      "and refuses it without a paired device");

/* ---- compile the store beside a harness and let it touch a real disk -------------------------- */
const harness = join(work, "main.swift");
const binary = join(work, "diagnostic-report-focused");
writeFileSync(harness, String.raw`
import Foundation

var checks = 0
func check(_ condition: @autoclosure () -> Bool, _ name: String) {
    guard condition() else {
        FileHandle.standardError.write(Data(("FAIL after \(checks) Swift checks: " + name + "\n").utf8))
        exit(1)
    }
    checks += 1
    print("  ok " + name)
}

let root = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
let folder = DiagnosticReport.directory(root: root)
let manager = FileManager.default

func body(_ object: [String: Any]) -> Data {
    try! JSONSerialization.data(withJSONObject: object)
}
func onDisk(_ url: URL) -> [String: Any]? {
    guard let data = try? Data(contentsOf: url),
          let parsed = try? JSONSerialization.jsonObject(with: data) else { return nil }
    return parsed as? [String: Any]
}

// ---- nothing there yet ------------------------------------------------------------------------
check(!manager.fileExists(atPath: DiagnosticReport.reportURL(root: root).path),
      "a Mac that has never been sent a report has no file")

// ---- 1. accepted means written, and the receipt names the file it wrote -----------------------
let first: [String: Any] = ["marker": "first-press",
                            "completeness": ["whole": true, "trace": ["dropped": 0]]]
guard case .success(let one) = DiagnosticReport.save(body(first), device: "phone-a",
                                                     now: Date(timeIntervalSince1970: 1_788_000_000),
                                                     root: root) else {
    FileHandle.standardError.write(Data("FAIL: an ordinary report was refused\n".utf8)); exit(1)
}
check(manager.fileExists(atPath: one.path), "the path the receipt names exists")
let read = onDisk(URL(fileURLWithPath: one.path))
check(read != nil, "and holds JSON")
check((read?["report"] as? [String: Any])?["marker"] as? String == "first-press",
      "and holds THIS report, not an older one — the receipt cannot name a stale file")
check(read?["clawdline_diagnostic_report"] as? Int == 1, "the envelope says what it is")
check(read?["written_by"] as? String == "phone-a", "and which device sent it")
check(read?["written_at"] as? String == "2026-08-29T10:40:00Z", "and when, in UTC")
check(one.bytes > 0 && one.bytes == (try! Data(contentsOf: URL(fileURLWithPath: one.path))).count,
      "the byte count in the receipt is the file's own size")
check(one.completenessStated, "a report carrying a completeness block is recorded as stating one")
check(((read?["completeness"] as? [String: Any])?["whole"] as? Bool) == true,
      "and that block is hoisted to the top of the envelope")
let mode = try! manager.attributesOfItem(atPath: one.path)[.posixPermissions] as! NSNumber
check(mode.intValue == 0o600, "the file is private: it carries session ids and project paths")
check(one.previousPath.isEmpty, "the first press has no press before it")

// ---- a report that says nothing about its own completeness says so ----------------------------
guard case .success(let two) = DiagnosticReport.save(body(["marker": "second-press"]),
                                                     device: "phone-a", root: root) else {
    FileHandle.standardError.write(Data("FAIL: a report with no completeness block was refused\n".utf8)); exit(1)
}
check(!two.completenessStated, "a report with no completeness block is not counted as stating one")
let silent = onDisk(URL(fileURLWithPath: two.path))
check(((silent?["completeness"] as? [String: Any])?["stated"] as? Bool) == false,
      "and the envelope says so in the same field rather than leaving it absent")

// ---- 2. rotation: a second press does not destroy the first ------------------------------------
check(two.previousPath == DiagnosticReport.previousURL(root: root).path,
      "the second press names where the first one went")
check((onDisk(URL(fileURLWithPath: two.previousPath))?["report"] as? [String: Any])?["marker"] as? String
        == "first-press",
      "and the first press is actually there")
check((try! manager.contentsOfDirectory(atPath: folder.path)).sorted() == ["previous.json", "report.json"],
      "two files, both named in advance — the directory cannot grow")

// ---- 3. over the limit is a typed refusal and an untouched disk --------------------------------
let filler = String(repeating: "x", count: DiagnosticReport.maxBytes)
let oversized = body(["marker": "too-big", "filler": filler])
check(oversized.count > DiagnosticReport.maxBytes, "the oversized fixture really is over the limit")
switch DiagnosticReport.save(oversized, device: "phone-a", root: root) {
case .success:
    FileHandle.standardError.write(Data("FAIL: a report over the limit was accepted\n".utf8)); exit(1)
case .failure(let refusal):
    check(refusal.code == "report_too_large", "over the limit is refused by name")
    check(refusal.status == 413, "with the status a client can branch on")
    check(refusal.message.contains(String(oversized.count))
          && refusal.message.contains(String(DiagnosticReport.maxBytes)),
          "and the refusal says both numbers, so the person knows by how much")
}
check((onDisk(DiagnosticReport.reportURL(root: root))?["report"] as? [String: Any])?["marker"] as? String
        == "second-press",
      "a refused report leaves the last good one exactly where it was")
check((try! manager.contentsOfDirectory(atPath: folder.path)).count == 2,
      "and writes nothing beside it")

// ---- the other two refusals -------------------------------------------------------------------
switch DiagnosticReport.save(Data(), device: "phone-a", root: root) {
case .success: FileHandle.standardError.write(Data("FAIL: an empty body was accepted\n".utf8)); exit(1)
case .failure(let refusal):
    check(refusal.code == "empty_report" && refusal.status == 400, "an empty body is its own refusal")
}
switch DiagnosticReport.save(Data("[1,2,3]".utf8), device: "phone-a", root: root) {
case .success: FileHandle.standardError.write(Data("FAIL: a JSON array was accepted\n".utf8)); exit(1)
case .failure(let refusal):
    check(refusal.code == "report_not_json" && refusal.status == 400,
          "a body that is not a JSON object is refused rather than stored as text")
}
switch DiagnosticReport.save(Data("not json at all".utf8), device: "phone-a", root: root) {
case .success: FileHandle.standardError.write(Data("FAIL: unparseable bytes were accepted\n".utf8)); exit(1)
case .failure(let refusal):
    check(refusal.code == "report_not_json", "and so is anything that will not parse")
}

// ---- the route takes whatever events it is given -----------------------------------------------
// The mechanism must survive the next defect having nothing to do with notifications, so nothing
// here may recognise an event name. This is the check that says so.
let unknown: [String: Any] = ["currentTrace": [["t": 1, "event": "gesture.swipe.begin", "data": [:]],
                                               ["t": 2, "event": "whatever.comes.next", "data": [:]]],
                              "completeness": ["whole": false]]
guard case .success(let three) = DiagnosticReport.save(body(unknown), device: "phone-b", root: root) else {
    FileHandle.standardError.write(Data("FAIL: a report about something else was refused\n".utf8)); exit(1)
}
let kept = (onDisk(URL(fileURLWithPath: three.path))?["report"] as? [String: Any])?["currentTrace"] as? [[String: Any]]
check(kept?.count == 2 && kept?[1]["event"] as? String == "whatever.comes.next",
      "events this route has never heard of are stored unread and unchanged")

print("\(checks) Swift checks passed")
`, "utf8");

const compile = spawnSync("xcrun", [
  "swiftc", "-swift-version", "5", "-target", "arm64-apple-macos13.0",
  storeSource, harness, "-o", binary,
], { encoding: "utf8" });
if (compile.status !== 0) {
  process.stderr.write(compile.stdout + compile.stderr);
  process.exitCode = compile.status || 1;
  throw new Error("the diagnostic report store did not compile on its own");
}
check(true, "the store compiles with Foundation and nothing else");

const run = spawnSync(binary, [join(work, "home")], { encoding: "utf8" });
process.stdout.write(run.stdout);
process.stderr.write(run.stderr);
check(run.status === 0, "the store's behaviour passes on a real disk");
const passed = /(\d+) Swift checks passed/.exec(run.stdout);
check(!!passed && Number(passed[1]) === 27,
      `every Swift check ran, not a prefix of them (${passed ? passed[1] : "none"} of 27)`);

/* ---- and the directory really is bounded ------------------------------------------------------ */
const written = readdirSync(join(work, "home", "Library", "Logs", "Clawdline", "diagnostics")).sort();
check(written.join(",") === "previous.json,report.json",
      "seen from outside the process, the directory holds exactly the two named files");

console.log(`diagnostic report store: ${checks} node checks passed`);
