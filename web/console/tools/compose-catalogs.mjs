// Add reviewed scattered copy to the frozen English and Traditional Chinese tables.
// Existing values win so a translator's explicit edit is never overwritten.
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const read = (name) => JSON.parse(fs.readFileSync(path.join(root, name), "utf8"))
const english = read("public/catalogs/en.json")
const chinese = read("public/catalogs/zh-Hant.json")
const source = []
function walk(directory) {
  for (const item of fs.readdirSync(directory, { withFileTypes: true })) {
    const file = path.join(directory, item.name)
    if (item.isDirectory()) { if (item.name !== "legacy") walk(file); continue }
    if (/\.tsx?$/u.test(file) && !/\.(test|e2e)\.tsx?$/u.test(file)) source.push(fs.readFileSync(file, "utf8"))
  }
}
walk(path.join(root, "src"))
const code = source.join("\n")
const extras = {}
for (const [id, value] of Object.entries(read("literal-candidates.json"))) {
  if (value.include && code.includes(`"${id.slice(8)}"`)) extras[id] = value
}
for (const [id, value] of Object.entries(read("template-candidates.json"))) {
  if (value.include && code.includes(`"${id.slice(9)}"`)) extras[id] = value
}
Object.assign(extras, read("nested-copy.json"), read("ui-copy.json"))
for (const [id, value] of Object.entries(extras)) {
  if (!english[id]) english[id] = value.en
  if (!chinese[id]) chinese[id] = value["zh-Hant"]
}
for (const [tag, catalog] of [["en", english], ["zh-Hant", chinese]]) {
  const ordered = Object.fromEntries(Object.entries(catalog).sort(([a], [b]) => a.localeCompare(b, "en")))
  if (process.argv.includes("--write")) fs.writeFileSync(path.join(root, `public/catalogs/${tag}.json`), JSON.stringify(ordered, null, 2) + "\n")
}
console.log(JSON.stringify({ english: Object.keys(english).length, chinese: Object.keys(chinese).length, added: Object.keys(extras).length }))
