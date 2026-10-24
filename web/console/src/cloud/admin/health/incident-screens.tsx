/* M05: durable incidents from the Cloud (private contract §11), as list and as one immutable
 * revision, with the Cloud's audited export and the operator's acknowledgement.
 *
 * What a revision proves is kept apart from what it lacks: facts and context are the frozen
 * records; gaps, producer loss and refused facts are listed as missing; the cause is "not
 * derived" and there are no hypotheses unless Cloud derives one (it does not, and the adapter
 * refuses one). Recovery evidence and human acknowledgement are separate panels: closed means a
 * person reviewed it, never that it is healthy. Exported bytes live only inside the panel of the
 * revision they belong to, so a changed account, incident or revision drops them. An agent
 * investigation is its own card after the summary (investigation-view.tsx). */
import { useEffect, useRef, useState } from "react"
import type { BundleFact, IncidentDetailBody, IncidentListBody, IncidentRow, IncidentState } from "./stored"
import type { Refusal, StoredState } from "./cloud"
import { heldIncidents, HEALTH_UI_LIMITS, mayLoadOlderIncidents, partBody } from "./cloud"
import { acknowledgeIncident, fetchIncidentExport, type ExportFailure, type VerifiedExport } from "./incident-export"
import type { HealthRoute, HealthScreen } from "./route"
import type { HealthSay, HealthWord } from "./words"
import { healthMs } from "./words"
import { stageRole } from "./stored"
import { eventWords, finalObserved, readIncident, stageWords, type NextTarget } from "./incident-summary"
import { HTable, Panel, Unknown } from "./parts"
import { At, Id, Link, PartFailed, Stale, type StoredNav } from "./stored-screens"
import { InvestigationPanel } from "./investigation-view"

/** What the incident screens may do beyond reading: export and acknowledge, under the page's session. */
export type IncidentActions = {
  get: typeof fetch
  apiOrigin: string
  /** Reload this screen's reads (after an acknowledgement). */
  refresh: () => void
  /** A refused session: clear everything held and let the shell sign in again. */
  refuse: (r: Refusal) => void
  /** The next page of the incident list, under the same frozen window and filter. */
  loadOlderIncidents: () => void
}

const STATE_GLYPH: Record<IncidentState, string> = { active: "■", recovering: "◇", recovered_awaiting_review: "✓", closed: "◼" }

export function IncidentBadge({ state, say }: { state: IncidentState | null; say: HealthSay }) {
  if (state === null) return <span className="adh-badge adh-inc-expired"><span aria-hidden="true" className="adh-glyph">⌛</span>{say("inc_state_expired")}</span>
  return (
    <span className={`adh-badge adh-inc-${state}`}>
      <span aria-hidden="true" className="adh-glyph">{STATE_GLYPH[state]}</span>
      {say(`inc_state_${state}` as HealthWord)}
    </span>
  )
}

function groupCell(row: { group: IncidentRow["group"] }, say: HealthSay, nav: StoredNav) {
  if (row.group.kind === "loss") return <span>{say("inc_groupLoss")}</span>
  if (row.group.correlation) return <Link to={{ screen: "M04", trace: row.group.correlation }} nav={nav}><Id v={row.group.correlation} /></Link>
  return <span className="adm-unknown">{row.group.phase_id === null ? "—" : say("st_phase", { id: row.group.phase_id })}</span>
}

/** The incident rows of one list read; used by M05, and narrowed by M01 and M04. */
export function IncidentTable({ rows, say, nav, caption }: { rows: IncidentRow[]; say: HealthSay; nav: StoredNav; caption: string }) {
  return (
    <HTable caption={caption}
      columns={[{ label: say("inc_colState") }, { label: say("inc_colIncident") }, { label: say("inc_colTrigger") }, { label: say("st_colMachine") }, { label: say("st_colSource") }, { label: say("inc_colGroup") }, { label: say("inc_colRevision"), num: true }, { label: say("inc_colFacts"), num: true }, { label: say("inc_colOpened") }, { label: say("inc_colRetain") }]}
      rows={rows.map((r) => ({
        key: r.id,
        head: <IncidentBadge state={r.state} say={say} />,
        cells: [
          <Link key="i" to={{ screen: "M05", incident: r.id }} nav={nav}><Id v={r.id} /></Link>,
          say(`inc_trigger_${r.trigger}` as HealthWord),
          <Link key="m" to={{ screen: "M02", machine: r.machine_id }} nav={nav}><Id v={r.machine_id} /></Link>,
          `${r.source} · ${r.producer_instance}`,
          <span key="g">{groupCell(r, say, nav)}</span>,
          String(r.revision),
          r.facts === null ? <Unknown key="f">{say("unknown")}</Unknown> : r.facts_refused > 0 ? say("inc_factsRefused", { n: r.facts, refused: r.facts_refused }) : String(r.facts),
          <At key="o" iso={r.opened_at} />,
          <span key="r"><At iso={r.retain_until} />{r.expired ? ` · ${say("inc_state_expired")}` : ""}</span>,
        ],
      }))} />
  )
}

/** Coverage words for a list read: off is not "none", and a refused incident is not absent. */
export function IncidentCoverage({ body, say }: { body: IncidentListBody; say: HealthSay }) {
  const off = body.source_coverage.incidents === "disabled"
  return (
    <>
      {off ? <p className="adm-stale" role="status">{say("inc_disabled")}</p> : null}
      <p className="adm-fine">{body.window ? say("inc_window", { from: body.window.from.slice(0, 10), to: body.window.to.slice(0, 10) }) : null} {say("inc_ruleNote", { rule: body.rule_version ?? "—", state: body.state_rule })}</p>
      {body.next_cursor !== null ? <p className="adm-fine">{say("inc_moreRows", { n: body.incidents.length })}</p> : null}
      {body.refusals.rows.length > 0 ? (
        <p className="adm-stale">{say("inc_refused", { n: body.refusals.rows.reduce((a, r) => a + r.count, 0), reasons: [...new Set(body.refusals.rows.map((r) => r.reason))].join(", ") })}{body.refusals.truncated ? ` ${say("inc_refusedMore")}` : ""}</p>
      ) : null}
      <p className="adm-fine">{say("inc_noAggregates")}</p>
    </>
  )
}

function IncidentList({ state, say, nav, pinned, loadOlder }: { state: StoredState; say: HealthSay; nav: StoredNav; pinned: boolean; loadOlder: () => void }) {
  const p = state.parts.incidents
  if (!p) return null
  if (p.status === "failed") return <PartFailed name="incidents" code={p.code} say={say} />
  const body = p.body as IncidentListBody
  const off = body.source_coverage.incidents === "disabled"
  const rows = heldIncidents(state)
  const older = mayLoadOlderIncidents(state, pinned)
  return (
    <Panel title={say("st_incidentsTitle")}>
      <Stale state={state} name="incidents" say={say} />
      <IncidentCoverage body={body} say={say} />
      {rows.length === 0
        ? <p className="adm-fine">{off ? say("inc_noneDisabled") : say("inc_none")}</p>
        : <IncidentTable rows={rows} say={say} nav={nav} caption={say("st_incidentsTitle")} />}
      {state.incOlder.length > 0 || older !== "none" ? <p className="adm-fine">{say(state.incCursor === null ? "inc_heldAll" : "inc_held", { n: rows.length, pages: 1 + state.incOlder.length })}</p> : null}
      {older === "yes" ? (
        <button type="button" className="adm-btn adm-btn-small" onClick={loadOlder} disabled={state.incBusy} aria-busy={state.incBusy}>
          {state.incBusy ? say("inc_loadingOlder") : say("inc_loadOlder")}
        </button>
      ) : older === "live" ? <p className="adm-fine">{say("inc_pauseToPage")}</p> : older === "bound" ? <p className="adm-fine">{say("inc_pageBound", { n: HEALTH_UI_LIMITS.incidentPages * HEALTH_UI_LIMITS.incidentRows })}</p> : null}
      {state.incFailure ? <p className="adm-stale" role="status">{say("inc_olderFailed", { reason: say(`st_fail_${state.incFailure}` as HealthWord) })}</p> : null}
      <p className="adm-fine">{say("inc_closedNote")}</p>
    </Panel>
  )
}

const capital = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

/** One card per frozen record: the event and stage in words first, every exact field under a
 *  disclosure. `ended` is false when nothing shows the operation finishing or recovering; the
 *  newest staged record then says so, because a completed stage is not a final response. */
function FactCards({ facts, say, nav, caption, added, ended = true }: { facts: BundleFact[]; say: HealthSay; nav: StoredNav; caption: string; added?: Set<string>; ended?: boolean }) {
  const newestStaged = [...facts].sort((a, b) => b.producer_seq - a.producer_seq).find((f) => f.stage !== null)?.id ?? null
  return (
    <ol className="adh-fcards" aria-label={caption}>
      {facts.map((f) => (
        <li key={f.id} className="adh-fcard">
          <p className="adh-fcard-head">
            <strong>{say("inc_card_seq", { seq: f.producer_seq })}</strong>
            {added?.has(f.id) ? <span className="adh-chip">{say("inc_added")}</span> : null}
          </p>
          <p className="adh-fcard-what">{eventWords(say, f.event)}</p>
          <dl className="adh-fcard-facts">
            {f.stage !== null ? (
              <div><dt>{say("inc_card_stage")}</dt><dd>{capital(stageWords(say, f.stage))}.{!ended && f.id === newestStaged && stageRole(f.stage) !== "final" ? ` ${say("inc_miss_noEndAfter")}` : ""}</dd></div>
            ) : null}
            {f.outcome !== null ? (
              <div><dt>{say("inc_card_result")}</dt><dd>{say(`st_out_${f.outcome}` as HealthWord)}{f.outcome === "pending" ? <span className="adh-fcard-note">{say("inc_out_pendingNote")}</span> : null}</dd></div>
            ) : null}
            {f.state !== null ? <div><dt>{say("inc_card_result")}</dt><dd>{say(`st_wd_${f.state}` as HealthWord)}</dd></div> : null}
            {f.elapsed_ms !== null ? <div><dt>{say("inc_card_measured")}</dt><dd>{healthMs(say, f.elapsed_ms)}</dd></div> : null}
            <div><dt>{say("inc_card_received")}</dt><dd><At iso={f.received_at} /></dd></div>
          </dl>
          <details className="adh-disclose">
            <summary>{say("inc_card_raw")}</summary>
            <dl className="adh-facts">
              <dt>{say("st_colSeq")}</dt><dd>{f.producer_seq}</dd>
              <dt>{say("inc_card_event")}</dt><dd><code className="adh-id">{f.event}</code></dd>
              <dt>{say("st_colStage")}</dt><dd><Id v={f.stage} /></dd>
              <dt>{say("st_colResult")}</dt><dd><Id v={f.outcome ?? f.state} /></dd>
              <dt>{say("inc_card_class")}</dt><dd><Id v={f.class} /></dd>
              <dt>{say("inc_card_kind")}</dt><dd><Id v={f.kind} /></dd>
              <dt>{say("inc_card_rule")}</dt><dd><Id v={f.rule} /></dd>
              <dt>{say("inc_card_measured")}</dt><dd>{f.elapsed_ms === null ? <span className="adm-unknown">—</span> : say("ms", { n: f.elapsed_ms })}</dd>
              <dt>{say("st_colBuild")}</dt><dd><Id v={f.build_commit} /> {f.build_commit ? <span className="adm-fine">{say("st_producerDeclared")}</span> : null}</dd>
              <dt>{say("st_colTrace")}</dt><dd>{f.correlation ? <Link to={{ screen: "M04", trace: f.correlation }} nav={nav}><Id v={f.correlation} /></Link> : <span className="adm-unknown">—</span>}</dd>
              <dt>{say("st_colReceived")}</dt><dd><code className="adh-id">{f.received_at}</code></dd>
              <dt>{say("st_colWall")}</dt><dd><code className="adh-id">{f.wall}</code></dd>
              <dt>{say("inc_card_boot")}</dt><dd><Id v={f.boot_id} /></dd>
              <dt>{say("inc_card_eventId")}</dt><dd><Id v={f.event_id} /></dd>
              <dt>{say("inc_card_id")}</dt><dd><Id v={f.id} /></dd>
              <dt>{say("inc_card_digest")}</dt><dd><Id v={f.digest} /></dd>
            </dl>
          </details>
        </li>
      ))}
    </ol>
  )
}

/** The first thing a person reads: derived from the same stored fields as the details below. */
function SummaryPanel({ d, say, nav }: { d: IncidentDetailBody; say: HealthSay; nav: StoredNav }) {
  const b = d.bundle
  const s = b.summary
  const latest = d.revision.number === d.incident.revision
  const r = readIncident({ summary: s, manifest: b.manifest, facts: b.facts, asOf: d.as_of }, say)
  const correlation = b.manifest.scope.group.correlation
  const target = (to: NextTarget): HealthScreen | null =>
    to === "trace" && correlation ? { screen: "M04", trace: correlation }
      : to === "machine" ? { screen: "M02", machine: b.manifest.scope.machine_id }
        : to === "monitoring" ? { screen: "M06" }
          : null
  const section = (title: HealthWord, lines: string[]) => (
    <section className="adh-sum-sec">
      <h3 className="adh-sum-h">{say(title)}</h3>
      <ul className="adh-sum-list">{lines.map((l, i) => <li key={i}>{l}</li>)}</ul>
    </section>
  )
  return (
    <Panel title={say("inc_summaryTitle")}>
      <p className="adh-badges"><IncidentBadge state={s.state} say={say} /> <span className="adh-chip">{say(`inc_trigger_${s.trigger}` as HealthWord)}</span> <span className="adh-chip">{say("inc_revisionsLabel", { n: d.revision.number, of: d.incident.revision })}</span></p>
      {!latest ? <p className="adm-stale" role="note">{say("inc_older", { n: d.revision.number, of: d.incident.revision })} <Link to={{ screen: "M05", incident: d.incident.id }} nav={nav}>{say("inc_toLatest")}</Link></p> : null}
      <p className="adh-inc-lead">{r.headline}</p>
      <div className="adh-sum">
        {section("inc_sum_observedTitle", r.observed)}
        {section("inc_sum_nowTitle", r.now)}
        {section("inc_sum_missingTitle", r.missing)}
        <section className="adh-sum-sec">
          <h3 className="adh-sum-h">{say("inc_sum_nextTitle")}</h3>
          <ul className="adh-sum-list">
            {r.next.map((n, i) => {
              const to = target(n.to)
              return <li key={i}>{to ? <Link to={to} nav={nav}>{n.text}</Link> : n.text}</li>
            })}
          </ul>
        </section>
      </div>
    </Panel>
  )
}

function exportFailureWord(say: HealthSay, code: ExportFailure): string {
  switch (code) {
    case "export_too_large": case "hash_mismatch": case "wrong_incident": case "not_json":
      return say(`inc_exp_${code}` as HealthWord)
    default:
      return say(`st_fail_${code}` as HealthWord)
  }
}

type ExportState =
  | { kind: "idle" }
  | { kind: "busy" }
  | { kind: "ready"; value: VerifiedExport }
  | { kind: "failed"; code: ExportFailure; expiredAt: string | null }

/** The Cloud's export of this revision. Its bytes are held only here, verified, and dropped when
 *  this panel goes (a changed incident, revision or account remounts it). */
function ExportPanel({ incidentId, revision, account, say, actions }: { incidentId: string; revision: number; account: string | null; say: HealthSay; actions: IncidentActions }) {
  const [st, setSt] = useState<ExportState>({ kind: "idle" })
  const [copy, setCopy] = useState<"idle" | "done" | "failed">("idle")
  const ctl = useRef<AbortController | null>(null)
  useEffect(() => () => { ctl.current?.abort(); ctl.current = null }, [])

  const run = () => {
    ctl.current?.abort()
    const mine = new AbortController()
    ctl.current = mine
    setSt({ kind: "busy" })
    setCopy("idle")
    void fetchIncidentExport(actions.get, actions.apiOrigin, incidentId, revision, account, mine.signal).then((r) => {
      if (mine.signal.aborted || ctl.current !== mine) return
      ctl.current = null
      if ("refused" in r) { setSt({ kind: "idle" }); actions.refuse(r.refused); return }
      setSt(r.ok ? { kind: "ready", value: r.value } : { kind: "failed", code: r.code, expiredAt: r.expiredAt ?? null })
    })
  }

  const download = (v: VerifiedExport) => {
    const url = URL.createObjectURL(new Blob([v.text], { type: "application/json" }))
    const a = document.createElement("a")
    a.href = url
    a.download = v.filename
    a.click()
    setTimeout(() => URL.revokeObjectURL(url), 0)
  }

  return (
    <Panel title={say("inc_exportTitle")}>
      <p className="adm-fine">{say("inc_exportNote")}</p>
      <div className="adm-notice-actions">
        <button type="button" className="adm-btn adm-btn-small" onClick={run} disabled={st.kind === "busy"} aria-busy={st.kind === "busy"}>
          {st.kind === "busy" ? say("inc_exporting") : st.kind === "ready" ? say("inc_exportAgain") : say("inc_export")}
        </button>
      </div>
      <div role="status" aria-live="polite">
        {st.kind === "failed" ? (
          <p className="adm-stale">{say("inc_exportFailed", { reason: exportFailureWord(say, st.code) })}{st.code === "incident_expired" && st.expiredAt ? ` ${say("inc_expiredAt", { time: st.expiredAt })}` : ""} <span className="adm-code">({st.code})</span></p>
        ) : st.kind === "ready" ? (
          <p className="adh-hash">{say("inc_exportVerified", { hash: st.value.contentHash, kib: Math.ceil(st.value.bytes / 1024) })}</p>
        ) : null}
      </div>
      {st.kind === "ready" ? (
        <>
          <dl className="adh-facts">
            <dt>{say("inc_file")}</dt><dd><code className="adh-id">{st.value.filename}</code></dd>
            <dt>{say("inc_exportedAt")}</dt><dd><At iso={st.value.exportedAt} /></dd>
            <dt>{say("inc_expiresAt")}</dt><dd><At iso={st.value.expiresAt} /></dd>
            <dt>{say("inc_colRevision")}</dt><dd>{say("inc_revOf", { n: st.value.revision, of: st.value.latestRevision })}</dd>
          </dl>
          <div className="adm-notice-actions">
            <button type="button" className="adm-btn adm-btn-small" onClick={() => download(st.value)}>{say("inc_download")}</button>
            <button type="button" className="adm-btn adm-btn-small adm-btn-quiet" onClick={() => {
              if (!navigator.clipboard) { setCopy("failed"); return }
              void navigator.clipboard.writeText(st.value.text).then(() => setCopy("done"), () => setCopy("failed"))
            }}>{say("inc_copy")}</button>
            <span role="status" className="adm-fine">{copy === "done" ? say("st_copied") : copy === "failed" ? say("st_copyFailed") : ""}</span>
          </div>
        </>
      ) : null}
    </Panel>
  )
}

type AckState = { kind: "idle" } | { kind: "confirm" } | { kind: "busy" } | { kind: "done"; revised: boolean } | { kind: "failed"; code: string }

/** Human review: an explicit two-step act. It writes a closing revision and never claims recovery. */
function AckPanel({ detail, account, say, actions, onDone }: { detail: IncidentDetailBody; account: string | null; say: HealthSay; actions: IncidentActions; onDone: () => void }) {
  const [st, setSt] = useState<AckState>({ kind: "idle" })
  const ctl = useRef<AbortController | null>(null)
  useEffect(() => () => { ctl.current?.abort() }, [])
  const s = detail.bundle.summary
  const latest = detail.revision.number === detail.incident.revision
  const send = () => {
    const mine = new AbortController()
    ctl.current = mine
    setSt({ kind: "busy" })
    void acknowledgeIncident(actions.get, actions.apiOrigin, detail.incident.id, account, mine.signal).then((r) => {
      if (mine.signal.aborted) return
      if ("refused" in r) { actions.refuse(r.refused); return }
      if (r.ok) { setSt({ kind: "done", revised: r.revised }); onDone() } else setSt({ kind: "failed", code: r.code })
    })
  }
  return (
    <Panel title={say("inc_ackTitle")}>
      {s.acknowledgement ? (
        <dl className="adh-facts">
          <dt>{say("inc_ackAt")}</dt><dd><At iso={s.acknowledgement.at} /></dd>
          <dt>{say("inc_ackThrough")}</dt><dd>{s.acknowledgement.through_seq}</dd>
          <dt>{say("inc_closure")}</dt><dd>{s.closure ? say(`inc_closure_${s.closure}` as HealthWord) : <Unknown>{say("unknown")}</Unknown>}</dd>
        </dl>
      ) : <p className="adm-fine">{say("inc_notAcked")}</p>}
      <p className="adm-fine">{say("inc_ackMeaning")}</p>
      {s.state !== "closed" && latest ? (
        st.kind === "idle" || st.kind === "failed" ? (
          <div className="adm-notice-actions">
            <button type="button" className="adm-btn adm-btn-small" onClick={() => setSt({ kind: "confirm" })}>{say("inc_ack")}</button>
            {st.kind === "failed" ? <span role="status" className="adm-stale">{say("inc_ackFailed", { reason: say(`st_fail_${st.code}` as HealthWord) })} <span className="adm-code">({st.code})</span></span> : null}
          </div>
        ) : st.kind === "confirm" || st.kind === "busy" ? (
          <div className="adm-notice" role="group" aria-label={say("inc_ackTitle")}>
            <p>{say(s.recovery.observed ? "inc_ackConfirmObserved" : "inc_ackConfirmUnobserved", { n: detail.incident.revision + 1 })}</p>
            <div className="adm-notice-actions">
              <button type="button" className="adm-btn adm-btn-small" onClick={send} disabled={st.kind === "busy"} aria-busy={st.kind === "busy"}>{st.kind === "busy" ? say("inc_acking") : say("inc_ackConfirm")}</button>
              <button type="button" className="adm-btn adm-btn-small adm-btn-quiet" onClick={() => setSt({ kind: "idle" })} disabled={st.kind === "busy"}>{say("inc_cancel")}</button>
            </div>
          </div>
        ) : <p role="status">{st.revised ? say("inc_acked") : say("inc_ackNoChange")}</p>
      ) : !latest ? <p className="adm-fine">{say("inc_ackOnLatest")}</p> : null}
    </Panel>
  )
}

function IncidentDetail({ state, route, say, nav, actions }: { state: StoredState; route: HealthRoute; say: HealthSay; nav: StoredNav; actions: IncidentActions }) {
  const incident = route.screen === "M05" ? route.incident! : ""
  const asked = route.screen === "M05" ? route.revision ?? null : null
  const p = state.parts.incident
  const back = <p className="adm-fine"><Link to={{ screen: "M05", incident: null }} nav={nav}>{say("inc_all")}</Link> · <Id v={incident} /></p>
  if (!p) return back
  if (p.status === "failed") {
    return (
      <div className="adh-screen">
        {back}
        {p.code === "incident_expired" ? (
          <div className="adm-notice" role="status">
            <p><strong>{say("inc_expiredTitle")}</strong></p>
            <p>{say("inc_expiredBody")}{p.expiredAt ? ` ${say("inc_expiredAt", { time: p.expiredAt })}` : ""}</p>
            <p className="adm-code">{say("failedCode", { code: p.code })}</p>
          </div>
        ) : <PartFailed name="incident" code={p.code} say={say} />}
        {p.code === "revision_not_found" && asked !== null ? <p><Link to={{ screen: "M05", incident }} nav={nav}>{say("inc_toLatest")}</Link></p> : null}
      </div>
    )
  }
  const d = p.body as IncidentDetailBody
  const b = d.bundle
  const m = b.manifest
  const s = b.summary
  const latest = d.revision.number === d.incident.revision
  const added = new Set(m.provenance.added.map((x) => x.id))
  const late = m.provenance.added.length > 0 && m.provenance.late
  const ended = finalObserved(b.facts) || s.recovery.observed
  return (
    <div className="adh-screen">
      {back}
      <Stale state={state} name="incident" say={say} />
      <SummaryPanel d={d} say={say} nav={nav} />
      <InvestigationPanel key={`${d.incident.id}|${route.scope.account ?? ""}`} incidentId={d.incident.id} revision={d.incident.revision} latest={latest} expired={d.incident.expired}
        investigations={d.investigations} account={route.scope.account} say={say} actions={actions} />

      <div className="adh-pair">
        <Panel title={say("inc_recoveryTitle")}>
          <p>{s.recovery.observed ? say("inc_recoveryObserved") : say("inc_recoveryNot")}</p>
          {s.recovery.event ? <p className="adm-fine">{say("inc_recoveryEvent", { event: eventWords(say, s.recovery.event) })} <At iso={s.recovery.received_at} /></p> : null}
          {s.recovery.unverified_reason ? <p className="adm-stale">{say(`inc_why_${s.recovery.unverified_reason}` as HealthWord)}</p> : null}
          <p className="adm-fine">{say("inc_noTimeRule")}</p>
        </Panel>
        <AckPanel detail={d} account={route.scope.account} say={say} actions={actions} onDone={actions.refresh} key={`${d.incident.id}|${d.revision.number}|${route.scope.account ?? ""}`} />
      </div>

      <Panel title={say("inc_evidenceTitle")}>
        <p className="adm-fine">{say("inc_evidenceNote")}</p>
        <FactCards facts={b.facts} say={say} nav={nav} caption={say("inc_factsCaption", { n: b.facts.length })} added={added} ended={ended} />
      </Panel>

      <Panel title={say("inc_hypothesesTitle")}>
        <p>{say("inc_rootCause")}</p>
        <p className="adm-fine">{say("inc_noHypotheses")}</p>
      </Panel>

      <Panel title={say("inc_contextTitle")}>
        {b.context.length === 0 ? <p className="adm-fine">{say("inc_noContext")}</p> : (
          <details className="adh-disclose">
            <summary>{say("inc_contextShow", { n: b.context.length })}</summary>
            <p className="adm-fine">{say("inc_contextNote")}</p>
            <FactCards facts={b.context} say={say} nav={nav} caption={say("inc_contextCaption", { n: b.context.length })} />
          </details>
        )}
      </Panel>

      <section className="adm-panel adh-panel" aria-label={say("inc_techTitle")}>
        <details className="adh-disclose adh-tech">
          <summary><span className="adm-h3">{say("inc_techTitle")}</span></summary>
          <p className="adm-fine">{say("inc_techNote")}</p>

          <h3 className="adh-sum-h">{say("inc_revisionTitle")}</h3>
          {s.state === "closed" ? <p className="adm-fine">{say("inc_closedNote")}</p> : null}
          <nav aria-label={say("inc_revisions")} className="adh-verdict-links">
            {d.incident.revisions.map((n) => n === d.revision.number
              ? <span key={n} aria-current="page" className="adh-chip">{say("inc_rev", { n })}</span>
              : <Link key={n} to={{ screen: "M05", incident: d.incident.id, revision: n === d.incident.revision ? null : n }} nav={nav}>{say("inc_rev", { n })}</Link>)}
          </nav>
          <dl className="adh-facts">
            <dt>{say("inc_hash")}</dt><dd><code className="adh-id adh-hash">{d.revision.content_hash}</code></dd>
            <dt>{say("inc_created")}</dt><dd><At iso={d.revision.created_at} /></dd>
            <dt>{say("inc_bytes")}</dt><dd>{say("inc_kib", { kib: Math.ceil(d.revision.bytes / 1024) })}</dd>
            <dt>{say("inc_colOpened")}</dt><dd><At iso={s.opened_at} /></dd>
            <dt>{say("inc_reason")}</dt><dd>{say(`inc_prov_${m.provenance.reason}` as HealthWord)}{late ? ` · ${say("inc_late")}` : ""}</dd>
            <dt>{say("inc_previous")}</dt>
            <dd>{m.provenance.previous_revision === null ? say("inc_noPrevious") : (
              <><Link to={{ screen: "M05", incident: d.incident.id, revision: m.provenance.previous_revision }} nav={nav}>{say("inc_rev", { n: m.provenance.previous_revision })}</Link> <code className="adh-id adh-hash">{m.provenance.previous_content_hash ?? "—"}</code></>
            )}</dd>
            <dt>{say("inc_colRetain")}</dt><dd><At iso={m.retention.bundle_retain_until} /> <span className="adm-fine">{say("inc_frozen")}</span></dd>
            <dt>{say("inc_rawRetain")}</dt><dd><At iso={m.retention.raw_retain_until_earliest} /></dd>
          </dl>

          <h3 className="adh-sum-h">{say("inc_knownTitle")}</h3>
          <dl className="adh-facts">
            <dt>{say("inc_lastProven")}</dt><dd>{s.stages.last_proven ? <><code className="adh-id">{s.stages.last_proven.stage}</code> · {say("inc_card_basis")}: {say(`inc_basis_${s.stages.last_proven.basis}` as HealthWord)}</> : <Unknown>{say("inc_noneProven")}</Unknown>}</dd>
            <dt>{say("inc_activeStage")}</dt><dd>{s.stages.active ? <code className="adh-id">{s.stages.active.stage}</code> : <span className="adm-unknown">—</span>}</dd>
            <dt>{say("inc_nextStage")}</dt><dd><Unknown>{say("unknown")}</Unknown></dd>
          </dl>

          <h3 className="adh-sum-h">{say("inc_missingTitle")}</h3>
          <dl className="adh-facts">
            <dt>{say("st_sequence")}</dt><dd>{say(`inc_exact_${m.sequence.exactness}` as HealthWord)}{m.sequence.reason ? ` (${m.sequence.reason})` : ""}</dd>
            <dt>{say("inc_present")}</dt><dd>{m.sequence.present.map((r) => (r.from === r.to ? String(r.from) : `${r.from}–${r.to}`)).join(", ") || "—"}</dd>
            <dt>{say("st_dropped")}</dt><dd>{say("st_droppedValue", { lost: m.dropped.transitions_lost ?? say("unknown"), overflow: m.dropped.overflow ?? say("unknown") })}</dd>
            <dt>{say("inc_refusedFacts")}</dt><dd>{m.dropped.facts_refused_by_bound}</dd>
            <dt>{say("inc_absent")}</dt><dd>{m.redaction.absent_fields.join(", ")} · {say("inc_ownerUnsupported")}</dd>
          </dl>
          {m.sequence.gaps.length === 0 ? <p className="adm-fine">{say("st_noGapsListed")}</p> : (
            <HTable caption={say("st_gapsTitle")} columns={[{ label: say("st_colSeq") }, { label: say("st_colCount"), num: true }, { label: say("st_colCause") }]}
              rows={m.sequence.gaps.map((g) => ({ key: `${g.from}-${g.to}`, head: `${g.from}–${g.to}`, cells: [g.count === null ? <Unknown key="c">{say("unknown")}</Unknown> : String(g.count), say(`st_cause_${g.cause}` as HealthWord)] }))} />
          )}
          <p className="adm-fine">{say("st_gapEdges")}</p>

          <h3 className="adh-sum-h">{say("inc_manifestTitle")}</h3>
          <dl className="adh-facts">
            <dt>{say("st_colMachine")}</dt><dd><Link to={{ screen: "M02", machine: m.scope.machine_id }} nav={nav}><Id v={m.scope.machine_id} /></Link></dd>
            <dt>{say("st_colSource")}</dt><dd>{m.scope.source} · {m.scope.producer_instance} <Id v={m.scope.source_id} /></dd>
            <dt>{say("st_colBoot")}</dt><dd><Id v={m.scope.boot_id} /> <span className="adm-fine">{m.sources.map((x) => say(`st_boot_${x.boot_order}` as HealthWord)).join(", ")}</span></dd>
            <dt>{say("inc_colGroup")}</dt><dd>{groupCell(m.scope, say, nav)}</dd>
            <dt>{say("st_colBuild")}</dt><dd>{m.build.commits.length === 0 ? <Unknown>{say("inc_noBuild")}</Unknown> : m.build.commits.map((c) => <Id key={c} v={c} />)} <span className="adm-fine">{say("st_producerDeclared")}</span></dd>
            <dt>{say("inc_windowLabel")}</dt><dd><At iso={m.window.from} /> – <At iso={m.window.to} /> <span className="adm-fine">{say("inc_receiveBasis")}</span></dd>
            <dt>{say("inc_clock")}</dt><dd>{m.clock.uncertainty_ms === null ? say("inc_clockUnknown") : say("ms", { n: m.clock.uncertainty_ms })} · {say("st_wallDisplayOnly")}</dd>
            <dt>{say("inc_rules")}</dt><dd>{m.rule_versions.grouping} · {m.rule_versions.state}</dd>
            <dt>{say("inc_redaction")}</dt><dd>{m.redaction.class}</dd>
            <dt>{say("inc_repair")}</dt><dd>{say("inc_repairNever")}</dd>
          </dl>
        </details>
      </section>
      <ExportPanel key={`${d.incident.id}|${d.revision.number}|${route.scope.account ?? ""}`} incidentId={d.incident.id} revision={d.revision.number} account={route.scope.account} say={say} actions={actions} />
    </div>
  )
}

export function M05({ state, route, say, nav, actions }: { state: StoredState; route: HealthRoute; say: HealthSay; nav: StoredNav; actions: IncidentActions }) {
  const incident = route.screen === "M05" ? route.incident : null
  if (incident !== null) return <IncidentDetail state={state} route={route} say={say} nav={nav} actions={actions} />
  return <div className="adh-screen"><IncidentList state={state} say={say} nav={nav} pinned={route.scope.until !== null} loadOlder={actions.loadOlderIncidents} /></div>
}

/** Open incidents on the page held (active, recovering, awaiting review): never "healthy" when none. */
export function openIncidents(state: StoredState): IncidentRow[] | null {
  const body = partBody(state, "incidents")
  return body === null ? null : body.incidents.filter((r) => r.state !== null && r.state !== "closed")
}
