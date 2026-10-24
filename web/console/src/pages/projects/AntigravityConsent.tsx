import { useEffect, useRef, useState } from "react"
import type { AntigravityConsentChange, AntigravityConsentStatus } from "@clawdline/contract"
import { catalogWord } from "../../catalog.js"
import { readAntigravityConsent, changeAntigravityConsent } from "./antigravity-consent-api.js"
import "./antigravity-consent.css"

const word = (key: string) => catalogWord("antigravityConsent", key)
type Pending = { input: AntigravityConsentChange; key: string }
/** The person approves a visible Project-wide scope. A failed write is never
 * automatically retried; refresh is read-only and a retry preserves its key. */
export function AntigravityConsent({ place, onBusy }: { place: string; onBusy(busy: boolean): void }) {
  const [status, setStatus] = useState<AntigravityConsentStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [approved, setApproved] = useState(false)
  const [problem, setProblem] = useState(false)
  const [pending, setPending] = useState<Pending | null>(null)
  const current = useRef(0)
  const gate = useRef(false)
  const checkbox = useRef<HTMLInputElement>(null)
  useEffect(() => {
    const generation = ++current.current
    void readAntigravityConsent(place).then(out => { if (current.current === generation) { setStatus(out); setProblem(false) } }, () => { if (current.current === generation) { setStatus(null); setProblem(true) } }).finally(() => { if (current.current === generation) setLoading(false) })
    return () => { ++current.current }
  }, [place])
  async function refresh() {
    if (gate.current) return
    const generation = ++current.current
    setLoading(true); setApproved(false)
    try { const out = await readAntigravityConsent(place); if (current.current === generation) { setStatus(out); setProblem(false) } }
    catch { if (current.current === generation) { setStatus(null); setProblem(true) } }
    finally { if (current.current === generation) setLoading(false) }
  }
  async function apply(operation: Pending) {
    if (gate.current || loading) return
    gate.current = true; setBusy(true); onBusy(true); setApproved(false)
    const generation = ++current.current
    let lost = false
    try { await changeAntigravityConsent(place, operation.input, operation.key) }
    catch { lost = true }
    // A mutation answer, including an old receipt, is never the current view.
    let fresh: AntigravityConsentStatus | null = null
    try { fresh = await readAntigravityConsent(place) } catch { /* explicitly unknown below */ }
    if (current.current === generation) {
      setStatus(fresh); setProblem(fresh === null); setPending(lost ? operation : null); setBusy(false)
    }
    gate.current = false; onBusy(false)
  }
  function change(action: "enable" | "disable") {
    if (!status || gate.current || loading || pending || (action === "enable" && (!approved || !status.preview))) return
    const input: AntigravityConsentChange = { action, revision: status.revision, scope: status.scope,
      ...(action === "enable" ? { preview: status.preview } : {}) }
    void apply({ input, key: crypto.randomUUID() })
  }
  const phase = status?.phase
  return <section className="antigravity-consent" aria-label={word("title")} aria-busy={busy || loading}>
    <div className="antigravity-consent-head"><h3>{word("title")}</h3><button type="button" onClick={() => void refresh()} disabled={busy || loading}>{word("refresh")}</button></div>
    <p>{word("scope")}</p>
    <p className="antigravity-consent-boundary">{word("effect")}</p>
    <p role="status">{loading ? word("loading") : problem || !phase ? word("unavailable") : word(phase)}</p>
    {status && !loading && <p className="antigravity-consent-files">{word(status.hooks === "installed" ? "hooksInstalled" : status.hooks === "removed" ? "hooksRemoved" : "hooksUnknown")}</p>}
    {pending && <div className="antigravity-consent-notice" role="alert"><p>{word("uncertain")}</p><div className="antigravity-consent-actions">
      <button type="button" onClick={() => void apply(pending)} disabled={busy || loading}>{word("retry")}</button>
      <button type="button" onClick={() => { setPending(null); setApproved(false); checkbox.current?.focus() }} disabled={busy || loading}>{word("review")}</button>
    </div></div>}
    {status && !loading && <>
      {status.preview && <label className="antigravity-consent-approval"><input ref={checkbox} type="checkbox" checked={approved} disabled={busy || !!pending} onChange={e => setApproved(e.currentTarget.checked)} /><span>{word("approve")}</span></label>}
      {!status.preview && <p>{word("helperUnavailable")}</p>}
      <div className="antigravity-consent-actions">
        <button className="antigravity-consent-connect" type="button" onClick={() => change("enable")} disabled={!approved || !status.preview || busy || !!pending}>{word(phase === "active" ? "repair" : "enable")}</button>
        {(phase === "active" || phase === "preparing" || phase === "disabled" && status.hooks !== "removed") && <button type="button" onClick={() => change("disable")} disabled={busy || !!pending}>{word(phase === "disabled" ? "cleanup" : "disable")}</button>}
      </div>
    </>}
  </section>
}
