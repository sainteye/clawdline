/* M05: the incident's agent investigation, as one card beside the summary.
 *
 * The newest investigation leads, in words: its state, the concrete reason and what to do next,
 * then the agent's enumerated report. Earlier ones fold away. Task success is never recovery, and
 * human review stays its own panel. A start names a machine: Cloud lists each real machine once
 * with whether it has exactly one investigation schedule, and only such a machine can be chosen.
 * The page sends that machine's own hook; it never guesses one from the webhook list. */
import { useEffect, useRef, useState } from "react"
import type { Refusal } from "./cloud"
import type { Investigation, Investigations, StartGate } from "./investigations"
import { reasonWords, startGate } from "./investigations"
import { fetchDestinations, newIdempotencyKey, startInvestigation, startRequestFor, START_REFUSALS, type Destination, type Destinations, type StartRequest } from "./investigation-actions"
import type { HealthSay, HealthWord } from "./words"
import { At, Id } from "./stored-screens"
import { Panel } from "./parts"

export type InvestigationActions = {
  get: typeof fetch
  apiOrigin: string
  refresh: () => void
  refuse: (r: Refusal) => void
}

const GLYPH = { investigating: "◇", needs_you: "■", verified_recovered: "✓" } as const

export function InvestigationBadge({ inv, say }: { inv: Investigation; say: HealthSay }) {
  if (!inv.state.known) return <span className="adh-badge adh-iv-unknown"><span aria-hidden="true" className="adh-glyph">?</span>{say("iv_state_unknown", { raw: inv.state.raw ?? "—" })}</span>
  const s = inv.state.value
  return <span className={`adh-badge adh-iv-${s}`}><span aria-hidden="true" className="adh-glyph">{GLYPH[s]}</span>{say(`iv_state_${s}` as HealthWord)}</span>
}

function Report({ inv, say }: { inv: Investigation; say: HealthSay }) {
  const r = inv.report
  if (r === null) return <p className="adm-fine">{say("iv_reportNone")}</p>
  return (
    <dl className="adh-facts">
      <dt>{say("iv_reportConclusion")}</dt>
      <dd>{r.conclusion.known ? say(`iv_conc_${r.conclusion.value}` as HealthWord) : say("iv_conc_unknown", { raw: r.conclusion.raw ?? "—" })}</dd>
      <dt>{say("iv_fixCommit")}</dt><dd><Id v={r.fix_commit} /></dd>
      <dt>{say("iv_evidenceRefs")}</dt>
      <dd>{r.evidence_refs.length === 0 ? <span className="adm-unknown">{say("iv_noRefs")}</span> : r.evidence_refs.map((x) => <Id key={x} v={x} />)}</dd>
    </dl>
  )
}

function Technical({ inv, say }: { inv: Investigation; say: HealthSay }) {
  return (
    <details className="adh-disclose">
      <summary>{say("iv_tech")}</summary>
      <dl className="adh-facts">
        <dt>{say("iv_f_reason")}</dt><dd><Id v={inv.reason === null ? null : inv.reason.known ? inv.reason.value : inv.reason.raw} /></dd>
        <dt>{say("iv_f_deliveryState")}</dt><dd><Id v={inv.delivery_state} /></dd>
        <dt>{say("iv_f_task")}</dt><dd><Id v={inv.task_terminal_state === null ? null : inv.task_terminal_state.known ? inv.task_terminal_state.value : inv.task_terminal_state.raw} /></dd>
        <dt>{say("iv_deadline")}</dt><dd><At iso={inv.deadline_at} /></dd>
        <dt>{say("iv_f_revision")}</dt><dd>{inv.incident_revision ?? <span className="adm-unknown">—</span>}</dd>
        <dt>{say("iv_f_hash")}</dt><dd><code className="adh-id adh-hash">{inv.incident_content_hash ?? "—"}</code></dd>
        <dt>{say("iv_f_hook")}</dt><dd><Id v={inv.hook_id} /></dd>
        <dt>{say("iv_f_machine")}</dt><dd><Id v={inv.machine_id} /></dd>
        <dt>{say("iv_f_delivery")}</dt><dd><Id v={inv.delivery_id} /></dd>
        <dt>{say("iv_f_investigation")}</dt><dd><Id v={inv.investigation_id} /></dd>
        {inv.recovery ? <><dt>{say("iv_f_recovery")}</dt><dd><Id v={inv.recovery.event_key} /> {inv.recovery.producer_seq !== null ? `· ${say("inc_card_seq", { seq: inv.recovery.producer_seq })}` : ""}</dd></> : null}
      </dl>
    </details>
  )
}

function Latest({ inv, say }: { inv: Investigation; say: HealthSay }) {
  const { why, next } = reasonWords(inv, say)
  return (
    <div className="adh-iv-latest">
      <p className="adh-badges"><InvestigationBadge inv={inv} say={say} /> <span className="adm-fine">{say("iv_started")} <At iso={inv.started_at} />{inv.closed_at ? <> · {say("iv_closed")} <At iso={inv.closed_at} /></> : null}</span></p>
      <p className="adh-inc-lead">{why}</p>
      <p><strong>{say("iv_nextLabel")}</strong> {next}</p>
      {inv.recovery ? (
        <p className="adm-fine">{say("iv_recoveryProof", { basis: inv.recovery.basis.known ? say(`iv_basis_${inv.recovery.basis.value}` as HealthWord) : say("iv_basis_unknown") })} <At iso={inv.recovery.received_at} /></p>
      ) : null}
      <h3 className="adh-sum-h">{say("iv_reportTitle")}</h3>
      <Report inv={inv} say={say} />
      <Technical inv={inv} say={say} />
    </div>
  )
}

function Earlier({ items, say }: { items: Investigation[]; say: HealthSay }) {
  if (items.length === 0) return null
  return (
    <details className="adh-disclose">
      <summary>{say("iv_earlier", { n: items.length })}</summary>
      <ol className="adh-iv-earlier">
        {items.map((inv) => (
          <li key={inv.investigation_id}>
            <p className="adh-badges"><InvestigationBadge inv={inv} say={say} /> <span className="adm-fine"><At iso={inv.started_at} /></span></p>
            <p className="adm-fine">{reasonWords(inv, say).why}</p>
            <Technical inv={inv} say={say} />
          </li>
        ))}
      </ol>
    </details>
  )
}

type DestState = { kind: "loading" } | { kind: "failed"; code: string } | { kind: "unsupported" } | { kind: "ready"; destinations: Destinations }
/** `pending` holds the key and body of a request whose outcome is not known yet, and the machine it was for. */
type Pending = { key: string; req: StartRequest; machine: string }
type StartState =
  | { kind: "closed" }
  | { kind: "picking" }
  | { kind: "busy"; pending: Pending }
  | { kind: "ambiguous"; pending: Pending; code: string }
  | { kind: "refused"; code: string }
  | { kind: "done"; replayed: boolean }

/** One row per machine. Only a `ready` row has a radio that can be checked; every other row says why and what to do. */
function MachineList({ d, name, chosen, locked, onPick, say }: { d: Extract<Destinations, { status: "ok" }>; name: string; chosen: string; locked: boolean; onPick: (id: string) => void; say: HealthSay }) {
  if (d.machines.length === 0) return <p className="adm-stale" role="status">{say("iv_destNone")}</p>
  return (
    <fieldset className="adh-iv-hooks" disabled={locked}>
      <legend className="adm-fine">{say("iv_destLegend")}</legend>
      {d.machines.map((m) => <MachineRow key={m.machine_id} m={m} name={name} checked={chosen === m.machine_id} onPick={onPick} say={say} />)}
      {!d.machines.some((m) => m.state === "ready") ? <p className="adm-stale">{say("iv_destNoneReady")}</p> : null}
      {!d.complete ? <p className="adm-stale" role="note">{say("iv_destIncomplete")}</p> : null}
    </fieldset>
  )
}

function MachineRow({ m, name, checked, onPick, say }: { m: Destination; name: string; checked: boolean; onPick: (id: string) => void; say: HealthSay }) {
  const ready = m.state === "ready"
  const next = m.state === "unknown" ? say("iv_destNext_unknown", { raw: m.raw ?? "—" }) : say(`iv_destNext_${m.state}` as HealthWord)
  return (
    <div className={`adh-iv-hook${ready ? "" : " adh-iv-hook-off"}`}>
      <label className="adh-iv-hook-pick">
        <input type="radio" name={name} value={m.machine_id} checked={checked} disabled={!ready} aria-describedby={`${name}-${m.machine_id}`} onChange={() => onPick(m.machine_id)} />
        <span className="adh-iv-hook-name">{m.machine_name}</span>
        <span className={ready ? "adm-fine" : "adm-stale"}>{say(`iv_dest_${m.state}` as HealthWord)}</span>
      </label>
      <p className="adm-fine" id={`${name}-${m.machine_id}`}>{next}</p>
      <details className="adh-disclose">
        <summary>{say("iv_destTech")}</summary>
        <dl className="adh-facts">
          <dt>{say("iv_destMachineId")}</dt><dd><Id v={m.machine_id} /></dd>
          <dt>{say("iv_destHookId")}</dt><dd><Id v={m.hook_id} /></dd>
          <dt>{say("iv_destReason")}</dt><dd><Id v={m.reason || null} /></dd>
        </dl>
      </details>
    </div>
  )
}

function refusalWord(say: HealthSay, code: string): string {
  return (START_REFUSALS as readonly string[]).includes(code) ? say(`iv_ref_${code}` as HealthWord) : say(`st_fail_${code}` as HealthWord)
}

function Start({ incidentId, revision, account, gate, say, actions }: { incidentId: string; revision: number; account: string | null; gate: StartGate; say: HealthSay; actions: InvestigationActions }) {
  const [st, setSt] = useState<StartState>({ kind: "closed" })
  const [dest, setDest] = useState<DestState>({ kind: "loading" })
  // The chosen machine is kept for this page only; nothing is stored or created.
  const [machineId, setMachineId] = useState("")
  const ctl = useRef<AbortController | null>(null)
  useEffect(() => () => { ctl.current?.abort() }, [])

  // Nothing is chosen for the operator, not even a sole ready machine: the choice is theirs to make.
  const loadDestinations = () => {
    ctl.current?.abort()
    const mine = new AbortController()
    ctl.current = mine
    setDest({ kind: "loading" })
    setMachineId("")
    void fetchDestinations(actions.get, actions.apiOrigin, mine.signal).then((r) => {
      if (mine.signal.aborted) return
      if ("refused" in r) { actions.refuse(r.refused); return }
      if (!r.ok) { setDest(r.code === "not_supported" ? { kind: "unsupported" } : { kind: "failed", code: r.code }); return }
      setDest({ kind: "ready", destinations: r.destinations })
    })
  }

  const send = (pending: Pending) => {
    const mine = new AbortController()
    ctl.current = mine
    setSt({ kind: "busy", pending })
    void startInvestigation(actions.get, actions.apiOrigin, incidentId, account, pending.req, pending.key, mine.signal).then((r) => {
      if (mine.signal.aborted) return
      if ("refused" in r) { actions.refuse(r.refused); return }
      if (r.ok) { setSt({ kind: "done", replayed: r.replayed }); actions.refresh(); return }
      if (!r.definite) { setSt({ kind: "ambiguous", pending, code: r.code }); return }
      setSt({ kind: "refused", code: r.code })
      // Another live investigation, or a moved revision: read the incident again to show what Cloud holds.
      if (r.code === "investigation_open" || r.code === "incident_revision_moved" || r.code === "investigation_limit") actions.refresh()
    })
  }

  if (gate !== "ok" && st.kind !== "busy" && st.kind !== "ambiguous") {
    return <p className="adm-fine" role="note">{say(`iv_gate_${gate}` as HealthWord)}</p>
  }
  if (st.kind === "closed" || st.kind === "done" || st.kind === "refused") {
    return (
      <div className="adm-notice-actions">
        <button type="button" className="adm-btn adm-btn-small" onClick={() => { setSt({ kind: "picking" }); loadDestinations() }}>{say("iv_start")}</button>
        <span role="status" className={st.kind === "refused" ? "adm-stale" : "adm-fine"}>
          {st.kind === "done" ? say(st.replayed ? "iv_replayed" : "iv_startedOk") : st.kind === "refused" ? <>{say("iv_refused", { reason: refusalWord(say, st.code) })} <span className="adm-code">({st.code})</span></> : ""}
        </span>
      </div>
    )
  }
  const locked = st.kind === "busy" || st.kind === "ambiguous"
  const req = dest.kind === "ready" ? startRequestFor(dest.destinations, machineId, revision) : null
  const retry = <button type="button" className="adm-btn adm-btn-small adm-btn-quiet" onClick={loadDestinations}>{say("tryAgain")}</button>
  return (
    <div className="adm-notice adh-iv-start" role="group" aria-label={say("iv_pickTitle")}>
      <p><strong>{say("iv_pickTitle")}</strong></p>
      <p className="adm-fine">{say("iv_bindNote")}</p>
      {dest.kind === "loading" ? <p className="adm-fine" role="status">{say("iv_destLoading")}</p>
        : dest.kind === "unsupported" ? <p className="adm-fine" role="note">{say("iv_destUnsupported")}</p>
          : dest.kind === "failed" ? <p className="adm-stale" role="status">{say("iv_destFailed", { reason: say(`st_fail_${dest.code}` as HealthWord) })} {retry}</p>
            : dest.destinations.status === "unavailable" ? <p className="adm-stale" role="status">{say("iv_destUnavailable")} {retry}</p>
              : <MachineList d={dest.destinations} name={`iv-machine-${incidentId}`} chosen={locked ? st.pending.machine : machineId} locked={locked} onPick={setMachineId} say={say} />}
      <p className="adm-fine">{say("iv_frozenNote", { n: revision })}</p>
      <div className="adm-notice-actions">
        {st.kind === "ambiguous" ? (
          <button type="button" className="adm-btn adm-btn-small" onClick={() => send(st.pending)}>{say("iv_retry")}</button>
        ) : (
          <button type="button" className="adm-btn adm-btn-small" disabled={req === null || st.kind === "busy"} aria-busy={st.kind === "busy"}
            onClick={() => { if (req !== null) send({ key: newIdempotencyKey(), req, machine: machineId }) }}>
            {st.kind === "busy" ? say("iv_sending") : say("iv_send")}
          </button>
        )}
        {!locked ? <button type="button" className="adm-btn adm-btn-small adm-btn-quiet" onClick={() => { ctl.current?.abort(); setSt({ kind: "closed" }) }}>{say("inc_cancel")}</button> : null}
      </div>
      <div role="status" aria-live="polite">
        {st.kind === "ambiguous" ? <p className="adm-stale">{say("iv_ambiguous")} <span className="adm-code">({st.code})</span></p> : null}
      </div>
    </div>
  )
}

/** The investigation card. `key` it by incident and account so a pending key never crosses scopes. */
export function InvestigationPanel({ incidentId, revision, latest, expired, investigations, account, say, actions }: {
  incidentId: string; revision: number; latest: boolean; expired: boolean; investigations: Investigations; account: string | null; say: HealthSay; actions: InvestigationActions
}) {
  const gate = startGate(investigations, latest, expired)
  return (
    <Panel title={say("iv_title")}>
      {investigations.status === "unsupported" ? <p className="adm-fine" role="note">{say("iv_unsupported")}</p>
        : investigations.status === "unavailable" ? <p className="adm-stale" role="status">{say("iv_unavailable")}</p>
          : investigations.items.length === 0 ? <p className="adm-fine">{investigations.unreadable > 0 ? say("iv_onlyUnreadable", { n: investigations.unreadable }) : say("iv_none")}</p> : (
            <>
              <Latest inv={investigations.items[0]} say={say} />
              {investigations.unreadable > 0 ? <p className="adm-stale">{say("iv_unreadable", { n: investigations.unreadable })}</p> : null}
              <Earlier items={investigations.items.slice(1)} say={say} />
            </>
          )}
      <p className="adm-fine">{say("iv_separateNote")}</p>
      <Start incidentId={incidentId} revision={revision} account={account} gate={gate} say={say} actions={actions} />
    </Panel>
  )
}
