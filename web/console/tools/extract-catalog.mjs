// Rebuild the source inventory from the existing console copy tables.
// Run from any directory: node web/console/tools/extract-catalog.mjs --write
import fs from "node:fs"
import path from "node:path"
import vm from "node:vm"
import { fileURLToPath } from "node:url"
import ts from "typescript"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const read = (name) => fs.readFileSync(path.join(root, name), "utf8")

function table(name, extra, lang = "en") {
  const module = { exports: {} }
  const code = ts.transpileModule(read(name) + "\n" + extra, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  vm.runInNewContext(code, {
    module, exports: module.exports, document: { documentElement: { lang } },
    navigator: { language: lang }, Intl, console, require: () => ({ catalogWord: () => "" }),
  }, { filename: name, timeout: 3000 })
  return module.exports
}

function legacyEnglish() {
  const source = read("src/legacy/js/core/i18n.js")
  const match = source.match(/export var T = (\{[\s\S]*?\n\});/)
  if (!match) throw new Error("legacy English table was not found")
  return vm.runInNewContext("(" + match[1] + ")", {}, { timeout: 3000 })
}

function sorted(value) {
  return Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b, "en")))
}

const next = table("src/next-strings.ts", "export { words as sourceWords }").sourceWords
const work = table("src/pages/work/words.ts", "export { words as sourceWords }").sourceWords
const verify = table("src/pages/verify/words.ts", "export { words as sourceWords }").sourceWords
const settingsEn = table("src/pages/settings/window/copy.ts", "export { ownWords, baseW }", "en")
const settingsZh = table("src/pages/settings/window/copy.ts", "export { ownWords, baseW }", "zh-Hant")
const bar = table("src/bar/words.ts", "export { baseWords }")
const legacy = legacyEnglish()
const copiedZh = JSON.parse(read("public/strings/zh-Hant.json"))
const visible = JSON.parse(read("visible-inventory.json"))

const sources = {
  legacy: "src/legacy/js/core/i18n.js:17 and public/strings/zh-Hant.json",
  next: "src/next-strings.ts:13",
  work: "src/pages/work/words.ts:28",
  verify: "src/pages/verify/words.ts:12",
  settings: "src/pages/settings/window/copy.ts:22,125",
  bar: "src/bar/words.ts:36",
  inline: "visible-inventory.json (direct JSX text and accessible names)",
}
const en = { lang: "en", dir: "ltr" }
const zh = { lang: "zh-Hant", dir: "ltr" }
const unresolvedEnglish = []
const unresolvedChinese = []
for (const [domain, sourceEn, sourceZh] of [
  ["legacy", legacy, copiedZh],
  ["next", next.en, next["zh-Hant"]],
  ["work", work.en, work["zh-Hant"]],
  ["verify", verify.en, verify["zh-Hant"]],
  ["settings", settingsEn.baseW, settingsZh.baseW],
  ["bar", bar.baseWords, bar.baseWords],
]) {
  for (const [key, value] of Object.entries(sourceEn)) {
    const id = `${domain}.${key}`
    if (typeof value !== "string") continue
    en[id] = value
    const translated = sourceZh[key]
    zh[id] = typeof translated === "string" ? translated : value
    if (domain === "settings" && /[\u3400-\u9fff]/u.test(value) || domain === "bar") unresolvedEnglish.push(id)
    if (typeof translated !== "string") unresolvedChinese.push(id)
  }
}
for (const { id, value } of visible) {
  en[id] = value
  zh[id] = value
  if (/[\u3400-\u9fff]/u.test(value)) unresolvedEnglish.push(id)
  else unresolvedChinese.push(id)
}

const inventory = {
  sources,
  counts: Object.fromEntries(Object.keys(sources).map((domain) => [domain, Object.keys(en).filter((key) => key.startsWith(domain + ".")).length])),
  unresolvedEnglish,
  unresolvedChinese,
}
if (process.argv.includes("--write")) {
  const destination = path.join(root, "public/catalogs")
  fs.mkdirSync(destination, { recursive: true })
  fs.writeFileSync(path.join(destination, "en.json"), JSON.stringify(sorted(en), null, 2) + "\n")
  fs.writeFileSync(path.join(destination, "zh-Hant.json"), JSON.stringify(sorted(zh), null, 2) + "\n")
  fs.writeFileSync(path.join(root, "catalog-inventory.json"), JSON.stringify(inventory, null, 2) + "\n")
}
console.log(JSON.stringify({ keys: Object.keys(en).length, counts: inventory.counts, unresolvedEnglish: unresolvedEnglish.length, unresolvedChinese: unresolvedChinese.length }))
