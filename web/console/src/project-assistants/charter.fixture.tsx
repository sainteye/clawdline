// The Bots fixture with team charters: machine "studio" has one for its Project,
// "notebook" has none. Exposes window.charterFixture for charter.e2e.mjs.
import { bootCatalog } from "../catalog"
import { createCharterFixture } from "./charter-fixture"
import { mountFixture } from "./fixtures"

const names = ["Project assistant", "Code review", "Interface design", "Documentation", "Research", "Testing"]
const bots = (machine: string) => names.map((name, i) => ({ bot_id: `bot-${i}`, name, lead: i === 0 && machine === "studio" }))
const v1 = "## 誰負責什麼\n\n- Project assistant：分派工作\n- Code review：審查每個修改\n\n## 一件事怎麼走\n\n先開卡，再派工。\n\n## 什麼要問你\n\n部署前先問。\n\n## Bot 之間怎麼溝通\n\n用交辦卡。\n\n## 交辦卡\n\n寫清楚目標。\n"
const v2 = v1.replace("先開卡，再派工。", "先開卡，再派工，做完回報。")
const v3 = v2.replace("用交辦卡。", "用交辦卡，不要私下改別人的檔案。").replace("寫清楚目標。", "寫清楚目標和驗收方式。")

export async function mountCharterFixture() {
  await bootCatalog(tag => `/catalog/${tag}.json`)
  mountFixture()
  const controls = (window as unknown as { botsFixture: { a: object; b: object } }).botsFixture
  const studio = createCharterFixture("studio", [{ project_id: "project", bots: bots("studio"), versions: [
    { text: v1, author: "template", reason: "" }, { text: v2, author: "person", reason: "編輯「一件事怎麼走」" }, { text: v3, author: "lead_proposal", reason: "交辦卡寫清楚驗收方式" }] }])
  const notebook = createCharterFixture("notebook", [{ project_id: "project", bots: bots("notebook") }])
  Object.assign(controls.a, { canCharters: studio.canCharters, canRemoveCharter: studio.canRemoveCharter, charterRequest: studio.charterRequest })
  Object.assign(controls.b, { canCharters: notebook.canCharters, canRemoveCharter: notebook.canRemoveCharter, charterRequest: notebook.charterRequest })
  Object.assign(window, { charterFixture: { studio, notebook } })
}
