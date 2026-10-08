import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import { catalogLabel, colonMark, listSeparator } from "../../punctuation.js"
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
    {gate ? gateStatus(gate) : item.gate_snapshot_cycle > 0 ? catalogWord("literal", "4d02b00558fa") : gateStatus()}
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
    { action: "direction", label: catalogWord("literal", "ff745d63f0ed") },
    ...(escalation.kind === "third_fail" ? [{ action: "revise_acceptance" as const, label: catalogWord("literal", "b6c1ba21d900") }] : [{ action: "retry" as const, label: catalogWord("literal", "4c3e804361fa") }]),
    { action: "reassign", label: catalogWord("literal", "f38983f7e110") },
    { action: "override", label: escalation.kind === "technical_verification" ? catalogWord("literal", "6f81e41d7af8") : catalogWord("literal", "3fbf7dc0b3f0") },
    { action: "cancel", label: catalogWord("literal", "72984e003fe2") },
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
    if (ok) setSaid(catalogWord("literal", "bae271f09e69"))
    else setFailure(catalogWord("literal", "9e4bf413b2fd"))
    setBusy(false)
  }
  return <form className="work-gate-decision" onSubmit={(event) => { event.preventDefault(); void submit() }}>
    <label>{catalogWord("inline", "7c8bd896636d")}
      <select value={action} disabled={busy} onChange={(event) => setAction(event.target.value as WorkGateDecisionAction)}>
        {actions.map((choice) => <option key={choice.action} value={choice.action}>{choice.label}</option>)}
      </select>
    </label>
    {action === "direction" && <label>{catalogWord("inline", "49e8de8e61c7")}<textarea value={direction} disabled={busy} onChange={(event) => setDirection(event.target.value)} /></label>}
    {action === "revise_acceptance" && <label>{catalogWord("inline", "6f970e03b8cf")}<textarea value={acceptance} disabled={busy} onChange={(event) => setAcceptance(event.target.value)} /></label>}
    {action === "reassign" && <label>{catalogWord("inline", "affef15cffea")}
      <select value={target} disabled={busy} onChange={(event) => setTarget(event.target.value)}>
        <option value="">{catalogWord("inline", "bf22013ec710")}</option>
        {sessions.map((session) => <option key={session.id} value={session.id}>{session.label}</option>)}
      </select>
    </label>}
    <label>{catalogWord("inline", "88e88c08f8b7")}<textarea value={reason} disabled={busy} onChange={(event) => setReason(event.target.value)} /></label>
    {action === "override" && <p className="work-note">{catalogWord("inline", "3052b22222ed")}</p>}
    {action === "cancel" && <p className="work-note">{catalogWord("inline", "2c787b36ee6a")}</p>}
    <button className={action === "cancel" ? "chip danger" : "chip on"} type="submit" disabled={!ready || busy} aria-busy={busy}>
      {busy ? catalogWord("literal", "622afbf65497") : catalogWord("literal", "7b1667febb0c")}
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
      if (bytes.length !== answer.manifest.byte_count) throw new Error(catalogWord("literal", "2d66bc997d16"))
      const digest = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)), (byte) => byte.toString(16).padStart(2, "0")).join("")
      if (digest !== answer.manifest.sha256) throw new Error(catalogWord("literal", "cd3ba7de1f2d"))
      const blob = new Blob([bytes], { type: "application/json" })
      const url = URL.createObjectURL(blob)
      const link = document.createElement("a")
      link.href = url
      link.download = `clawdline-gate-${item.id}-${answer.manifest.sha256.slice(0, 12)}.json`
      link.click()
      window.setTimeout(() => URL.revokeObjectURL(url), 60_000)
      setExported(answer)
      setSaid(catalogFormat("template", "1eeb4ffcc2cd", [answer.manifest.round_count]))
    } catch (error) { setFailure(failureWords(error)) } finally { setBusy(false) }
  }
  const purge = async () => {
    if (!saved || !exported || busy) return
    setBusy(true); setFailure(""); setSaid("")
    const ok = await run(`gate-purge-${item.id}`, async () => {
      await purgeWorkGate(item.id, exported.manifest.item_version, exported.manifest.sha256)
      return readWorkV2Item(item.id)
    })
    if (ok) { setSaid(catalogWord("literal", "c8759c3fdb18")); setExported(null); setSaved(false) }
    else setFailure(catalogWord("literal", "532eeda3d5c1"))
    setBusy(false)
  }
  return <details className="work-gate-recovery"><summary>{catalogWord("inline", "7cfd32c58198")}</summary>
    <p>{catalogWord("inline", "ad0453521a69")}</p>
    <button className="chip" type="button" disabled={busy} aria-busy={busy} onClick={() => void download()}>{busy ? catalogWord("literal", "30d4d152f338") : catalogWord("literal", "ed21e82d12c2")}</button>
    {exported && <div className="work-gate-export">
      <p>{catalogFormat("count", "exportRounds", [exported.manifest.round_count])}<code>{exported.manifest.sha256}</code></p>
      <label><input type="checkbox" checked={saved} onChange={(event) => setSaved(event.target.checked)} />{catalogWord("inline", "3294338af7c8")}</label>
      <button className="chip danger" type="button" disabled={!saved || busy || exported.manifest.round_count === 0} onClick={() => void purge()}>{catalogWord("inline", "845e56fbb58d")}</button>
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
  return <section className="work-gate-escalation" aria-label={catalogWord("inline", "01e580a9f105")}>
    <h5>{escalation.kind === "third_fail" ? catalogWord("literal", "bf753874893f") : catalogWord("literal", "f987cffe62e7")}</h5>
    <p>{escalation.reason}</p>
    {escalation.state === "waiting_user" ? <GateDecision item={item} escalation={escalation} sessions={sessions} run={run} />
      : <p>{catalogWord("inline", "5abf04119230")}</p>}
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
  return <section className="work-gate-detail" aria-label={catalogWord("inline", "9737e4f2e725")}>
    <h4>{catalogWord("inline", "9737e4f2e725")}</h4>
    <p className="work-gate-current"><WorkGateLine item={item} /></p>
    {!loading && !error && !detail && <p>{catalogWord("inline", "ce6184e46a66")}<button className="chip" type="button" onClick={retry}>{catalogWord("inline", "aa2938c4e84f")}</button></p>}
    <p>{catalogLabel("inline", "a24529fd35aa")}{item.gate_snapshot_cycle ? catalogFormat("template", "0d6f06c58208", [item.gate_snapshot_cycle, gateSnapshotText(item.gate_snapshot_cycle, item.planning_gate, item.verify_gate)]) : catalogWord("literal", "cd8db91c094e")}{catalogWord("inline", "fa13f75c4f30")}</p>
    <div className="work-gate-acceptance"><strong>{catalogWord("inline", "b1d809e93c19")}</strong>
      <div dangerouslySetInnerHTML={markdown(item.acceptance_criteria)} />
      <small>{catalogWord("inline", "5f76b2bf82dd")} {item.acceptance_version}{catalogWord("inline", "d7e1f4d17c57")} {item.acceptance_digest}</small>
    </div>
    {compact && <dl className="work-gate-metrics">
      <div><dt>{catalogWord("inline", "61d8b2decbe9")}</dt><dd>{compact.metrics.rounds}</dd></div><div><dt>{catalogWord("inline", "425305e25df9")}</dt><dd>{compact.metrics.fails}</dd></div>
      <div><dt>{catalogWord("inline", "1dd74b5e1e7c")}</dt><dd>{compact.metrics.findings}</dd></div><div><dt>{catalogWord("inline", "23154087857c")}</dt><dd>{compact.metrics.overrides}</dd></div>
    </dl>}
    {compact?.current_authorization && <p className="work-gate-authority"><strong>{AUTHORITY[compact.current_authorization.kind] ?? catalogWord("literal", "10da14d9c382")}</strong>{colonMark()}{compact.current_authorization.reason}</p>}
    {compact?.escalation && compact.escalation.state !== "resolved" && <GateEscalation item={item}
      escalation={compact.escalation} sessions={sessions} run={run} />}
    {detail && <>
      <h5>{catalogWord("inline", "4d03d4102d36")}</h5>
      {!detail.recent_rounds.length && <p>{catalogWord("inline", "edd5f8a3232a")}</p>}
      {detail.recent_rounds.map((round) => <details className="work-gate-round" key={round.id}>
        <summary>{roundStatus({ id: round.id, state: round.state, verdict: round.result?.verdict, candidate_commit: round.candidate.commit, criteria_digest: round.acceptance.digest, created_at: round.created_at })} · {when(round.created_at)} · {round.checker_persona}</summary>
        <p>{catalogLabel("inline", "9b4d84c4d0a4")}<code>{round.candidate.commit}</code></p>
        {round.stale_reason && <p>{catalogLabel("inline", "e88de75bf9dc")}{round.stale_reason}</p>}
        {round.result?.summary && <p>{round.result.summary}</p>}
        {round.result?.claims.map((claim, index) => <div className="work-gate-claim" key={index}>
          <strong>{claim.state === "passed" ? catalogWord("literal", "0841cf12ec8c") : claim.state === "failed" ? catalogWord("literal", "c0b14e64a4b1") : catalogWord("literal", "ca57dc5b26d8")}{colonMark()}{claim.criterion}</strong>
          {claim.reason && <p>{claim.reason}</p>}
          {claim.evidence.length > 0 && <ul>{claim.evidence.map((evidence, n) => <li key={n}>{evidence}</li>)}</ul>}
          {claim.evidence_artifacts.length > 0 && <p>{catalogLabel("inline", "9af9547ec91b")}{claim.evidence_artifacts.join(listSeparator())}</p>}
        </div>)}
        {!round.result && <p>{catalogWord("inline", "8c5574d18a73")}</p>}
      </details>)}
      {detail.recent_rounds_truncated && <p>{catalogWord("inline", "74f6181c1a32")}</p>}
    </>}
    {item.gate_snapshot_cycle > 0 && item.verify_gate && <GateRecovery item={item} run={run} />}
  </section>
}
