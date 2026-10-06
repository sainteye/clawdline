// Freeze the English keys whose polarity or action state requires manual review.
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const english = JSON.parse(fs.readFileSync(path.join(root, "public/catalogs/en.json"), "utf8"))
const patterns = {
  negation: /\b(?:not|no|never|cannot|can't|without)\b/iu,
  state: /\b(?:pending|done|completed?|finished?|failed?|acknowledged?|accepted|rejected|refused|delivered|sent|unsent|observed|executed|applied|saved|lost|dropped|expired|cancelled|canceled|running|stopped|waiting|closed|open)\b/iu,
  permission: /\b(?:permission|allow(?:ed)?|deny|denied|forbidden|authorized|authorization|access|read.only|write|revoke|revoked|pair(?:ed|ing)?|sign.?in|signed.?out|credential)\b/iu,
  deletion: /\b(?:delet(?:e|ed|ion|ing)?|remov(?:e|ed|al|ing)?|undo|revert|restore|erase|discard|throw(?:s|n)? away|irrecoverable|permanent)\b/iu,
  uncertainty: /\b(?:unconfirmed|unknown|unavailable|unreadable|unreachable|incomplete|uncertain|ambiguous|missing|not found)\b/iu,
}
const entries = Object.entries(english).filter(([key]) => key !== "lang" && key !== "dir")
const categories = Object.fromEntries(Object.entries(patterns).map(([name, pattern]) => [name,
  entries.filter(([, value]) => pattern.test(value)).map(([key]) => key).sort(),
]))
const union = [...new Set(Object.values(categories).flat())].sort()
const result = {
  version: 1,
  englishKeys: entries.length,
  counts: Object.fromEntries(Object.entries(categories).map(([name, keys]) => [name, keys.length])),
  unionCount: union.length,
  categories,
  union,
}
const file = path.join(root, "semantic-risk-keys.json")
if (process.argv.includes("--write")) fs.writeFileSync(file, JSON.stringify(result, null, 2) + "\n")
if (process.argv.includes("--check") && fs.readFileSync(file, "utf8") !== JSON.stringify(result, null, 2) + "\n") {
  console.error("semantic-risk-keys.json is out of date")
  process.exitCode = 1
}
console.log(JSON.stringify({ englishKeys: result.englishKeys, ...result.counts, union: result.unionCount }))
