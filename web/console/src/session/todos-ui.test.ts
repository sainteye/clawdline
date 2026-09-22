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
  assert.match(source, /const WORK_MILESTONES = \["實作", "驗證", "Commit \/ Merge", "部署", "完成"\]/)
  assert.match(source, /function WorkMilestones/)
  assert.match(source, /state === "done" \? "✓"/)
  assert.match(source, /page\.recent_items\.map/)
  assert.match(source, /最近完成的看板項目/)
  assert.match(styles, /\.session-work-milestones li\[data-state="done"\]/)
  assert.match(styles, /var\(--ok\)/)
})

test("closing a Session reads and names unfinished Board items before it can continue", () => {
  const confirmation = readFileSync(new URL("../overlays/action-confirm.ts", import.meta.url), "utf8")
  assert.match(confirmation, /readSessionWorkV2\(pending\.id\)/)
  assert.match(confirmation, /assigned_items/)
  assert.match(confirmation, /workState === "loading"/)
  assert.match(confirmation, /endWorkOpen/)
  assert.match(confirmation, /endWorkUnreadable/)
})
