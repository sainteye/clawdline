import type { WorkGateCompactRead, WorkGateRoundSummary } from "@clawdline/contract"

export const AUTHORITY: Record<string, string> = {
  pass: "獨立 checker PASS",
  ai_override: "AI（Epic owner）人工覆核通過 · 非 checker PASS",
  person_override: "你決定覆核通過 · 非 checker PASS",
  technical_ai_override: "AI（Epic owner）技術覆核通過 · 未取得 checker PASS",
  technical_person_override: "你決定技術覆核通過 · 未取得 checker PASS",
}

/** Names both immutable switch values so a compact card never hides which gate was captured. */
export function gateSnapshotText(cycle: number, planning: boolean, verification: boolean): string {
  if (cycle === 0) return "成功指派後擷取"
  return `規劃${planning ? "開" : "關"} · 獨立驗證${verification ? "開" : "關"}`
}

export function gateStatus(gate?: WorkGateCompactRead): string {
  if (!gate || gate.gate_snapshot_cycle === 0) return "Gate 設定尚未擷取"
  const authority = gate.current_authorization
  if (authority) return AUTHORITY[authority.kind] ?? "驗證授權種類未知 · 請查看詳情"
  const escalation = gate.escalation
  if (escalation && escalation.state !== "resolved") {
    return escalation.state === "waiting_user" ? "驗證升級 · 等你決定" : "驗證升級 · 等 Epic owner 決定"
  }
  if (!gate.verify_gate) return gate.planning_gate ? "規劃 gate 開啟 · 未要求獨立驗證" : "兩道 gate 均關閉"
  const round = gate.latest_round
  if (!round) return "獨立驗證尚未開始"
  if (round.state === "complete" && round.verdict === "PASS") return "checker 回報 PASS · 尚無有效合併授權"
  return roundStatus(round)
}

export function roundStatus(round: WorkGateRoundSummary): string {
  if (round.state === "stale") return "驗證已過期 · 不能當作 PASS"
  if (round.state === "technical_failure") return "驗證技術失敗 · 上層 Epic owner 修復後重試；沒有上層時由你決定"
  if (round.state !== "complete") return ({ queued: "驗證待派送", dispatching: "驗證派送中", running: "獨立驗證中" } as Record<string, string>)[round.state] ?? "驗證狀態未知"
  if (round.verdict === "PASS") return "獨立驗證 PASS"
  if (round.verdict === "FAIL") return "獨立驗證 FAIL · 負責 Session 修正後提交新候選"
  if (round.verdict === "NEEDS_WORK") return "NEEDS_WORK · 負責 Session 須補證或重新送驗"
  return "驗證結果未知 · 負責 Session 檢查結果後重新送驗"
}
