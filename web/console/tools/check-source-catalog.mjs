// Every word the code asks for must exist in the catalogs that answer it.
//
// `nextWord` and its siblings read `catalogWord`, whose last fallback is the
// key's own id (`src/catalog.ts`). A key added to a typed source table but not
// to `public/catalogs/` therefore reaches a person as `next.cloudAllOldVersion`
// on screen — not as English, and not as a build failure. The hosted catalog
// check cannot see it either: that one validates the other languages against
// `en.json`, so a hole in `en.json` is a hole in its reference.
//
// So: every source key must be in `en.json` and `zh-Hant.json`, and every
// English source value must be the one `en.json` serves. The other seven
// languages keep falling back to English by design (`docs/localization.md`),
// so a key may be absent from them — but a translation still present must
// carry the same placeholders, because rewriting an English sentence's holes
// leaves every older translation of it both wrong and invalid. `--write` adds
// what is missing, resynchronises English, and drops the secondary
// translations whose holes no longer match so they fall back to English
// instead of showing a sentence from two revisions ago.
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

const next = table("src/next-strings.ts", "export { words as sourceWords }").sourceWords
const work = table("src/pages/work/words.ts", "export { words as sourceWords }").sourceWords
const verify = table("src/pages/verify/words.ts", "export { words as sourceWords }").sourceWords
const settings = table("src/pages/settings/window/copy.ts", "export { ownWords, baseW }", "en")
const settingsZh = table("src/pages/settings/window/copy.ts", "export { ownWords, baseW }", "zh-Hant")
const bar = table("src/bar/words.ts", "export { baseWords }")

// `english: false` where the table's "English" column is already translated
// copy (settings carries Chinese, the bar one word for both), so only the key
// is required there, never the value.
const domains = [
  { name: "next", en: next.en, zh: next["zh-Hant"], english: true },
  { name: "work", en: work.en, zh: work["zh-Hant"], english: true },
  { name: "verify", en: verify.en, zh: verify["zh-Hant"], english: true },
  { name: "settings", en: settings.baseW, zh: settingsZh.baseW, english: false },
  { name: "bar", en: bar.baseWords, zh: bar.baseWords, english: false },
]

const SECONDARY = ["ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"]
const catalogs = {}
for (const tag of ["en", "zh-Hant", ...SECONDARY]) {
  catalogs[tag] = { file: `public/catalogs/${tag}.json`, data: JSON.parse(read(`public/catalogs/${tag}.json`)) }
}

/** The same rule `validate-catalog.mjs` applies, so this cannot disagree with it. */
const holes = (value) => [...value.matchAll(/\{([A-Za-z_][A-Za-z0-9_]*)\}/g)].map((match) => match[1]).sort().join(",")

const missing = { en: [], "zh-Hant": [] }
const drifted = []
const incompatible = []
for (const domain of domains) {
  for (const [key, value] of Object.entries(domain.en)) {
    if (typeof value !== "string") continue
    const id = `${domain.name}.${key}`
    for (const tag of ["en", "zh-Hant"]) {
      if (!Object.hasOwn(catalogs[tag].data, id)) missing[tag].push({ id, domain: domain.name, key })
    }
    if (domain.english && Object.hasOwn(catalogs.en.data, id) && catalogs.en.data[id] !== value) {
      drifted.push({ id, domain: domain.name, key, shown: catalogs.en.data[id], source: value })
    }
    const wanted = holes(value)
    if (Object.hasOwn(catalogs["zh-Hant"].data, id) && holes(catalogs["zh-Hant"].data[id]) !== wanted) {
      incompatible.push({ id, tag: "zh-Hant", required: true })
    }
    for (const tag of SECONDARY) {
      if (Object.hasOwn(catalogs[tag].data, id) && holes(catalogs[tag].data[id]) !== wanted) {
        incompatible.push({ id, tag, required: false })
      }
    }
  }
}

/** Keep the file's existing order; a new key joins the end of its own domain. */
function write(tag, additions, updates, removals = new Set()) {
  const { file, data } = catalogs[tag]
  const pending = new Map(additions.map((item) => [item.id, item.value]))
  const result = {}
  const ids = Object.keys(data)
  for (const [index, id] of ids.entries()) {
    if (!removals.has(id)) result[id] = Object.hasOwn(updates, id) ? updates[id] : data[id]
    const domain = id.slice(0, id.indexOf("."))
    const nextId = ids[index + 1] ?? ""
    if (nextId.slice(0, nextId.indexOf(".")) === domain) continue
    for (const [newId, value] of [...pending]) {
      if (newId.slice(0, newId.indexOf(".")) !== domain) continue
      result[newId] = value
      pending.delete(newId)
    }
  }
  for (const [newId, value] of pending) result[newId] = value
  fs.writeFileSync(path.join(root, file), JSON.stringify(result, null, 2) + "\n")
}

const stranded = incompatible.filter((item) => !item.required)
const mistranslated = incompatible.filter((item) => item.required)
const writing = process.argv.includes("--write")
if (writing) {
  const sourceOf = Object.fromEntries(domains.map((domain) => [domain.name, domain]))
  for (const tag of ["en", "zh-Hant"]) {
    const additions = missing[tag].map(({ id, domain, key }) => {
      const table = sourceOf[domain]
      const value = tag === "en" ? table.en[key] : table.zh?.[key]
      return { id, value: typeof value === "string" ? value : table.en[key] }
    })
    const updates = tag === "en"
      ? Object.fromEntries(drifted.map(({ id, source }) => [id, source])) : {}
    if (additions.length || Object.keys(updates).length) write(tag, additions, updates)
  }
  for (const tag of SECONDARY) {
    const removals = new Set(stranded.filter((item) => item.tag === tag).map((item) => item.id))
    if (removals.size) write(tag, [], {}, removals)
  }
  console.log(`source catalog: added ${missing.en.length} English and ${missing["zh-Hant"].length} Chinese keys, ` +
    `resynchronised ${drifted.length} English values, dropped ${stranded.length} secondary translations whose placeholders no longer match`)
  if (mistranslated.length) {
    for (const { id } of mistranslated) console.error(`${id}: the Traditional Chinese value's placeholders do not match the English source; fix it by hand`)
    process.exitCode = 1
  }
} else if (missing.en.length || missing["zh-Hant"].length || drifted.length || incompatible.length) {
  for (const tag of ["en", "zh-Hant"]) {
    for (const { id } of missing[tag]) console.error(`${id}: in the source table, absent from ${catalogs[tag].file} (it would be shown as "${id}")`)
  }
  for (const { id, shown, source } of drifted) {
    console.error(`${id}: en.json serves ${JSON.stringify(shown)}, the source table says ${JSON.stringify(source)}`)
  }
  for (const { id, tag, required } of incompatible) {
    console.error(`${id}: the ${tag} value's placeholders do not match the English source` +
      (required ? "; Traditional Chinese must be fixed by hand" : "; --write drops it so it falls back to English"))
  }
  console.error("Run: node web/console/tools/check-source-catalog.mjs --write")
  process.exitCode = 1
} else {
  const total = domains.reduce((sum, domain) => sum + Object.keys(domain.en).length, 0)
  // A Chinese value may legitimately differ: zh-Hant.json is where a reviewed
  // translation is edited, and the source table can be the older of the two.
  // So this is counted and named, never enforced and never rewritten.
  const chinese = []
  for (const domain of domains) {
    for (const [key, value] of Object.entries(domain.zh ?? {})) {
      const id = `${domain.name}.${key}`
      if (typeof value === "string" && Object.hasOwn(catalogs["zh-Hant"].data, id) &&
        catalogs["zh-Hant"].data[id] !== value) chinese.push(id)
    }
  }
  console.log(`source catalog: ${total} keys in en.json and zh-Hant.json, English values and placeholders match`)
  if (chinese.length) {
    console.log(`source catalog: ${chinese.length} Chinese values differ from their source table ` +
      `(a reviewed translation, or copy not carried over): ${chinese.join(", ")}`)
  }
}
