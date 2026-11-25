import { test } from "node:test"
import assert from "node:assert/strict"
import { readdirSync, readFileSync } from "node:fs"
import { dirname, join, relative, sep } from "node:path"
import { fileURLToPath } from "node:url"

// Every screen that offers a model for an agent reads the daemon's one model
// catalog through web/console/src/model-catalog (docs/model-catalog.md). Before
// 2026-11-25 the Settings, Start, Board, schedule and voice pickers each had
// their own list: a hard-coded ["haiku","sonnet","opus"], the model options of
// /v1/settings/default-models, or a Grok that was default-only by fiat. This
// test fails when one of those comes back.

const src = join(dirname(fileURLToPath(import.meta.url)), "..")

/** Product sources: no tests, fixtures or e2e drivers, which stage answers on purpose. */
export function productSources(root: string): string[] {
  const out: string[] = []
  const visit = (dir: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const at = join(dir, entry.name)
      if (entry.isDirectory()) { if (entry.name !== "node_modules") visit(at); continue }
      if (!/\.(ts|tsx|js|mjs)$/.test(entry.name) || /\.(test|e2e|fixture)\.|\.d\.ts$/.test(entry.name)) continue
      out.push(relative(root, at).split(sep).join("/"))
    }
  }
  visit(root)
  return out.sort()
}

const ALIAS = `(["'])(?:haiku|sonnet|opus|fable)\\1`
/** Two Claude aliases next to each other in a list literal: a hand-written model list. */
const HARD_CODED_LIST = new RegExp(`\\[\\s*${ALIAS}\\s*,\\s*${ALIAS}`)
/** Reading the default-models answer's `models` table for a picker's options. */
const DEFAULT_MODELS_OPTIONS = /\.models\s*\?{0,1}\.{0,1}\s*\[|\bdefaultModelOptions\b/
const DEFAULT_MODELS_ROUTE = "/v1/settings/default-models"
const CATALOG_ROUTE = "/v1/model-catalog"

/** What the guard finds in one file's text, by rule. */
export function findings(path: string, text: string): string[] {
  const found: string[] = []
  if (path.startsWith("model-catalog/")) return found
  if (HARD_CODED_LIST.test(text)) found.push(`${path}: a hard-coded model list`)
  if (text.includes(DEFAULT_MODELS_ROUTE) && DEFAULT_MODELS_OPTIONS.test(text)) found.push(`${path}: model options from ${DEFAULT_MODELS_ROUTE}`)
  // The route itself is named only where it is carried: the Cloud relay and the captured Bot source.
  if (text.includes(CATALOG_ROUTE) && !path.startsWith("cloud/")) found.push(`${path}: reads ${CATALOG_ROUTE} outside model-catalog/`)
  return found
}

test("the guard recognises each list it exists to catch", () => {
  assert.equal(findings("pages/schedules.tsx", `const MODELS = ["haiku", "sonnet", "opus"]`).length, 1)
  assert.equal(findings("pages/x.tsx", `const M = ['opus','sonnet']`).length, 1)
  assert.equal(findings("pages/settings/api.ts", `fetch("/v1/settings/default-models"); const list = snapshot.models?.[assistant]`).length, 1)
  assert.equal(findings("realtime/copilot.ts", `fetch('/v1/settings/default-models'); d.models[a.id]`).length, 1)
  assert.equal(findings("session/Start.tsx", `fetch("/v1/model-catalog")`).length, 1)
  assert.deepEqual(findings("model-catalog/machine.ts", `fetch("/v1/model-catalog")`), [])
  assert.deepEqual(findings("pages/settings/api.ts", `fetch("/v1/settings/default-models") // the saved defaults`), [])
  assert.deepEqual(findings("overlays/info.ts", `const one = "opus"`), [])
})

test("no console source outside model-catalog/ fetches or hard-codes an agent model list", () => {
  const all = productSources(src).flatMap(path => findings(path, readFileSync(join(src, path), "utf8")))
  assert.deepEqual(all, [])
})
