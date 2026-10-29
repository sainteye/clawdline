import { catalogWord } from "../catalog.js"
import { useEffect, useState, useSyncExternalStore } from "react"
import type { SettingsRequest, SettingsSnapshot } from "@clawdline/contract"
import { readNotificationSettings, writeNotificationSettings } from "./settings/api.js"
import { settingsFailureSentence } from "./settings/window/failure.js"
import { pushShape, sendPushTest, startPush, subscribePush, togglePush } from "../push/push.js"
import { legacyState } from "../legacy/overlay-bridge.js"
import type { PageModule } from "./types.js"
import { Switch } from "./settings/Switch.js"
import "./notifications/notifications.css"

const notificationsEn = {
  "push_on_model_capacity": "A model is at capacity",
  "push_on_model_capacityHint": "Notify as soon as detected, once per stop. Choose another model or retry.",
  "push_on_waiting": "A Session needs my answer",
  "push_on_waitingHint": "Once per unanswered stop, after ten minutes.",
  "orchestrator_agent_notify": "An agent sends me a notification",
  "orchestrator_agent_notifyHint": "Includes unsolicited notes; subject to the agent notification limit.",
  "push_on_board_decision": "A Board decision needs an answer",
  "push_on_board_decisionHint": "Once per pending decision.",
  "push_on_board_done": "A Board item is done",
  "push_on_board_doneHint": "Opens its assigned Session while that Session exists.",
  "push_on_schedule_failure": "A schedule fails or cannot run",
  "push_on_schedule_failureHint": "Missed or refused runs also require that schedule's failure switch; unreadable files notify once per change.",
  "push_on_capacity": "A capacity limit is reached",
  "push_on_capacityHint": "The limit and its current reading remain in diagnostics.",
  "push_on_dead_letter": "A task completion cannot reach its Session",
  "push_on_dead_letterHint": "Opens its root Session when it still exists.",
  "push_on_delivery": "A Session reports delivery",
  "push_on_deliveryHint": "When a Session records its delivery report.",
  "push_on_fanout": "A batch of dispatched tasks finishes",
  "push_on_fanoutHint": "When all tasks in the same batch finish.",
  "smart_notifications": "Use details in completion notifications",
  "smart_notificationsHint": "Uses the Session's delivery summary or task titles instead of generic wording. This does not create another notification.",
  "push_on_app_release_success": "An App release succeeds",
  "push_on_app_release_successHint": "After the running App passes its version check.",
  "push_on_cloud_release_success": "A Cloud release succeeds",
  "push_on_cloud_release_successHint": "After the production site passes its checks.",
  "push_on_release_failure": "An App or Cloud release fails",
  "push_on_release_failureHint": "When app installation or Cloud production verification fails.",
  "title": "Notifications",
  "intro": "Choose which events may notify your subscribed devices. These settings apply to this machine; browser permission is set separately on each device.",
  "deviceTitle": "This device",
  "deviceOn": "Notifications are on.",
  "deviceBlocked": "The browser blocks notifications for this site.",
  "deviceHomeScreen": "On iPhone or iPad, add this site to the Home Screen first.",
  "deviceUnsupported": "This browser or address cannot receive notifications.",
  "deviceOff": "Notifications are off on this device.",
  "on": "On",
  "off": "Off",
  "sendTest": "Send a test",
  "eventsTitle": "When to notify me",
  "loading": "Loading preferences…",
  "destinationSession": "Opens the Session while it exists.",
  "destinationOptionalSession": "Opens the named Session, or the list if none was named.",
  "destinationList": "Opens the list."
} as const

const notificationsZh: Record<keyof typeof notificationsEn, string> = {
  "push_on_model_capacity": "模型容量不足",
  "push_on_model_capacityHint": "偵測到時立即通知，每次停住最多一次；可換模型或重試。",
  "push_on_waiting": "Session 等我回答或授權",
  "push_on_waitingHint": "等待十分鐘後，每次停住最多通知一次。",
  "orchestrator_agent_notify": "Agent 主動通知我",
  "orchestrator_agent_notifyHint": "包含主動建立的待辦 Note，並受 Agent 通知次數限制。",
  "push_on_board_decision": "看板決策等我回答",
  "push_on_board_decisionHint": "每個待回答決策最多通知一次。",
  "push_on_board_done": "看板項目完成",
  "push_on_board_doneHint": "若指派的 Session 還在，就開啟它。",
  "push_on_schedule_failure": "排程失敗或無法執行",
  "push_on_schedule_failureHint": "錯過或拒絕執行還需開啟該排程的「失敗時通知」；檔案無法讀取時每次變更最多通知一次。",
  "push_on_capacity": "系統容量達到警示值",
  "push_on_capacityHint": "即使不推播，診斷頁仍保留警示與讀數。",
  "push_on_dead_letter": "任務完成訊息無法送達 Session",
  "push_on_dead_letterHint": "若原本的 Root Session 還在，就開啟它。",
  "push_on_delivery": "Session 回報交件",
  "push_on_deliveryHint": "Session 記錄交件回報時通知。",
  "push_on_fanout": "整批派工結束",
  "push_on_fanoutHint": "同一批派出的任務全部結束時通知。",
  "smart_notifications": "完成通知顯示交件與任務內容",
  "smart_notificationsHint": "在通知中帶入 Session 交件摘要或任務名稱；不會多送一則通知。",
  "push_on_app_release_success": "App 新版本發布成功",
  "push_on_app_release_successHint": "運行中的 App 通過版本驗收後通知。",
  "push_on_cloud_release_success": "Cloud 新版本發布成功",
  "push_on_cloud_release_successHint": "正式網站通過發布驗收後通知。",
  "push_on_release_failure": "App 或 Cloud 發布失敗",
  "push_on_release_failureHint": "App 安裝或 Cloud 正式環境驗收失敗時通知。",
  "title": "通知",
  "intro": "逐項選擇哪些事件可以推播到已訂閱的裝置。這些設定套用到這台機器；每台裝置的瀏覽器權限需分別開啟。",
  "deviceTitle": "這台裝置",
  "deviceOn": "通知已開啟。",
  "deviceBlocked": "瀏覽器已封鎖這個網站的通知。",
  "deviceHomeScreen": "在 iPhone 或 iPad 上，請先將網站加入主畫面。",
  "deviceUnsupported": "這個瀏覽器或網址無法接收通知。",
  "deviceOff": "這台裝置的通知尚未開啟。",
  "on": "開",
  "off": "關",
  "sendTest": "送一則測試",
  "eventsTitle": "何時通知我",
  "loading": "正在讀取通知設定…",
  "destinationSession": "若 Session 還在，點擊後開啟它。",
  "destinationOptionalSession": "有指定 Session 時開啟它，否則開啟清單。",
  "destinationList": "點擊後開啟清單。"
}

export const notificationsWords = { en: notificationsEn, "zh-Hant": notificationsZh }

function word(key: keyof typeof notificationsEn): string {
  return catalogWord("notifications", key)
}

type Preference = keyof Pick<SettingsRequest,
  "push_on_model_capacity" | "push_on_waiting" | "orchestrator_agent_notify" | "push_on_board_decision" |
  "push_on_board_done" | "push_on_schedule_failure" | "push_on_capacity" |
  "push_on_dead_letter" | "push_on_app_release_success" | "push_on_cloud_release_success" | "push_on_release_failure" |
  "push_on_delivery" | "push_on_fanout" | "smart_notifications">

type Row = { key: Preference; destination: "Session" | "list" | "optional-session" | "none" }

const rows: Row[] = [
  { key: "push_on_model_capacity", destination: "Session" },
  { key: "push_on_waiting", destination: "Session" },
  { key: "orchestrator_agent_notify", destination: "optional-session" },
  { key: "push_on_board_decision", destination: "list" },
  { key: "push_on_board_done", destination: "none" },
  { key: "push_on_schedule_failure", destination: "list" },
  { key: "push_on_capacity", destination: "list" },
  { key: "push_on_dead_letter", destination: "none" },
  { key: "push_on_delivery", destination: "Session" },
  { key: "push_on_fanout", destination: "Session" },
  { key: "smart_notifications", destination: "none" },
  { key: "push_on_app_release_success", destination: "list" },
  { key: "push_on_cloud_release_success", destination: "list" },
  { key: "push_on_release_failure", destination: "list" },
]

function current(snapshot: Partial<SettingsSnapshot>, key: Preference): boolean {
  const value = snapshot[key]
  if (typeof value === "boolean") return value
  if (key === "push_on_waiting") return snapshot.orchestrator_agent_notify ?? true
  if (key === "smart_notifications") return false
  if (key === "push_on_app_release_success" || key === "push_on_cloud_release_success" || key === "push_on_release_failure") return snapshot.push_on_deploy ?? false
  return true
}

function NotificationsPage({ shown }: { shown: boolean }) {
  const push = useSyncExternalStore(subscribePush, pushShape)
  const [settings, setSettings] = useState<Partial<SettingsSnapshot> | null>(null)
  const [pending, setPending] = useState<Preference | null>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    if (!shown) return
    startPush()
    let live = true
    void readNotificationSettings().then((answer) => {
      if (live) { setSettings(answer); setError("") }
    }, (cause: unknown) => { if (live) setError(settingsFailureSentence(cause)) })
    return () => { live = false }
  }, [shown])

  const change = async (key: Preference) => {
    if (!settings || pending) return
    setPending(key)
    setError("")
    try {
      const answer = await writeNotificationSettings({ [key]: !current(settings, key) } as Partial<SettingsRequest>)
      setSettings(answer)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setPending(null)
    }
  }

  return <section id="notifications" className="page notification-page" data-page-view="notifications" hidden={!shown}>
    <div className="notification-wrap">
      <h1>{word("title")}</h1>
      <p>{word("intro")}</p>
      <div className="notification-device">
        <h2 id="notification-device-title">{word("deviceTitle")}</h2>
        <p id="notification-device-state">{push.state === "on" ? word("deviceOn") : push.state === "blocked" ? word("deviceBlocked") : push.state === "homescreen" ? word("deviceHomeScreen") : push.state === "unsupported" ? word("deviceUnsupported") : word("deviceOff")}</p>
        {(push.state === "on" || push.state === "off") && <Switch labelledBy="notification-device-title" describedBy="notification-device-state" on={push.state === "on"} stateText={push.state === "on" ? word("on") : word("off")} disabled={push.busy} onToggle={togglePush} />}
        {push.state === "on" && <button type="button" disabled={push.testing} onClick={() => sendPushTest(legacyState.openId || legacyState.selectedId || null)}>{word("sendTest")}</button>}
        <p role="status">{push.said}</p>
      </div>
      <h2>{word("eventsTitle")}</h2>
      {error && <p className="notification-error" role="alert">{error}</p>}
      {!settings && !error && <p>{word("loading")}</p>}
      {settings && <div className="notification-rows">{rows.map((row) => <label className="notification-row" key={row.key}>
        <span><strong>{word(row.key)}</strong><small>{word(`${row.key}Hint`)} {row.destination === "Session" ? word("destinationSession") : row.destination === "optional-session" ? word("destinationOptionalSession") : row.destination === "list" ? word("destinationList") : ""}</small></span>
        <input type="checkbox" checked={current(settings, row.key)} disabled={pending !== null} onChange={() => void change(row.key)} />
      </label>)}</div>}
    </div>
  </section>
}

export const page: PageModule = { id: "notifications", Component: NotificationsPage }
