import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { addedBySession } from "./todo-author.ts"

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

test("Session todo icon controls use the shared centered vectors", () => {
  assert.match(source, /<WorkIcon name="add" \/><\/button>/)
  assert.match(source, /className="work-modal-close"[^>]*><WorkIcon name="close" \/><\/button>/)
  assert.match(source, /className="session-owned-complete"[\s\S]*?<WorkIcon name="check" \/>/)
  assert.match(source, /className="session-owned-open"[\s\S]*?<WorkIcon name="open" \/>/)
  assert.doesNotMatch(source, /className="session-todos-add"[^>]*>\+<\/button>/)
})

test("direct Session todos upload and render durable images", () => {
  const source = readFileSync(new URL("./Todos.tsx", import.meta.url), "utf8")
  const api = readFileSync(new URL("../pages/work/api.ts", import.meta.url), "utf8")
  assert.match(source, /prepareReferencePicture/)
  assert.match(source, /addDirectTodoV2Image/)
  assert.match(source, /todo\.images/)
  assert.match(api, /session-todos\/\$\{encodeURIComponent\(terminalID\)\}\/\$\{todoID\}\/images/)
})

test("direct todo attachments are compact file links instead of previews", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  assert.match(source, /<ReferenceImage key=\{image\.id\} image=\{image\} compact \/>/)
  assert.match(source, /if \(compact\) return source \? <a className="session-todo-image-link"/)
  assert.match(styles, /\.session-direct-todo \.session-todo-images \{[^}]*grid-template-columns:\s*minmax\(0, 1fr\)/)
  assert.match(styles, /\.session-todo-image-link \{[^}]*display:\s*flex/)
  assert.match(styles, /\.session-todo-image-link \{[^}]*min-height:\s*42px/)
})

test("an expanded Session todo fold has a clickable glass backdrop over the conversation", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  assert.match(source, /<button className="session-todos-backdrop"[^>]*aria-label="收起 Session 待辦"/)
  assert.match(source, /onClick=\{\(\) => setOpen\(false\)\}/)
  assert.match(styles, /\.session-todos::after \{[^}]*backdrop-filter:\s*blur\(/)
  assert.match(styles, /\.session-todos-backdrop \{[^}]*pointer-events:\s*none/)
  assert.match(styles, /\.session-todos\[open\] \{[^}]*box-shadow:/)
  assert.match(styles, /\.session-todos\[open\]::after \{[^}]*opacity:\s*1/)
  assert.match(styles, /\.session-todos\[open\] > \.session-todos-backdrop \{[^}]*pointer-events:\s*auto/)
  assert.match(styles, /\.session-todos\[open\] \.session-todos-body \{[^}]*animation:/)
  assert.match(styles, /@media \(prefers-reduced-motion:\s*reduce\)[\s\S]*?\.session-todos[\s\S]*?animation:\s*none/)
})

test("owned Board work shows explicit release milestones and recent completion", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  const milestones = readFileSync(new URL("../pages/work/WorkMilestones.tsx", import.meta.url), "utf8")
  assert.match(source, /<WorkMilestones phase=\{item\.phase\} \/>/)
  assert.match(milestones, /state === "done" \? "check"/)
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

test("an open assigned Board item can remind its Session from the detail", () => {
  const api = readFileSync(new URL("../pages/work/api.ts", import.meta.url), "utf8")
  assert.match(source, /remindWorkV2/)
  assert.match(source, /再次提醒 Session/)
  assert.match(source, /已再次提醒這個 Session/)
  assert.match(source, /!!item\.owner_session && !item\.closed_at/)
  assert.match(api, /items\/\$\{item\.id\}\/remind/)
})

test("a recent Board item opens its durable completion report in one click", () => {
  const report = readFileSync(new URL("../pages/work/WorkCompletionReport.tsx", import.meta.url), "utf8")
  const order = readFileSync(new URL("../pages/work/completion-report-order.js", import.meta.url), "utf8")
  const api = readFileSync(new URL("../pages/work/api.ts", import.meta.url), "utf8")
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  assert.match(api, /documents\?: WorkV2Document\[\]/)
  assert.match(api, /interface WorkV2Document[\s\S]*created_at:\s*number/)
  assert.match(source, /completionReports\(item\)\.length/)
  assert.match(source, /session-owned-complete/)
  assert.match(source, /aria-label="已完成"/)
  assert.match(source, /結案報告/)
  assert.match(source, /<WorkCompletionReports item=\{item\} expanded/)
  assert.match(source, /createPortal/)
  assert.match(source, /document\.body/)
  assert.match(order, /role === "completion_report"/)
  assert.match(report, /completionReportsNewestFirst/)
  assert.match(report, /寫於 \{when\(document\.created_at\)\}/)
  assert.match(report, /L\.richTextHTML\(completionReportText\(document\.body\)\)/)
  assert.match(styles, /\.work-completion-report/)
  assert.match(styles, /\.work-completion-report-body\s*\{[^}]*color:\s*var\(--ink\)[^}]*font-size:\s*15px/)
  assert.match(styles, /\.work-completion-report-body\s+:is\(h2, h3, h4\)\s*\{[^}]*color:\s*var\(--ink\)[^}]*font-size:\s*16px/)
  assert.match(styles, /\.work-item-detail-modal[^}]*overflow-y:\s*auto[^}]*touch-action:\s*pan-y/)
  assert.match(styles, /\.work-item-detail-panel[^}]*overflow:\s*visible/)
})

test("folded Session todos show finished, active and not-started counts", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  assert.match(source, /<TodoProgressSummary progress=\{todoProgress\(page, row\.sessionId\)\} \/>/)
  assert.match(source, /aria-label=\{todoProgressLabel\(progress\)\}/)
  assert.match(source, /\{ key: "done", icon: "check", word: "完成" \}/)
  assert.match(source, /\{ key: "active", icon: "half", word: "進行中" \}/)
  assert.match(source, /\{ key: "waiting", icon: "circle", word: "未開始" \}/)
  assert.doesNotMatch(source, /session-todos-completed/)
  assert.match(styles, /\.session-todos-state\[data-state="done"\] \{ color: var\(--ok\); \}/)
  assert.match(styles, /\.session-todos-bar > \[data-state="active"\] \{ background: var\(--warn\); \}/)
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
  assert.match(confirmation, /if \(recentWork\.length \|\| completedDirect\.length\) \{\s*(?:\/\/[^\n]*\s*)*const completed = document\.createElement\("section"\)/,
    "the completed summary is drawn only when something was completed")
  assert.match(confirmation, /endWorkNoOpen/)
  assert.match(confirmation, /endWorkReadyToClose/)
  assert.match(confirmation, /endWorkUnreadable/)
  assert.match(confirmation, /closeabilityPlainReasons/)
  assert.match(styles, /\.end-work-completed-mark[\s\S]*?color:\s*var\(--ok\)/)
  assert.match(styles, /\.end-work-status\.is-ready[\s\S]*?color:\s*var\(--ink\)/)
  assert.match(styles, /\.end-work-ready-mark[\s\S]*?color:\s*var\(--ok\)/)
})

test("a row the Session added itself is labelled as such, and a person's row is not", () => {
  const conversation = "10000000-0000-4000-8000-000000000004"
  assert.equal(addedBySession({ created_by: conversation }, conversation), true)
  assert.equal(addedBySession({ created_by: "device:phone" }, conversation), false)
  assert.equal(addedBySession({ created_by: "local" }, conversation), false)
  assert.equal(addedBySession({}, conversation), false)
  // A Session whose conversation is not known yet labels nothing.
  assert.equal(addedBySession({ created_by: conversation }, undefined), false)
  assert.equal(addedBySession({ created_by: "" }, ""), false)
})

test("the Session-added label replaces the sent/read receipt, and the controls stay", () => {
  const words = readFileSync(new URL("../pages/work/words.ts", import.meta.url), "utf8")
  assert.match(source, /const own = addedBySession\(todo, conversation\)/)
  assert.match(source, /conversation=\{row\.sessionId\}/)
  assert.match(source, /\{own\s*&& <span className="session-todo-author" aria-label=\{workWord\("todoAddedBySessionLabel"\)\}/)
  assert.match(source, /\{\(completed \|\| !own\) && <span\s*className="session-todo-receipt"/)
  assert.match(words, /todoAddedBySession: "Added by Session"/)
  assert.match(words, /todoAddedBySession: "Session 建立"/)
  // Delete, Complete and Send are not gated on who wrote the row.
  assert.match(source, /<button className="session-todo-delete" type="button" disabled=\{busy\} aria-label="刪除待辦"[\s\S]*?onClick=\{\(\) => onAction\("delete"\)\}><WorkIcon name="delete" \/><\/button>/)
  assert.match(source, /className="chip on session-todo-send"[\s\S]*?<WorkIcon name="send" \/>\{send\.label\}/)
  assert.doesNotMatch(source, /own && <button|!own && <button/)
})
