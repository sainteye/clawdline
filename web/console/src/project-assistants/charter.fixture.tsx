// The Bots fixture with team charters: machine "studio" has one for its Project,
// "notebook" has none. Exposes window.charterFixture for charter.e2e.mjs.
import { bootCatalog } from "../catalog"
import { createCharterFixture, sampleWorkflow, sampleWorkflowProposal } from "./charter-fixture"
import { mountFixture } from "./fixtures"

const names = ["Project assistant", "Code review", "Interface design", "Documentation", "Research", "Testing"]
const bots = (machine: string) => names.map((name, i) => ({ bot_id: `bot-${i}`, name, lead: i === 0 && machine === "studio" }))
// "?ask" holds the live free text under 什麼要問你, which converts into the two-column ask policy.
const askText = "不用問，直接做：已審、檢查全綠的修正，整合進 main、推 GitHub、裝 dev；Lens 驗收通過後部署 app.clawdline.com。\n一定要問：公開 Release、改 clawdline.com、刪除資料、花錢、產品方向取捨、檢查沒過要不要照上。\n例外：一次性、總費用確定低於 NTD 5 的外部 API 呼叫不用先問；先設好費用或次數上限，不自動重試，做完回報實際次數與費用。\n要問時用 ask_person 在自己的聊天跳決策卡，同一件事只問一次；不要請使用者轉貼文字，也不要請別的 Bot 替你問。"
const ask = new URLSearchParams(location.search).has("ask")
const v1 = "## 誰負責什麼\n\n- Project assistant：分派工作\n- Code review：審查每個修改\n\n## 一件事怎麼走\n\n先開卡，再派工。\n\n## 什麼要問你\n\n部署前先問。\n\n## Bot 之間怎麼溝通\n\n用交辦卡。\n\n## 交辦卡\n\n寫清楚目標。\n"
const v2 = v1.replace("先開卡，再派工。", "先開卡，再派工，做完回報。")
const v3plain = v2.replace("用交辦卡。", "用交辦卡，不要私下改別人的檔案。").replace("寫清楚目標。", "寫清楚目標和驗收方式。")
const v3 = ask ? v3plain.replace("部署前先問。", askText) : v3plain

// 品質驗收 goes to Code review and Interface design, 發版步驟 to Documentation, 文件寫法 to nobody.
const seededPlaybooks = [
  { title: "品質驗收", text: "每次交付前，先跑完和修改相關的測試。\n\n- 列出驗證過的項目\n- 畫面修改附上截圖", bot_ids: ["bot-1", "bot-2"] },
  { title: "發版步驟", text: "1. 標記 commit。\n2. 寫版本說明。\n3. 部署前先問。", bot_ids: ["bot-3"] },
  { title: "文件寫法", text: "**先寫結論**，再寫原因。", bot_ids: [] },
]

export async function mountCharterFixture() {
  await bootCatalog(tag => `/catalog/${tag}.json`)
  mountFixture()
  const controls = (window as unknown as { botsFixture: { a: object; b: object } }).botsFixture
  // "?playbooks" is a machine at API 127 whose charter holds three playbooks; without it the machine is older.
  const playbooks = new URLSearchParams(location.search).has("playbooks")
  // "?workflow" is a machine at API 128 whose charter has the team workflow and the lead's pending workflow proposal.
  const flow = new URLSearchParams(location.search).has("workflow")
  const team = sampleWorkflow({ atlas: "bot-0", xavia: "bot-1", fuse: "bot-2", lens: "bot-3", beacon: "bot-4" })
  const studio = createCharterFixture("studio", [{ project_id: "project", bots: bots("studio"), versions: [
    { text: v1, author: "template", reason: "" }, { text: v2, author: "person", reason: "編輯「一件事怎麼走」" }, { text: v3, author: "lead_proposal", reason: "交辦卡寫清楚驗收方式" }],
    ...(playbooks ? { playbooks: seededPlaybooks } : {}), ...(flow ? { workflow: team, workflow_proposal: sampleWorkflowProposal(team) } : {}) }])
  const notebook = createCharterFixture("notebook", [{ project_id: "project", bots: bots("notebook") }])
  Object.assign(controls.a, { canCharters: studio.canCharters, canRemoveCharter: studio.canRemoveCharter, canCharterConflicts: studio.canCharterConflicts, canCharterPlaybooks: () => playbooks, canCharterWorkflow: () => flow, charterRequest: studio.charterRequest })
  Object.assign(controls.b, { canCharters: notebook.canCharters, canRemoveCharter: notebook.canRemoveCharter, canCharterConflicts: notebook.canCharterConflicts, charterRequest: notebook.charterRequest })
  Object.assign(window, { charterFixture: { studio, notebook } })
}
