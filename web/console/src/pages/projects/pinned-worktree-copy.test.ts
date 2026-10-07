import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { decoratePinnedWorktree, fixedWorktreeWord, localizeWorktreeStatus } from "./pinned-worktree-copy.ts"

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
