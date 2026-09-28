#!/usr/bin/env node

// Give a linked worktree usable web dependencies without making every branch
// install the same 70 MB tree again. External packages may be shared with the
// primary checkout when both package locks match. Workspace packages never are:
// their links always point back into the worktree that is being built.

import { spawnSync } from "node:child_process"
import { createHash } from "node:crypto"
import {
  copyFileSync,
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  realpathSync,
  renameSync,
  rmSync,
  statSync,
  symlinkSync,
  writeFileSync,
} from "node:fs"
import { basename, dirname, join, resolve } from "node:path"

const markerName = ".clawdline-deps.json"

function run(command, args, options = {}) {
  const result = spawnSync(command, args, { encoding: "utf8", ...options })
  if (result.error) throw result.error
  return result
}

function git(cwd, ...args) {
  const result = run("git", args, { cwd })
  if (result.status !== 0) throw new Error(result.stderr.trim() || `git ${args[0]} failed`)
  return result.stdout.trim()
}

function digest(path) {
  return createHash("sha256").update(readFileSync(path)).digest("hex")
}

function readJSON(path) {
  return JSON.parse(readFileSync(path, "utf8"))
}

function workspaces(web) {
  const configured = readJSON(join(web, "package.json")).workspaces
  if (!Array.isArray(configured)) throw new Error("web/package.json workspaces must be an array")
  return configured.map((relative) => {
    if (typeof relative !== "string" || /[*?[\]{}]/.test(relative)) {
      throw new Error(`web workspace ${JSON.stringify(relative)} must be a literal directory`)
    }
    const path = resolve(web, relative)
    const name = readJSON(join(path, "package.json")).name
    if (typeof name !== "string" || name === "") throw new Error(`${relative}/package.json has no package name`)
    return { relative, path, name }
  })
}

function workspaceParts(name) {
  if (!name.startsWith("@")) return { parent: "", leaf: name }
  const slash = name.indexOf("/")
  if (slash < 2 || slash === name.length - 1) throw new Error(`invalid workspace package name ${name}`)
  return { parent: name.slice(0, slash), leaf: name.slice(slash + 1) }
}

function linkDirectory(target, path) {
  mkdirSync(dirname(path), { recursive: true })
  symlinkSync(target, path, process.platform === "win32" ? "junction" : "dir")
}

function mirror(source, destination) {
  if (statSync(source).isDirectory()) linkDirectory(realpathSync(source), destination)
  else copyFileSync(source, destination)
}

function linkWorkspaces(modules, rows) {
  for (const row of rows) {
    const { parent, leaf } = workspaceParts(row.name)
    const path = parent ? join(modules, parent, leaf) : join(modules, leaf)
    rmSync(path, { recursive: true, force: true })
    linkDirectory(row.path, path)
  }
}

function primaryCheckout(repo) {
  const common = resolve(repo, git(repo, "rev-parse", "--path-format=absolute", "--git-common-dir"))
  return basename(common) === ".git" ? dirname(common) : repo
}

function npmCommand() {
  if (process.env.CLAWDLINE_NPM) return process.env.CLAWDLINE_NPM
  return process.platform === "win32" ? "npm.cmd" : "npm"
}

function validInstall(web) {
  const modules = join(web, "node_modules")
  if (!existsSync(modules) || lstatSync(modules).isSymbolicLink() || !statSync(modules).isDirectory()) return false
  const checked = run(npmCommand(), ["--prefix", web, "ls", "--all", "--silent"], { stdio: "ignore" })
  return checked.status === 0
}

function writeMarker(modules, marker) {
  writeFileSync(join(modules, markerName), JSON.stringify({ schema: 1, ...marker }, null, 2) + "\n")
}

function localWorkspaceLinksAreReady(modules, rows) {
  try {
    return rows.every((row) => {
      const { parent, leaf } = workspaceParts(row.name)
      const linked = parent ? join(modules, parent, leaf) : join(modules, leaf)
      return realpathSync(linked) === realpathSync(row.path)
    })
  } catch {
    return false
  }
}

// Return true for either a generated, current tree or a physical install the
// user/npm owns. Remove only our stale generated tree and the old whole-tree
// symlink whose workspace links resolve into another checkout.
function keepExisting(modules, lockHash, rows) {
  if (!existsSync(modules)) return false
  const found = lstatSync(modules)
  if (found.isSymbolicLink()) {
    rmSync(modules, { force: true })
    return false
  }
  if (!found.isDirectory()) throw new Error(`${modules} exists and is not a directory`)
  const markerPath = join(modules, markerName)
  if (!existsSync(markerPath)) return true
  let marker
  try { marker = readJSON(markerPath) } catch { marker = null }
  const sourceReady = marker?.mode !== "shared" ||
    (typeof marker.source === "string" && existsSync(marker.source) && statSync(marker.source).isDirectory())
  if (marker?.schema === 1 && marker.lock === lockHash && sourceReady && localWorkspaceLinksAreReady(modules, rows)) {
    return true
  }
  rmSync(modules, { recursive: true, force: true })
  return false
}

function publish(stageModules, modules, lockHash, rows) {
  try {
    renameSync(stageModules, modules)
  } catch (error) {
    // Two checks in one worktree may prepare at the same time. A complete tree
    // that won the rename is the result; no process observes our staging tree.
    if (!keepExisting(modules, lockHash, rows)) throw error
    rmSync(stageModules, { recursive: true, force: true })
  }
}

function sharedView(web, sourceModules, modules, lockHash, rows) {
  const stage = mkdtempSync(join(web, ".clawdline-node-modules-"))
  try {
    const internal = new Map()
    for (const row of rows) {
      const { parent, leaf } = workspaceParts(row.name)
      if (!internal.has(parent)) internal.set(parent, new Set())
      internal.get(parent).add(leaf)
    }

    for (const entry of readdirSync(sourceModules, { withFileTypes: true })) {
      if (entry.name === markerName || entry.name.startsWith(".node_modules-")) continue
      const hiddenWorkspaces = internal.get(entry.name)
      if (hiddenWorkspaces && entry.name.startsWith("@")) {
        const scope = join(stage, entry.name)
        mkdirSync(scope, { recursive: true })
        for (const child of readdirSync(join(sourceModules, entry.name), { withFileTypes: true })) {
          if (!hiddenWorkspaces.has(child.name)) mirror(join(sourceModules, entry.name, child.name), join(scope, child.name))
        }
      } else if (!internal.get("")?.has(entry.name)) {
        mirror(join(sourceModules, entry.name), join(stage, entry.name))
      }
    }
    linkWorkspaces(stage, rows)
    writeMarker(stage, { mode: "shared", lock: lockHash, source: sourceModules })
    publish(stage, modules, lockHash, rows)
    console.log(`web dependencies: reused the primary checkout's external packages for ${lockHash.slice(0, 12)}`)
  } catch (error) {
    rmSync(stage, { recursive: true, force: true })
    throw error
  }
}

function isolatedInstall(web, modules, lockHash, rows) {
  const stage = mkdtempSync(join(web, ".clawdline-web-install-"))
  try {
    copyFileSync(join(web, "package.json"), join(stage, "package.json"))
    copyFileSync(join(web, "package-lock.json"), join(stage, "package-lock.json"))
    for (const row of rows) {
      const destination = join(stage, row.relative)
      mkdirSync(destination, { recursive: true })
      copyFileSync(join(row.path, "package.json"), join(destination, "package.json"))
    }
    console.log(`web dependencies: installing an isolated tree for ${lockHash.slice(0, 12)}`)
    const installed = run(npmCommand(), ["ci", "--ignore-scripts"], { cwd: stage, stdio: "inherit" })
    if (installed.status !== 0) throw new Error(`npm ci exited ${installed.status}`)
    const stageModules = join(stage, "node_modules")
    if (!existsSync(stageModules)) throw new Error("npm ci did not create node_modules")
    linkWorkspaces(stageModules, rows)
    writeMarker(stageModules, { mode: "isolated", lock: lockHash })
    publish(stageModules, modules, lockHash, rows)
  } finally {
    rmSync(stage, { recursive: true, force: true })
  }
}

function main() {
  const repo = git(process.cwd(), "rev-parse", "--show-toplevel")
  const web = join(repo, "web")
  const lock = join(web, "package-lock.json")
  if (!existsSync(lock)) throw new Error(`no web/package-lock.json under ${repo}`)
  const lockHash = digest(lock)
  const rows = workspaces(web)
  const modules = join(web, "node_modules")
  if (keepExisting(modules, lockHash, rows)) return

  const primary = primaryCheckout(repo)
  const primaryWeb = join(primary, "web")
  const primaryLock = join(primaryWeb, "package-lock.json")
  const sourceModules = join(primaryWeb, "node_modules")
  if (primary !== repo && existsSync(primaryLock) && digest(primaryLock) === lockHash && validInstall(primaryWeb)) {
    sharedView(web, sourceModules, modules, lockHash, rows)
  } else {
    isolatedInstall(web, modules, lockHash, rows)
  }
}

try {
  main()
} catch (error) {
  console.error(`web dependencies: ${error instanceof Error ? error.message : String(error)}`)
  process.exitCode = 1
}
