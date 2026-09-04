#!/usr/bin/env node

// What Clawdline tells `codex app-server` it is, compiled and run rather than grepped.
//
// **What it is here for.** `Sources/CodexNaming.swift` sent `"version": "0.6.0"` in its
// `initialize` for as long as 0.6.0 was current and then for the whole of 0.7.0, because the string
// was typed rather than derived and nothing in the suite compared it with anything. A `grep -c`
// over `Tests/` for that line answered zero on 2026-09-04, which is the whole story: the drift was
// not hard to find, there was simply nobody to find it.
//
// **Why it does not live in the Swift suite.** `Sources/CodexNaming.swift` names `Assistant`,
// `TargetSession`, `SessionState`, `Config` and more, so compiling it means compiling the module,
// which is the 288-second run this repository serialises behind a machine-wide lock. The identity
// block is bounded by two marker comments instead — the same arrangement `Tests/test-sh-lock.mjs`
// uses on `test.sh` — and this file lifts it out and compiles it against the real
// `Sources/Compat.swift` and a handful of stand-ins in about a second. Nothing here is a stand-in
// for the thing under test: the enum and the release table are the shipped bytes.
//
// **The markers are asserted, not assumed.** Each must occur exactly once, and the lifted block
// must contain the enum. A slice that silently becomes empty — or becomes the whole file — is the
// failure mode that makes a source-reading guard worthless, so both are checked before the
// compiler is called.

import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..");
const namingPath = resolve(process.env.CLAWDLINE_CODEX_NAMING_SOURCE
  || join(repoRoot, "Sources", "CodexNaming.swift"));
const compatPath = resolve(process.env.CLAWDLINE_COMPAT_SOURCE
  || join(repoRoot, "Sources", "Compat.swift"));
const buildPath = resolve(process.env.CLAWDLINE_BUILD_SCRIPT || join(repoRoot, "build.sh"));

const OPEN = "// >>> clawdline client identity >>>";
const CLOSE = "// <<< clawdline client identity <<<";

let checks = 0;
let failures = 0;
const check = (what, ok) => {
  checks += 1;
  if (!ok) failures += 1;
  console.log(`  ${ok ? "✓" : "✗"} ${what}`);
};
const stop = (why) => {
  console.log(`  ✗ ${why}`);
  console.log(`codex client identity: stopped after ${checks} checks — ${why}`);
  process.exit(1);
};
const occurrences = (haystack, needle) => haystack.split(needle).length - 1;

const work = mkdtempSync(join(process.env.TMPDIR || tmpdir(), "clawdline-codex-identity-"));
// `stop()` and the successful ending both leave through `process.exit`, which skips `finally`. An
// exit handler is the one path all of them share, so the compile directory goes whichever way this
// file ends.
process.on("exit", () => { try { rmSync(work, { recursive: true, force: true }); } catch { /* gone */ } });
try {
  const naming = readFileSync(namingPath, "utf8");
  const compat = readFileSync(compatPath, "utf8");
  const build = readFileSync(buildPath, "utf8");

  // ---- the version this checkout is actually building ------------------------------------------
  // `build.sh` is where the version is stamped, and `Tests/TestHarness.swift`'s `appVersion()`
  // reads exactly this line. Parsed here rather than passed in, so the number the assertions below
  // use comes from the same place the bundle's does.
  const stamped = build.split("\n").filter((line) => line.includes("CFBundleShortVersionString"));
  check("build.sh stamps CFBundleShortVersionString on exactly one line", stamped.length === 1);
  if (stamped.length !== 1) stop("cannot read the version this checkout builds");
  const version = (stamped[0].match(/<string>([^<]*)<\/string>/) || [])[1];
  check("and that line carries a version, not an empty element",
        typeof version === "string" && /^\d+\.\d+\.\d+$/.test(version));
  if (!version) stop("build.sh's CFBundleShortVersionString is empty");
  console.log(`    build.sh says ${version}`);

  // ---- lift the identity block -----------------------------------------------------------------
  check("the identity block's opening marker occurs exactly once in CodexNaming.swift",
        occurrences(naming, OPEN) === 1);
  check("and its closing marker exactly once", occurrences(naming, CLOSE) === 1);
  if (occurrences(naming, OPEN) !== 1 || occurrences(naming, CLOSE) !== 1) {
    stop("the markers this suite lifts the block out by are not where it can find them");
  }
  const start = naming.indexOf(OPEN);
  const end = naming.indexOf(CLOSE);
  check("the closing marker comes after the opening one", end > start);
  if (end <= start) stop("the identity block's markers are inverted");
  const block = naming.slice(start, end + CLOSE.length);
  check("the lifted block is a real slice of the file, not the whole of it and not nothing",
        block.length > 200 && block.length < naming.length / 2);
  check("and it contains the enum this suite is about",
        /enum ClawdlineClientIdentity\b/.test(block));

  // ---- the call site --------------------------------------------------------------------------
  // The block can be perfect and still be bypassed. `initialize` is the one caller, and this is the
  // check that says so: the request passes the derived params, and the file types no version.
  check("the initialize request passes the derived params rather than a literal object",
        occurrences(naming, "ClawdlineClientIdentity.initializeParams()") === 1
        && /request\(method: "initialize",\s*\n?\s*params: ClawdlineClientIdentity\.initializeParams\(\)/
             .test(naming));
  check("and CodexNaming.swift types no three-part version literal of its own",
        !/"\d+\.\d+\.\d+"/.test(naming));

  // ---- compile the real bytes ------------------------------------------------------------------
  // Stand-ins for what `Sources/Compat.swift` reaches out of itself for, and nothing else. They are
  // deliberately dull: if one of them ever has to know something about a version, this file is
  // testing its own fixture rather than the app.
  const subject = join(work, "identity.swift");
  writeFileSync(subject, `import Foundation\n\n${block}\n`, "utf8");
  const harness = join(work, "main.swift");
  writeFileSync(harness, String.raw`
import Foundation

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

let stamped = CommandLine.arguments[1]

// The arm every process without an app bundle takes — the test suite, and this harness.
check(ClawdlineClientIdentity.version(bundleVersion: nil) == stamped,
      "with no bundle to ask, the version is the compatibility table's newest release, and it is "
      + "the version build.sh stamps (" + stamped + ")")
check(Compat.releases.first?.clawdline == stamped,
      "which is the same statement read the other way round: the table's newest row is this build")

// The arm the app itself takes. Sampling only the fallback would leave the path that actually ships
// unmeasured, which is how the literal survived three releases in the first place.
check(ClawdlineClientIdentity.version(bundleVersion: "9.9.9") == "9.9.9",
      "inside a bundle, the bundle's own version is what goes out")
check(ClawdlineClientIdentity.version(bundleVersion: "") == stamped,
      "an empty version is absent rather than a version — a bundle-less process has the key missing")
check(ClawdlineClientIdentity.version(bundleVersion: "   ") == stamped,
      "and so is a blank one")

let info = ClawdlineClientIdentity.clientInfo(bundleVersion: nil)
check(info["name"] == "clawdline", "the name Codex is told is unchanged")
check(info["title"] == "Clawdline", "and so is the title")
check(info["version"] == stamped, "and the version in clientInfo is the derived one")
check(info.count == 3, "clientInfo carries those three keys and nothing else")

let params = ClawdlineClientIdentity.initializeParams(bundleVersion: "1.2.3")
check((params["clientInfo"] as? [String: String])?["version"] == "1.2.3",
      "initializeParams puts that clientInfo under the key app-server reads")
check(params.count == 1, "and sends nothing else in initialize's params")

// The bytes that actually leave the process. CodexNameServer hands params to JSONSerialization, so
// this is the shape Codex sees rather than a Swift dictionary that looks right.
let encoded = try! JSONSerialization.data(withJSONObject: ClawdlineClientIdentity.initializeParams(bundleVersion: nil))
let text = String(decoding: encoded, as: UTF8.self)
check(text.contains("\"version\":\"" + stamped + "\""),
      "and serialised for the wire it carries " + stamped)
check(!text.contains("0.6.0") || stamped == "0.6.0",
      "the version that drifted for three releases is not in the payload unless it is current")

print("\(checks) swift checks passed")
`, "utf8");

  const binary = join(work, "codex-client-identity");
  const compile = spawnSync("xcrun", [
    "swiftc", "-swift-version", "5", "-target", "arm64-apple-macos13.0",
    subject, compatPath, harness, "-o", binary,
  ], { encoding: "utf8" });
  if (compile.status !== 0) {
    process.stderr.write(compile.stdout + compile.stderr);
    stop("the lifted identity block did not compile");
  }
  check("the lifted block compiles against the real Compat.swift", true);

  const run = spawnSync(binary, [version], { encoding: "utf8" });
  process.stdout.write(run.stdout);
  process.stderr.write(run.stderr);
  const swiftChecks = Number((run.stdout.match(/(\d+) swift checks passed/) || [])[1] || 0);
  check("the compiled identity block ran to the end", run.status === 0);
  // A count in the output is what tells "clean" from "never looked": a harness that exits 0 having
  // asserted nothing reads exactly like one that asserted everything.
  check(`and asserted all 13 of its checks (saw ${swiftChecks})`, swiftChecks === 13);

  console.log(failures === 0
    ? `codex client identity: all ${checks} node checks passed, ${swiftChecks} swift checks passed`
    : `codex client identity: ${failures} of ${checks} node checks failed`);
  process.exit(failures === 0 ? 0 : 1);
} finally {
  rmSync(work, { recursive: true, force: true });
}
