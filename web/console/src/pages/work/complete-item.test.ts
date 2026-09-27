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

test("the Session item detail completes through the Board card's inline confirmation", () => {
  const todos = readFileSync(new URL("../../session/Todos.tsx", import.meta.url), "utf8")
  // A Session opens the Board's own card, so it has no completion of its own.
  assert.match(todos, /onOpen=\{\(\) => openWorkItem\(item\)\}/)
  assert.doesNotMatch(todos, /completeWorkV2/)
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
