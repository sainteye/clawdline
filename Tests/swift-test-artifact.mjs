// No Swift compiler or machine compile slot is used here. Each fixture owns its fake
// toolchain, SDK, repository, publication directory and lock record under private TMPDIR.
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { chmodSync, copyFileSync, cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync,
  readdirSync, renameSync, rmSync, statSync, linkSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { canonicalIdentity } from "../tools/verified-test-run.mjs";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const helperSource = readFileSync(join(repository, "tools/swift-test-artifact.sh"), "utf8");
const runner = readFileSync(join(repository, "test.sh"), "utf8");
const sourceManifest = readFileSync(join(repository, "tools/swift-source-manifest.sh"), "utf8");
const directory = mkdtempSync(join(tmpdir(), "clawdline-swift-artifact-"));
const started = Date.now();
const only = process.argv[2] === "--case" && process.argv.length === 4 ? process.argv[3] : null;
if (process.argv.length > 2 && !only) throw new Error("usage: node Tests/swift-test-artifact.mjs [--case <exact name>]");
let checks = 0, failures = 0, fixtureNumber = 0;
const results = [];
const sha = (bytes) => createHash("sha256").update(bytes).digest("hex");
const check = (name, body) => {
  if (only && only !== name) return;
  checks++;
  try { body(); results.push({ name, status: "pass" }); }
  catch (error) {
    failures++;
    results.push({ name, status: "fail", message: error.message });
    process.stderr.write(`FAIL ${name}: ${error.message}\n`);
  }
};
const section = (name) => {
  const start = runner.indexOf(`# >>> clawdline ${name} >>>`);
  const end = runner.indexOf(`# <<< clawdline ${name} <<<`);
  assert.ok(start >= 0 && end > start, `missing ${name} boundary`);
  return runner.slice(start, end);
};
const shell = (code, cwd, env = {}) => spawnSync("/bin/bash", ["-c", code], {
  cwd, encoding: "utf8", timeout: 20000, env: { ...process.env, ...env }, maxBuffer: 1024 * 1024,
});
const put = (path, value) => { mkdirSync(dirname(path), { recursive: true }); writeFileSync(path, value); };

function fixture() {
  const base = join(directory, String(++fixtureNumber));
  const root = join(base, "repo");
  const cache = join(base, "cache");
  const sdk = join(base, "sdk");
  const toolchain = join(base, "toolchain");
  const compiler = join(toolchain, "bin/swiftc");
  const lock = join(base, "lock");
  const output = join(base, "out/clawdline-tests");
  const calls = join(base, "calls.jsonl");
  const temp = join(base, "tmp");
  for (const path of [root, cache, sdk, lock, temp]) mkdirSync(path, { recursive: true, mode: 0o700 });
  put(join(root, "Sources/Library.swift"), "let value = 1\n");
  put(join(root, "Tests/main.swift"), "print(value)\n");
  put(join(root, "Tests/TestGroupManifest.swift"), 'let expectedOrderedTestGroupTitles: [String] = [\n'
    + '    "first group",\n    "second " + "group",\n]\n');
  put(join(root, "tools/swift-test-artifact.sh"), helperSource);
  put(join(root, "tools/swift-source-manifest.sh"), '# test manifest\nclawdline_swift_test_target=arm64-apple-macos13.0\n');
  copyFileSync(join(repository, "tools/verified-test-run.mjs"), join(root, "tools/verified-test-run.mjs"));
  put(join(root, "test.sh"), runner);
  put(join(root, "Resources/input.dat"), "resource A\n");
  put(join(sdk, "SDKSettings.json"), '{"version":"one"}\n');
  put(join(sdk, "usr/lib/libA.tbd"), "sdk library one\n");
  put(join(toolchain, "lib/swift/runtime"), "runtime one\n");
  put(join(toolchain, "version"), "Fake Swift version 1\n");
  put(join(toolchain, "settings.json"), "{}\n");
  put(join(lock, "holder.txt"), "token=fixture-token\n");
  const python = spawnSync("/usr/bin/which", ["python3"], { encoding: "utf8" }).stdout.trim();
  put(compiler, `#!${python}
import json, os, pathlib, sys, time
base = pathlib.Path(__file__).resolve().parent.parent
settings = json.loads((base / "settings.json").read_text())
if "--version" in sys.argv:
    print((base / "version").read_text(), end="")
    sys.exit(0)
if "-print-target-info" in sys.argv:
    if settings.get("parent_probe_swap"):
        counter = pathlib.Path(${JSON.stringify(join(base, "probe-count"))})
        n = int(counter.read_text()) + 1 if counter.exists() else 1
        counter.write_text(str(n))
        if n == 3:
            parent = pathlib.Path(${JSON.stringify(dirname(output))})
            parent.rename(parent.with_name("out-original"))
            parent.symlink_to(settings["parent_probe_swap"])
    target_info = settings.get("targetInfo", {
        "compilerVersion": "Fake Swift version 1",
        "target": {"triple": "arm64-apple-macosx13.0"},
        "paths": settings.get("paths", {"runtimeResourcePath": str(base / "lib/swift")})
    })
    print(json.dumps(target_info))
    sys.exit(0)
with open(${JSON.stringify(calls)}, "a") as stream:
    stream.write(json.dumps({"argv": sys.argv[1:], "env": dict(os.environ)}) + "\\n")
time.sleep(settings.get("delay", 0))
if settings.get("change_source"):
    pathlib.Path("Sources/Library.swift").write_text("let value = 99\\n")
if settings.get("lose_lock"):
    pathlib.Path(${JSON.stringify(join(lock, "holder.txt"))}).write_text("token=other\\n")
if settings.get("exit"):
    sys.exit(settings["exit"])
if settings.get("signal"):
    os.kill(os.getpid(), settings["signal"])
if not settings.get("no_output"):
    output = pathlib.Path(sys.argv[sys.argv.index("-o") + 1])
    output.write_text('#!${python}\\nimport os\\n'
        + 'titles = os.environ.get("CLAWDLINE_TEST_GROUPS", "").split("\\\\n")\\n'
        + 'for title in ["first group", "second group"]:\\n'
        + '    if title in titles: print("  ✓ " + title)\\n'
        + 'print(str(len(set(titles))) + " focused checks passed")\\n')
    output.chmod(0o700)
    if settings.get("precreate"):
        final = output.parent.parent.with_name(output.parent.parent.name.removesuffix(".publishing"))
        if settings["precreate"] == "directory": final.mkdir()
        elif settings["precreate"] == "file": final.write_text("competitor")
        elif settings["precreate"] == "symlink": final.symlink_to(${JSON.stringify(join(base, "competitor"))})
        pathlib.Path(${JSON.stringify(join(base, "competitor-inode"))}).write_text(str(final.lstat().st_ino))
    if settings.get("parent_swap"):
        parent = pathlib.Path(${JSON.stringify(dirname(output))})
        parent.mkdir(exist_ok=True)
        parent.rename(parent.with_name("out-original"))
        parent.symlink_to(settings["parent_swap"])
`);
  chmodSync(compiler, 0o700);
  // Only the private fixture index is written; the real checkout's index is never touched.
  for (const args of [["init", "-q"], ["add", "--", "Sources", "Tests", "tools", "test.sh", "Resources"],
    ["-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture"]]) {
    const result = spawnSync("git", args, { cwd: root, encoding: "utf8" });
    assert.equal(result.status, 0, result.stderr);
  }
  const env = {
    TMPDIR: temp, CLAWDLINE_SWIFT_TEST_CACHE_DIR: cache, CLAWDLINE_SWIFT_TEST_SWIFTC: compiler,
    SDKROOT: sdk, CLAWDLINE_SUITE_LOCK_DIR: lock, CLAWDLINE_SWIFT_TEST_ARTIFACT: "reuse",
    CLAWDLINE_TEST_GROUPS: "first group", PATH: dirname(compiler) + ":" + process.env.PATH,
  };
  const setup = `. tools/swift-test-artifact.sh
clawdline_suite_lock_token=fixture-token
clawdline_confirm_suite_lock() { [ "$(cat "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt")" = 'token=fixture-token' ]; }
clawdline_swift_test_compile_resources=(tools/swift-source-manifest.sh tools/swift-test-artifact.sh test.sh Resources)
clawdline_library_sources=(Sources/Library.swift)
clawdline_test_sources=(Tests/main.swift)
clawdline_suite_jobs_flags=(-j 1)
clawdline_swift_test_target=arm64-apple-macos13.0
clawdline_linux_package_focused_only=0
clawdline_test_profile=infrastructure
BIN=${JSON.stringify(output)}
progress_phase() { :; }
clawdline_suite_lock_phase() { :; }
`;
  const args = "-swift-version 5 -target arm64-apple-macos13.0 -j 1 -- Sources/Library.swift Tests/main.swift";
  const run = (options = {}) => shell("set -euo pipefail\n" + setup
    + (options.code ?? `clawdline_swift_test_artifact "$BIN" ${options.args ?? args}`), root,
    { ...env, ...options.env });
  const publications = () => readdirSync(cache).filter(name => /^[a-f0-9]{64}$/.test(name));
  const count = () => existsSync(calls) ? readFileSync(calls, "utf8").trim().split("\n").length : 0;
  const settings = (value) => put(join(toolchain, "settings.json"), JSON.stringify(value));
  return { base, root, cache, sdk, toolchain, compiler, lock, output, calls, temp, env, setup,
    args, run, publications, count, settings };
}

const receipt = result => {
  assert.equal(result.status, 0, result.stderr);
  const rows = result.stdout.split("\n").filter(line => line.startsWith("CLAWDLINE_SWIFT_TEST_ARTIFACT "));
  assert.equal(rows.length, 1, result.stdout);
  return JSON.parse(rows[0].slice("CLAWDLINE_SWIFT_TEST_ARTIFACT ".length));
};
const refused = (result, code) => {
  assert.notEqual(result.status, 0, result.stdout);
  assert.match(result.stderr, new RegExp(code));
  assert.ok(!result.stdout.includes("CLAWDLINE_SWIFT_TEST_ARTIFACT "));
};
const inode = path => { const s = statSync(path); return [s.dev, s.ino]; };
const ledgerIdentity = (f, extra = {}) => {
  const env = { ...process.env, ...f.env, CLAWDLINE_TEST_GROUPS: "",
    CLAWDLINE_VERIFY_QUESTION_ID: "artifact.environment",
    CLAWDLINE_VERIFICATION_TREE_SHA: "1234567890123456789012345678901234567890",
    CLAWDLINE_VERIFICATION_REPOSITORY_ID: "fixture", ...extra };
  // The old wrapper used process.env for its version probe; keep even the red control fake-only.
  const previousPath = process.env.PATH;
  let result;
  try { process.env.PATH = env.PATH; result = canonicalIdentity(f.root, ["./test.sh"], env); }
  finally { process.env.PATH = previousPath; }
  delete result.request_id;
  return result;
};

try {
  check("F1 Apple Swift 6.2.4 paths target-info shape is accepted", () => {
    const f = fixture();
    f.settings({ targetInfo: { compilerVersion: "Apple Swift version 6.2.4",
      swiftCompilerTag: "swiftlang-6.2.4.1.4",
      target: { triple: "arm64-apple-macosx13.0", unversionedTriple: "arm64-apple-macosx",
        moduleTriple: "arm64-apple-macos", compatibilityLibraries: [], librariesRequireRPath: false },
      paths: { sdkPath: f.sdk, runtimeResourcePath: join(f.toolchain, "lib/swift"),
        runtimeLibraryPaths: [], runtimeLibraryImportPaths: [] } } });
    const result = receipt(f.run());
    assert.equal(result.reused, false);
    assert.equal(f.count(), 1);
  });
  check("F1 linked compiler binds the resolved toolchain runtime", () => {
    const f = fixture(), alias = join(f.base, "wrapper/bin/swiftc");
    mkdirSync(dirname(alias), { recursive: true }); symlinkSync(f.compiler, alias);
    const options = { env: { CLAWDLINE_SWIFT_TEST_SWIFTC: alias } };
    const first = receipt(f.run(options));
    put(join(f.toolchain, "lib/swift/runtime"), "changed actual runtime\n");
    const second = receipt(f.run(options));
    assert.notEqual(first.identity_sha256, second.identity_sha256);
    assert.equal(second.reused, false); assert.equal(f.count(), 2);
    assert.equal(receipt(f.run(options)).reused, true);
  });
  check("F1 target-info external runtime bytes participate in identity", () => {
    const f = fixture(), external = join(f.base, "runtime");
    put(join(external, "lib.dylib"), "runtime A");
    f.settings({ paths: { runtimeResourcePath: join(f.toolchain, "lib/swift"),
      runtimeLibraryPaths: [external], runtimeLibraryImportPaths: [external] } });
    const first = receipt(f.run()); put(join(external, "lib.dylib"), "runtime B");
    const second = receipt(f.run());
    assert.notEqual(first.identity_sha256, second.identity_sha256);
    assert.equal(second.reused, false); assert.equal(f.count(), 2);
  });
  for (const [name, paths] of [["missing runtime", {}], ["relative runtime", { runtimeResourcePath: "relative" }],
    ["unknown closure field", { runtimeResourcePath: "/", futurePath: "/unmanifested" }]]) {
    check(`F1 refuses unprovable ${name}`, () => {
      const f = fixture(); f.settings({ paths });
      refused(f.run(), "artifact_compiler_resources_unprovable"); assert.equal(f.count(), 0);
    });
  }
  check("F1 refuses target-info sdkPath that does not identify the requested SDK", () => {
    const f = fixture();
    f.settings({ paths: { sdkPath: join(f.base, "other-sdk"),
      runtimeResourcePath: join(f.toolchain, "lib/swift") } });
    refused(f.run(), "artifact_compiler_resources_unprovable"); assert.equal(f.count(), 0);
  });
  for (const [name, shape] of [
    ["legacy resourcePaths root", f => ({ compilerVersion: "Fake Swift version 1",
      target: { triple: "arm64-apple-macosx13.0" },
      resourcePaths: { runtimeResourcePath: join(f.toolchain, "lib/swift") } })],
    ["mixed paths roots", f => ({ compilerVersion: "Fake Swift version 1",
      target: { triple: "arm64-apple-macosx13.0" },
      paths: { runtimeResourcePath: join(f.toolchain, "lib/swift") },
      resourcePaths: { runtimeResourcePath: join(f.toolchain, "lib/swift") } })],
    ["unknown paths root", f => ({ compilerVersion: "Fake Swift version 1",
      target: { triple: "arm64-apple-macosx13.0" },
      futurePaths: { runtimeResourcePath: join(f.toolchain, "lib/swift") } })],
  ]) check(`F1 refuses unprovable ${name}`, () => {
    const f = fixture(); f.settings({ targetInfo: shape(f) });
    refused(f.run(), "artifact_compiler_resources_unprovable"); assert.equal(f.count(), 0);
  });
  for (const [name, mutate, prepare] of [
    ["compiler path", f => {
      const path = join(f.base, "alternate/bin/swiftc");
      cpSync(f.toolchain, dirname(dirname(path)), { recursive: true });
      return { CLAWDLINE_SWIFT_TEST_SWIFTC: path };
    }],
    ["compiler bytes with stable version", f => put(f.compiler, readFileSync(f.compiler, "utf8") + "\n# changed compiler\n")],
    ["actual runtime bytes", f => put(join(f.toolchain, "lib/swift/runtime"), "other runtime")],
    ["SDK path", f => { const path = join(f.base, "other-sdk"); cpSync(f.sdk, path, { recursive: true }); return { SDKROOT: path }; }],
    ["SDK bytes with stable version", f => put(join(f.sdk, "usr/lib/libA.tbd"), "other library")],
    ["artifact recipe", f => put(join(f.root, "tools/swift-test-artifact.sh"), helperSource + "\n# changed recipe\n")],
    ["target recipe", f => put(join(f.root, "tools/swift-source-manifest.sh"), "clawdline_swift_test_target=x86_64-apple-macos13.0\n")],
    ["ignored compile resource", f => put(join(f.root, "Resources/generated/input"), "new resource bytes"), f => {
      put(join(f.root, ".gitignore"), "Resources/generated\n");
      put(join(f.root, "Resources/generated/input"), "original resource bytes");
    }],
  ]) check(`F2 outer ledger separates ${name} before any compile`, () => {
    const f = fixture(); prepare?.(f); const first = ledgerIdentity(f);
    assert.deepEqual(ledgerIdentity(f), first);
    const extra = mutate(f) ?? {}, second = ledgerIdentity(f, extra);
    assert.notEqual(first.environment_sha256, second.environment_sha256);
    delete first.environment_sha256; delete second.environment_sha256;
    assert.deepEqual(first, second); assert.equal(f.count(), 0);
  });
  check("F2 outer ledger refuses unreadable artifact inputs before reservation", () => {
    const f = fixture();
    assert.throws(() => ledgerIdentity(f, { SDKROOT: join(f.base, "missing-sdk") }), /artifact_preflight_identity_unavailable/);
    assert.equal(f.count(), 0);
  });
  check("F2 outer ledger differentiates artifact and uncached recipes", () => {
    const f = fixture();
    assert.notEqual(ledgerIdentity(f).environment_sha256,
      ledgerIdentity(f, { CLAWDLINE_SWIFT_TEST_ARTIFACT: "off" }).environment_sha256);
    assert.equal(f.count(), 0);
  });
  check("F3 case alias cannot replace the shared cache inode", () => {
    const f = fixture(), first = receipt(f.run()), path = join(f.cache, first.identity_sha256, "clawdline-tests");
    const alias = join(f.base, "CACHE");
    // On a case-sensitive volume, use a filesystem alias and report that case-folding is unavailable.
    if (!existsSync(alias)) { symlinkSync(f.cache, alias); console.log("case-folding unavailable: symlink alias control only"); }
    assert.deepEqual(inode(alias), inode(f.cache));
    const before = inode(path), bytes = readFileSync(path);
    refused(f.run({ code: `clawdline_swift_test_artifact ${JSON.stringify(join(alias, first.identity_sha256, "clawdline-tests"))} ${f.args}` }),
      "artifact_output_must_be_outside_cache");
    assert.deepEqual(inode(path), before); assert.deepEqual(readFileSync(path), bytes);
    assert.equal(statSync(path).mode & 0o777, 0o500); assert.equal(receipt(f.run()).reused, true);
  });
  check("F3 cache case alias under repository is refused", () => {
    const f = fixture(), alias = join(f.base, "REPO");
    if (!existsSync(alias)) symlinkSync(f.root, alias);
    refused(f.run({ env: { CLAWDLINE_SWIFT_TEST_CACHE_DIR: join(alias, "cache") } }), "artifact_cache_must_be_outside_repository");
    assert.equal(f.count(), 0);
  });
  check("F3 multiply linked cache binary is refused without modifying either link", () => {
    const f = fixture(), first = receipt(f.run()), binary = join(f.cache, first.identity_sha256, "clawdline-tests");
    rmSync(f.output); linkSync(binary, f.output); const before = inode(binary), bytes = readFileSync(binary);
    refused(f.run(), "artifact_binary_not_private");
    assert.deepEqual(inode(binary), before); assert.deepEqual(inode(f.output), before);
    assert.deepEqual(readFileSync(binary), bytes); assert.equal(statSync(binary).mode & 0o777, 0o500);
  });
  for (const phase of ["compile", "copy"]) check(`F3 output parent swapped during ${phase} is preserved and refused`, () => {
    const f = fixture(), victim = join(f.base, "victim");
    put(join(victim, "clawdline-tests"), "do not replace"); const before = inode(join(victim, "clawdline-tests"));
    f.settings(phase === "compile" ? { parent_swap: victim } : { parent_probe_swap: victim });
    refused(f.run(), "artifact_output_parent_changed");
    assert.deepEqual(inode(join(victim, "clawdline-tests")), before);
    assert.equal(readFileSync(join(victim, "clawdline-tests"), "utf8"), "do not replace");
    assert.ok(!readdirSync(victim).some(n => n.startsWith(".clawdline-artifact-")));
    assert.ok(!readdirSync(f.cache).some(n => n.endsWith(".publishing")));
  });
  check("F3 cold and warm delivery use distinct private inodes and preserve output hardlinks", () => {
    const f = fixture(), cold = receipt(f.run()), binary = join(f.cache, cold.identity_sha256, "clawdline-tests");
    const first = inode(f.output); assert.notDeepEqual(first, inode(binary)); assert.equal(statSync(f.output).nlink, 1);
    const retained = join(f.base, "retained-output"); linkSync(f.output, retained);
    assert.equal(receipt(f.run()).reused, true);
    assert.notDeepEqual(inode(f.output), first); assert.notDeepEqual(inode(f.output), inode(binary));
    assert.deepEqual(inode(retained), first); assert.equal(statSync(f.output).nlink, 1);
    assert.equal(statSync(binary).nlink, 1); assert.equal(statSync(f.output).mode & 0o777, 0o700);
  });
  for (const kind of ["directory", "file", "symlink"]) check(`F4 atomic publication preserves a competing ${kind}`, () => {
    const f = fixture(); f.settings({ precreate: kind });
    const result = f.run(); refused(result, "artifact_publication_conflict"); assert.equal(result.status, 74);
    assert.equal(f.publications().length, 1);
    const target = join(f.cache, f.publications()[0]);
    const observed = spawnSync("python3", ["-c", "import os,sys; print(os.lstat(sys.argv[1]).st_ino)", target], { encoding: "utf8" });
    assert.equal(observed.status, 0);
    assert.equal(observed.stdout.trim(), readFileSync(join(f.base, "competitor-inode"), "utf8"));
    if (kind === "directory") assert.deepEqual(readdirSync(target), []);
    if (kind === "file") assert.equal(readFileSync(target, "utf8"), "competitor");
    assert.equal(readdirSync(f.cache).length, 1); assert.equal(existsSync(f.output), false);
  });
  for (const [name, settings, status, kind, raw] of [["normal exit", { exit: 9 }, 9, "exit", 9],
    ["high exit", { exit: 137 }, 137, "exit", 137], ["signal", { signal: 15 }, 143, "signal", -15]]) {
    check(`F6 compiler ${name} preserves status and typed failure evidence`, () => {
      const f = fixture(); f.settings(settings); const result = f.run();
      refused(result, "artifact_compile_failed"); assert.equal(result.status, status);
      const line = result.stderr.split("\n").find(row => row.startsWith("CLAWDLINE_SWIFT_TEST_COMPILE_FAILURE "));
      assert.ok(line, "missing typed compiler failure");
      assert.deepEqual(JSON.parse(line.slice("CLAWDLINE_SWIFT_TEST_COMPILE_FAILURE ".length)),
        { version: 1, kind, returncode: raw, exit_status: status, ...(kind === "signal" ? { signal: 15 } : {}) });
      assert.deepEqual(readdirSync(f.cache), []); assert.equal(existsSync(f.output), false);
    });
  }
  check("cold compile publishes one binary/metadata pair and only compile provenance", () => {
    const f = fixture(), cold = receipt(f.run());
    assert.equal(cold.reused, false);
    assert.equal(cold.kind, "compile_only");
    assert.equal(f.count(), 1);
    assert.equal(f.publications().length, 1);
    const dir = join(f.cache, f.publications()[0]);
    assert.deepEqual(readdirSync(dir).sort(), ["clawdline-tests", "metadata.json"]);
    assert.equal(sha(readFileSync(f.output)), cold.binary_sha256);
    const metadata = JSON.parse(readFileSync(join(dir, "metadata.json")));
    assert.equal(metadata.identity.compiler.version, "Fake Swift version 1\n");
    assert.equal(metadata.identity.argv[metadata.identity.argv.indexOf("-target") + 1], "arm64-apple-macos13.0");
    assert.equal(metadata.identity.sources.length, 2);
    assert.ok(!/CLAWDLINE_TEST_SEAL|CLAWDLINE_CLOUD_TESTS_COMPLETE|checks passed/.test(JSON.stringify(cold)));
  });
  check("warm hit validates bytes and restores a private executable without compiling", () => {
    const f = fixture(), cold = receipt(f.run());
    rmSync(f.output);
    const warm = receipt(f.run());
    assert.equal(warm.reused, true);
    assert.equal(warm.identity_sha256, cold.identity_sha256);
    assert.equal(f.count(), 1);
    const ran = spawnSync(f.output, [], { env: f.env, encoding: "utf8" });
    assert.equal(ran.status, 0, ran.stderr);
    assert.match(ran.stdout, /1 focused checks passed/);
  });

  const invalidations = [
    ["source bytes", f => put(join(f.root, "Sources/Library.swift"), "let value = 2\n")],
    ["source order", f => ({ args: f.args.replace("Sources/Library.swift Tests/main.swift", "Tests/main.swift Sources/Library.swift") })],
    ["source manifest bytes", f => put(join(f.root, "tools/swift-source-manifest.sh"), "# different manifest\n")],
    ["compiler executable bytes", f => writeFileSync(f.compiler, readFileSync(f.compiler, "utf8") + "\n# compiler changed\n")],
    ["compiler version", f => put(join(f.toolchain, "version"), "Fake Swift version 2\n")],
    ["compiler location", f => {
      const second = join(f.toolchain, "bin/swiftc-second"); copyFileSync(f.compiler, second); chmodSync(second, 0o700);
      return { env: { CLAWDLINE_SWIFT_TEST_SWIFTC: second } };
    }],
    ["toolchain runtime content", f => put(join(f.toolchain, "lib/swift/runtime"), "runtime two\n")],
    ["SDK version", f => put(join(f.sdk, "SDKSettings.json"), '{"version":"two"}\n')],
    ["SDK library content with unchanged version", f => put(join(f.sdk, "usr/lib/libA.tbd"), "sdk library two\n")],
    ["SDK added file", f => put(join(f.sdk, "new-header.h"), "new header\n")],
    ["SDK removed file", f => rmSync(join(f.sdk, "usr/lib/libA.tbd"))],
    ["SDK location with identical bytes", f => {
      const second = join(f.base, "sdk-second"); cpSync(f.sdk, second, { recursive: true });
      return { env: { SDKROOT: second } };
    }],
    ["target", f => ({ args: f.args.replace("arm64-apple", "x86_64-apple") })],
    ["ordered compiler flags", f => ({ args: f.args.replace("-swift-version 5 -target arm64-apple-macos13.0", "-target arm64-apple-macos13.0 -swift-version 5") })],
    ["compiler flag value", f => ({ args: f.args.replace("-j 1", "-j 2") })],
    ["compile resource", f => put(join(f.root, "Resources/input.dat"), "resource B\n")],
    ["ignored compile resource", f => put(join(f.root, "Resources/generated/input"), "changed generated bytes\n"), f => {
      put(join(f.root, ".gitignore"), "Resources/generated\n");
      put(join(f.root, "Resources/generated/input"), "generated\n");
    }],
    ["untracked working overlay", f => put(join(f.root, "untracked.md"), "new overlay\n")],
    ["tracked non-source working overlay", f => put(join(f.root, "test.sh"), runner + "\n# changed\n")],
    ["deleted tracked working overlay", f => rmSync(join(f.root, "Tests/TestGroupManifest.swift"))],
    ["source executable mode", f => chmodSync(join(f.root, "Sources/Library.swift"), 0o755)],
    ["resource symlink target bytes", f => put(join(f.base, "external-resource"), "two"), f => {
      const target = join(f.base, "external-resource"); put(target, "one");
      symlinkSync(target, join(f.root, "Resources/linked"));
    }],
  ];
  for (const [dimension, mutate, prepare] of invalidations) check(`invalidates ${dimension}`, () => {
    const f = fixture(); prepare?.(f);
    const cold = receipt(f.run());
    const options = mutate(f) ?? {};
    const next = receipt(f.run(options));
    assert.equal(next.reused, false);
    assert.notEqual(next.identity_sha256, cold.identity_sha256);
    assert.equal(f.count(), 2);
  });
  check("a new Git tree invalidates even when the visible overlay bytes stay identical", () => {
    const f = fixture();
    put(join(f.root, "Sources/Library.swift"), "let value = 3\n");
    const old = receipt(f.run());
    assert.equal(spawnSync("git", ["add", "--", "Sources/Library.swift"], { cwd: f.root }).status, 0);
    assert.equal(spawnSync("git", ["-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
      "commit", "-qm", "next"], { cwd: f.root }).status, 0);
    const next = receipt(f.run());
    assert.notEqual(next.identity_sha256, old.identity_sha256);
  });
  check("an initialized exact snapshot without a commit binds its complete working overlay", () => {
    const f = fixture();
    put(join(f.root, ".git/HEAD"), "ref: refs/heads/unborn-snapshot\n");
    const result = receipt(f.run());
    const subject = JSON.parse(readFileSync(join(f.cache, result.identity_sha256, "metadata.json"))).identity.subject;
    assert.equal(subject.head_tree, null);
    assert.equal(subject.head_state, "unborn");
    assert.match(subject.working_overlay_sha256, /^[a-f0-9]{64}$/);
    assert.equal(receipt(f.run()).reused, true);
  });
  check("repository identity invalidates when only the Git common directory moves", () => {
    const f = fixture(), first = receipt(f.run());
    renameSync(join(f.root, ".git"), join(f.base, "relocated.git"));
    put(join(f.root, ".git"), "gitdir: ../relocated.git\n");
    const second = receipt(f.run());
    assert.notEqual(first.identity_sha256, second.identity_sha256);
    assert.equal(second.reused, false);
    const subject = key => JSON.parse(readFileSync(join(f.cache, key, "metadata.json"))).identity.subject;
    const before = subject(first.identity_sha256), after = subject(second.identity_sha256);
    assert.notEqual(before.repository_sha256, after.repository_sha256);
    delete before.repository_sha256; delete after.repository_sha256;
    assert.deepEqual(before, after);
  });
  check("runtime group selection is not a compile input and reuses the same artifact", () => {
    const f = fixture(), first = receipt(f.run());
    const second = receipt(f.run({ env: { CLAWDLINE_TEST_GROUPS: "second group" } }));
    assert.equal(second.identity_sha256, first.identity_sha256);
    assert.equal(second.reused, true);
  });
  check("ambient compiler injection is removed from the compile environment", () => {
    const f = fixture();
    receipt(f.run({ env: { SWIFT_EXEC: "malicious", CPATH: "/unmanifested", LIBRARY_PATH: "/unmanifested" } }));
    const call = JSON.parse(readFileSync(f.calls, "utf8").trim());
    assert.equal(call.env.SWIFT_EXEC, undefined);
    assert.equal(call.env.CPATH, undefined);
    assert.equal(call.env.LIBRARY_PATH, undefined);
    assert.ok(call.argv.includes("-sdk") && call.argv.includes("-module-cache-path"));
  });

  for (const [name, damage, code] of [
    ["binary tamper", dir => { chmodSync(join(dir, "clawdline-tests"), 0o700); put(join(dir, "clawdline-tests"), "tampered"); }, "artifact_digest_mismatch"],
    ["metadata tamper", dir => put(join(dir, "metadata.json"), "{}"), "artifact_digest_mismatch"],
    ["malformed metadata", dir => put(join(dir, "metadata.json"), "{"), "artifact_metadata_invalid"],
    ["duplicate metadata keys", dir => {
      const path = join(dir, "metadata.json"), value = readFileSync(path, "utf8");
      // 0500 is decimal 320. Duplicate keys must fail even when the parser's last value agrees.
      put(path, value.replace('{"binary_mode":320,', '{"binary_mode":320,"binary_mode":320,'));
    }, "artifact_digest_mismatch"],
    ["partial publication", dir => rmSync(join(dir, "metadata.json")), "artifact_incomplete"],
    ["extra publication file", dir => put(join(dir, "unexpected"), "x"), "artifact_incomplete"],
    ["symlinked binary", dir => { rmSync(join(dir, "clawdline-tests")); symlinkSync("/bin/true", join(dir, "clawdline-tests")); }, "artifact_corrupt"],
  ]) check(`refuses ${name} without a success or a recompile`, () => {
    const f = fixture(); receipt(f.run());
    const before = readFileSync(f.output);
    damage(join(f.cache, f.publications()[0]));
    refused(f.run(), code);
    assert.equal(f.count(), 1);
    assert.deepEqual(readFileSync(f.output), before);
  });
  for (const [name, settings, code] of [
    ["compiler failure", { exit: 9 }, "artifact_compile_failed"],
    ["compiler produces no binary", { no_output: true }, "artifact_binary_missing"],
    ["source changes during compile", { change_source: true }, "artifact_inputs_changed"],
    ["lock changes during compile", { lose_lock: true }, "artifact_lock_lost"],
  ]) check(`refuses ${name} and cleans its unpublished output`, () => {
    const f = fixture(); f.settings(settings);
    refused(f.run(), code);
    assert.equal(f.publications().length, 0);
    assert.equal(readdirSync(f.cache).length, 0);
    assert.equal(existsSync(f.output), false);
  });
  check("does not compile without the existing shell lock confirmation", () => {
    const f = fixture();
    refused(f.run({ code: `unset -f clawdline_confirm_suite_lock\nclawdline_swift_test_artifact "$BIN" ${f.args}` }), "artifact_lock_required");
    assert.equal(f.count(), 0);
  });
  check("lock confirmation sees the caller's live renewer job without a subshell", () => {
    const f = fixture();
    const result = f.run({ code: `sleep 2 &\nfixture_renewer=$!\n`
      + `clawdline_confirm_suite_lock() { jobs -p | grep -qx "$fixture_renewer" || { echo artifact_renewer_not_owned >&2; return 73; }; }\n`
      + `clawdline_swift_test_artifact "$BIN" ${f.args}\nwait "$fixture_renewer"` });
    receipt(result);
    assert.equal(f.count(), 1);
  });
  check("SDK ancestor symlinks are sealed as graph edges without omitting target bytes", () => {
    const f = fixture();
    symlinkSync(".", join(f.sdk, "usr/lib/self"));
    const first = receipt(f.run());
    put(join(f.sdk, "usr/lib/libA.tbd"), "changed through a cyclic graph\n");
    const changed = receipt(f.run());
    assert.notEqual(first.identity_sha256, changed.identity_sha256);
    assert.equal(changed.reused, false);
  });
  check("refuses unknown file-bearing flags and response files", () => {
    const f = fixture();
    for (const flag of ["@flags.rsp", "-I", "-Xcc", "-load-plugin-executable", "-sdk"]) {
      refused(f.run({ args: `${flag} unknown ${f.args}` }), "artifact_flag_not_manifested");
    }
    assert.equal(f.count(), 0);
  });
  check("a known flag cannot smuggle an external file through its value", () => {
    const f = fixture();
    for (const option of ["-framework /outside/Foo", "-D @scratch@/file", "-j 0"]) {
      refused(f.run({ args: `${option} ${f.args}` }), "artifact_flag_value_not_manifested");
    }
    assert.equal(f.count(), 0);
  });
  check("refuses cache under the repository before any compile", () => {
    const f = fixture();
    refused(f.run({ env: { CLAWDLINE_SWIFT_TEST_CACHE_DIR: join(f.root, "cache") } }), "artifact_cache_must_be_outside_repository");
    assert.equal(f.count(), 0);
  });
  check("output path aliases cannot replace the shared cache binary", () => {
    const f = fixture(); receipt(f.run());
    const dir = join(f.cache, f.publications()[0]);
    const alias = join(f.base, "output-alias"); symlinkSync(dir, alias);
    refused(f.run({ code: `BIN=${JSON.stringify(join(alias, "clawdline-tests"))}\n`
      + `clawdline_swift_test_artifact "$BIN" ${f.args}` }), "artifact_output_must_be_outside_cache");
    assert.equal(f.count(), 1);
    assert.equal(receipt(f.run()).reused, true);
  });
  // This case needs two processes in flight. A compiler-side delay opens the race without
  // ever reserving the real machine lock; the loser may observe either busy or the completed hit.
  if (!only || only === "concurrent publication executes one compiler and exposes only a complete pair") {
    const f = fixture(); f.settings({ delay: 1 });
    const code = "set -euo pipefail\n" + f.setup + `clawdline_swift_test_artifact "$BIN" ${f.args}`;
    const runAsync = () => new Promise(resolvePromise => {
      const child = spawn("/bin/bash", ["-c", code], { cwd: f.root, env: { ...process.env, ...f.env } });
      let stdout = "", stderr = "";
      child.stdout.on("data", data => { stdout += data; });
      child.stderr.on("data", data => { stderr += data; });
      child.on("close", status => resolvePromise({ status, stdout, stderr }));
    });
    const both = await Promise.all([runAsync(), runAsync()]);
    check("concurrent publication executes one compiler and exposes only a complete pair", () => {
      assert.equal(f.count(), 1);
      assert.equal(f.publications().length, 1);
      assert.equal(readdirSync(f.cache).length, 1);
      assert.equal(both.filter(r => r.status === 0).length >= 1, true);
      for (const result of both) if (result.status !== 0) refused(result, "artifact_publication_busy");
      assert.equal(receipt(f.run()).reused, true);
    });
  }
  check("an abandoned publishing directory never counts as a cache hit", () => {
    const f = fixture(); receipt(f.run());
    const key = f.publications()[0];
    rmSync(join(f.cache, key), { recursive: true });
    mkdirSync(join(f.cache, key + ".publishing"));
    refused(f.run(), "artifact_publication_busy");
    assert.equal(f.count(), 1);
  });

  for (const [name, selected] of [["empty", ""], ["newline-only", "\n"], ["whitespace-only", "  "],
    ["unknown", "absent group"], ["mixed known/unknown", "first group\nabsent group"]]) check(`focused entry refuses ${name} selection before compiling`, () => {
    const f = fixture();
    const r = spawnSync("/bin/bash", ["test.sh", "--swift-focused"], {
      cwd: f.root, env: { ...process.env, ...f.env, CLAWDLINE_TEST_GROUPS: selected }, encoding: "utf8",
    });
    assert.notEqual(r.status, 0);
    assert.match(r.stderr, /focused_selection_(empty|unknown)/);
    assert.equal(f.count(), 0);
  });
  check("focused entry refuses absent selection and does not become full mode", () => {
    const f = fixture(), env = { ...process.env, ...f.env };
    delete env.CLAWDLINE_TEST_GROUPS;
    const r = spawnSync("/bin/bash", ["test.sh", "--swift-focused"], { cwd: f.root, env, encoding: "utf8" });
    assert.equal(r.status, 2);
    assert.match(r.stderr, /focused_selection_empty/);
    assert.equal(f.count(), 0);
  });
  check("current real Swift manifest including literal concatenations is selectable", () => {
    const result = shell('. tools/swift-test-artifact.sh\nclawdline_validate_swift_test_selection required', repository,
      { CLAWDLINE_TEST_GROUPS: "palette colours parse" });
    assert.equal(result.status, 0, result.stderr);
  });
  check("selection accepts duplicate requests with Swift Set semantics", () => {
    const f = fixture();
    const result = f.run({ code: "clawdline_validate_swift_test_selection required",
      env: { CLAWDLINE_TEST_GROUPS: "first group\nfirst group\nsecond group\n" } });
    assert.equal(result.status, 0, result.stderr);
  });
  check("unknown Swift manifest expressions fail closed instead of skipping titles", () => {
    const f = fixture();
    put(join(f.root, "Tests/TestGroupManifest.swift"), 'let expectedOrderedTestGroupTitles: [String] = [\n    dynamicTitle(),\n]\n');
    refused(f.run({ code: "clawdline_validate_swift_test_selection required" }), "focused_manifest_invalid");
  });

  const focusedLog = "  ✓ first group\n1 focused checks passed\n";
  for (const [name, log, pass] of [
    ["valid focused result", focusedLog, true], ["zero checks", "  ✓ first group\n0 focused checks passed\n", false],
    ["missing receipt", "  ✓ first group\n", false], ["duplicate receipt", focusedLog + "1 focused checks passed\n", false],
    ["extra zero receipt", focusedLog + "0 focused checks passed\n", false],
    ["unknown executed group", focusedLog.replace("first", "other"), false],
    ["duplicate executed group", "  ✓ first group\n" + focusedLog, false],
    ["failed group", focusedLog + "  ✗ first group\n", false],
    ["forged full count", focusedLog + "1 checks passed\n", false],
    ["forged Cloud roster", focusedLog + "CLAWDLINE_CLOUD_TESTS_COMPLETE v=1 suite_count=12 suites=forged\n", false],
    ["forged full tuple", focusedLog + 'CLAWDLINE_TEST_SEAL {"version":1}\n', false],
  ]) check(`focused receipt ${pass ? "accepts" : "refuses"} ${name}`, () => {
    const f = fixture(); put(join(f.base, "focused.log"), log);
    const result = f.run({ code: `clawdline_verify_focused_test_receipt ${JSON.stringify(join(f.base, "focused.log"))}` });
    if (pass) assert.equal(result.status, 0, result.stderr);
    else refused(result, "focused_receipt_");
  });
  check("the full verifier refuses focused evidence even if both full lines were forged", () => {
    const roster = runner.match(/^cloud_suite_roster='([^']+)'$/m)[1].split(",");
    const cloud = `CLAWDLINE_CLOUD_TESTS_COMPLETE v=1 suite_count=${roster.length} suites=`
      + roster.map((name) => `${name}:2`).join(",");
    const full = "7 checks passed";
    const f = fixture(); put(join(f.base, "forged.log"), `${full}\n${cloud}\n`);
    const result = spawnSync("/bin/bash", ["test.sh", "--verify-completion-receipts", join(f.base, "forged.log")],
      { cwd: f.root, env: { ...process.env, ...f.env }, encoding: "utf8" });
    assert.equal(result.status, 125);
    assert.match(result.stderr, /focused_run_cannot_verify_full_receipt/);
  });
  check("the full emitter itself refuses focused context with plausible full evidence", () => {
    const f = fixture();
    const roster = runner.match(/^cloud_suite_roster='([^']+)'$/m)[1].split(",");
    const cloud = `CLAWDLINE_CLOUD_TESTS_COMPLETE v=1 suite_count=${roster.length} suites=`
      + roster.map((name) => `${name}:2`).join(",");
    const full = "7 checks passed";
    put(join(f.base, "full.log"), `${full}\n${cloud}\n`);
    put(join(f.root, "Tests/main.swift"), 'check("witness", true)\n');
    const start = runner.indexOf("emit_complete_test_seal_receipt() {");
    const end = runner.indexOf("\nis_unfiltered_test_run()", start);
    const result = f.run({ code: 'cloud_receipt_lines() { sed -n "/^CLAWDLINE_CLOUD_TESTS_COMPLETE /p" "$1"; }\nvalidate_cloud_completion_receipt() { :; }\n'
      + runner.slice(start, end) + '\nemit_complete_test_seal_receipt ' + JSON.stringify(join(f.base, "full.log")) });
    refused(result, "focused_run_cannot_emit_full_receipt");
    assert.equal(result.status, 125);
    assert.ok(!result.stdout.includes("CLAWDLINE_TEST_SEAL"));
  });
  check("actual compile dispatch opts into reuse and preserves the uncached compiler path", () => {
    const f = fixture();
    const block = section("swift artifact invocation");
    assert.equal(receipt(f.run({ code: block })).reused, false);
    assert.equal(receipt(f.run({ code: block })).reused, true);
    const original = f.run({ code: block, env: { CLAWDLINE_SWIFT_TEST_ARTIFACT: "off" } });
    assert.equal(original.status, 0, original.stderr);
    assert.equal(f.count(), 2);
    assert.ok(!original.stdout.includes("CLAWDLINE_SWIFT_TEST_ARTIFACT"));
  });
  check("both compile paths stay below the original machine lock and before its confirmation", () => {
    const start = runner.indexOf("# >>> clawdline swift artifact invocation >>>");
    const end = runner.indexOf("# <<< clawdline swift artifact invocation <<<");
    const lock = runner.indexOf("\nclawdline_acquire_suite_lock || exit $?\n");
    const confirm = runner.indexOf("\nclawdline_confirm_suite_lock || exit $?\n", end);
    const binary = runner.indexOf('"$BIN" Resources/mascots', confirm);
    assert.ok(lock > 0 && lock < start && end < confirm && confirm < binary);
    assert.match(runner, /else\n  clawdline_verify_focused_test_receipt "\$LOG" \|\| exit \$\?/);
    assert.match(runner, /^\s*node Tests\/swift-test-artifact\.mjs$/m);
    assert.match(sourceManifest, /clawdline_swift_test_compile_resources=\([\s\S]*Resources\n\)/);
    assert.match(section("swift artifact invocation"), /-target "\$clawdline_swift_test_target"/);
    assert.match(sourceManifest, /^clawdline_swift_test_target=[A-Za-z0-9_.-]+$/m);
  });
  check("full receipt validation is structural rather than tied to a checked-in total", () => {
    assert.ok(!/^expected_(?:swift|cloud)_receipt=/m.test(runner));
    assert.ok(!/^expected_swift_receipt_witness=/m.test(runner));
    const f = fixture(), log = join(f.base, "full.log");
    const roster = runner.match(/^cloud_suite_roster='([^']+)'$/m)[1].split(",");
    const cloud = `CLAWDLINE_CLOUD_TESTS_COMPLETE v=1 suite_count=${roster.length} suites=`
      + roster.map((name, index) => `${name}:${index + 1}`).join(",");
    put(log, `321 checks passed\n${cloud}\n`);
    const env = { ...process.env, TMPDIR: f.temp };
    delete env.CLAWDLINE_TEST_GROUPS; delete env.CLAWDLINE_VERIFY_QUESTION_ID;
    const good = spawnSync("/bin/bash", ["test.sh", "--verify-completion-receipts", log],
      { cwd: f.root, env, encoding: "utf8" });
    assert.equal(good.status, 0, good.stderr);
    put(log, `321 checks passed\n${cloud.replace(`${roster[1]}:2`, `${roster[0]}:2`)}\n`);
    const duplicate = spawnSync("/bin/bash", ["test.sh", "--verify-completion-receipts", log],
      { cwd: f.root, env, encoding: "utf8" });
    assert.equal(duplicate.status, 125);
  });
} finally {
  rmSync(directory, { recursive: true, force: true });
}
console.log(`${checks - failures}/${checks} swift-test-artifact checks passed (${((Date.now() - started) / 1000).toFixed(2)}s)`);
if (!checks) throw new Error("swift-test-artifact: zero test cases selected");
if (process.env.CLAWDLINE_ARTIFACT_TEST_REPORT) writeFileSync(process.env.CLAWDLINE_ARTIFACT_TEST_REPORT,
  JSON.stringify({ checks, failures, seconds: (Date.now() - started) / 1000, results }, null, 2) + "\n");
if (failures) process.exitCode = 1;
