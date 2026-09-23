import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"

const source = readFileSync(new URL("./Todos.tsx", import.meta.url), "utf8")

test("fleet refreshes do not clear and reload the same Session todos", () => {
  assert.match(source, /const rowID = row\?\.id \?\? ""/)
  assert.match(source, /const load = useCallback[\s\S]*?readSessionWorkV2\(rowID\)[\s\S]*?}, \[rowID\]\)/)
  assert.doesNotMatch(source, /}, \[row\]\)/)
  assert.doesNotMatch(source, /useEffect\(\(\) => \{ if \(open\) void load\(\) \}/)
})

test("opening an answered todo fold explicitly refreshes it once", () => {
  assert.match(source, /if \(next && page !== null\) void load\(\)/)
})

test("direct Session todos upload and render durable images", () => {
  const source = readFileSync(new URL("./Todos.tsx", import.meta.url), "utf8")
  const api = readFileSync(new URL("../pages/work/api.ts", import.meta.url), "utf8")
  assert.match(source, /prepareReferencePicture/)
  assert.match(source, /addDirectTodoV2Image/)
  assert.match(source, /todo\.images/)
  assert.match(api, /session-todos\/\$\{encodeURIComponent\(terminalID\)\}\/\$\{todoID\}\/images/)
})

test("owned Board work shows explicit release milestones and recent completion", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  const milestones = readFileSync(new URL("../pages/work/WorkMilestones.tsx", import.meta.url), "utf8")
  assert.match(source, /<WorkMilestones phase=\{item\.phase\} \/>/)
  assert.match(milestones, /state === "done" \? "✓"/)
  assert.match(source, /page\.recent_items\.map/)
  assert.match(source, /最近完成的看板項目/)
  assert.match(styles, /\.work-milestones li\[data-state="done"\]/)
  assert.match(styles, /var\(--ok\)/)
})

test("empty Session todos use one message and hide empty section furniture", () => {
  assert.match(source, /const empty = page !== null && !hasAssigned && !hasRecent && !hasDirect/)
  assert.match(source, /\{page && hasAssigned && <section[\s\S]*?這個 Session 尚未關閉的負責項目/)
  assert.match(source, /\{page && hasRecent && <section[\s\S]*?最近完成的看板項目/)
  assert.match(source, /\{page && hasDirect && <section[\s\S]*?直接交給這個 Session 的待辦/)
  assert.match(source, /\{empty && <p className="session-todos-empty">目前沒有待辦。<\/p>\}/)
  assert.doesNotMatch(source, /目前沒有負責中的項目/)
  assert.doesNotMatch(source, /目前沒有直接待辦/)
})

test("folded Session todos expose a nonzero recent completion count", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  assert.match(source, /const completedCount = page\?\.recent_items\.length \?\? 0/)
  assert.match(source, /\{!!completedCount && <span className="session-todos-completed"/)
  assert.match(source, /aria-label=\{`最近完成 \$\{completedCount\} 個項目`\}/)
  assert.match(source, /<span aria-hidden="true">✓<\/span>\{completedCount\}/)
  assert.match(styles, /\.session-todos-completed[\s\S]*?color: var\(--ok\)/)
})

test("closing a Session reads and names unfinished Board items before it can continue", () => {
  const confirmation = readFileSync(new URL("../overlays/action-confirm.ts", import.meta.url), "utf8")
  assert.match(confirmation, /readSessionWorkV2\(pending\.id\)/)
  assert.match(confirmation, /assigned_items/)
  assert.match(confirmation, /workState === "loading"/)
  assert.match(confirmation, /endWorkOpen/)
  assert.match(confirmation, /endWorkUnreadable/)
})
