import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"

const source = readFileSync(new URL("./WorkV2.tsx", import.meta.url), "utf8")
const workSteps = readFileSync(new URL("./WorkSteps.tsx", import.meta.url), "utf8")
const styles = readFileSync(new URL("./work.css", import.meta.url), "utf8")
const tokens = readFileSync(new URL("../../legacy/tokens.css", import.meta.url), "utf8")
const sessions = readFileSync(new URL("../../Sessions.tsx", import.meta.url), "utf8")
const todos = readFileSync(new URL("../../session/Todos.tsx", import.meta.url), "utf8")
const app = readFileSync(new URL("../../App.tsx", import.meta.url), "utf8")
const boardSettings = readFileSync(new URL("../settings/BoardBlock.tsx", import.meta.url), "utf8")

function hexContrast(a: string, b: string) {
  const luminance = (hex: string) => [1, 3, 5].reduce((sum, start, index) => {
    const channel = Number.parseInt(hex.slice(start, start + 2), 16) / 255
    const linear = channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
    return sum + linear * [0.2126, 0.7152, 0.0722][index]
  }, 0)
  const [light, dark] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (light + 0.05) / (dark + 0.05)
}

test("the Project picker draws each Project mark in its trigger and menu", () => {
  assert.match(source, /function ProjectPicker/)
  assert.match(source, /places\.map[\s\S]*<Mark icon=\{place\.icon/)
  assert.doesNotMatch(source, /<option key=\{p\.id\} value=\{p\.id\}>\{p\.label\}<\/option>/)
})

test("work kinds are an explained icon list instead of a select", () => {
  for (const kind of ["Feature", "Issue", "Epic", "Refactor", "Plan"]) assert.match(source, new RegExp(`label: "${kind}"`))
  assert.match(source, /className="work-kind-list"/)
  assert.match(source, /className="work-kind-icon"/)
  assert.doesNotMatch(source, /<select[^>]*value=\{kind\}/)
})

test("the create modal closes from its close button, Escape, or the backdrop", () => {
  assert.match(source, /aria-label="關閉"/)
  assert.match(source, /event\.key === "Escape"/)
  assert.match(source, /event\.target === event\.currentTarget/)
})

test("interactive work controls announce the pointer and disabled state", () => {
  assert.match(styles, /\.work-page button[^}]*cursor: pointer/)
  assert.match(styles, /button:disabled[^}]*cursor: not-allowed/)
})

test("work cards add, show, open, and remove durable reference images", () => {
  assert.match(source, /＋ 參考圖片/)
  assert.match(source, /accept="image\/\*,\.heic,\.heif"/)
  assert.match(source, /prepareReferencePicture/)
  assert.match(source, /useReferenceImage\(image\.id\)/)
  assert.match(source, /deleteWorkV2Image/)
  assert.match(styles, /\.work-reference-images/)
})

test("unassigned executable work is visible and assignable", () => {
  assert.match(source, /const unassigned = visibleItems\.filter\(\(item\) => item\.area === "unassigned"/)
  assert.match(source, /<BoardRegion title="待指派" items=\{unassigned\}/)
  assert.match(source, /const assignable = item\.area !== "planning" && !item\.closed_at && !item\.owner_session/)
  assert.doesNotMatch(source, /item\.area === "execution"/)
})

test("assignment choices show Session activity, unfinished work, and selected details", () => {
  assert.match(source, /function SessionAssignmentPicker/)
  assert.match(source, /sessionActivityName\(session\.state\)/)
  assert.match(source, /`\$\{counts\.board\} 看板 · \$\{counts\.todos\} TODO`/)
  assert.match(source, /title="還在做"/)
  assert.match(source, /title="直接待辦"/)
  assert.match(source, /title="最近完成"/)
  assert.match(source, /`\$\{item\.project\.label\} · 完成 \$\{when\(item\.closed_at\)\}`/)
  assert.match(source, /readSessionWorkV2\(session\.id\)/)
  assert.match(styles, /\.work-session-detail/)
  assert.doesNotMatch(source, /<select className="work-input"[^>]*aria-label="指派既有 Session"/)
})

test("AI role suggestions need no separate Settings consent", () => {
  assert.doesNotMatch(boardSettings, /settings-board-ai/)
  assert.doesNotMatch(boardSettings, /set_ai_consent/)
})

test("the assignment picker shows one compact AI suggestion action", () => {
  assert.match(source, /aiSuggestionBusy \? "AI 建議中…" : "AI 建議"/)
  assert.doesNotMatch(source, /讓 AI 判斷角色/)
  assert.doesNotMatch(source, /只有按下按鈕才會把項目類型/)
  assert.doesNotMatch(styles, /\.work-persona-ai\s*\{/)
})

test("assigned Board items show their generated TODO receipts", () => {
  assert.match(workSteps, /function WorkSteps/)
  assert.match(source, /item\.steps/)
  assert.match(workSteps, /TODO · \{done\} \/ \{steps\.length\}/)
  assert.match(styles, /\.work-item-steps/)
  assert.match(todos, /<WorkSteps steps=\{item\.steps\}/)
})

test("closed Board cards show their completion time instead of another update time", () => {
  assert.match(source, /item\.closed_at \? `完成 \$\{when\(item\.closed_at\)\}` : `更新 \$\{when\(item\.updated_at\)\}`/)
})

test("long detail descriptions start folded and can be opened without folding short ones", () => {
  assert.match(source, /<WorkDescription key=\{item\.id\} description=\{item\.description\} id=\{item\.id\} \/>/)
  assert.match(source, /foldDescription=\{!created\}/)
  assert.match(source, /scrollHeight > .*lineHeight/)
  assert.match(source, /ResizeObserver/)
  assert.match(source, /\{long && <button className="work-description-toggle"/)
  assert.match(source, /aria-expanded=\{expanded\}/)
  assert.match(styles, /\.work-card-description:not\(\[data-expanded="true"\]\) \{[^}]*max-height:/)
})

test("Board lanes keep items folded until their accessible summary opens the shared detail modal", () => {
  assert.match(source, /function CompactWorkCard/)
  assert.match(source, /<CompactWorkCard key=\{item\.id\}/)
  assert.match(source, /className="work-card-summary"/)
	assert.match(source, /aria-describedby=\{describedBy\}/)
	assert.match(source, /id=\{gateDescriptionID\}/)
	assert.match(source, /id=\{gateSnapshotDescriptionID\}/)
	assert.match(source, /\[gateShown && gateSnapshotDescriptionID, gateShown && gateDescriptionID,/)
	assert.match(source, /\.filter\(Boolean\)\.join\(" "\) \|\| undefined/)
	assert.match(source, /id=\{attentionDescriptionID\}/)
	assert.match(source, /本輪：\{gateSnapshotText\(/)
  assert.match(source, /setOpenedItem\(item\)/)
  assert.match(source, /<CreatedWorkModal item=\{openedItem\} created=\{false\}/)
  assert.match(styles, /\.work-card-summary-description \{[^}]*-webkit-line-clamp:\s*2/)
})

test("the Board detail modal owns focus while open and returns it to its summary", () => {
  assert.match(source, /function useModalFocus/)
  assert.match(source, /previous\.current\?\.isConnected[\s\S]*previous\.current\.focus\(\{ preventScroll: true \}\)/)
  assert.match(source, /event\.key !== "Tab"/)
  assert.match(source, /initialFocus\.current\?\.focus/)
  assert.match(app, /\[role="dialog"\]\[aria-modal="true"\]/)
})

test("the Board can filter lifecycle state and search titles and descriptions", () => {
  assert.match(source, /aria-label="篩選項目狀態"/)
  assert.match(source, /搜尋標題與內容/)
  assert.match(source, /readWorkV2\(selectedProject \|\| undefined, status, search\)/)
  assert.match(source, /readWorkV2\(project \|\| undefined, status, search, nextCursor\)/)
  assert.match(source, /載入更多項目/)
  assert.match(styles, /\.work-filter-bar/)
  assert.match(styles, /\.work-search/)
})

test("Plan items require an explicit accessible switch before the Board shows them", () => {
  assert.match(source, /const \[showPlans, setShowPlans\] = useState\(false\)/)
  assert.match(source, /visibleWorkItems\(items, showPlans\)/)
  assert.match(source, /aria-pressed=\{showPlans\}/)
  assert.match(source, />顯示 Plan<\/button>/)
  assert.match(source, /items=\{visibleItems\.filter/)
  assert.match(source, /visibleItems\.length === 0/)
  assert.match(styles, /\.work-plan-toggle/)
})

test("Plan, Epic, and Feature cards offer an explicit conversion before implementation", () => {
  assert.match(source, /work-plan-card/)
  assert.match(source, /PLAN · 未排入執行/)
  assert.match(source, /item\.phase === "created" \|\| item\.phase === "assigned"/)
  assert.match(source, /plan \? "轉成可執行項目" : "轉成 Plan"/)
  assert.match(source, /className="work-convert-cta"/)
  assert.match(source, /<fieldset className="work-plan-kind-field"><legend>轉換後的項目類型<\/legend>/)
  assert.match(source, /type="radio" name=\{`plan-conversion-/)
  assert.match(source, /convertWorkV2\(item, plan \? conversionKind : "plan"\)/)
  assert.match(styles, /\.work-plan-card/)
  assert.match(styles, /\.work-plan-convert/)
  assert.match(styles, /\.work-convert-cta/)
})

test("the Board distinguishes initial loading, paging, empty, stale, and error states", () => {
  assert.match(source, /loading \? <BoardSkeleton \/>/)
  assert.match(source, /role="status" aria-label=\{L\.strings\.webLoading\}/)
  assert.match(source, /aria-busy=\{loading \|\| refreshing \|\| paging/)
  assert.match(source, /failure && <p className="work-note" role="alert">/)
  assert.match(source, /loaded && !failure && visibleItems\.length === 0/)
  assert.match(source, /setFailure\(failureWords\(e\)\)/)
  assert.match(styles, /\.work-board-skeleton \{[^}]*min-height:/)
  assert.match(styles, /@media \(prefers-reduced-motion: reduce\)[\s\S]*\.work-skeleton-line::after \{ animation: none; \}/)
})

test("gate status, loading, and read errors stay readable in cards and detail", () => {
  const gate = readFileSync(new URL("./WorkGate.tsx", import.meta.url), "utf8")
  assert.match(gate, /className="work-gate-line"/)
  assert.match(gate, /loading && <p role="status">正在讀取驗證證據…<\/p>/)
  assert.match(gate, /error && <p role="alert">驗證詳情讀取失敗：\{error\}/)
  assert.match(gate, /type="button" onClick=\{retry\}>重試<\/button>/)
  const current = gate.indexOf('<p className="work-gate-current">')
  const readState = gate.indexOf('{loading && <p role="status">')
  const acceptance = gate.indexOf('<div className="work-gate-acceptance">')
  assert.ok(current < readState && readState < acceptance, "read state follows the current result and precedes long evidence")
  assert.match(styles, /\.work-gate-line \{[^}]*overflow-wrap: anywhere;/)
  assert.match(styles, /\.work-gate-detail \{[^}]*overflow-wrap: anywhere;/)
  assert.match(styles, /\.work-gate-acceptance small \{[^}]*color: var\(--dim\);/)
  assert.match(styles, /\.work-gate-metrics dt \{[^}]*color: var\(--dim\);[^}]*font-size: 12px;/)
  assert.match(tokens, /--dim:\s+#9a978f;/)
  assert.match(tokens, /--bg:\s+#0e0e11;/)
  assert.match(tokens, /--card:\s+#16161a;/)
  assert.ok(hexContrast("#9a978f", "#0e0e11") >= 4.5)
  assert.ok(hexContrast("#9a978f", "#16161a") >= 4.5)
})

test("closed Board cards retain and open Agent completion reports", () => {
  const report = readFileSync(new URL("./WorkCompletionReport.tsx", import.meta.url), "utf8")
  const reportOrder = readFileSync(new URL("./completion-report-order.js", import.meta.url), "utf8")
  assert.match(source, /<WorkCompletionReports item=\{item\}/)
  assert.match(report, /結案報告/)
  assert.match(report, /completionReportsNewestFirst/)
  assert.match(reportOrder, /role === "completion_report"/)
  assert.match(report, /寫於 \{when\(document\.created_at\)\}/)
  assert.match(styles, /\.work-completion-report/)
})

test("assigned work links to its current Session instead of offering assignment again", () => {
  assert.match(source, /import \{ sessionFragment \} from "\.\.\/\.\.\/session\/address\.js"/)
  assert.match(source, /sessions\.find\(\(session\) => session\.sessionId === item\.owner_session\)/)
  assert.match(source, /className="work-session-link" href=\{sessionFragment\(owner\.id\)\}/)
  assert.match(source, /aria-label=\{`前往正在實作「\$\{item\.title\}」的 Session`\}/)
})

test("assigned work can remind its current Session from the Board card", () => {
  assert.match(source, /remindWorkV2\(item\)/)
  assert.match(source, /提醒 Session/)
  assert.match(source, /reminded \? "已提醒"/)
  assert.match(source, /!!item\.owner_session && !item\.closed_at/)
})

test("started Board cards show the same lifecycle milestones as their owning Session", () => {
  const milestones = readFileSync(new URL("./WorkMilestones.tsx", import.meta.url), "utf8")
  assert.match(source, /<WorkMilestones phase=\{item\.phase\} \/>/)
  assert.match(milestones, /if \(!workMilestonesShown\(phase\)\) return null/)
  assert.match(milestones, /className="work-milestones"/)
  assert.match(milestones, /aria-label="項目進度"/)
  assert.match(styles, /\.work-milestones li\[data-state="done"\]/)
  assert.match(styles, /\.work-milestones li\[data-state="current"\]/)
})

test("the create modal accepts reference pictures before creating the item", () => {
  const pictures = readFileSync(new URL("./ReferencePictures.tsx", import.meta.url), "utf8")
  assert.match(pictures, /＋ 加入參考圖片/)
  assert.match(source, /<PendingPictures images=\{images\} busy=\{busy\} note="建立項目後上傳"/)
  assert.match(source, /deployment_policy: "agent_decides" as const/)
  assert.match(source, /onCreate\(body, images, decision\.key\)/)
  assert.match(source, /addWorkV2Image\(answer\.item\.id/)
})

test("Board cards name the action an Agent needs from the person", () => {
  assert.match(source, /item\.user_action/)
  assert.match(source, /需要你做的事/)
  assert.match(styles, /\.work-user-action/)
})

test("the Session list shortcut creates an item and keeps its assignment card in a modal", () => {
  assert.match(sessions, /id="work-create-go"/)
  assert.match(sessions, /aria-label="新增看板項目"/)
  assert.match(sessions, /onClick=\{\(\) => openNewWorkItem\(\)\}/)
  assert.match(source, /onOpenNewWorkItem/)
  assert.match(source, /setCreatedItem\(created\)/)
  assert.match(source, /<CreatedWorkModal item=\{createdItem\}/)
  assert.match(source, /<WorkCard item=\{item\} sessions=\{sessions\}/)
  assert.match(styles, /\.work-created-panel/)
  assert.match(styles, /\.work-created-modal[^}]*overflow:\s*auto/)
  assert.match(styles, /\.work-created-panel[^}]*overflow:\s*visible/)
})

test("the create actions have breathing room above them", () => {
  assert.match(styles, /\.work-new-modal \.work-actions[^}]*margin-top:/)
})

test("voice Board drafts are prefilled and still require the create confirmation", () => {
  const command = readFileSync(new URL("../../session/Command.tsx", import.meta.url), "utf8")
  const event = readFileSync(new URL("./new-item.ts", import.meta.url), "utf8")
  assert.match(command, /draft\.kind === "work"/)
  assert.match(command, /places\?\.find\(\(place\) => place\.id === draft\.place_id\)/)
  assert.match(command, /openNewWorkItem\(\{[\s\S]*projectID: draft\.place_id[\s\S]*project,[\s\S]*description: draft\.description/)
  assert.match(event, /new CustomEvent<NewWorkItemDraft>/)
  assert.match(source, /initialDraft=\{createDraft\}/)
  assert.match(source, /\[initialDraft\.project, \.\.\.places\]/)
  assert.match(source, /語音已填入草稿；按「建立」前不會新增看板項目。/)
  assert.match(source, /onSubmit=\{\(e\) => \{[\s\S]*onCreate\(/)
})

test("reference pictures use fetch-backed object URLs so Cloud can render their bytes", () => {
  const hook = readFileSync(new URL("./useReferenceImage.ts", import.meta.url), "utf8")
  assert.match(source, /function WorkReferenceImage/)
  // Board cards and to-do rows both read through the one hook, so both share
  // the page's Cloud limit (`reference-images.ts`).
  assert.match(source, /useReferenceImage\(image\.id\)/)
  assert.match(todos, /useReferenceImage\(image\.id\)/)
  assert.doesNotMatch(source, /fetch\(`\/v1\/work\/v2\/images\//)
  assert.doesNotMatch(todos, /fetch\(`\/v1\/work\/v2\/images\//)
  // The card draws the small copy; the red pen is given the original.
  assert.match(hook, /referenceImages\.load\(id, "thumb"/)
  assert.match(hook, /referenceImages\.load\(id, "full"/)
  assert.match(source, /<PictureMarkup picture=\{\{ id: image\.id, url: full \}\}/)
  assert.match(hook, /URL\.createObjectURL/)
  assert.match(hook, /URL\.revokeObjectURL/)
})

test("every Board card exposes edit and guarded delete flows", () => {
  assert.match(source, /function EditWorkModal/)
  assert.match(source, /編輯看板項目/)
  assert.match(source, /editWorkV2\(item, title, description\)/)
  assert.match(source, /function DeleteWorkModal/)
  assert.match(source, /刪除看板項目？/)
  assert.match(source, /deleteWorkV2\(item\)/)
  assert.match(source, /執行紀錄仍會保留/)
})

test("edit and delete dialogs close from Escape, close control, or backdrop", () => {
  assert.match(source, /function useModalDismiss/)
  assert.match(source, /event\.key === "Escape"/)
  assert.match(source, /event\.target === event\.currentTarget/)
  assert.match(source, /className="work-modal-close"/)
})

test("work controls use reusable vector icons instead of font glyph positioning", () => {
  const icons = readFileSync(new URL("./WorkIcon.tsx", import.meta.url), "utf8")
  const steps = readFileSync(new URL("./WorkSteps.tsx", import.meta.url), "utf8")
  assert.match(icons, /<svg className="work-icon"/)
  assert.match(icons, /name === "add"/)
  assert.match(icons, /name === "close"/)
  assert.match(icons, /name === "edit"/)
  assert.match(icons, /name === "delete"/)
  assert.match(icons, /name === "remind"/)
  assert.match(source, /<WorkIcon name="edit" \/> 編輯/)
  assert.match(source, /<WorkIcon name="delete" \/> 刪除/)
  assert.match(source, /className="work-modal-close"[^>]*><WorkIcon name="close" \/><\/button>/)
  assert.match(source, /aria-label=\{`移除參考圖片[\s\S]*?<WorkIcon name="close" \/>/)
  assert.match(source, /className="work-kind-radio"><WorkIcon name=\{kind === value \? "radio" : "circle"\} \/>/)
  assert.match(steps, /<WorkIcon name=\{step\.done \? "check" : "circle"\} \/>/)
  assert.doesNotMatch(source, /className="work-modal-close"[^>]*>×<\/button>/)
  assert.match(styles, /\.work-icon \{[^}]*display:\s*block[^}]*width:\s*1em[^}]*height:\s*1em/)
  assert.match(styles, /\.work-modal-image-list li button \{[^}]*display:\s*grid[^}]*place-items:\s*center/)
})

test("a recording microphone is an accent ring, never a filled disc that hides its own icon", () => {
  const pressed = /\.work-voice-mic\[aria-pressed="true"\] \{([^}]*)\}/.exec(styles)?.[1] ?? ""
  assert.match(pressed, /color:\s*var\(--accent\)/)
  assert.doesNotMatch(pressed, /background:\s*var\(--accent\)/)
  // A phone keeps :hover after a tap; unguarded, it painted the icon the fill's colour.
  assert.match(styles, /@media \(hover: hover\) \{\s*\.work-voice-mic:not\(:disabled\):hover/)
})

test("a reference picture's remove control is the only button drawn as the round corner badge", () => {
  // The picture itself became a red-pen button; a rule on every direct child
  // button made it absolute, 24 px tall and pill-shaped, squashing the thumbnail.
  assert.doesNotMatch(styles, /\.work-reference-image\s*>\s*button\b/)
  assert.match(styles, /\.work-reference-image > \.work-reference-remove \{[^}]*position: absolute/)
  assert.match(source, /<button className="work-reference-remove" type="button" aria-label=\{`移除參考圖片/)
})

test("a dictating text box is never inside a <label>, which would hand its Done to Cancel", () => {
  // Done redraws the row, so the button it was pressed on has left the page by
  // the time the click bubbles; a <label> then reads the click as its own and
  // forwards it to its first control — the redrawn row's Cancel. The recording
  // was dropped and nothing was sent (WebKit and Chromium, 2026-09-25).
  for (const [name, text] of [["WorkV2.tsx", source], ["Todos.tsx", todos]] as const) {
    assert.doesNotMatch(text, /<label>[^<]*<VoiceTextarea/, name)
  }
  const voice = readFileSync(new URL("./VoiceTextarea.tsx", import.meta.url), "utf8")
  assert.doesNotMatch(voice.replace(/\/\/.*|\/\*[\s\S]*?\*\//g, ""), /<label\b/)
  assert.match(voice, /aria-labelledby=\{label \? heading : undefined\}/)
  assert.match(source, /<VoiceTextarea label="描述" value=\{description\} maxLength=\{65536\}/)
  assert.match(source, /<VoiceTextarea label="描述" value=\{description\} onValue=\{setDescription\} \/>/)
})

test("an assigned card can be moved to another or a new Session", () => {
  assert.match(source, /const reassignable = item\.area !== "planning" && !item\.closed_at && !!item\.owner_session/)
  assert.match(source, /<WorkIcon name="reassign" \/> 改派/)
  assert.match(source, /\(assignable \|\| \(reassignable && reassigning\)\) && <div className="work-assignment">/)
  assert.match(source, /assignmentCandidates\(sessions, item\)/)
})

test("opening Epic reassignment brings the picker into view", () => {
  // An Epic can be much taller than the viewport: its gate and children sit
  // between the toolbar button and the newly mounted assignment picker.
  assert.match(source, /autoFocus=\{focusAssignment \|\| \(epic && reassigning\)\}/)
})

test("an Epic keeps its frame and label without spanning both desktop columns", () => {
  assert.match(source, /const cardClass = epic \? "work-card work-v2-card work-epic-card"/)
  assert.match(source, /EPIC · 大型項目/)
  assert.match(styles, /\.work-cards \{[^}]*grid-template-columns: repeat\(2, minmax\(0, 1fr\)\)/)
  assert.doesNotMatch(styles, /\.work-cards > \.work-epic-card \{[^}]*grid-column:/)
  assert.match(styles, /--work-epic: var\(--peer\)/)
})

test("an Epic shows its plan gate, its plan documents, and what assigning it means", () => {
  const report = readFileSync(new URL("./WorkCompletionReport.tsx", import.meta.url), "utf8")
  assert.match(source, /\{epicGateShown\(item\) && <EpicGateChecklist item=\{item\} \/>\}/)
  assert.match(source, />計劃書<\/li>/)
  assert.match(source, />Child Review<\/li>/)
  assert.match(source, /<WorkEpicPlanDocuments item=\{item\} \/>/)
  assert.match(source, /\{epic && <p className="work-epic-assign-note">/)
  assert.match(report, /epicPlanDocuments\(item\.documents\)/)
  assert.match(source, /epic: \{ icon: "◆", label: "Epic", description: "可指派的大型工作/)
  assert.doesNotMatch(source, /先放在規劃區的大型工作主題/)
})

test("an Epic lists its children with progress, and a child links back to its Epic", () => {
  assert.match(source, /\{epic && <EpicChildren item=\{item\} sessions=\{sessions\} \/>\}/)
  assert.match(source, /<EpicParentLine item=\{item\} \/>/)
  assert.match(source, /aria-label="子項目"/)
  assert.match(source, /屬於 Epic：/)
  assert.match(source, /shortWorkID\(parent\.id\)/)
  // Closed children are left out of the 進行中 list, so the family is read with every status.
  assert.match(source, /readWorkV2\(project \|\| undefined, "all"\)/)
  assert.match(source, /#work \[data-work-id=/)
  assert.match(styles, /\.work-epic-children \{/)
  assert.match(styles, /\.work-epic-parent \{[^}]*var\(--work-epic\)/)
})

test("the Board puts each open question inside the item that explains it", () => {
  assert.match(source, /readDecisions\(\)/)
  assert.match(source, /decisionsForWorkItem\(decisions, item\.id\)/)
  assert.match(source, /<WorkItemDecisions decisions=\{decisions\}/)
  assert.match(source, /answerDecision\(decision\.id, option\.id\)/)
  assert.doesNotMatch(source, /未連結的舊問題/)
  assert.match(styles, /\.work-item-decisions/)
})

test("Agent proposals are compact review rows with labelled detail on demand", () => {
  assert.match(source, /<ProposalQueue proposals=\{visibleProposals\}/)
  assert.match(source, /為什麼要做/)
  assert.match(source, /Explain/)
  assert.match(source, /詳細說明/)
  assert.match(source, /<dt>會改什麼<\/dt>/)
  assert.match(source, /<dt>完成後會看到什麼<\/dt>/)
  assert.match(styles, /\.work-proposal \{[\s\S]*grid-template-columns/)
  assert.doesNotMatch(source, /work-fold-body work-cards[^\n]*proposals\.map/)
})

test("a Session opens its Board item as the Board's own card, with the same controls", () => {
  assert.match(todos, /openWorkItem\(item\)/)
  assert.doesNotMatch(todos, /function WorkItemDetailModal/)
  assert.match(source, /onOpenWorkItem\(/)
  assert.match(source, /\{openedItem && <CreatedWorkModal item=\{openedItem\} created=\{false\}/)
  assert.match(source, /<WorkCard item=\{item\}[^>]*reportsExpanded=\{!created\}/)
})
