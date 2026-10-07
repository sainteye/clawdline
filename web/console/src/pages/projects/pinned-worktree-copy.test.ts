import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { decoratePinnedWorktree, fixedWorktreeWord, hideUnrecordedWorktreeFacts, localizeWorktreeStatus } from "./pinned-worktree-copy.ts"

test("worktree labels from both pinned branches resolve to one catalog key", () => {
  assert.deepEqual(fixedWorktreeWord("已落地、內容相同的殘留工作樹", ["landedIdentical"]),
    { text: "Landed; identical residue", key: "landedIdentical" })
  assert.deepEqual(fixedWorktreeWord("Landed; identical residue", ["landedIdentical"]),
    { text: "Landed; identical residue", key: "landedIdentical" })
  assert.equal(fixedWorktreeWord("Landed; identical residue", ["inUse"]), null)
})

test("refresh and refusal copy changes without swallowing dynamic details", () => {
  assert.equal(localizeWorktreeStatus("保留上一次觀測。重新觀測忙碌中，請稍後再試。 · 503"),
    "Showing the previous observation. Refresh is busy; try again shortly. · 503")
  assert.equal(localizeWorktreeStatus("Observation refreshed. This browser remains read-only. Previous observation: Active inventory was not observed."),
    "Observation refreshed. This browser remains read-only. Previous observation: Active inventory was not observed.")
  assert.equal(localizeWorktreeStatus("User title: worktree refresh failed"), "User title: worktree refresh failed")
})

test("the pinned lifecycle adapter settles after one mutation and keeps counts", () => {
  const node = (initial: string) => {
    let value = initial
    let writes = 0
    return {
      get textContent() { return value },
      set textContent(next: string) { value = next; writes++ },
      get writes() { return writes },
    }
  }
  const summary = node("2 個工作樹 · 1 使用中 · 0 已暫存 · 0 已修改 · 0 未追蹤 · 0 未知")
  const status = node("觀測完成；沒有找到工作樹。")
  const root = {
    querySelector(selector: string) { return selector === "#project-worktree-summary" ? summary : status },
    querySelectorAll() { return [] },
  }
  const decorate = () => decoratePinnedWorktree(root as never, () => undefined)
  decorate()
  assert.equal(summary.textContent, "2 worktrees · 1 active · 0 staged · 0 modified · 0 untracked · 0 unknown")
  assert.equal(status.textContent, "Observation complete; no worktrees were found.")
  assert.deepEqual([summary.writes, status.writes], [1, 1])
  decorate()
  assert.deepEqual([summary.writes, status.writes], [1, 1])
})

test("foreign worktrees lose unrecorded task fields without losing observed facts", () => {
  const make = () => ({ removed: false, dataset: {}, remove() { this.removed = true } })
  const owner = make(), purpose = make(), story = make(), lifecycle = make(), nextOwner = make()
  const facts = Array.from({ length: 5 }, make)
  const nodes: Record<string, ReturnType<typeof make>> = {
    ".worktree-owner, .worktree-owner-link": owner,
    ".worktree-purpose": purpose,
    ".worktree-story": story,
    ".worktree-lifecycle-facts": lifecycle,
    ".worktree-next-owner": nextOwner,
  }
  const card = {
    querySelector(selector: string) { return nodes[selector]?.removed ? null : nodes[selector] ?? null },
    querySelectorAll(selector: string) {
      return selector === ".worktree-lifecycle-facts .worktree-fact"
        ? facts.filter(fact => !fact.removed) : []
    },
  }
  const row = {
    owner: { title: "", evidence: "not_a_clawdline_managed_worktree" },
    context: { purpose: "", state: "unknown", originSession: { title: "" } },
  }
  hideUnrecordedWorktreeFacts(card as never, row)
  hideUnrecordedWorktreeFacts(card as never, row)
  assert.deepEqual([owner, purpose, story, lifecycle, nextOwner].map(node => node.removed),
    [true, true, true, true, true])
  assert.equal(facts.every(fact => fact.removed), true)
})

test("recorded worktree facts survive repeated decoration even with missing dates", () => {
  const make = () => ({ removed: false, dataset: {} as Record<string, string>, remove() { this.removed = true } })
  const facts = Array.from({ length: 5 }, make)
  const card = {
    querySelector() { return null },
    querySelectorAll() { return facts.filter(fact => !fact.removed) },
  }
  const row = {
    owner: { title: "Release root", evidence: "exact_task_worktree_record" },
    context: { purpose: "Release", createdAt: "2026-10-08T00:00:00Z", state: "success",
      originSession: { title: "Release root" } },
  }
  hideUnrecordedWorktreeFacts(card as never, row)
  hideUnrecordedWorktreeFacts(card as never, row)
  assert.deepEqual(facts.map(fact => fact.removed), [false, true, true, false, false])
})
