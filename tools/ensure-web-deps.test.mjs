import assert from "node:assert/strict"
import { execFileSync, spawnSync } from "node:child_process"
import { chmodSync, mkdtempSync, mkdirSync, readFileSync, realpathSync, rmSync, statSync, symlinkSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, join } from "node:path"
import test from "node:test"
import { fileURLToPath } from "node:url"

const script = join(dirname(fileURLToPath(import.meta.url)), "ensure-web-deps.mjs")

function writeJSON(path, value) {
  mkdirSync(dirname(path), { recursive: true })
  writeFileSync(path, JSON.stringify(value, null, 2) + "\n")
}

function git(cwd, ...args) {
  return execFileSync("git", args, { cwd, encoding: "utf8" }).trim()
}

function fixture() {
  const holder = mkdtempSync(join(tmpdir(), "clawdline-web-deps-test-"))
  const main = join(holder, "main")
  const worktree = join(holder, "worktree")
  mkdirSync(main)
  git(main, "init", "-q", "-b", "main")
  git(main, "config", "user.email", "test@example.invalid")
  git(main, "config", "user.name", "Web dependency test")

  writeJSON(join(main, "web/package.json"), {
    name: "fixture",
    private: true,
    workspaces: ["contract", "core", "console"],
  })
  writeJSON(join(main, "web/package-lock.json"), { name: "fixture", lockfileVersion: 3, packages: {} })
  for (const name of ["contract", "core", "console"]) {
    writeJSON(join(main, `web/${name}/package.json`), { name: `@clawdline/${name}`, private: true })
    writeFileSync(join(main, `web/${name}/branch.txt`), "main\n")
  }
  git(main, "add", "web")
  git(main, "commit", "-qm", "fixture")
  git(main, "worktree", "add", "-q", "-b", "feature", worktree, "main")
  writeFileSync(join(worktree, "web/core/branch.txt"), "feature\n")

  const modules = join(main, "web/node_modules")
  mkdirSync(join(modules, "external-package"), { recursive: true })
  writeJSON(join(modules, "external-package/package.json"), { name: "external-package", version: "1.0.0" })
  mkdirSync(join(modules, ".bin"), { recursive: true })
  mkdirSync(join(modules, "@clawdline"), { recursive: true })
  for (const name of ["contract", "core", "console"]) {
    symlinkSync(`../../${name}`, join(modules, "@clawdline", name), "dir")
  }

  const log = join(holder, "npm.log")
  const fakeNPM = join(holder, "fake-npm.mjs")
  writeFileSync(fakeNPM, `#!/usr/bin/env node
import { appendFileSync, mkdirSync, symlinkSync, writeFileSync } from "node:fs";
import { join } from "node:path";
appendFileSync(process.env.FAKE_NPM_LOG, JSON.stringify({cwd: process.cwd(), args: process.argv.slice(2)}) + "\\n");
if (process.argv.includes("ls")) process.exit(0);
if (!process.argv.includes("ci")) process.exit(9);
const modules = join(process.cwd(), "node_modules");
mkdirSync(join(modules, "external-package"), {recursive: true});
writeFileSync(join(modules, "external-package", "package.json"), '{"name":"external-package","version":"2.0.0"}\\n');
mkdirSync(join(modules, "@clawdline"), {recursive: true});
for (const name of ["contract", "core", "console"]) symlinkSync('../../' + name, join(modules, "@clawdline", name), 'dir');
`)
  chmodSync(fakeNPM, 0o755)
  return {
    holder,
    main,
    worktree,
    log,
    env: { ...process.env, CLAWDLINE_NPM: fakeNPM, FAKE_NPM_LOG: log },
    cleanup() { rmSync(holder, { recursive: true, force: true }) },
  }
}

function ensure(f) {
  const run = spawnSync(process.execPath, [script], { cwd: f.worktree, env: f.env, encoding: "utf8" })
  assert.equal(run.status, 0, run.stderr || run.stdout)
  return run
}

test("a matching worktree reuses external packages but resolves workspaces locally", () => {
  const f = fixture()
  try {
    ensure(f)
    const modules = join(f.worktree, "web/node_modules")
    assert.equal(statSync(modules).isDirectory(), true)
    assert.equal(realpathSync(join(modules, "external-package")), realpathSync(join(f.main, "web/node_modules/external-package")))
    assert.equal(realpathSync(join(modules, "@clawdline/core")), realpathSync(join(f.worktree, "web/core")))
    assert.equal(readFileSync(join(modules, "@clawdline/core/branch.txt"), "utf8"), "feature\n")

    const before = statSync(modules).ino
    ensure(f)
    assert.equal(statSync(modules).ino, before, "a second run should keep the ready dependency view")
    assert.equal(readFileSync(f.log, "utf8").trim().split("\n").length, 1, "only the first run should validate the source install")
  } finally {
    f.cleanup()
  }
})

test("a changed lockfile replaces the shared view with an isolated npm ci install", () => {
  const f = fixture()
  try {
    ensure(f)
    writeJSON(join(f.worktree, "web/package-lock.json"), { name: "fixture", lockfileVersion: 3, packages: { changed: {} } })
    ensure(f)

    const modules = join(f.worktree, "web/node_modules")
    assert.equal(statSync(join(modules, "external-package")).isDirectory(), true)
    assert.equal(realpathSync(join(modules, "@clawdline/core")), realpathSync(join(f.worktree, "web/core")))
    const calls = readFileSync(f.log, "utf8").trim().split("\n").map(JSON.parse)
    assert.deepEqual(calls.at(-1).args, ["ci", "--ignore-scripts"])
    assert.notEqual(calls.at(-1).cwd, join(f.worktree, "web"), "npm ci should run in a staging tree")
  } finally {
    f.cleanup()
  }
})
