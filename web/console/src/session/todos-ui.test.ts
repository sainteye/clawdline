import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { addedBySession } from "./todo-author.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { pattern, said, word, wordCall } from "../catalog-testing.ts"

const source = readFileSync(new URL("./Todos.tsx", import.meta.url), "utf8")

test("owned item questions render beside the summary and verify uncertain answers", () => {
  const api = readFileSync(new URL("../pages/work/api.ts", import.meta.url), "utf8")
  assert.match(source, /decisions=\{page\.open_decisions\?\.filter/)
  assert.match(source, /<\/button>\s*\{decisionsError/)
  assert.match(source, /decision\.options\.map/)
  assert.match(source, /await answerDecision\(decision\.id, option\.id\)/)
  assert.match(source, /await readDecision\(decision\.id\)/)
})

// Not covered: a fleet refresh does not clear and reload to-dos the page already has. What stood
// here read the component's source; asserting it needs a DOM runner.

test("Session todo reads use durable conversation identity when it is available", () => {
  const api = readFileSync(new URL("../pages/work/api.ts", import.meta.url), "utf8")
  assert.match(api, /conversationID \? `conversation:\$\{conversationID\}` : terminalID/)
  assert.match(source, /readSessionWorkV2\(rowID, rowSessionID\)/)
  assert.match(source, /readSessionWorkSummaryV2\(rowID, rowSessionID\)/)
})

test("Session to-do and attention reads allow a response past the refresh interval", () => {
  const api = readFileSync(new URL("../pages/work/api.ts", import.meta.url), "utf8")
  assert.match(api, /readSessionWorkV2[\s\S]*?call<SessionWorkV2>\([^\n]*30_000\)/)
  assert.match(api, /readSessionWorkSummaryV2[\s\S]*?call<SessionWorkSummaryV2>\([^\n]*15_000\)/)
  assert.match(api, /readHumanInterventionsV2[\s\S]*?call<HumanInterventionsV2>\([^\n]*30_000\)/)
})

test("a Session with no first conversation does not report a to-do read failure", () => {
  assert.match(source, /!readReady \? <span id="session-todos-count">\{nextWord\("sessionNotStartedShort"\)\}<\/span>/)
})

// Not covered: opening an answered to-do fold refreshes it exactly once. What stood here read
// the component's source; asserting it needs a DOM runner.

test("a failed read is shown in the header, not as loading, and tapping it retries without toggling the fold", () => {
  // Loading only while there is no page and no failure.
  assert.match(source, /retrying=\{reading\} onRetry=\{\(\) => \{ void refresh\(true\) \}\}/)
  assert.match(source, /className="session-todos-failed"[\s\S]*?title=\{tip\}[\s\S]*?onClick=\{\(ev\) => \{ ev\.preventDefault\(\); ev\.stopPropagation\(\); onRetry\(\) \}\}/)
  assert.match(source, /nextWord\("todosRetryTip", \{ reason \}\)/)
  // The failure's words stay in the fold as well, and only a success clears it.
  // A machine too old for the read says that instead (machine/NeedsUpdate.tsx).
  assert.match(source, /\{readReady && readFailure && \(readFailure\.update\s*\? <NeedsUpdate update=\{readFailure\.update\} \/>\s*: <p className="work-note" role="alert">\{readFailure\.words\}<\/p>\)\}/)
  // Both reads — the full page while the fold is open, the bounded summary
  // while it is folded — clear the failure only on their own success.
  // A failed refresh keeps the last good page.
})

// Not covered: an open Session page refreshes its to-dos through one read at a time. What stood
// here read the component's source; asserting it needs a DOM runner.

test("Session todo icon controls use the shared centered vectors", () => {
  assert.match(source, /<WorkIcon name="add" \/><\/button>/)
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
  assert.match(styles, /\.session-direct-todo \.session-todo-images \{[^}]*grid-template-columns:\s*minmax\(0, 1fr\)/)
  assert.match(styles, /\.session-todo-image-link \{[^}]*display:\s*flex/)
})

test("an expanded Session todo fold has a clickable glass backdrop over the conversation", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  assert.match(source, pattern`<button className="session-todos-backdrop"[^>]*aria-label=\{${wordCall("收起 Session 待辦")}\}`)
  assert.match(source, /onClick=\{\(\) => setOpen\(false\)\}/)
  assert.match(styles, /\.session-todos::after \{[^}]*backdrop-filter:\s*blur\(/)
  assert.match(styles, /\.session-todos-backdrop \{[^}]*pointer-events:\s*none/)
  assert.match(styles, /\.session-todos\[open\] > \.session-todos-backdrop \{[^}]*pointer-events:\s*auto/)
  assert.match(styles, /\.session-todos\[open\] \.session-todos-body \{[^}]*animation:/)
  assert.match(styles, /@media \(prefers-reduced-motion:\s*reduce\)[\s\S]*?\.session-todos[\s\S]*?animation:\s*none/)
})

test("owned Board work shows explicit release milestones and recent completion", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  const milestones = readFileSync(new URL("../pages/work/WorkMilestones.tsx", import.meta.url), "utf8")
  assert.match(source, /<WorkMilestones phase=\{item\.phase\} verifyGate=\{item\.verify_gate\}\s+onComplete=\{/)
  assert.match(source, /page\.recent_items\.map/)
  assert.match(source, pattern`${wordCall("最近完成的看板項目")}`)
	assert.match(source, pattern`${word("已完成 {arg0}")}, \[when\(item\.closed_at\)\]\)`)
  assert.match(styles, /\.work-milestones li\[data-state="done"\]/)
  assert.match(styles, /var\(--ok\)/)
})

test("direct todos explain receipts, allow a read row to be sent again, and retain completion", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  assert.match(source, /filter\(\(todo\) => !todo\.completed_at\)/)
  assert.match(source, /filter\(\(todo\) => !!todo\.completed_at\)/)
  // A read row can be sent again: Send's words and state come from todoSend.
  const send = readFileSync(new URL("./todo-send.ts", import.meta.url), "utf8")
  assert.match(send, /if \(todo\.read_at\) return \{ kind: "again", label: catalogWord\("literal", "3aa8e038c09d"\) \}/)
  assert.match(source, pattern`${wordCall("已同步到 Session，尚未完成")}`)
  assert.match(source, pattern`${wordCall("最近完成的直接待辦")}`)
  assert.match(source, /session-todo-check completed/)
  assert.match(source, pattern`aria-label=\{${wordCall("恢復為未完成")}\}[\s\S]*?onClick=\{\(\) => onAction\("reopen"\)\}`)
  assert.match(styles, /\.session-direct-todo\.completed/)
  assert.match(styles, /var\(--ok\)/)
})

// Not covered: a completed to-do keeps a neutral title while its status stays green. What stood
// here read the component's source; asserting it needs a DOM runner.

test("empty Session todos use one message and hide empty section furniture", () => {
  assert.match(source, pattern`\{page && hasAssigned && <section[\s\S]*?${wordCall("這個 Session 尚未關閉的負責項目")}`)
  assert.match(source, pattern`\{page && hasRecent && <section[\s\S]*?${wordCall("最近完成的看板項目")}`)
  assert.match(source, pattern`\{page && hasDirect && <section className="session-todos-list" aria-label=\{${wordCall("直接待辦")}\}>[\s\S]*?openDirect\.map`)
  assert.doesNotMatch(source, pattern`${said("直接交給這個 Session 的待辦。")}`)
  assert.match(source, pattern`\{page && hasCompletedDirect && <section[\s\S]*?${wordCall("最近完成的直接待辦")}`)
  assert.match(source, pattern`\{empty && <p className="session-todos-empty">\{${wordCall("目前沒有待辦。")}\}<\/p>\}`)
  assert.doesNotMatch(source, pattern`${said("目前沒有負責中的項目")}`)
  assert.doesNotMatch(source, pattern`${said("目前沒有直接待辦")}`)
})

test("assigned Board items open the Board's current card with the requested user action", () => {
  const work = readFileSync(new URL("../pages/work/WorkV2.tsx", import.meta.url), "utf8")
  assert.match(source, /<SessionOwnedItem item=\{item\}/)
  assert.match(source, /onClick=\{onOpen\}/)
  assert.match(source, /openWorkItem\(item\)/)
  assert.match(work, pattern`${wordCall("需要你做的事")}`)
  assert.match(work, /item\.user_action/)
  assert.match(work, /item\.description/)
  assert.match(work, /item\.images\.map/)
})

test("an open assigned Board item can remind its Session from the detail", () => {
  const api = readFileSync(new URL("../pages/work/api.ts", import.meta.url), "utf8")
  const work = readFileSync(new URL("../pages/work/WorkV2.tsx", import.meta.url), "utf8")
  // Only an open, owned item shows the reminder.
  assert.match(work, pattern`\{!!item\.owner_session && !item\.closed_at && <button[\s\S]*?remindWorkV2\(item\)[\s\S]*?${wordCall("提醒 Session")}`)
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
  assert.match(source, pattern`className="session-owned-complete" role="img" aria-label=\{${wordCall("已完成")}\}`)
  assert.match(source, pattern`${wordCall("結案報告")}`)
  const work = readFileSync(new URL("../pages/work/WorkV2.tsx", import.meta.url), "utf8")
  assert.match(work, /<WorkCompletionReports item=\{item\} expanded=\{reportsExpanded\}/)
  assert.match(work, /reportsExpanded=\{!created\}/)
  assert.match(work, /createPortal/)
  assert.match(work, /document\.body/)
  assert.match(report, /completionReportsNewestFirst/)
  assert.match(report, pattern`\{${wordCall("寫於")}\} \{when\(document\.created_at\)\}`)
  assert.match(report, /L\.richTextHTML\(completionReportText\(document\.body\)\)/)
  assert.match(styles, /\.work-completion-report/)
  assert.match(styles, /\.work-item-detail-modal[^}]*overflow-y:\s*auto[^}]*touch-action:\s*pan-y/)
  assert.match(styles, /\.work-item-detail-panel[^}]*overflow:\s*visible/)
})

test("folded Session todos show finished, active and not-started counts", () => {
  const styles = readFileSync(new URL("../pages/work/work.css", import.meta.url), "utf8")
  assert.match(source, /<TodoProgressSummary progress=\{todoProgress\(page, row\.sessionId\)\} \/>/)
  assert.match(source, /aria-label=\{todoProgressLabel\(progress\)\}/)
  assert.match(source, pattern`\{ key: "done", icon: "check", word: ${wordCall("完成")} \}`)
  assert.match(source, pattern`\{ key: "active", icon: "half", word: ${wordCall("進行中")} \}`)
  assert.match(source, pattern`\{ key: "waiting", icon: "circle", word: ${wordCall("未開始")} \}`)
})

test("closing a Session reads and names unfinished Board items before it can continue", () => {
  const confirmation = readFileSync(new URL("../overlays/action-confirm.ts", import.meta.url), "utf8")
  const styles = readFileSync(new URL("../overlays/action-confirm.css", import.meta.url), "utf8")
  assert.match(confirmation, /readSessionWorkV2\(pending\.id\)/)
  assert.match(confirmation, /assigned_items/)
  assert.match(confirmation, /recent_items/)
  assert.match(confirmation, /direct_todos/)
  assert.match(confirmation, /endWorkOpen/)
  assert.match(confirmation, /endWorkCompletedSummary/)
  assert.match(confirmation, /endWorkNoOpen/)
  assert.match(confirmation, /endWorkReadyToClose/)
  assert.match(confirmation, /endWorkUnreadable/)
  assert.match(confirmation, /closeabilityPlainReasons/)

  // While the list is read the sheet holds a skeleton of its answer, and the
  // answer eases the height rather than making the sheet jump open.
  assert.match(confirmation, /easeHeight\(node\("action-confirm-sheet"\), \(\) => this\.renderEnd\(pending\)\)/)
  assert.match(confirmation, /prefers-reduced-motion: reduce/)
  assert.match(styles, /@media \(prefers-reduced-motion: reduce\) \{\s*\.end-work-bone \{ animation: none; \}/)
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
  assert.match(source, /conversation=\{row\.sessionId\}/)
  assert.match(source, /\{own\s*&& <span className="session-todo-author" aria-label=\{workWord\("todoAddedBySessionLabel"\)\}/)
  assert.match(words, /todoAddedBySession: "Added by Session"/)
  assert.match(words, /todoAddedBySession: "Session 建立"/)
  // Delete, Complete and Send are not gated on who wrote the row.
  assert.match(source, pattern`<button className="session-todo-delete" type="button" disabled=\{busy\} aria-label=\{${wordCall("刪除待辦")}\}[\s\S]*?onClick=\{\(\) => onAction\("delete"\)\}><WorkIcon name="delete" \/><\/button>`)
})

test("an item changed in the Board card is read again by the Session fold that opened it", () => {
  const board = readFileSync(new URL("../pages/work/WorkV2.tsx", import.meta.url), "utf8")
  assert.match(source, /watchTodoRefresh\(\(\) => \{ void one\.ask\(true\) \}, browserRefreshEnvironment\(onWorkItemChanged\), TODO_SAFETY_MS\)/,
    "the Session fold does not listen to the Board")
})

test("the Note polls only while visible, rereads on return, and both reads retry one transient failure", () => {
  const note = readFileSync(new URL("./Interventions.tsx", import.meta.url), "utf8")
  assert.doesNotMatch(note, /window\.setInterval\(/, "the Note polls a hidden page")
  // Which pace, and whether there is a lane at all, is `attentionReads`;
  // `attention-reads.test.ts` holds that.
  assert.match(note, /readWithOneRetry\(\(\) => readHumanInterventionsV2\(target\.conversation\)\)/)
  assert.match(source, /readWithOneRetry\(\(\) => readSessionWorkV2\(rowID, rowSessionID\)\)/)
  assert.match(source, /readWithOneRetry\(\(\) => readSessionWorkSummaryV2\(rowID, rowSessionID\)\)/)
})
