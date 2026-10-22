#!/usr/bin/env node
// Refresh only after reviewing changed sources and both language versions.
import fs from "node:fs"
import path from "node:path"
import { createHash } from "node:crypto"
import { fileURLToPath } from "node:url"
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const catalogPath = path.join(root, "web/console/src/pages/transparency/catalog.json")
const catalog = JSON.parse(fs.readFileSync(catalogPath, "utf8"))
const hash = value => createHash("sha256").update(value).digest("hex")
const sources = new Set(catalog.cards.flatMap(card => card.sources.map(source => source.path)))
for (const file of [
  "internal/app/orchestrator/scheduled.go", "internal/adapters/taskdir/broker.go",
  "internal/app/start.go", "internal/app/squad_launch.go", "internal/adapters/squadfiles/launch.go",
  "internal/adapters/projects/launch.go", "internal/adapters/projects/claude_language.go",
  "internal/app/orchestrator/language.go", "internal/app/orchestrator/brief.go",
  "internal/app/orchestrator/child_guide.go", "internal/app/orchestrator/handoffs.go",
  "internal/app/orchestrator/handoff_pack.go", "internal/app/orchestrator/policy.go",
  "internal/app/orchestrator/policy_projection.go", "internal/app/orchestrator/dispatch-policy.md",
  "internal/app/orchestrator/notice.go", "internal/app/orchestrator/session_pause.go",
  "internal/app/orchestrator/compact.go", "internal/app/work_v2_todos.go",
  "internal/transport/http/start.go", "internal/transport/http/coordinator.go",
  "internal/transport/http/work_v2_brief.go", "internal/transport/http/project_memory.go",
  "internal/adapters/memory/memory.go", "internal/adapters/skillfile/skillfile.go",
  "internal/domain/snippet/default_workflow.go", "internal/config/config.go",
  "skills/clawdline/SKILL.md", "skills/clawdline/guide.md", "skills/clawdline/guide.zh-TW.md",
  "skills/sections.go", "skills/skills.go", "cmd/clawdline/session.go",
]) sources.add(file)
// A conservative superset of the Go guard's AST-discovered provider sinks.
function walk(dir) {
  for (const entry of fs.readdirSync(path.join(root, dir), { withFileTypes: true })) {
    const file = dir + "/" + entry.name
    if (entry.isDirectory()) walk(file)
    else if (file.endsWith(".go") && !file.endsWith("_test.go")) {
      const text = fs.readFileSync(path.join(root, file), "utf8")
      if (text.includes("developer_instructions") || text.includes("--append-system-prompt") || text.includes("injectSteps") || /\bPreInvocationResponse\s*\(/.test(text)) sources.add(file)
    }
  }
}
walk("internal"); walk("cmd")
catalog.sourceChecks = Object.fromEntries([...sources].sort().map(file => [file, hash(fs.readFileSync(path.join(root, file)))]))
catalog.copyChecks = {}
for (const lang of ["en", "zh-Hant"]) {
  const words = JSON.parse(fs.readFileSync(path.join(root, `web/console/public/catalogs/${lang}.json`), "utf8"))
  const reviewed = Object.fromEntries(Object.keys(words).filter(key => key.startsWith("transparency.")).sort().map(key => [key, words[key]]))
  // Go encoding/json's HTML escaping, used by the repository guard.
  const encoded = JSON.stringify(reviewed).replace(/[<>&\u2028\u2029]/g, c => "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"))
  catalog.copyChecks[lang] = hash(encoded)
}
fs.writeFileSync(catalogPath, JSON.stringify(catalog, null, 2) + "\n")
console.log(`Transparency review receipt: ${sources.size} sources and both languages. Review content before committing.`)
