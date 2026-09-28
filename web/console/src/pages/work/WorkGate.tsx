import { useState } from "react"
import * as L from "../../legacy/bridge.js"
import type { WorkGateCompactRead, WorkGateDetailRead, WorkGateEscalation, WorkGateDecisionAction } from "@clawdline/contract"
import { decideWorkGate, exportWorkGate, purgeWorkGate, readWorkV2Item, type GateExport, type WorkV2Item } from "./api.js"
import { failureWords, when } from "./shared.js"
import { AUTHORITY, gateSnapshotText, gateStatus, roundStatus } from "./gate-status.js"
import { epicGateDetailShown } from "./epic-gate.js"

function compactOf(item: WorkV2Item): WorkGateCompactRead | undefined {
  const verification = item.verification
  return verification && "compact" in verification ? verification.compact : verification
}

export function WorkGateLine({ item, id }: { item: WorkV2Item; id?: string }) {
  const gate = compactOf(item)
  return <span id={id} className="work-gate-line" data-gate-state={gate?.latest_round?.state ?? "none"}>
    {gate ? gateStatus(gate) : item.gate_snapshot_cycle > 0 ? "驗證摘要無法讀取 · 請查看詳情" : gateStatus()}
  </span>
}

function markdown(value: string) {
  return { __html: L.richTextHTML(value) }
}

function GateDecision({ item, escalation, sessions, run }: {
  item: WorkV2Item
  escalation: WorkGateEscalation
  sessions: { id: string; label: string }[]
  run: (key: string, task: () => Promise<unknown>) => Promise<boolean>
}) {
  const [action, setAction] = useState<WorkGateDecisionAction>("direction")
  const [reason, setReason] = useState("")
  const [direction, setDirection] = useState("")
  const [acceptance, setAcceptance] = useState(item.acceptance_criteria)
  const [target, setTarget] = useState("")
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState("")
  const [said, setSaid] = useState("")
  const actions: { action: WorkGateDecisionAction; label: string }[] = [
    { action: "direction", label: "給修正方向" },
    ...(escalation.kind === "third_fail" ? [{ action: "revise_acceptance" as const, label: "修訂驗收條件" }] : [{ action: "retry" as const, label: "修復後重試驗證" }]),
    { action: "reassign", label: "改派 Session" },
    { action: "override", label: escalation.kind === "technical_verification" ? "技術覆核通過（不是 PASS）" : "覆核通過（不是 PASS）" },
    { action: "cancel", label: "取消項目" },
  ]
  const ready = !!reason.trim() && (action !== "direction" || !!direction.trim()) &&
    (action !== "revise_acceptance" || !!acceptance.trim()) && (action !== "reassign" || !!target)
  const submit = async () => {
    if (!ready || busy) return
    setBusy(true); setFailure(""); setSaid("")
    const ok = await run(`gate-decision-${item.id}`, () => decideWorkGate(item, action, reason.trim(), {
      ...(action === "direction" ? { direction: direction.trim() } : {}),
      ...(action === "revise_acceptance" ? { acceptance_criteria: acceptance } : {}),
      ...(action === "reassign" ? { target_session_id: target } : {}),
    }))
    if (ok) setSaid("決定已記錄；項目狀態正在更新。")
    else setFailure("決定未完成。請確認項目最新版本與原因，再重試。")
    setBusy(false)
  }
  return <form className="work-gate-decision" onSubmit={(event) => { event.preventDefault(); void submit() }}>
    <label>處置
      <select value={action} disabled={busy} onChange={(event) => setAction(event.target.value as WorkGateDecisionAction)}>
        {actions.map((choice) => <option key={choice.action} value={choice.action}>{choice.label}</option>)}
      </select>
    </label>
    {action === "direction" && <label>給負責 Session 的具體方向<textarea value={direction} disabled={busy} onChange={(event) => setDirection(event.target.value)} /></label>}
    {action === "revise_acceptance" && <label>新的驗收 Markdown<textarea value={acceptance} disabled={busy} onChange={(event) => setAcceptance(event.target.value)} /></label>}
    {action === "reassign" && <label>接手 Session
      <select value={target} disabled={busy} onChange={(event) => setTarget(event.target.value)}>
        <option value="">選擇 Session</option>
        {sessions.map((session) => <option key={session.id} value={session.id}>{session.label}</option>)}
      </select>
    </label>}
    <label>決定原因<textarea value={reason} disabled={busy} onChange={(event) => setReason(event.target.value)} /></label>
    {action === "override" && <p className="work-note">這會授權目前固定的候選提交，不會產生 checker PASS；覆核種類與原因會保留在紀錄中。</p>}
    {action === "cancel" && <p className="work-note">這會取消整個項目並釋放負責 Session。</p>}
    <button className={action === "cancel" ? "chip danger" : "chip on"} type="submit" disabled={!ready || busy} aria-busy={busy}>
      {busy ? "記錄中…" : "確認處置"}
    </button>
    {said && <p role="status">{said}</p>}
    {failure && <p className="work-note" role="alert">{failure}</p>}
  </form>
}

function GateRecovery({ item, run }: { item: WorkV2Item; run: (key: string, task: () => Promise<unknown>) => Promise<boolean> }) {
  const [exported, setExported] = useState<GateExport | null>(null)
  const [saved, setSaved] = useState(false)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState("")
  const [said, setSaid] = useState("")
  const download = async () => {
    setBusy(true); setFailure(""); setSaid(""); setSaved(false)
    try {
      const answer = await exportWorkGate(item.id)
      const bytes = new TextEncoder().encode(answer.document)
      if (bytes.length !== answer.manifest.byte_count) throw new Error("匯出位元組數與 manifest 不符；沒有清除任何證據。")
      const digest = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)), (byte) => byte.toString(16).padStart(2, "0")).join("")
      if (digest !== answer.manifest.sha256) throw new Error("匯出雜湊與 manifest 不符；沒有清除任何證據。")
      const blob = new Blob([bytes], { type: "application/json" })
      const url = URL.createObjectURL(blob)
      const link = document.createElement("a")
      link.href = url
      link.download = `clawdline-gate-${item.id}-${answer.manifest.sha256.slice(0, 12)}.json`
      link.click()
      window.setTimeout(() => URL.revokeObjectURL(url), 60_000)
      setExported(answer)
      setSaid(`已準備下載 ${answer.manifest.round_count} 輪驗證詳情；確認檔案已保存後才可清除。`)
    } catch (error) { setFailure(failureWords(error)) } finally { setBusy(false) }
  }
  const purge = async () => {
    if (!saved || !exported || busy) return
    setBusy(true); setFailure(""); setSaid("")
    const ok = await run(`gate-purge-${item.id}`, async () => {
      await purgeWorkGate(item.id, exported.manifest.item_version, exported.manifest.sha256)
      return readWorkV2Item(item.id)
    })
    if (ok) { setSaid("符合條件的舊驗證詳情已清除；總計、最新狀態與稽核紀錄仍保留。"); setExported(null); setSaved(false) }
    else setFailure("清除未完成。匯出內容或項目版本可能已變更；請重新匯出再確認。")
    setBusy(false)
  }
  return <details className="work-gate-recovery"><summary>驗證詳情容量與匯出</summary>
    <p>詳情達容量上限時，先匯出可清除的已結束輪次，再核對匯出雜湊與項目版本後清除。進行中的輪次、最新狀態與累計數字不會被清除。</p>
    <button className="chip" type="button" disabled={busy} aria-busy={busy} onClick={() => void download()}>{busy ? "處理中…" : "匯出可清除的驗證詳情"}</button>
    {exported && <div className="work-gate-export">
      <p>匯出輪次：{exported.manifest.round_count} · SHA-256：<code>{exported.manifest.sha256}</code></p>
      <label><input type="checkbox" checked={saved} onChange={(event) => setSaved(event.target.checked)} /> 我已確認匯出檔案保存完成</label>
      <button className="chip danger" type="button" disabled={!saved || busy || exported.manifest.round_count === 0} onClick={() => void purge()}>清除已匯出的舊詳情</button>
    </div>}
    {said && <p role="status">{said}</p>}{failure && <p role="alert">{failure}</p>}
  </details>
}

function GateEscalation({ item, escalation, sessions, run }: {
  item: WorkV2Item
  escalation: WorkGateEscalation
  sessions: { id: string; label: string }[]
  run: (key: string, task: () => Promise<unknown>) => Promise<boolean>
}) {
  return <section className="work-gate-escalation" aria-label="驗證需要處理">
    <h5>{escalation.kind === "third_fail" ? "連續第三次 FAIL" : "技術驗證無法完成"}</h5>
    <p>{escalation.reason}</p>
    {escalation.state === "waiting_user" ? <GateDecision item={item} escalation={escalation} sessions={sessions} run={run} />
      : <p>目前由上層 Epic owner Session 決定；若該 Session 離線滿 15 分鐘，會改由你決定。可從 Epic 的子項清單追蹤負責 Session；此處沒有代替 Agent 行使的操作。</p>}
  </section>
}

/** Keep a required person decision visible without showing the full gate panel on a non-Epic. */
export function WorkGateAttention({ item, sessions, run }: {
  item: WorkV2Item
  sessions: { id: string; label: string }[]
  run: (key: string, task: () => Promise<unknown>) => Promise<boolean>
}) {
  if (item.kind === "epic") return null
  const escalation = compactOf(item)?.escalation
  if (!escalation || escalation.state === "resolved") return null
  return <><GateEscalation item={item} escalation={escalation} sessions={sessions} run={run} />
    {escalation.kind === "technical_verification" && <GateRecovery item={item} run={run} />}</>
}

export function WorkGateDetail({ item, loading, error, sessions, run, retry }: {
  item: WorkV2Item
  loading: boolean
  error: string
  sessions: { id: string; label: string }[]
  run: (key: string, task: () => Promise<unknown>) => Promise<boolean>
  retry: () => void
}) {
  const compact = compactOf(item)
  const detail: WorkGateDetailRead | undefined = item.verification && "compact" in item.verification ? item.verification : undefined
  if (!epicGateDetailShown(item)) return null
  return <section className="work-gate-detail" aria-label="規劃與獨立驗證">
    <h4>規劃與獨立驗證</h4>
    <p className="work-gate-current"><WorkGateLine item={item} /></p>
    {loading && <p role="status">正在讀取驗證證據…</p>}
    {error && <p role="alert">驗證詳情讀取失敗：{error} <button className="chip" type="button" onClick={retry}>重試</button></p>}
    {!loading && !error && !detail && <p>尚未載入驗證詳情。<button className="chip" type="button" onClick={retry}>讀取詳情</button></p>}
    <p>本輪擷取：{item.gate_snapshot_cycle ? `第 ${item.gate_snapshot_cycle} 輪 · ${gateSnapshotText(item.gate_snapshot_cycle, item.planning_gate, item.verify_gate)}` : "尚未成功指派；將在第一次成功指派時擷取當時設定"}。後續修改全域設定不會改動本輪。</p>
    <div className="work-gate-acceptance"><strong>驗收條件（Markdown）</strong>
      <div dangerouslySetInnerHTML={markdown(item.acceptance_criteria)} />
      <small>版本 {item.acceptance_version} · SHA-256 {item.acceptance_digest}</small>
    </div>
    {compact && <dl className="work-gate-metrics">
      <div><dt>驗證輪次</dt><dd>{compact.metrics.rounds}</dd></div><div><dt>FAIL</dt><dd>{compact.metrics.fails}</dd></div>
      <div><dt>發現問題</dt><dd>{compact.metrics.findings}</dd></div><div><dt>覆核通過</dt><dd>{compact.metrics.overrides}</dd></div>
    </dl>}
    {compact?.current_authorization && <p className="work-gate-authority"><strong>{AUTHORITY[compact.current_authorization.kind] ?? "未知授權種類"}</strong>：{compact.current_authorization.reason}</p>}
    {compact?.escalation && compact.escalation.state !== "resolved" && <GateEscalation item={item}
      escalation={compact.escalation} sessions={sessions} run={run} />}
    {detail && <>
      <h5>最近驗證證據</h5>
      {!detail.recent_rounds.length && <p>目前沒有可顯示的驗證輪次。</p>}
      {detail.recent_rounds.map((round) => <details className="work-gate-round" key={round.id}>
        <summary>{roundStatus({ id: round.id, state: round.state, verdict: round.result?.verdict, candidate_commit: round.candidate.commit, criteria_digest: round.acceptance.digest, created_at: round.created_at })} · {when(round.created_at)} · {round.checker_persona}</summary>
        <p>固定候選提交：<code>{round.candidate.commit}</code></p>
        {round.stale_reason && <p>過期原因：{round.stale_reason}</p>}
        {round.result?.summary && <p>{round.result.summary}</p>}
        {round.result?.claims.map((claim, index) => <div className="work-gate-claim" key={index}>
          <strong>{claim.state === "passed" ? "已驗證" : claim.state === "failed" ? "未通過" : "無法驗證"}：{claim.criterion}</strong>
          {claim.reason && <p>{claim.reason}</p>}
          {claim.evidence.length > 0 && <ul>{claim.evidence.map((evidence, n) => <li key={n}>{evidence}</li>)}</ul>}
          {claim.evidence_artifacts.length > 0 && <p>證據附件：{claim.evidence_artifacts.join("、")}</p>}
        </div>)}
        {!round.result && <p>沒有可用的 checker 結果；這輪不能當作 PASS。</p>}
      </details>)}
      {detail.recent_rounds_truncated && <p>只顯示最近的有界詳情；累計數字仍包含較早輪次。</p>}
    </>}
    {item.gate_snapshot_cycle > 0 && item.verify_gate && <GateRecovery item={item} run={run} />}
  </section>
}
