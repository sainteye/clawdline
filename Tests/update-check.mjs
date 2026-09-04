#!/usr/bin/env node

// Whether there is a newer Clawdline — compiled and run, against the shipped bytes.
//
// **What it is here for.** A check that answers "nothing newer" when it has in fact been
// rate-limited is worse than no check at all: it is a silence that reads as an all-clear, and the
// person on the old build never finds out. So the thing this suite is really about is the third
// state — that `unavailable` exists, that every way of failing lands in it, and that none of them
// can be spelled `current`. The one that matters most is the parse: if GitHub's shape moves and
// `tag_name` stops matching, a scan that found nothing must not look like a feed that had nothing.
//
// **Why it does not live in the Swift suite.** `Sources/UpdateCheck.swift` is mostly decision and
// no AppKit, but the file also holds the URLSession feed, the store and the checker, and compiling
// it means compiling the module — the run this repository serialises behind a machine-wide lock.
// The decision half is bounded by two marker comments instead, the same arrangement
// `Tests/codex-client-identity.mjs` uses on `Sources/CodexNaming.swift`, and this lifts it out and
// compiles it against the real `Sources/Compat.swift` in about a second. Nothing here is a
// stand-in for the thing under test: the enum and its rules are the shipped bytes.
//
// **The markers are asserted, not assumed.** Each must occur exactly once and the lifted block
// must contain the enum, because a slice that silently becomes empty — or becomes the whole file —
// is the failure that makes a source-reading guard worthless.

import { mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..");
const sourcePath = resolve(process.env.CLAWDLINE_UPDATE_CHECK_SOURCE
  || join(repoRoot, "Sources", "UpdateCheck.swift"));
const compatPath = resolve(process.env.CLAWDLINE_COMPAT_SOURCE
  || join(repoRoot, "Sources", "Compat.swift"));
const mainPath = resolve(process.env.CLAWDLINE_MAIN_SOURCE
  || join(repoRoot, "Sources", "main.swift"));
const packagePath = resolve(process.env.CLAWDLINE_PACKAGE_SOURCE
  || join(repoRoot, "Package.swift"));
const copyDir = resolve(process.env.CLAWDLINE_COPY_DIR || join(repoRoot, "Sources"));

const OPEN = "// >>> clawdline update reading >>>";
const CLOSE = "// <<< clawdline update reading <<<";
// Every check the Swift harness below makes. Named here because a harness that exits 0 having
// asserted nothing looks exactly like one that asserted everything — the count is the difference.
const SWIFT_CHECKS = 46;

let checks = 0;
let failures = 0;
const check = (what, ok) => {
  checks += 1;
  if (!ok) failures += 1;
  console.log(`  ${ok ? "✓" : "✗"} ${what}`);
};
const stop = (why) => {
  console.log(`  ✗ ${why}`);
  console.log(`update check: stopped after ${checks} checks — ${why}`);
  process.exit(1);
};
const occurrences = (haystack, needle) => haystack.split(needle).length - 1;

const work = mkdtempSync(join(process.env.TMPDIR || tmpdir(), "clawdline-update-check-"));
process.on("exit", () => { try { rmSync(work, { recursive: true, force: true }); } catch { /* gone */ } });
try {
  const source = readFileSync(sourcePath, "utf8");
  const compat = readFileSync(compatPath, "utf8");
  const appMain = readFileSync(mainPath, "utf8");

  // ---- no dependency was taken on for this ------------------------------------------------------
  // The README's badge says `dependencies-none` and that is a property of the product, not a mood.
  // Sparkle is the obvious way to do this feature and it is the one thing this may not do.
  const manifest = readFileSync(packagePath, "utf8");
  check("Package.swift still declares no dependencies",
        /dependencies:\s*\[\s*\]/.test(manifest) || !/\bdependencies:/.test(manifest));
  check("and the update check imports Foundation and nothing else",
        source.split("\n").filter((line) => /^import /.test(line)).join(",") === "import Foundation");
  check("no third-party updater is named anywhere in it",
        !/Sparkle|SUUpdater|appcast/i.test(source));
  // It only says. Replacing the running app is build.sh's job, and build.sh closes and reopens the
  // app somebody is using — an update check must not be able to do that behind their back.
  check("and it installs nothing: no unzip, no ditto, no relaunch",
        !/\bditto\b|\bunzip\b|NSWorkspace|Process\(\)/.test(source));

  // ---- lift the decision block -----------------------------------------------------------------
  check("the reading block's opening marker occurs exactly once", occurrences(source, OPEN) === 1);
  check("and its closing marker exactly once", occurrences(source, CLOSE) === 1);
  if (occurrences(source, OPEN) !== 1 || occurrences(source, CLOSE) !== 1) {
    stop("the markers this suite lifts the block out by are not where it can find them");
  }
  const start = source.indexOf(OPEN);
  const end = source.indexOf(CLOSE);
  check("the closing marker comes after the opening one", end > start);
  if (end <= start) stop("the reading block's markers are inverted");
  const block = source.slice(start, end + CLOSE.length);
  check("the lifted block is a real slice of the file, not the whole of it and not nothing",
        block.length > 2000 && block.length < source.length);
  check("and it contains the enum this suite is about", /enum UpdateCheck\b/.test(block));
  check("the version comparison is Compat's, not a second one written here",
        occurrences(block, "Compat.compare(") === 1 && !/func compare\(/.test(block));

  // ---- the call sites --------------------------------------------------------------------------
  // The decision can be right and never reach anybody. These are the two places it is shown.
  check("the menu offers the newer release through the localised row",
        /L\.t\.updateAvailable\(latest:/.test(appMain));
  check("and opens the releases page rather than a URL typed a second time",
        /URL\(string: UpdateCheck\.releasesPage\)/.test(appMain)
        && occurrences(appMain, "github.com/sainteye/clawdline/releases") === 0);
  check("the compatibility rows are worded from Compat.Standing, in the reader's language",
        /Compat\.standings\(newerRelease:/.test(appMain) && /L\.t\.compatNote\(standing\)/.test(appMain));
  check("and the check is started at launch", /UpdateChecker\.shared\.start\(\)/.test(appMain));
  check("Compat's ahead arm is reached only with a release in hand",
        /case \.orderedDescending:\s*\n\s*guard let newerRelease, !newerRelease\.isEmpty else \{ return nil \}/
          .test(compat));

  // ---- fourteen languages, and fourteen different sentences --------------------------------------
  // The compiler already refuses a `Copy` that is missing a member, so counting them proves little.
  // What it cannot see is English pasted into a translation: fourteen implementations that all
  // compile and all say the same thing.
  const copyFiles = readdirSync(copyDir).filter((name) => /^Copy\+.*\.swift$/.test(name));
  check("there are thirteen Copy files to read", copyFiles.length === 13);
  const rows = [];
  for (const name of copyFiles) {
    const text = readFileSync(join(copyDir, name), "utf8");
    for (const line of text.split("\n")) {
      const hit = line.match(/^\s*"(Clawdline .*|O Clawdline .*|Вышла Clawdline .*)"$/);
      if (hit) rows.push(hit[1]);
    }
  }
  check(`fourteen structs word the update row (saw ${rows.length})`, rows.length === 14);
  check("and no two of them say the same thing", new Set(rows).size === rows.length);
  check("every one of them names both versions",
        rows.every((row) => row.includes("\\(latest)") && row.includes("\\(installed)")));
  const noteArms = [];
  for (const name of copyFiles) {
    const text = readFileSync(join(copyDir, name), "utf8");
    noteArms.push(occurrences(text, "func compatNote(_ standing: Compat.Standing) -> String"));
  }
  check("and every Copy file implements the compatibility wording once per struct",
        noteArms.reduce((a, b) => a + b, 0) === 14);

  // ---- compile the real bytes ------------------------------------------------------------------
  const subject = join(work, "reading.swift");
  writeFileSync(subject, `import Foundation\n\n${block}\n`, "utf8");
  const harness = join(work, "main.swift");
  writeFileSync(harness, String.raw`
import Foundation

// Stand-ins for what Sources/Compat.swift reaches out of itself for, and nothing else.
enum Assistant: String, CaseIterable {
    case claude, codex
    var command: String { rawValue }
    var label: String { rawValue }
}
extension Process { func waitQuietly() { waitUntilExit() } }

var checks = 0
func check(_ condition: @autoclosure () -> Bool, _ name: String) {
    guard condition() else {
        FileHandle.standardError.write(Data(("FAIL after \(checks) Swift checks: " + name + "\n").utf8))
        exit(1)
    }
    checks += 1
    print("  ✓ " + name)
}

let running = "9.9.9"
func body(_ text: String) -> Data { Data(text.utf8) }
func ok(_ tag: String) -> UpdateCheck.Answer {
    .http(status: 200, body: body("{\"tag_name\": \"" + tag + "\", \"name\": \"a release\"}"))
}
func read(_ answer: UpdateCheck.Answer, _ installed: String = running) -> UpdateCheck.Outcome {
    UpdateCheck.outcome(for: answer, installed: installed)
}

// ---- a feed that answers ----------------------------------------------------------------------
check(read(ok("v9.9.10")) == .newer(latest: "9.9.10"),
      "a tag newer than this build is a release to move to, with its v taken off")
check(read(ok("9.9.10")) == .newer(latest: "9.9.10"),
      "and a tag without the v reads the same, because the v is a habit and not a promise")
check(read(ok("v9.9.9")) == .current(latest: "9.9.9"),
      "the same version is not an update")
check(read(ok("v9.9.8")) == .current(latest: "9.9.8"),
      "and neither is an older one — a machine building from source is ahead of the feed")
check(read(ok("v9.10.0")) == .newer(latest: "9.10.0"),
      "ten is after nine, because the comparison is Compat's and not a string's")

// ---- a feed that answers something this cannot read ---------------------------------------------
// The one that matters. Every arm here must be unavailable, and the assertion is written as
// "is not current" as well as "is unreadable", because the defect being guarded against is
// precisely a failure wearing the all-clear's clothes.
let noTag = read(.http(status: 200, body: body("{\"name\": \"a release\"}")))
check(noTag == .unavailable(.unreadable), "a 200 with no tag_name in it is unreadable")
check(noTag != .current(latest: ""), "and it is not current — a scan that stopped matching is not a feed with nothing in it")
check(read(.http(status: 200, body: body("{\"tag_name\": 7}"))) == .unavailable(.unreadable),
      "a tag_name that is not a string is unreadable")
check(read(.http(status: 200, body: body("[{\"tag_name\": \"v9.9.10\"}]"))) == .unavailable(.unreadable),
      "an array where the object should be is unreadable, however good the tag inside it looks")
check(read(.http(status: 200, body: body("<html>sign in</html>"))) == .unavailable(.unreadable),
      "and a captive portal answering 200 with a login page is unreadable")
check(read(ok("nightly")) == .unavailable(.unreadable),
      "a tag that is a word rather than a version is unreadable, not zero")
check(read(.http(status: 200, body: Data())) == .unavailable(.unreadable),
      "an empty body is unreadable")

// ---- a feed that refuses ------------------------------------------------------------------------
let limited = read(.http(status: 403, body: body(
    "{\"message\": \"API rate limit exceeded for 203.0.113.7.\"}")))
check(limited == .unavailable(.rateLimited),
      "GitHub's hourly limit is told apart from other refusals by the words install.sh looks for")
check(read(.http(status: 403, body: body("{\"message\": \"Forbidden\"}"))) == .unavailable(.http(403)),
      "a 403 that is not a limit keeps its status rather than being guessed at")
check(read(.http(status: 404, body: body("{\"message\": \"Not Found\"}"))) == .unavailable(.http(404)),
      "and so does a repository that has moved")
check(read(.http(status: 500, body: body("oops"))) == .unavailable(.http(500)),
      "and so does GitHub having a bad afternoon")
check(read(.unreachable("The Internet connection appears to be offline."))
        == .unavailable(.unreachable("The Internet connection appears to be offline.")),
      "nothing answering at all keeps the system's own words for the log")
check(read(ok("v9.9.10"), "") == .unavailable(.unknownVersion),
      "a process that cannot say what version it is compares nothing")
check(read(ok("v9.9.10"), "   ") == .unavailable(.unknownVersion),
      "and a blank version is absent rather than 0.0.0, which would announce an update to every test binary")

// ---- no failure is ever an offer ----------------------------------------------------------------
let everyFailure: [UpdateCheck.Failure] = [
    .rateLimited, .http(403), .unreachable("offline"), .unreadable, .unknownVersion,
]
check(everyFailure.allSatisfy { UpdateCheck.Outcome.unavailable($0).newerRelease == nil },
      "no way of failing offers a release to move to")
check(everyFailure.allSatisfy { !UpdateCheck.Outcome.unavailable($0).isAnswer },
      "and none of them counts as an answer, so none of them is kept for a day")
check(UpdateCheck.Outcome.newer(latest: "9.9.10").newerRelease == "9.9.10"
      && UpdateCheck.Outcome.current(latest: "9.9.9").newerRelease == nil,
      "only newer offers one")
check(Set(everyFailure.map { UpdateCheck.describe($0) }).count == 5,
      "and the five of them are five different sentences, not one shrug")

// ---- how often ----------------------------------------------------------------------------------
let now = Date(timeIntervalSince1970: 1_800_000_000)
func reading(_ outcome: UpdateCheck.Outcome, ago: TimeInterval) -> UpdateCheck.Reading {
    UpdateCheck.Reading(at: now.addingTimeInterval(-ago), installed: running, outcome: outcome)
}
check(UpdateCheck.isDue(now: now, last: nil), "with nothing stored, the first launch asks")
check(!UpdateCheck.isDue(now: now, last: reading(.current(latest: "9.9.9"), ago: 23 * 3600)),
      "an answer from this morning is not asked again, however many times the app is relaunched")
check(UpdateCheck.isDue(now: now, last: reading(.current(latest: "9.9.9"), ago: 25 * 3600)),
      "an answer from yesterday is")
check(!UpdateCheck.isDue(now: now, last: reading(.unavailable(.rateLimited), ago: 30 * 60)),
      "a rate-limited address is not asked again inside the hour it was told to wait")
check(UpdateCheck.isDue(now: now, last: reading(.unavailable(.rateLimited), ago: 61 * 60)),
      "and is asked once that hour is over")
check(UpdateCheck.isDue(now: now, last: reading(.current(latest: "9.9.9"), ago: -3600)),
      "a reading stamped in the future is due rather than a lock — a clock that was wrong must not silence this forever")

// ---- what the log says --------------------------------------------------------------------------
let lines = [
    UpdateCheck.logLine(reading(.newer(latest: "9.9.10"), ago: 0)),
    UpdateCheck.logLine(reading(.current(latest: "9.9.9"), ago: 0)),
    UpdateCheck.logLine(reading(.unavailable(.rateLimited), ago: 0)),
    UpdateCheck.logLine(reading(.unavailable(.unreadable), ago: 0)),
]
check(Set(lines).count == 4, "the four things that can happen are four different lines in the log")
check(lines.allSatisfy { $0.hasPrefix("update check: ") },
      "and all of them are findable by one word in a log full of other things")
check(lines[2].contains("hourly limit") && lines[2].contains("next attempt"),
      "a rate-limited check says whose limit it was and when it will try again")
check(!lines[2].contains("newest release") && !lines[3].contains("newest release"),
      "and neither failure borrows the sentence a successful check uses")

// ---- kept between launches ----------------------------------------------------------------------
let everyOutcome: [UpdateCheck.Outcome] = [
    .newer(latest: "9.9.10"), .current(latest: "9.9.9"),
    .unavailable(.rateLimited), .unavailable(.http(403)),
    .unavailable(.unreachable("offline")), .unavailable(.unreadable),
    .unavailable(.unknownVersion),
]
check(everyOutcome.allSatisfy { outcome in
          let before = UpdateCheck.Reading(at: now, installed: running, outcome: outcome)
          return UpdateCheck.decode(UpdateCheck.encode(before)) == before
      },
      "every one of the seven readings survives being written down and read back")
let storedLimit = UpdateCheck.encode(
    UpdateCheck.Reading(at: now, installed: running, outcome: .unavailable(.rateLimited)))
check(storedLimit.contains("\"state\" : \"unavailable\"") && storedLimit.contains("\"reason\" : \"rate_limited\""),
      "and the file says which of the three states it is in words a person can read")
check(UpdateCheck.decode(storedLimit)?.outcome != .current(latest: "9.9.9"),
      "a stored failure does not come back as an answer")
check(UpdateCheck.decode("") == nil, "an empty file is nothing, not a reading")
check(UpdateCheck.decode("not json at all") == nil, "and neither is a file somebody edited badly")
check(UpdateCheck.decode("{\"state\": \"current\", \"latest\": \"9.9.9\", \"installed\": \"9.9.9\"}") == nil,
      "a reading with no timestamp is refused rather than treated as new")
check(UpdateCheck.decode("{\"at\": \"2027-01-15T00:00:00Z\", \"installed\": \"9.9.9\", \"state\": \"current\"}") == nil,
      "and so is a current that does not say what it was current against")
check(UpdateCheck.decode("{\"at\": \"2027-01-15T00:00:00Z\", \"installed\": \"9.9.9\", \"state\": \"marvellous\"}") == nil,
      "a state written by a later version is refused, not read as the nearest one this knows")
check(UpdateCheck.decode("{\"at\": \"2027-01-15T00:00:00Z\", \"installed\": \"9.9.9\", \"state\": \"unavailable\", \"reason\": \"http\"}") == nil,
      "and an http failure with no status is not an http failure")
let ancient = UpdateCheck.Reading(at: now.addingTimeInterval(-100 * 24 * 3600),
                                  installed: "9.9.8", outcome: .newer(latest: "9.9.9"))
check(UpdateCheck.decode(UpdateCheck.encode(ancient))?.installed == "9.9.8",
      "a stored reading remembers which build took it, so an offer cannot outlive the install that answers it")

check(UpdateCheck.feed.hasPrefix("https://api.github.com/repos/") && UpdateCheck.feed.hasSuffix("/releases/latest"),
      "the feed is the release endpoint install.sh reads")
check(UpdateCheck.releasesPage.hasPrefix("https://github.com/") && !UpdateCheck.releasesPage.contains("api."),
      "and the page a person is sent to is one a browser can open")
check(UpdateCheck.afterAnswer == 24 * 3600 && UpdateCheck.afterFailure == 3600,
      "a day between answers, an hour after a failure, and both are stated rather than sprinkled")

print("\(checks) swift checks passed")
`, "utf8");

  const binary = join(work, "update-check");
  const compile = spawnSync("xcrun", [
    "swiftc", "-swift-version", "5", "-target", "arm64-apple-macos13.0",
    subject, compatPath, harness, "-o", binary,
  ], { encoding: "utf8" });
  if (compile.status !== 0) {
    process.stderr.write(compile.stdout + compile.stderr);
    stop("the lifted reading block did not compile");
  }
  check("the lifted block compiles against the real Compat.swift", true);

  const run = spawnSync(binary, { encoding: "utf8" });
  process.stdout.write(run.stdout);
  process.stderr.write(run.stderr);
  const swiftChecks = Number((run.stdout.match(/(\d+) swift checks passed/) || [])[1] || 0);
  check("the compiled reading block ran to the end", run.status === 0);
  check(`and asserted all ${SWIFT_CHECKS} of its checks (saw ${swiftChecks})`,
        swiftChecks === SWIFT_CHECKS);

  console.log(failures === 0
    ? `update check: all ${checks} node checks passed, ${swiftChecks} swift checks passed`
    : `update check: ${failures} of ${checks} node checks failed`);
  process.exit(failures === 0 ? 0 : 1);
} finally {
  rmSync(work, { recursive: true, force: true });
}
