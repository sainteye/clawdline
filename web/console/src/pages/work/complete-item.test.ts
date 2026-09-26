import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { completeConfirmWords, openSteps } from "./complete-item.ts"

const step = (done: boolean) => ({ id: "s", title: "t", done, position: 0, created_by: "", completed_by: "",
  completed_at: null, version: 1 })

test("the manual-completion confirmation says how many steps are still open", () => {
  assert.equal(openSteps({ steps: undefined }), 0)
  assert.equal(openSteps({ steps: [step(true), step(false), step(false)] }), 2)
  assert.equal(completeConfirmWords({ steps: [step(true), step(false), step(false)] }), "還有 2 個步驟未完成，仍要標記完成嗎？")
  assert.equal(completeConfirmWords({ steps: [step(true)] }), "確定要標記這個項目完成嗎？")
})

test("the Session item detail completes only after an inline confirmation", () => {
  const todos = readFileSync(new URL("../../session/Todos.tsx", import.meta.url), "utf8")
  assert.doesNotMatch(todos, /window\.confirm/)
  // The first press only opens the confirmation; the second calls the route.
  assert.match(todos, /onClick=\{\(\) => setConfirming\(true\)\}>\s*<WorkIcon name="check" \/>標記完成/)
  assert.match(todos, /completeConfirmWords\(item\)[\s\S]*?onComplete\(\)[\s\S]*?確認標記完成[\s\S]*?onClick=\{\(\) => setConfirming\(false\)\}>取消/)
  assert.match(todos, /const answer = await completeWorkV2\(item\)[\s\S]*?setDetail\([\s\S]*?answer\.item[\s\S]*?await refresh\(true\)/)
  // A failure names the action that failed.
  assert.match(todos, /setDetailActionFailure\(`提醒傳送失敗：\$\{failureWords\(error\)\}`\)/)
  assert.match(todos, /setDetailActionFailure\(`標記完成失敗：\$\{failureWords\(error\)\}`\)/)
  assert.doesNotMatch(todos, />提醒傳送失敗：\{actionFailure\}/)
})

test("a Board card completes only after an inline confirmation", () => {
  const board = readFileSync(new URL("./WorkV2.tsx", import.meta.url), "utf8")
  assert.doesNotMatch(board, /window\.confirm/)
  assert.match(board, /!item\.closed_at && <button type="button" disabled=\{!!busy\}\s+onClick=\{\(\) => \{ clearFailure\(\); setCompleting\(true\) \}\}><WorkIcon name="check" \/> 完成<\/button>/)
  assert.match(board, /completeConfirmWords\(item\)[\s\S]*?run\(`complete-\$\{item\.id\}`, \(\) => completeWorkV2\(item\)\)[\s\S]*?確認標記完成[\s\S]*?setCompleting\(false\)[\s\S]*?取消/)
})

test("a work action chip lays its icon to the left of its words", () => {
  // `.work-icon` is a block, so in a plain chip it took a line of its own above
  // 標記完成 and 確認標記完成 instead of sitting beside them.
  const css = readFileSync(new URL("./work.css", import.meta.url), "utf8")
  assert.match(css, /\.work-actions \.chip:has\(> \.work-icon\) \{[^}]*display: inline-flex;[^}]*align-items: center;/)
})
