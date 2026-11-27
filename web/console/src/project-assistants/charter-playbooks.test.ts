import { test } from "node:test"
import assert from "node:assert/strict"
import chinese from "../../public/catalogs/zh-Hant.json" with { type: "json" }
// @ts-expect-error Node executes source extensions.
import { activateCatalog, catalogFormat, resetCatalogForTest } from "../catalog.ts"
// @ts-expect-error Node executes source extensions.
import { readsCurrentAt } from "./charter-api.ts"
// @ts-expect-error Node executes source extensions.
import { assignmentChange, changedPlaybooks, firstSentence, playbookReasonKey, playbookState, unreadByActivity } from "./charter-playbooks.ts"

const reader = (bot_id: string, version: number, extra: Record<string, unknown> = {}) =>
  ({ bot_id, name: bot_id, hidden: false, lead: false, turn_id: "", running: false, queued: false, version, ...extra })

test("a card previews the first sentence without list or heading marks", () => {
  assert.equal(firstSentence("1. Tag the commit.\n2. Write the release note."), "Tag the commit.")
  assert.equal(firstSentence("\n\n## 品質驗收\n\n每次交付前，先跑完測試。再列出驗證項目。"), "品質驗收")
  assert.equal(firstSentence("每次交付前，先跑完和修改相關的測試。\n\n- 列出驗證過的項目"), "每次交付前，先跑完和修改相關的測試。")
  assert.equal(firstSentence("**先寫結論**，再寫原因。"), "先寫結論，再寫原因。")
  // A decimal point is not a full stop; a sentence with no stop is the whole line.
  assert.equal(firstSentence("- Use version 1.2 of the tool"), "Use version 1.2 of the tool")
  assert.equal(firstSentence("   \n"), "")
  assert.equal(firstSentence("a".repeat(120), 10), "aaaaaaaaa…")
})

test("a card says current, unread or unassigned from its assigned Bots", () => {
  assert.equal(playbookState({ read: [reader("a", 5)], unread: [] }), "current")
  assert.equal(playbookState({ read: [reader("a", 5)], unread: [reader("b", 3)] }), "unread")
  assert.equal(playbookState({ read: [], unread: [] }), "unassigned")
})

test("a Bot is current at its own delivery version, not the charter's", () => {
  assert.equal(readsCurrentAt(6, reader("a", 3, { delivery_version: 3 })), true)
  assert.equal(readsCurrentAt(6, reader("a", 3, { delivery_version: 5 })), false)
  // An older machine has no delivery version: the charter's version counts.
  assert.equal(readsCurrentAt(6, reader("a", 5)), false)
  assert.equal(readsCurrentAt(0, reader("a", 0)), false)
})

test("the unread reminder separates Bots in a turn from idle ones", () => {
  const split = unreadByActivity([reader("lens", 3, { running: true }), reader("orbit", 3), reader("beacon", 3, { queued: true })])
  assert.deepEqual(split.working.map(r => r.bot_id), ["lens", "beacon"])
  assert.deepEqual(split.idle.map(r => r.bot_id), ["orbit"])
})

test("reassigning names who starts reading and who is told it no longer applies", () => {
  assert.deepEqual(assignmentChange(["a", "b"], ["b", "c"]), { added: ["c"], removed: ["a"] })
  assert.deepEqual(assignmentChange(["a"], ["a"]), { added: [], removed: [] })
})

test("history shows each changed playbook by id: added, edited, renamed, reassigned, removed", () => {
  const before = [
    { id: "pb-1", title: "品質驗收", text: "先跑測試。", bot_ids: ["a"] },
    { id: "pb-2", title: "發版", text: "標記。", bot_ids: ["b"] },
    { id: "pb-3", title: "不變", text: "x", bot_ids: ["a", "b"] },
  ]
  const after = [
    { id: "pb-1", title: "品質驗收", text: "先跑測試。\n附截圖。", bot_ids: ["a", "c"] },
    { id: "pb-3", title: "不變", text: "x\n", bot_ids: ["b", "a"] },
    { id: "pb-4", title: "文件", text: "先寫結論。", bot_ids: [] },
  ]
  const changes = changedPlaybooks(before, after)
  assert.deepEqual(changes.map(c => [c.id, c.kind]), [["pb-1", "edited"], ["pb-4", "added"], ["pb-2", "removed"]])
  assert.deepEqual(changes[0].lines.map(l => [l.kind, l.text]), [["same", "先跑測試。"], ["added", "附截圖。"]])
  assert.deepEqual(changes[0].assigned, { before: ["a"], after: ["a", "c"] })
  assert.deepEqual(changes[1].lines.map(l => l.kind), ["added"])
  assert.deepEqual(changes[2].lines.map(l => [l.kind, l.text]), [["removed", "標記。"]])
  const renamed = changedPlaybooks([before[1]], [{ ...before[1], title: "發版步驟" }])
  assert.equal(renamed[0].renamedFrom, "發版")
  assert.deepEqual(renamed[0].lines, [])
  assert.equal(renamed[0].assigned, undefined)
  assert.deepEqual(changedPlaybooks(before, before), [])
})

test("each playbook write records its reason in the person's words", () => {
  const oldDocument = globalThis.document
  Object.defineProperty(globalThis, "document", { configurable: true, value: { documentElement: { lang: "en", dir: "ltr", setAttribute() {} } } })
  try {
    activateCatalog(chinese, "zh-Hant")
    const reason = (kind: "add" | "edit" | "assign" | "remove") => catalogFormat("teamCharter", playbookReasonKey(kind), ["品質驗收"])
    assert.equal(reason("edit"), "編輯 Playbook『品質驗收』")
    assert.equal(reason("add"), "新增 Playbook『品質驗收』")
    assert.equal(reason("assign"), "更改 Playbook『品質驗收』交給誰")
    assert.equal(reason("remove"), "刪除 Playbook『品質驗收』")
    resetCatalogForTest()
    assert.equal(catalogFormat("teamCharter", playbookReasonKey("edit"), ["Quality checks"]), "Edited playbook “Quality checks”")
  } finally {
    resetCatalogForTest()
    Object.defineProperty(globalThis, "document", { configurable: true, value: oldDocument })
  }
})
