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
	assert.match(source, /`已完成 \$\{when\(item\.closed_at\)\}`/)
  assert.match(styles, /\.work-milestones li\[data-state="done"\]/)
  assert.match(styles, /var\(--ok\)/)
})

test("direct todos explain receipts, allow a read row to be sent again, and retain completion", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  assert.match(source, /filter\(\(todo\) => !todo\.completed_at\)/)
  assert.match(source, /filter\(\(todo\) => !!todo\.completed_at\)/)
  assert.match(source, /再次 Send/)
  assert.match(source, /已同步到 Session，尚未完成/)
  assert.match(source, /最近完成的直接待辦/)
  assert.match(source, /session-todo-check completed/)
  assert.match(styles, /\.session-direct-todo\.completed/)
  assert.match(styles, /var\(--ok\)/)
})

test("completed todo titles stay neutral while completion status stays green", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  assert.match(styles, /\.session-owned-item\.completed b,\s*\.session-direct-todo\.completed b \{ color: var\(--ink\); \}/)
  assert.match(styles, /\.session-todo-check\.completed,\s*\.session-direct-todo\.completed \.session-todo-receipt \{ color: var\(--ok\); \}/)
})

test("empty Session todos use one message and hide empty section furniture", () => {
  assert.match(source, /const empty = page !== null && !hasAssigned && !hasRecent && !hasDirect && !hasCompletedDirect/)
  assert.match(source, /\{page && hasAssigned && <section[\s\S]*?這個 Session 尚未關閉的負責項目/)
  assert.match(source, /\{page && hasRecent && <section[\s\S]*?最近完成的看板項目/)
  assert.match(source, /\{page && hasDirect && <section[\s\S]*?直接交給這個 Session 的待辦/)
  assert.match(source, /\{page && hasCompletedDirect && <section[\s\S]*?最近完成的直接待辦/)
  assert.match(source, /\{empty && <p className="session-todos-empty">目前沒有待辦。<\/p>\}/)
  assert.doesNotMatch(source, /目前沒有負責中的項目/)
  assert.doesNotMatch(source, /目前沒有直接待辦/)
})

test("assigned Board items open a current detail modal with the requested user action", () => {
  assert.match(source, /<SessionOwnedItem item=\{item\}/)
  assert.match(source, /onClick=\{onOpen\}/)
  assert.match(source, /readWorkV2Item\(item\.id\)/)
  assert.match(source, /<WorkItemDetailModal item=\{detail\}/)
  assert.match(source, /需要你做的事/)
  assert.match(source, /item\.user_action/)
  assert.match(source, /item\.description/)
  assert.match(source, /item\.images\.map/)
})

test("a recent Board item opens its durable completion report in one click", () => {
  const report = readFileSync(new URL("../pages/work/WorkCompletionReport.tsx", import.meta.url), "utf8")
  const api = readFileSync(new URL("../pages/work/api.ts", import.meta.url), "utf8")
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  assert.match(api, /documents\?: WorkV2Document\[\]/)
  assert.match(source, /completionReports\(item\)\.length/)
  assert.match(source, /session-owned-complete/)
  assert.match(source, /aria-label="已完成"/)
  assert.match(source, /結案報告/)
  assert.match(source, /<WorkCompletionReports item=\{item\} expanded/)
  assert.match(source, /createPortal/)
  assert.match(source, /document\.body/)
  assert.match(report, /role === "completion_report"/)
  assert.match(report, /L\.richTextHTML\(document\.body\)/)
  assert.match(styles, /\.work-completion-report/)
  assert.match(styles, /\.work-item-detail-modal[^}]*overflow-y:\s*auto[^}]*touch-action:\s*pan-y/)
  assert.match(styles, /\.work-item-detail-panel[^}]*overflow:\s*visible/)
})

test("folded Session todos expose a nonzero recent completion count", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  assert.match(source, /const completedCount = \(page\?\.recent_items\.length \?\? 0\) \+ completedDirect\.length/)
  assert.match(source, /\{!!completedCount && <span className="session-todos-completed"/)
  assert.match(source, /aria-label=\{`最近完成 \$\{completedCount\} 個項目`\}/)
  assert.match(source, /<span aria-hidden="true">✓<\/span>\{completedCount\}/)
  assert.match(styles, /\.session-todos-completed[\s\S]*?color: var\(--ok\)/)
})

test("closing a Session reads and names unfinished Board items before it can continue", () => {
  const confirmation = readFileSync(new URL("../overlays/action-confirm.ts", import.meta.url), "utf8")
  const styles = readFileSync(new URL("../overlays/action-confirm.css", import.meta.url), "utf8")
  assert.match(confirmation, /readSessionWorkV2\(pending\.id\)/)
  assert.match(confirmation, /assigned_items/)
  assert.match(confirmation, /recent_items/)
  assert.match(confirmation, /direct_todos/)
  assert.match(confirmation, /workState === "loading"/)
  assert.match(confirmation, /endWorkOpen/)
  assert.match(confirmation, /endWorkCompletedSummary/)
  assert.match(confirmation, /endWorkNoOpen/)
  assert.match(confirmation, /endWorkReadyToClose/)
  assert.match(confirmation, /endWorkUnreadable/)
  assert.match(confirmation, /closeabilityPlainReasons/)
  assert.match(styles, /\.end-work-completed-mark[\s\S]*?color:\s*var\(--ok\)/)
})
