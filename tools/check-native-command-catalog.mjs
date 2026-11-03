#!/usr/bin/env node
// Compare a reviewed snapshot with provider sources saved by the maintainer.
// No network calls or CLI/model execution. See docs/native-commands.md.
import { readFileSync } from "node:fs"
import { fileURLToPath } from "node:url"
import assert from "node:assert/strict"

const [claudePath, codexPath] = process.argv.slice(2)
if (!claudePath || !codexPath) throw new Error("usage: node tools/check-native-command-catalog.mjs commands.md slash_command.rs")
const catalog = JSON.parse(readFileSync(fileURLToPath(new URL("../web/console/src/legacy/native-commands.json", import.meta.url)), "utf8"))
const claude = readFileSync(claudePath,"utf8")
const expectedClaude = [...claude.matchAll(/^\| `\/([^` ]+)[^`]*` \| (.*)$/gm)]
 .filter(match => !match[2].startsWith("Removed"))
 .map(match=>match[1]).sort()
assert.ok(expectedClaude.length, "no Claude command table found")
assert.deepEqual(catalog.claude.commands.map(row=>row.name).sort(), expectedClaude)

const codex = readFileSync(codexPath,"utf8")
const body = codex.split("pub enum SlashCommand {")[1]?.split("\n}")[0]
assert.ok(body, "no Codex command enum found")
const expectedCodex=[]
const excluded = new Set(["Rollout","TestApproval","MemoryDrop","MemoryUpdate"])
// Release-hidden diagnostics are excluded; model-provided service tiers are
// checked separately by the CLI and are not members of this enum.
let attribute=""
for (const line of body.split("\n")) {
 if (line.trim().startsWith("#[strum(")) attribute=line
 const variant=line.match(/^    ([A-Za-z]+),$/)?.[1]
 if (!variant) continue
 if (!excluded.has(variant)) expectedCodex.push(attribute.match(/(?:to_string|serialize) = "([^"]+)"/)?.[1] ?? variant.replace(/[A-Z]/g,(letter,at)=>(at?"-":"")+letter.toLowerCase()))
 attribute=""
}
const actualCodex = catalog.codex.commands.map(row=>row.name).filter(name=>name!=="fast").sort()
assert.deepEqual(actualCodex,expectedCodex.sort())
for (const provider of [catalog.claude,catalog.codex]) {
 assert.equal(new Set(provider.commands.map(row=>row.name)).size,provider.commands.length)
 for (const command of provider.commands) assert.ok(command.description && command.descriptionZhHant)
}
console.log(`native commands: ${expectedClaude.length} Claude entries and ${expectedCodex.length} Codex built-ins match; fast is a service-tier fallback`)
