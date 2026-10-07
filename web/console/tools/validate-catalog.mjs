// Validate one catalog. Initial release is complete; later secondary gaps are measured.
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const args = process.argv.slice(2)
const named = (flag, fallback) => {
  const at = args.indexOf(flag)
  return at < 0 ? fallback : args[at + 1]
}
const reference = named("--reference", path.join(root, "public/catalogs/en.json"))
const target = named("--target", args[0] && !args[0].startsWith("--") ? args[0] : reference)
const expectedTag = named("--tag", path.basename(target, ".json"))
const initial = args.includes("--initial")
const errors = []
function read(file, array = false) {
  try {
    const value = JSON.parse(fs.readFileSync(file, "utf8"))
    if (array ? !Array.isArray(value) : !value || Array.isArray(value) || typeof value !== "object") throw new Error(array ? "not a JSON array" : "not a JSON object")
    return value
  } catch (error) {
    errors.push(`${file}: ${error.message}`)
    return {}
  }
}
const en = read(reference)
const candidate = read(target)
const baseline = read(path.join(root, "public/catalogs/baseline-keys.json"))
const own = (object, key) => Object.prototype.hasOwnProperty.call(object, key)
const holes = (value) => [...value.matchAll(/\{([A-Za-z_][A-Za-z0-9_]*)\}/g)].map((match) => match[1]).sort().join(",")
if (!Number.isSafeInteger(baseline.version) || baseline.version < 1 || !Array.isArray(baseline.keys)) errors.push("baseline-keys.json should have a positive integer version and keys")
if (Array.isArray(baseline.keys) && new Set(baseline.keys).size !== baseline.keys.length) errors.push("duplicate baseline keys")
for (const key of Array.isArray(baseline.keys) ? baseline.keys : []) if (!own(en, key)) errors.push(`baseline key removed from English: ${key}`)
const strict = initial || expectedTag === "en" || expectedTag === "zh-Hant"
for (const key of Object.keys(en)) {
  if (!own(candidate, key)) {
    if (strict || (Array.isArray(baseline.keys) && baseline.keys.includes(key))) errors.push(`missing ${key}`)
    continue
  }
  const value = candidate[key]
  if (typeof value !== "string" || !value.trim()) {
    errors.push(`empty or non-string ${key}`)
    continue
  }
  const forms = value.split("\x1f")
  const referenceForms = en[key].split("\x1f")
  if (forms.length !== referenceForms.length || forms.some((form) => !form.trim())) errors.push(`plural form mismatch ${key}`)
  if (key !== "lang" && key !== "dir" && forms.some((form, index) => holes(form) !== holes(referenceForms[index] ?? ""))) errors.push(`placeholder mismatch ${key}`)
  if (/<\/?[A-Za-z][^>]*>/u.test(value) || /[\u0000-\u0008\u000B\u000C\u000E-\u001E]/u.test(value)) errors.push(`unsafe markup or control character ${key}`)
}
for (const key of Object.keys(candidate)) if (!own(en, key)) errors.push(`extra ${key}`)
if (candidate.lang !== expectedTag) errors.push(`lang should be ${expectedTag}`)
if (candidate.dir !== "ltr") errors.push("dir should be ltr")
const untranslated = Object.keys(en).filter((key) => key !== "lang" && key !== "dir" && candidate[key] === en[key]).length
const missing = Object.keys(en).filter((key) => !own(candidate, key))
console.log(JSON.stringify({ file: target, keys: Object.keys(candidate).length, untranslated, missing: missing.length, coverage: Math.round(10000 * (Object.keys(en).length - missing.length) / Object.keys(en).length) / 100, errors: errors.length }))
for (const error of errors) console.error(error)
if (errors.length) process.exitCode = 1
