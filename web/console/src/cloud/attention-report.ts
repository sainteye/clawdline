// @ts-expect-error -- Node's strip-types tests import the source module directly.
import { buildAttentionOverview, targetKey, type AttentionKind, type AttentionOverview, type MachineStatusSnapshot, type StatusSession } from "./attention-model.ts"

export type AttentionLocale = "en" | "zh-Hant-TW"

const copy = {
  en: {
    title: "Needs attention", report: "Manager report", asOf: "Prepared", machine: "Machine", platform: "Platform",
    source: "Source", observed: "Observed", generation: "Snapshot generation", completeness: "Completeness",
    freshness: "Freshness", gap: "Gap", none: "None reported", unknown: "Unknown", noItems: "No known items need attention.",
    incomplete: "Coverage is incomplete. Unknown machines are not counted as zero.",
    current: "Current", stale: "Stale observation", offline: "Offline", unknownFresh: "Unknown",
    complete: "Complete", partial: "Partial", unknownComplete: "Unknown",
    denied: "Session status permission denied", revoked: "Machine access revoked", unreadable: "Session status unreadable",
    session: "Session", state: "State", next: "Next step", kinds: "Attention", target: "Target", activityPolicy: "No-activity threshold", failedAgents: "Failed subagents", provenance: "Provenance",
    reply: "Waiting for a reply", blocked: "Closing blocked", failed: "Subagent failure", no_progress: "No recent activity",
    unconfirmed: "Completed, not confirmed", offlineKind: "Machine offline",
    review: "Open this Session on its source machine and review its status.",
    checkMachine: "Check this machine's connection and refresh its status.",
    restore: "Restore status access to this machine and refresh its snapshot.",
    refresh: "Refresh this machine's snapshot before acting on retained observations.",
    inspectGap: "Inspect the reported gap and refresh this machine.",
    gaps: { snapshot_pending: "Snapshot is still loading", snapshot_generation_unavailable: "Snapshot generation unavailable",
      attention_kind_unavailable: "Attention type unavailable", session_freshness_incomplete: "Some Session observations are stale",
      no_progress_data_unavailable: "Activity time or no-activity policy unavailable", old_version: "Machine version lacks this projection",
      session_source_incomplete: "Session source or completeness is unavailable", session_target_unavailable: "Some Session targets lack an execution generation",
      reply_signal_unavailable: "Waiting state does not identify who must reply", blocked_failed_signal_unavailable: "Blocked or failed status is not fully available",
      session_blocked_failed_unavailable: "Whole-Session blocked and failed states are not projected",
      no_permission: "Status permission denied", unresponsive: "Machine did not answer", event_gap: "Event gap; refresh required",
      bad_projection: "Invalid status projection", offline: "Machine offline", stale: "Snapshot is stale", unknown: "Snapshot unavailable" },
  },
  "zh-Hant-TW": {
    title: "需要注意", report: "管理者報告", asOf: "製作時間", machine: "機器", platform: "平台",
    source: "來源", observed: "觀察時間", generation: "快照世代", completeness: "完整性",
    freshness: "新鮮度", gap: "資料缺口", none: "未回報", unknown: "未知", noItems: "目前沒有已知的待處理項目。",
    incomplete: "涵蓋範圍不完整；未知機器不能算成零。",
    current: "即時", stale: "過期觀察", offline: "離線", unknownFresh: "未知",
    complete: "完整", partial: "部分", unknownComplete: "未知",
    denied: "沒有 Session 狀態讀取權限", revoked: "機器授權已撤銷", unreadable: "無法讀取 Session 狀態",
    session: "Session", state: "狀態", next: "下一步", kinds: "注意類別", target: "目標", activityPolicy: "無動靜門檻", failedAgents: "失敗子代理數", provenance: "原始來源",
    reply: "等待回覆", blocked: "關閉受阻", failed: "子代理失敗", no_progress: "長時間無動靜",
    unconfirmed: "完成未確認", offlineKind: "機器離線",
    review: "到來源機器開啟此 Session，檢查其狀態。",
    checkMachine: "檢查這台機器的連線，然後重新讀取狀態。",
    restore: "恢復這台機器的狀態讀取權限，再更新快照。",
    refresh: "先更新這台機器的快照，再處理過期觀察。",
    inspectGap: "檢查所列資料缺口，再更新這台機器。",
    gaps: { snapshot_pending: "快照仍在載入", snapshot_generation_unavailable: "缺少快照世代",
      attention_kind_unavailable: "缺少注意類別", session_freshness_incomplete: "部分 Session 觀察已過期",
      no_progress_data_unavailable: "缺少活動時間或無動靜門檻", old_version: "機器版本沒有此狀態投影",
      session_source_incomplete: "缺少 Session 來源或完整性", session_target_unavailable: "部分 Session 目標缺少執行世代",
      reply_signal_unavailable: "等待狀態無法判斷由誰回覆", blocked_failed_signal_unavailable: "受阻或失敗狀態資料不完整",
      session_blocked_failed_unavailable: "投影未提供整個 Session 受阻或失敗狀態",
      no_permission: "沒有狀態讀取權限", unresponsive: "機器沒有回應", event_gap: "事件有缺口，需重新讀取",
      bad_projection: "狀態投影無效", offline: "機器離線", stale: "快照已過期", unknown: "無法取得快照" },
  },
} as const

export function attentionWords(locale: AttentionLocale) { return copy[locale] }

export function attentionKindWord(kind: AttentionKind, locale: AttentionLocale): string {
  const words = copy[locale]
  return kind === "offline" ? words.offlineKind : words[kind]
}

export function machineGap(machine: MachineStatusSnapshot, locale: AttentionLocale): string {
  const words = copy[locale]
  if (machine.access === "revoked") return words.revoked
  if (machine.access === "denied") return words.denied
  if (machine.gap) return machine.gap.split(",").map((code) => words.gaps[code as keyof typeof words.gaps] ?? code).join("; ")
  if (machine.freshness === "offline") return words.offline
  if (machine.freshness === "unknown") return words.unknown
  if (machine.sessions === null || machine.completeness === "unknown") return words.unreadable
  return machine.completeness === "partial" ? words.partial : words.none
}

export function nextStep(machine: MachineStatusSnapshot, session: StatusSession | null, locale: AttentionLocale): string {
  const words = copy[locale]
  if (machine.access !== "readable") return words.restore
  if (machine.freshness === "offline") return words.checkMachine
  if (machine.freshness !== "current" || (session?.freshness && session.freshness !== "current")) return words.refresh
  if (machine.completeness !== "complete") return words.inspectGap
  if (machine.gap && !session) return words.inspectGap
  return session ? words.review : words.none
}

export function freshnessWord(machine: MachineStatusSnapshot, locale: AttentionLocale): string {
  const words = copy[locale]
  return { current: words.current, stale: words.stale, offline: words.offline, unknown: words.unknownFresh }[machine.freshness]
}

function safe(value: string): string { return value.replace(/[\\`*_{}\[\]()#+.!|<>\n\r]/g, "\\$&") }
function when(value: number | null, unknown: string): string {
  return value === null || !Number.isFinite(value) || Math.abs(value) > 8.64e15 ? unknown : new Date(value).toISOString()
}

/** Markdown is generated locally from status-only snapshots, with every source named. */
export function formatAttentionReport(overview: AttentionOverview, now: number, locale: AttentionLocale): string {
  const w = copy[locale]
  const lines = [`# ${w.report}`, "", `${w.asOf}: ${when(now, w.unknown)}`, ""]
  if (!overview.complete) lines.push(`> ${w.incomplete}`, "")
  if (overview.entries.length === 0) lines.push(w.noItems, "")
  for (const machine of overview.machines) {
    lines.push(`## ${safe(machine.name)}`, "", `- ${w.machine}: ${safe(machine.machine_id)}`,
      `- ${w.platform}: ${safe(machine.platform)}`, `- ${w.source}: ss/${safe(machine.machine_id)}`,
      `- ${w.observed}: ${when(machine.observedAt, w.unknown)}`,
      `- ${w.generation}: ${machine.snapshotGeneration ? safe(machine.snapshotGeneration) : w.unknown}`,
      `- ${w.completeness}: ${{ complete: w.complete, partial: w.partial, unknown: w.unknownComplete }[machine.completeness]}`,
      `- ${w.freshness}: ${freshnessWord(machine, locale)}`,
      `- ${w.activityPolicy}: ${machine.noProgressAfterMs ? `${machine.noProgressAfterMs / 1000}s` : w.unknown}`,
      `- ${w.gap}: ${safe(machineGap(machine, locale))}`, "")
    const relevant = overview.entries.filter((entry) => entry.machine === machine)
    const byTarget = new Map<string, { session: StatusSession | null; kinds: AttentionKind[]; evidence: "current" | "retained" | "unknown" }>()
    for (const entry of relevant) {
      const key = entry.session ? targetKey(entry.session.target) : "machine"
      const row = byTarget.get(key) ?? { session: entry.session, kinds: [], evidence: entry.evidence }
      row.kinds.push(entry.kind)
      byTarget.set(key, row)
    }
    for (const { session, kinds, evidence } of byTarget.values()) {
      if (!session) {
        lines.push(`- ${w.kinds}: ${kinds.map((kind) => attentionKindWord(kind, locale)).join(", ")}`,
          `- ${w.next}: ${nextStep(machine, null, locale)}`, "")
        continue
      }
      lines.push(`### ${safe(session.title || session.target.session_id)}`, "",
        `- ${w.target}: ${safe(targetKey(session.target))}`,
        `- ${w.source}: ss/${safe(machine.machine_id)}/${safe(session.target.session_id)}`,
        `- ${w.provenance}: ${session.source?.provenance ? safe(session.source.provenance) : w.unknown}`,
        `- ${w.observed}: ${when(session.source?.observedAt ?? null, w.unknown)}`,
        `- ${w.generation}: ${session.source?.snapshotGeneration ? safe(session.source.snapshotGeneration) : w.unknown}`,
        `- ${w.completeness}: ${session.source?.inventoryComplete === null || session.source?.inventoryComplete === undefined ? w.unknownComplete : session.source.inventoryComplete ? w.complete : w.partial}`,
        `- ${w.state}: ${safe(session.state)}`,
        `- ${w.freshness}: ${evidence === "current" ? w.current : evidence === "retained" ? w.stale : w.unknownFresh}`,
        `- ${w.kinds}: ${kinds.map((kind) => attentionKindWord(kind, locale)).join(", ")}`,
        ...(typeof session.failedAgentCount === "number" ? [`- ${w.failedAgents}: ${session.failedAgentCount}`] : []),
        `- ${w.next}: ${nextStep(machine, session, locale)}`, "")
    }
    if (relevant.length === 0 && (machine.completeness !== "complete" || machine.access !== "readable" || machine.freshness !== "current")) {
      lines.push(`- ${w.next}: ${nextStep(machine, null, locale)}`, "")
    }
  }
  return lines.join("\n").trimEnd() + "\n"
}

export function reportFromSnapshots(machines: readonly MachineStatusSnapshot[], now: number, locale: AttentionLocale): string {
  return formatAttentionReport(buildAttentionOverview(machines, now), now, locale)
}
