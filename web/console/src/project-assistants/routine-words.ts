import {catalogWord} from "../catalog"

export const en={
  event:"Filtered event",webhook:"Webhook",source:"Event source",eventName:"Event name",filters:"All filters must match",filterKey:"Field name",filterValue:"Field value",addFilter:"Add filter",removeFilter:"Remove filter",
  eventHint:"Source and event names match exactly; * matches every event from that source. Filters compare top-level strings. messageContains checks text for a substring. A source label does not prove the sender’s identity.",
  eventUnsupported:"Update this source to API75 to create event schedules or manage webhook keys.",
  invalidEvent:"Check the trigger fields. Event names and filter keys allow 200 UTF-8 bytes, values 4000 bytes, and at most 20 unique filters.",
  waitingEvent:"Waiting for an event",manageHook:"Manage webhook key",viewKey:"Retrieve key",hideKey:"Hide and clear key",rotateKey:"Replace key",confirmRotate:"Confirm replacement",
  rotateWarning:"Replace this key? Senders using the current key will stop working until they receive the new key.",
  localURL:"Local relative address",key:"Webhook key",
  publicURL:"Public Cloud address",
  publicHint:"Cloud has acknowledged this source’s current webhook key. Cloud receives incoming event bytes and authentication proof; the source verifies them again. Cloud admission and Host admission do not mean the Bot has finished its work.",
  publicUnavailable:"A public address is unavailable until Cloud confirms the current source enrollment. The local address and key remain available.",
  localHint:"This address belongs to the source machine. It is not a public Cloud URL. Send with Authorization: Bearer followed by the key, or use the key as the GitHub webhook secret. Keys are shown only after an explicit request and are cleared when this panel or source changes.",
  hookReadOnly:"A current interactive write grant is required to retrieve or replace a key.",
  hookHeld:"The previous key operation has no confirmed response. Retry only that same saved command; replacement must not create a second key operation.",
  retryHook:"Retry the saved key operation",followUps:"Waiting follow-ups",followUp:"Follow-up",followUpChain:"Continuation count",followUpCancelUnavailable:"Human follow-up cancellation is not available on this source API.",
  delivery:"Delivery ID",retryCount:"Safe retry count",retryAt:"Retry no earlier than",eventRun:"Webhook delivery",scheduledRun:"Scheduled run",testRun:"Test run",otherRun:"Source reported by the host",unknownRun:"The effects of this run are unknown. It cannot be retried from this history.",
} as const
export const zhHant:Record<keyof typeof en,string>={
  event:"篩選事件",webhook:"Webhook",source:"事件來源",eventName:"事件名稱",filters:"所有篩選條件都必須符合",filterKey:"欄位名稱",filterValue:"欄位值",addFilter:"新增篩選條件",removeFilter:"移除篩選條件",
  eventHint:"來源與事件名稱採完全相符；* 會接受該來源的所有事件。篩選條件比對最上層字串，messageContains 檢查 text 是否包含指定文字。來源標籤不代表寄件者身分已驗證。",
  eventUnsupported:"請將此來源更新至 API75，才能建立事件排程或管理 Webhook 金鑰。",
  invalidEvent:"請檢查觸發條件。事件名稱與篩選欄位名稱上限為 200 個 UTF-8 位元組，欄位值為 4000 個位元組，最多 20 個不重複條件。",
  waitingEvent:"等待事件",manageHook:"管理 Webhook 金鑰",viewKey:"取得金鑰",hideKey:"隱藏並清除金鑰",rotateKey:"更換金鑰",confirmRotate:"確認更換",
  rotateWarning:"確定更換金鑰？仍使用目前金鑰的寄件端會停止運作，直到改用新金鑰。",
  localURL:"本機相對位址",key:"Webhook 金鑰",
  publicURL:"公開 Cloud 位址",
  publicHint:"Cloud 已確認此來源目前的 Webhook 金鑰。Cloud 會接收傳入事件的原始內容與驗證證明，來源也會再次驗證。Cloud 或主機接受事件，都不代表 Bot 已完成工作。",
  publicUnavailable:"Cloud 確認目前來源的登錄後，才會提供公開位址。本機位址與金鑰仍可使用。",
  localHint:"此位址屬於來源機器，不是公開 Cloud 網址。請使用 Authorization: Bearer 加上金鑰傳送，或將金鑰設為 GitHub Webhook secret。只有明確要求後才顯示金鑰，關閉面板或切換來源時會清除。",
  hookReadOnly:"取得或更換金鑰需要目前有效的互動寫入權限。",
  hookHeld:"上次金鑰操作尚未收到確定回覆。只能重試同一筆已儲存指令；更換操作不可另建第二筆指令。",
  retryHook:"重試已儲存的金鑰操作",followUps:"等待中的後續提醒",followUp:"後續提醒",followUpChain:"連續接續次數",followUpCancelUnavailable:"此來源 API 尚未提供人工取消後續提醒的操作。",
  delivery:"傳遞識別碼",retryCount:"安全重試次數",retryAt:"最早重試時間",eventRun:"Webhook 傳遞",scheduledRun:"排程執行",testRun:"測試執行",otherRun:"主機回報的來源",unknownRun:"此次執行的效果尚未確定，無法從歷史紀錄重試。",
}
export const routineWord=(key:keyof typeof en)=>catalogWord("botRoutineControls",key)
