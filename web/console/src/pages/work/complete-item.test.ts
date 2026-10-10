import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
test("the manual-completion confirmation includes an open-step warning", () => {
  const helper = readFileSync(new URL("./complete-item.ts", import.meta.url), "utf8")
  assert.match(helper, /filter\(\(step\) => !step\.done\)\.length/)
  assert.match(helper, /open > 0 \? catalogFormat\("template", "61504537731d", \[open\]\) : catalogWord\("literal", "ee952fba0441"\)/)
})

test("the Session milestone asks before completing its own item", () => {
  const todos = readFileSync(new URL("../../session/Todos.tsx", import.meta.url), "utf8")
  assert.match(todos, /onComplete=\{\(\) => run\(`complete-\$\{item\.id\}`, \(\) => completeWorkV2\(item\)\)\}/)
  assert.match(todos, /onComplete=\{onComplete \? \(\) => setConfirming\(true\) : undefined\}/)
})

test("the Board card and the completion milestone share a confirmation dialog", () => {
  const board = readFileSync(new URL("./WorkV2.tsx", import.meta.url), "utf8")
  assert.match(board, /onConfirm=\{\(\) => \{ void run\(`complete-\$\{item\.id\}`, \(\) => completeWorkV2\(item\)\)/)
  assert.match(board, /<WorkMilestones phase=\{item\.phase\} verifyGate=\{item\.verify_gate\}[\s\S]*?onComplete=\{!item\.closed_at/)
  const milestones = readFileSync(new URL("./WorkMilestones.tsx", import.meta.url), "utf8")
  assert.match(milestones, /<button type="button" onClick=\{onComplete\}>/)
})

test("a work action chip lays its icon to the left of its words", () => {
  // `.work-icon` is a block, so in a plain chip it took a line of its own above
  // 標記完成 and 確認標記完成 instead of sitting beside them.
  const css = readFileSync(new URL("./work.css", import.meta.url), "utf8")
  assert.match(css, /\.work-actions \.chip:has\(> \.work-icon\) \{[^}]*display: inline-flex;[^}]*align-items: center;/)
})
