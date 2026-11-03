import { useCallback, useEffect, useRef, useState } from "react"
import type { DoctorReport } from "@clawdline/contract"
import { catalogWord, currentCatalogTag } from "../catalog.js"
import { client } from "../client.js"
import { readCloudConfig } from "../cloud/copied.js"
import { onMachineChanged } from "../cloud/machine-switch.js"
import { useMachineVersion } from "../machine/NeedsUpdate.js"
import { supportCall, SupportFailure, requestKey } from "../support/api.js"
import { useSupportImages, SupportImagePicker } from "../support/images.js"
import "../support/support.css"
import { supportError } from "../support/words.js"
import type { Question } from "../support/model.js"
import { decodeReport, supportBody, DOCTOR_DESCRIPTION_CHARACTERS, DOCTOR_REPORT_BYTES } from "./model.js"
import "./doctor.css"

const w = (key: string) => catalogWord("doctor", key)
type Pending = { account: string; key: string; body: { title: string; body: string; topic: string; request_type: string; source_language: string; reply_locale: string; image_ids: string[] } }
const pendingKey = "clawdline-doctor-pending-v1"
function savedPending(): Pending | null {
  try { const p = JSON.parse(sessionStorage.getItem(pendingKey) ?? "null"); return p && typeof p.account === "string" && typeof p.key === "string" && typeof p.body?.body === "string" && p.body.body.startsWith("Clawdline Doctor\n") ? p : null } catch { return null }
}

export function DoctorBlock({ collect = true, apiOrigin }: { collect?: boolean; apiOrigin?: string }) {
  const [revision, change] = useState(0)
  useEffect(() => onMachineChanged(() => change(n => n + 1)), [])
  return <DoctorCheck key={revision} collect={collect} apiOrigin={apiOrigin} />
}
function DoctorCheck({ collect, apiOrigin }: { collect: boolean; apiOrigin?: string }) {
  const known = useMachineVersion()
  const [report, setReport] = useState<DoctorReport | null>(null), [busy, setBusy] = useState(false)
  const [error, setError] = useState(""), [description, setDescription] = useState("")
  const [consent, setConsent] = useState(false), [sent, setSent] = useState<string | null>(null)
  const pending = useRef<Pending | null>(savedPending()), [uncertain, setUncertain] = useState(!!pending.current)
  const alive = useRef(true), working = useRef(false), file = useRef<HTMLInputElement>(null)
  useEffect(() => { alive.current = true; return () => { alive.current = false } }, [])
  let origin = apiOrigin
  if (!origin) { try { origin = readCloudConfig(window as unknown as { __clawdlineCloud?: unknown })?.apiOrigin } catch { /* local reports can still be downloaded */ } }
  const imageCall = useCallback(async <T,>(path: string, method?: string, body?: unknown, key?: string): Promise<T> => {
    if (!origin) throw new SupportFailure("support_unavailable", true)
    return supportCall<T>(origin, path, method, body, key)
  }, [origin])
  const images = useSupportImages(imageCall, origin ?? "", "doctor-v1")
  const fail = (e: unknown) => setError(e instanceof SupportFailure ? supportError(e.code) : w("failed"))
  const run = async () => {
    if (working.current || images.busy || uncertain) return
    working.current = true; setBusy(true); setError(""); setSent(null); setReport(null); setConsent(false)
    try {
      const response = await fetch(client.url("/v1/doctor"), { cache: "no-store", signal: AbortSignal.timeout(10_000) })
      if (!response.ok) throw new Error("doctor_unavailable")
      const next = decodeReport(await response.text())
      const web = readCloudConfig(window as unknown as { __clawdlineCloud?: unknown })?.build
      if (web && /^[A-Za-z0-9][A-Za-z0-9.+_-]{0,95}$/.test(web)) next.web_version = web
      if (alive.current) setReport(next)
    } catch (e) { if (alive.current) fail(e) }
    finally { working.current = false; if (alive.current) setBusy(false) }
  }
  const importFile = async (selected: File | undefined) => {
    if (!selected || working.current || uncertain) return
    working.current = true; setBusy(true); setError(""); setSent(null); setConsent(false); setReport(null)
    try {
      if (selected.size > DOCTOR_REPORT_BYTES) throw new Error("invalid_report")
      const next = decodeReport(await selected.text())
      if (alive.current) setReport(next)
    } catch { if (alive.current) setError(w("invalid")) }
    finally { working.current = false; if (alive.current) setBusy(false) }
  }
  const download = () => {
    if (!report) return
    const url = URL.createObjectURL(new Blob([JSON.stringify(report)], { type: "application/json" }))
    const a = document.createElement("a"); a.href = url; a.download = "clawdline-doctor.json"; a.click(); setTimeout(() => URL.revokeObjectURL(url), 0)
  }
  const submit = async () => {
    if (working.current || images.busy || !origin || (!pending.current && (!report || !consent))) return
    working.current = true; setBusy(true); setError("")
    try {
      const identity = await supportCall<{ account_id: string }>(origin, "/v1/auth/session")
      if (typeof identity.account_id !== "string" || !identity.account_id) throw new Error("account_unknown")
      if (pending.current && pending.current.account !== identity.account_id) { setError(w("accountChanged")); return }
      const image_ids = pending.current ? pending.current.body.image_ids : await images.upload()
      const locale = currentCatalogTag()
      const input = pending.current ?? { account: identity.account_id, key: requestKey(), body: { title: "Clawdline Doctor", body: supportBody(report!, description), topic: "agents", request_type: "issue", source_language: locale, reply_locale: locale, image_ids } }
      // Saving before POST is required for a timeout/reload to replay exactly the same action.
      sessionStorage.setItem(pendingKey, JSON.stringify(input)); pending.current = input
      const answer = await supportCall<{ question: Question }>(origin, "/v1/support/questions", "POST", input.body, input.key)
      if (!answer.question?.id || !/^sq_[a-f0-9]{40}$/.test(answer.question.id) || answer.question.community_published === true) throw new SupportFailure("support_response_unreadable", false)
      try { sessionStorage.removeItem(pendingKey) } catch { /* The receipt is authoritative; a retained key remains safe to replay. */ }
      pending.current = null; images.clear()
      if (alive.current) { setUncertain(false); setSent(answer.question.id) }
    } catch (e) {
      if (e instanceof SupportFailure && e.definite) { pending.current = null; try { sessionStorage.removeItem(pendingKey) } catch { /* retry with a new action only after a definite refusal */ } }
      if (alive.current) { setUncertain(!!pending.current); fail(e) }
    } finally { working.current = false; if (alive.current) setBusy(false) }
  }
  return <section className="doctor-block" aria-labelledby="doctor-title">
    <h2 id="doctor-title">{w("title")}</h2><p>{w("lead")}</p>
    <div className="doctor-actions">
      {collect && <button className="chip" disabled={busy || images.busy || uncertain || (known.apiLevel !== undefined && known.apiLevel < 32)} onClick={() => void run()}>{busy ? w("working") : w("check")}</button>}
      <button className="chip" disabled={busy || images.busy || uncertain} onClick={() => file.current?.click()}>{w("import")}</button>
      <input ref={file} type="file" accept="application/json,.json" hidden onChange={e => { void importFile(e.currentTarget.files?.[0]); e.currentTarget.value = "" }} />
    </div>
    {collect && known.apiLevel !== undefined && known.apiLevel < 32 && <p>{w("update")}</p>}
    {error && <p role="alert">{error}</p>}
    {report && <>
      <p>{new Date(report.at * 1000).toLocaleString()} · Clawdline {report.version} · {report.os}/{report.arch}</p>
      <ul>{report.tools.map(t => <li key={t.id}>{t.id === "claude" ? "Claude Code" : "Codex"} · {t.version ?? w(t.state)}</li>)}</ul>
      <ul>{report.providers.map(p => <li key={p.id}><strong>{p.id}</strong> · {w(p.state)}{p.used_percent != null ? ` · ${Math.round(p.used_percent)}%` : ""}{p.problem ? ` · ${w(p.problem)}` : ""}</li>)}</ul>
      <p>{w("coverage")} {report.history.length} · {w("dropped")} {report.coverage.dropped} · {w(report.coverage.state)}{report.coverage.lost_unknown ? " · " + w("incomplete") : ""}</p>
      <p>{w("gaps")}</p>
      <details><summary>{w("preview")}</summary><pre>{JSON.stringify(report, null, 2)}</pre></details>
      <div className="doctor-actions"><button className="chip" onClick={download}>{w("download")}</button></div>
    </>}
    {(report || uncertain) && !sent && <>
      {uncertain ? <><p role="status">{w("uncertain")}</p><details><summary>{w("pendingPreview")}</summary><pre>{pending.current?.body.body}</pre></details></> : <>
        <label>{w("description")}<textarea rows={3} maxLength={DOCTOR_DESCRIPTION_CHARACTERS} value={description} disabled={busy || images.busy} onPaste={images.paste} onChange={e => setDescription(e.target.value)} /></label>
        {origin && <SupportImagePicker images={images} disabled={busy} />}
        <label className="doctor-consent"><input type="checkbox" checked={consent} disabled={busy} onChange={e => setConsent(e.target.checked)} />{w("consent")}</label>
      </>}
      {origin ? <button className="chip doctor-submit" disabled={busy || images.busy || (!uncertain && !consent)} onClick={() => void submit()}>{busy ? w("working") : uncertain ? w("retry") : w("submit")}</button>
        : <p><a href="https://app.clawdline.com/help/doctor" target="_blank" rel="noopener noreferrer">{w("cloud")}</a></p>}
    </>}
    {origin && <p><a href="/help/doctor">{w("reports")}</a></p>}
    {sent && <p role="status">{w("sent")} <a href={"/help/questions/" + sent}>{sent}</a></p>}
  </section>
}
