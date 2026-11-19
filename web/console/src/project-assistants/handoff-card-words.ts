import {catalogWord, currentCatalogTag, resolveTag} from "../catalog"

export const en = {
  prepare: "Prepare a fresh conversation",
  title: "Conversation handoff",
  draft: "Your next message starts a fresh conversation with this summary.",
  adopted: "The source accepted this handoff with your message.",
  kept: "Kept the current conversation.",
  withdrawn: "The source changed. Prepare a new summary before starting a fresh conversation.",
  open: "View summary",
  hide: "Hide summary",
  edit: "Edit summary",
  summary: "Handoff summary",
  empty: "There are no earlier messages to carry forward.",
  save: "Save summary",
  cancel: "Cancel",
  keep: "Keep current conversation",
  history: "View original history",
  load: "Check handoff",
  changed: "This handoff changed. Check the current summary before sending.",
  unavailable: "The source could not confirm the handoff. Check its current state.",
  limit: "The summary exceeds the message size limit.",
}

export const zhHant: Record<keyof typeof en, string> = {
  prepare: "準備新對話",
  title: "對話交接",
  draft: "下一則訊息會帶著這份摘要開啟新對話。",
  adopted: "來源已將交接稿與你的訊息一起受理。",
  kept: "已保留目前的對話。",
  withdrawn: "來源已變更。若要開啟新對話，請重新準備摘要。",
  open: "查看摘要",
  hide: "收起摘要",
  edit: "編輯摘要",
  summary: "交接摘要",
  empty: "沒有可帶入新對話的早期訊息。",
  save: "儲存摘要",
  cancel: "取消",
  keep: "保留目前對話",
  history: "查看原對話紀錄",
  load: "查詢交接狀態",
  changed: "交接稿已變更，請先查看目前的摘要再送出。",
  unavailable: "來源目前無法確認交接，請查詢最新狀態。",
  limit: "摘要超過訊息大小上限。",
}

export function handoffWords(language: string): Record<keyof typeof en, string> {
  const tag = resolveTag(language)
  const base = tag === "zh-Hant" ? zhHant : en
  if (tag !== currentCatalogTag()) return base
  return Object.fromEntries(Object.entries(base).map(([key, fallback]) => {
    const value = catalogWord("botHandoff", key)
    return [key, value === `botHandoff.${key}` ? fallback : value]
  })) as Record<keyof typeof en, string>
}
