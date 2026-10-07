// Reproducible screen/flow inventory for catalog entries outside the semantic risk union.
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const read = name => JSON.parse(fs.readFileSync(path.join(root, name), "utf8"))
const english = read("public/catalogs/en.json")
const risk = new Set(read("semantic-risk-keys.json").union)
const sourceByKey = new Map()
for (const file of ["literal-inventory.json", "visible-inventory.json", "template-inventory.json"]) {
  for (const item of read(file)) if (!sourceByKey.has(item.id)) sourceByKey.set(item.id, item.file)
}
const namedSources = {
  bar: "src/bar/words.ts", legacy: "src/legacy/js/core/i18n.js", next: "src/next-strings.ts",
  projects: "src/pages/projects/project-list.ts", settings: "src/pages/settings/window/copy.ts",
  squad: "src/pages/Squad.tsx",
  documents: "src/pages/documents/copy-adapter.ts and src/legacy/js/view/documents.js",
  ui: "src/ui-language", verify: "src/pages/verify/words.ts", work: "src/pages/work/words.ts",
  worktree: "src/pages/projects/pinned-worktree-copy.json",
  http: "internal/productcopy/http_refusals/en.json",
}
function groupFor(key) {
  const [domain, name] = key.split(".", 2)
  const file = sourceByKey.get(key)
  if (file) {
    const base = path.basename(file).replace(/\.(tsx?|jsx?)$/u, "")
    if (file.includes("/projects/") && /^project-files|ProjectFiles$/u.test(base)) return "Projects / files"
    if (file.includes("/work/") && base === "WorkV2") return "Work / Board"
    if (file.startsWith("src/pages/projects/")) {
      if (/Sync/u.test(base)) return "Projects / sync"
      if (/Unify|unify/u.test(base)) return "Projects / unify"
      if (/Setup|setup/u.test(base)) return "Projects / setup"
      return "Projects / other controls"
    }
    if (file.startsWith("src/pages/work/")) return "Work / other controls"
    if (file.startsWith("src/session/")) return /Interventions|Todos/u.test(base) ? `Session / ${base}` : "Session / other controls"
    if (file.startsWith("src/pages/settings/")) return "Settings / controls"
    if (file.startsWith("src/pages/squad/")) return "Squad / controls"
    if (file.startsWith("src/machine/")) return "Machine / dashboard"
    return `Console / ${base}`
  }
  const prefix = domain === "legacy" ? /^web([A-Z][a-z]+)/u.exec(name)?.[1] : /^[a-z]+/u.exec(name)?.[0]
  const allowed = {
    legacy: new Set(["Info", "Coord", "Schedule", "Plan", "Project", "Ledger", "Snippet", "Cloud", "Command", "Door", "Keys", "Start", "Fail", "Git", "Notice", "Settings", "Voice"]),
    next: new Set(["terminal", "cloud", "persona", "schedule", "archive", "end", "image", "restore", "send", "signed", "bg", "default", "projects", "links"]),
    work: new Set(["usage", "digest", "op", "document", "proposal", "todo"]),
  }
  return `${domain} / ${allowed[domain]?.has(prefix) ? prefix : "other"}`
}
const groups = new Map()
for (const key of Object.keys(english).sort()) {
  if (key === "lang" || key === "dir" || risk.has(key)) continue
  const name = groupFor(key)
  if (!groups.has(name)) groups.set(name, { name, keys: [], sources: new Set() })
  const group = groups.get(name)
  group.keys.push(key)
  group.sources.add(sourceByKey.get(key) || namedSources[key.split(".")[0]])
}
const result = {
  version: 1,
  englishContentKeys: Object.keys(english).length - 2,
  riskKeys: risk.size,
  nonRiskCount: [...groups.values()].reduce((n, group) => n + group.keys.length, 0),
  groups: [...groups.values()].sort((a, b) => a.name.localeCompare(b.name)).map(group => ({
    name: group.name, count: group.keys.length, sources: [...group.sources].sort(), keys: group.keys,
  })),
}
const serialized = JSON.stringify(result, null, 2) + "\n"
const target = path.join(root, "semantic-screen-groups.json")
if (process.argv.includes("--write")) fs.writeFileSync(target, serialized)
if (process.argv.includes("--check") && fs.readFileSync(target, "utf8") !== serialized) {
  console.error("semantic-screen-groups.json is out of date")
  process.exitCode = 1
}
console.log(JSON.stringify({ groups: result.groups.length, risk: result.riskKeys, nonRisk: result.nonRiskCount }))
