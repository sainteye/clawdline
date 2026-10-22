import { strict as assert } from "node:assert"
import { readFileSync } from "node:fs"
import { test } from "node:test"
// @ts-expect-error Node's strip-types test runner needs the source extension.
import { filterHash, filtersFromHash, matches, toggleFilter } from "./model.ts"
const cards = JSON.parse(readFileSync(new URL("./catalog.json", import.meta.url), "utf8")).cards
const empty = () => filtersFromHash("")
const visible = (hash: string) => cards.filter((entry: Parameters<typeof matches>[0]) => matches(entry, filtersFromHash(hash))).map((entry: { id: string }) => entry.id)

test("ordinary Codex startup defaults exclude optional memory, role and guide reads", () => {
  assert.deepEqual(visible("#page=transparency&scope=default&agents=codex"), ["codex-language", "codex-note"])
  assert.deepEqual(visible("#page=transparency&scope=default&agents=claude"), [])
})
test("facet selection is OR within a row and AND between rows", () => {
  const both = visible("#page=transparency&agents=claude,codex&scope=default,automatic")
  assert(both.includes("codex-language")); assert(both.includes("claude-language")); assert(both.includes("memory-index"))
  assert(!both.includes("board")); assert(!both.includes("claude-role"))
  assert.deepEqual(visible("#page=transparency&agents=claude&scenes=role&stages=start"), ["claude-role"])
})
test("delivery rules are loaded with guides, rather than newly injected on completion", () => {
  assert(visible("#page=transparency&stages=start&scenes=skill").includes("report"))
  assert(!visible("#page=transparency&stages=finish").includes("report"))
  assert(visible("#page=transparency&stages=finish").includes("close"))
})
test("filter links round-trip, invalid values are discarded, toggling and reset preserve empty-means-all", () => {
  let f = toggleFilter(empty(), "agents", "codex")
  f = toggleFilter(f, "scope", "default")
  assert.deepEqual(filtersFromHash(filterHash(f)), f)
  assert.deepEqual(filtersFromHash("#agents=codex,codex,unknown&scope=bogus").agents, ["codex"])
  assert.deepEqual(toggleFilter(f, "agents", "bad"), f)
  assert.equal(visible(filterHash(empty())).length, cards.length)
  assert.deepEqual(toggleFilter(toggleFilter(empty(), "agents", "codex"), "agents", "codex"), empty())
})

test("native context has its own conditional provider filter", () => {
  assert(visible("#agents=antigravity&scope=conditional&scenes=memory&stages=turn").includes("antigravity-context"))
  assert.deepEqual(visible("#agents=antigravity&scope=default"), [])
  assert(!visible("#agents=codex").includes("antigravity-context"))
  const f = filtersFromHash("#agents=antigravity&scenes=memory")
  assert.deepEqual(f.agents, ["antigravity"])
  assert.deepEqual(filtersFromHash(filterHash(f)), f)
})
