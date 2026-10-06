// Import the producer-owned fixed HTTP sentences after all nine translations are reviewed.
// node web/console/tools/merge-http-refusals.mjs --source-dir <dir> --expected-count <n> [--write]
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import { spawnSync } from "node:child_process"
import { fileURLToPath } from "node:url"
import { displayRefusal } from "./http-refusal-display.mjs"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const args = process.argv.slice(2)
const at = args.indexOf("--source-dir")
if (at < 0 || !args[at + 1]) throw new Error("pass --source-dir with the reviewed HTTP refusal catalogs")
const sourceDir = path.resolve(args[at + 1])
const write = args.includes("--write")
const expectedAt = args.indexOf("--expected-count")
const expectedCount = expectedAt < 0 ? null : Number(args[expectedAt + 1])
if (expectedCount !== null && (!Number.isSafeInteger(expectedCount) || expectedCount < 1)) {
  throw new Error("--expected-count must be a positive integer")
}
const tags = ["en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"]
const read = file => JSON.parse(fs.readFileSync(file, "utf8"))
const ordered = object => Object.fromEntries(Object.entries(object).sort(([a], [b]) => a.localeCompare(b, "en")))
const sources = Object.fromEntries(tags.map(tag => [tag, read(path.join(sourceDir, `${tag}.json`))]))
const keys = Object.keys(sources.en).sort()
if (!keys.length || (expectedCount !== null && keys.length !== expectedCount) || keys.some(key => !/^http\.[0-9a-f]{16}$/u.test(key))) {
  throw new Error(`the HTTP English source must have stable http.<digest> keys; got ${keys.length}`)
}
for (const tag of tags) {
  const target = sources[tag]
  if (!target || Array.isArray(target) || typeof target !== "object") throw new Error(`${tag}: expected a flat object`)
  const actual = Object.keys(target).sort()
  if (actual.length !== keys.length || actual.some((key, index) => key !== keys[index])) {
    throw new Error(`${tag}: HTTP key set differs from English`)
  }
}

const baselinePath = path.join(root, "public/catalogs/baseline-keys.json")
const baseline = read(baselinePath)
if (baseline.version !== 1 || !Array.isArray(baseline.keys)) throw new Error("expected the first-release baseline")
const catalogs = Object.fromEntries(tags.map(tag => [tag, read(path.join(root, `public/catalogs/${tag}.json`))]))
const before = Object.keys(catalogs.en).sort()
if (baseline.keys.length !== before.length || baseline.keys.some((key, index) => key !== before[index])) {
  throw new Error("baseline keys differ from the current English catalog")
}
for (const tag of tags) {
  for (const key of keys) {
    if (Object.hasOwn(catalogs[tag], key) && catalogs[tag][key] !== displayRefusal(key, sources[tag][key])) {
      throw new Error(`${tag}: refusing to replace an already reviewed ${key}`)
    }
  }
}
const merged = Object.fromEntries(tags.map(tag => [tag, ordered({
  ...catalogs[tag],
  ...Object.fromEntries(keys.map(key => [key, displayRefusal(key, sources[tag][key])])),
})]))
const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "clawdline-http-catalogs-"))
try {
  for (const tag of tags) fs.writeFileSync(path.join(scratch, `${tag}.json`), JSON.stringify(merged[tag], null, 2) + "\n")
  for (const tag of tags) {
    const check = spawnSync(process.execPath, [path.join(root, "tools/validate-catalog.mjs"),
      "--reference", path.join(scratch, "en.json"), "--target", path.join(scratch, `${tag}.json`), "--tag", tag, "--initial",
    ], { encoding: "utf8" })
    if (check.status !== 0) throw new Error(`${tag}: ${check.stderr || check.stdout}`)
  }
} finally {
  fs.rmSync(scratch, { recursive: true, force: true })
}
if (write) {
  for (const tag of tags) fs.writeFileSync(path.join(root, `public/catalogs/${tag}.json`), JSON.stringify(merged[tag], null, 2) + "\n")
  fs.writeFileSync(baselinePath, JSON.stringify({ version: 1, keys: Object.keys(merged.en).sort() }, null, 2) + "\n")
}
console.log(JSON.stringify({ sourceDir, imported: keys.length, total: Object.keys(merged.en).length, validated: tags, write }))
