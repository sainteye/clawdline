import type { ProjectSetup, ProjectUnifyStatus } from "@clawdline/contract"
import type { ProjectPlace, ProjectSetupEvidence } from "../work/api.js"

export type SetupTone = "ready" | "missing" | "attention" | "not-applicable" | "unknown"
/** `action: "unify"` is a row whose button opens that Project's unify preview. */
export interface SetupCapability {
  key: string; label: string; detail: string; tone: SetupTone; complete: boolean; applicable: boolean; action?: "unify"
}

/** The places answer's setup with the unify fields the machine adds (api/v1 ProjectSetup). */
type SetupWithUnify = ProjectSetupEvidence & Pick<ProjectSetup, "unify" | "unify_count">

const unifyTone: Record<ProjectUnifyStatus, SetupTone> = { unified: "ready", drifting: "attention", unknown: "unknown" }

/**
 * Whether this Project's Claude and Codex sessions read the same rules and
 * skills, from the machine's unify plan. A daemon older than the field sends
 * none: that is 未知 and left out of the score, never 已共用. A count the
 * machine did not send is not shown as 0.
 */
function unifyCapability(setup: SetupWithUnify): SetupCapability {
  const status = setup.unify
  const detail = status === "unified" ? "已共用"
    : status === "drifting" ? (typeof setup.unify_count === "number" && setup.unify_count > 0 ? `有落差（${setup.unify_count} 項）` : "有落差")
      : status === "unknown" ? "未知：有規則檔或 skill 讀不到" : "未知：這台機器的 Clawdline 還不會回報"
  return {
    key: "unify", label: "Claude／Codex 共用", detail, tone: status ? unifyTone[status] : "unknown",
    complete: status === "unified", applicable: status !== undefined, action: status ? "unify" : undefined,
  }
}

const iconWords: Record<ProjectSetupEvidence["icon"], string> = {
  mirrored: "已從來源機器同步",
  override: "已自訂",
  registry: "已登記",
  generated: "目前使用自動產生圖示",
}

const activityWords: Record<ProjectSetupEvidence["deploy_activity"], string> = {
  idle: "目前沒有進度",
  running: "正在進行",
  succeeded: "最近成功",
  failed: "最近失敗",
  unknown: "尚無目前狀態",
}

/** The visible facts and their human consequence, kept out of JSX so
 * loading an older daemon can be tested as an explicit unknown state. */
export function projectSetupCapabilities(place: ProjectPlace): SetupCapability[] {
  const setup = place.setup
  if (!setup) return [
    { key: "unknown", label: "配置健檢", detail: "這台機器尚未提供健檢資料；更新 Clawdline 後再讀取。", tone: "unknown", complete: false, applicable: false },
  ]
  const deployTone: SetupTone = setup.deploy === "attention" || (setup.deploy === "ready" && setup.deploy_activity === "failed")
    ? "attention"
    : setup.deploy === "not_applicable" ? "not-applicable" : setup.deploy
  const deployDetail = setup.deploy === "ready"
    ? `進度已接上 · ${activityWords[setup.deploy_activity]}`
    : setup.deploy === "attention" ? "找到狀態資料，但需要檢查"
      : setup.deploy === "not_applicable" ? "目前的 GitHub workflow 進度機制不適用"
        : place.repo ? "尚未收到 deploy 進度" : "先設定 origin，才能對應 deploy"
  const serverDetail = setup.servers === "ready"
    ? `已宣告 ${setup.server_count} 個開發服務`
    : setup.servers === "attention" ? ".devstack.json 讀取失敗"
      : setup.servers === "empty" ? ".devstack.json 尚未宣告服務"
        : "尚未設定 .devstack.json"
  return [
    { key: "icon", label: "Project 圖示", detail: iconWords[setup.icon], tone: setup.icon === "generated" ? "missing" : "ready", complete: setup.icon !== "generated", applicable: true },
    { key: "deploy", label: "Deploy 進度", detail: deployDetail, tone: deployTone, complete: setup.deploy === "ready", applicable: setup.deploy !== "not_applicable" },
    { key: "servers", label: "開發服務", detail: serverDetail, tone: setup.servers === "ready" ? "ready" : setup.servers === "attention" ? "attention" : "missing", complete: setup.servers === "ready", applicable: true },
    { key: "sync", label: "跨機器識別", detail: setup.sync === "ready" ? "已用 origin 對應同一個 Project" : "尚未設定 origin", tone: setup.sync, complete: setup.sync === "ready", applicable: true },
    unifyCapability(setup),
  ]
}

export function projectSetupProgress(place: ProjectPlace): { complete: number; total: number } | null {
  const capabilities = projectSetupCapabilities(place).filter(row => row.applicable)
  if (!place.setup) return null
  return { complete: capabilities.filter(row => row.complete).length, total: capabilities.length }
}

/**
 * The reviewable first message for a Project-setup Session.
 *
 * It names the portable guide rather than copying its evolving contract into
 * the console. The checklist here still states the boundary that matters at
 * review time: configuration is allowed; an operational deploy or restart is
 * not smuggled into the same press.
 */
export function projectSetupInstructions(place: Pick<ProjectPlace, "label" | "path">): string {
  return [
    "請把這個專案在 Clawdline 裡的顯示設定完整，並留下可驗證的結果。",
    "",
    `Project 名稱：${JSON.stringify(place.label)}`,
    `Project root：${JSON.stringify(place.path)}`,
    "",
    "開始前先完整執行並閱讀 `clawdline guide zh-TW project`，再讀這個 repository 的 AGENTS.md、README、build/deploy scripts 與現有 process manager 設定。",
    "",
    "依 guide 逐項處理：",
    "1. 確認 Project 已登記，而且名稱與像素圖示有意義。",
    "2. 讓 deploy／CI 與長時間本機工作能用正確、會過期的狀態收據顯示進度。",
    "3. 用最小而真實的 `.devstack.json` 宣告開發伺服器的 port 或 URL。",
    "4. 分別驗證 JSON、repository tests、Clawdline 的 Project／server 讀取，以及 Session 上實際顯示的狀態。",
    "",
    "沿用專案既有的 deploy 與 server 管理方式；不要為 Clawdline 另建第二套。除非我在這個 Session 另外明確要求，否則不要啟動、停止、重啟或部署任何東西。修改 git 以外的使用者設定檔前，先顯示準備改動的完整 entry；最後列出改了什麼、不適用什麼，以及仍無法驗證的項目。",
  ].join("\n")
}
