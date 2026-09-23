import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"

const source = readFileSync(new URL("./WorkV2.tsx", import.meta.url), "utf8")
const workSteps = readFileSync(new URL("./WorkSteps.tsx", import.meta.url), "utf8")
const styles = readFileSync(new URL("./work.css", import.meta.url), "utf8")
const sessions = readFileSync(new URL("../../Sessions.tsx", import.meta.url), "utf8")
const todos = readFileSync(new URL("../../session/Todos.tsx", import.meta.url), "utf8")

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
  assert.match(source, /\/v1\/work\/v2\/images\/\$\{image\.id\}/)
  assert.match(source, /deleteWorkV2Image/)
  assert.match(styles, /\.work-reference-images/)
})

test("unassigned executable work is visible and assignable", () => {
  assert.match(source, /const unassigned = items\.filter\(\(item\) => item\.area === "unassigned"/)
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
  assert.match(source, /readSessionWorkV2\(session\.id\)/)
  assert.match(styles, /\.work-session-detail/)
  assert.doesNotMatch(source, /<select className="work-input"[^>]*aria-label="指派既有 Session"/)
})

test("assigned Board items show their generated TODO receipts", () => {
  assert.match(workSteps, /function WorkSteps/)
  assert.match(source, /item\.steps/)
  assert.match(workSteps, /TODO · \{done\} \/ \{steps\.length\}/)
  assert.match(styles, /\.work-item-steps/)
  assert.match(todos, /<WorkSteps steps=\{item\.steps\}/)
})

test("assigned work links to its current Session instead of offering assignment again", () => {
  assert.match(source, /import \{ sessionFragment \} from "\.\.\/\.\.\/session\/address\.js"/)
  assert.match(source, /sessions\.find\(\(session\) => session\.sessionId === item\.owner_session\)/)
  assert.match(source, /className="work-session-link" href=\{sessionFragment\(owner\.id\)\}/)
  assert.match(source, /aria-label=\{`前往正在實作「\$\{item\.title\}」的 Session`\}/)
})

test("Board cards show the same lifecycle milestones as their owning Session", () => {
  const milestones = readFileSync(new URL("./WorkMilestones.tsx", import.meta.url), "utf8")
  assert.match(source, /<WorkMilestones phase=\{item\.phase\} \/>/)
  assert.match(milestones, /className="work-milestones"/)
  assert.match(milestones, /aria-label="項目進度"/)
  assert.match(styles, /\.work-milestones li\[data-state="done"\]/)
  assert.match(styles, /\.work-milestones li\[data-state="current"\]/)
})

test("the create modal accepts reference pictures before creating the item", () => {
  assert.match(source, /＋ 加入參考圖片/)
  assert.match(source, /建立項目後上傳/)
  assert.match(source, /deployment_policy: "agent_decides" }, images\)/)
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
  assert.match(command, /openNewWorkItem\(\{[\s\S]*projectID: draft\.place_id[\s\S]*description: draft\.description/)
  assert.match(event, /new CustomEvent<NewWorkItemDraft>/)
  assert.match(source, /initialDraft=\{createDraft\}/)
  assert.match(source, /語音已填入草稿；按「建立」前不會新增看板項目。/)
  assert.match(source, /onSubmit=\{\(e\) => \{[\s\S]*onCreate\(/)
})

test("reference pictures use fetch-backed object URLs so Cloud can render their bytes", () => {
  assert.match(source, /function WorkReferenceImage/)
  assert.match(source, /fetch\(`\/v1\/work\/v2\/images\/\$\{image\.id\}`/)
  assert.match(source, /URL\.createObjectURL/)
  assert.match(source, /URL\.revokeObjectURL/)
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
