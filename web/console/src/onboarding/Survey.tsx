import { bindNativeModalBackdrop } from "../modal-backdrop.js"
import { useEffect, useRef, useState } from "react"
import { currentCatalogTag } from "../catalog.js"
import { LOCAL_KEY, OPEN_SURVEY, REASONS, SOURCES, TOOLS, readLocal, surveyRequest, toggleTool, validAnswers, type Answers, type LocalSurvey, type SurveyState } from "./model.js"
import { surveyWord as w, type SurveyWord } from "./words.js"
import "./survey.css"

const INITIAL: SurveyState = { version: 1, revision: 0, status: "new", answers: null }
const EMPTY: Answers = { reason: "", tools: [], source: "" }

export function SurveyForm({ initial, local, busy, error, onSubmit, onClose, onReload, onDismiss }: {
  initial: Answers | null; local: boolean; busy: boolean; error: string | null;
  onSubmit: (answers: Answers) => void; onClose: () => void; onReload?: () => void; onDismiss?: () => void;
}) {
  const [answers, setAnswers] = useState<Answers>(initial ?? EMPTY)
  const [invalid, setInvalid] = useState(false)
  const picker = useRef<HTMLDetailsElement>(null)
  const reason = useRef<HTMLSelectElement>(null)
  useEffect(() => { setAnswers(initial ?? EMPTY) }, [initial])
  useEffect(() => {
    const close = (event: PointerEvent) => { if (picker.current && !picker.current.contains(event.target as Node)) picker.current.open = false }
    document.addEventListener("pointerdown", close)
    return () => document.removeEventListener("pointerdown", close)
  }, [])
  return <form className="survey-card" onSubmit={event => {
    event.preventDefault()
    if (!validAnswers(answers)) { setInvalid(true); reason.current?.focus(); return }
    setInvalid(false); onSubmit(answers)
  }}>
    <header className="survey-heading">
      <h1 id="survey-title">{w("title")}</h1>
      {onDismiss && <button type="button" className="survey-secondary" disabled={busy} onClick={onDismiss}>{w("close")}</button>}
    </header>
    <p className="survey-intro">{w("intro")}</p>
    <fieldset disabled={busy} className="survey-fields">
      <label className="survey-field" htmlFor="survey-reason"><span>{w("reason")}</span>
        <select ref={reason} id="survey-reason" value={answers.reason} required onChange={e => setAnswers({ ...answers, reason: e.target.value })}>
          <option value="" disabled>{w("choose")}</option>
          {REASONS.map(key => <option key={key} value={key}>{w(`reason_${key}` as SurveyWord)}</option>)}
        </select>
      </label>
      <div className="survey-field">
        <span id="survey-tools-label">{w("tools")} <small>{w("multiple")}</small></span>
        <details ref={picker} className="survey-tools" onKeyDown={e => {
          if (e.key === "Escape" && picker.current?.open) { e.preventDefault(); e.stopPropagation(); picker.current.open = false; picker.current.querySelector("summary")?.focus() }
        }}>
          <summary aria-labelledby="survey-tools-label survey-tools-value" aria-disabled={busy} onClick={e => { if (busy) e.preventDefault() }}>
            <span id="survey-tools-value">{answers.tools.length ? answers.tools.map(key => w(`tool_${key}` as SurveyWord)).join(", ") : w("chooseTools")}</span>
          </summary>
          <fieldset className="survey-tool-options" aria-labelledby="survey-tools-label">
            {TOOLS.map(tool => <label key={tool}><input type="checkbox" checked={answers.tools.includes(tool)} onChange={() => setAnswers({ ...answers, tools: toggleTool(answers.tools, tool) })} /><span>{w(`tool_${tool}` as SurveyWord)}</span></label>)}
          </fieldset>
        </details>
      </div>
      <label className="survey-field" htmlFor="survey-source"><span>{w("source")}</span>
        <select id="survey-source" value={answers.source} required onChange={e => setAnswers({ ...answers, source: e.target.value })}>
          <option value="" disabled>{w("choose")}</option>
          {SOURCES.map(key => <option key={key} value={key}>{w(`source_${key}` as SurveyWord)}</option>)}
        </select>
      </label>
    </fieldset>
    {invalid ? <p className="survey-error" role="alert">{w("required")}</p> : null}
    {error ? <p className="survey-error" role="alert">{w(error === "survey_revision_conflict" ? "conflict" : "failed")}</p> : null}
    <div className="survey-actions">
      <button className="survey-submit" type="submit" disabled={busy || error === "survey_revision_conflict"}>{w(busy ? "saving" : "submit")}</button>
      <button className="survey-secondary" type="button" onClick={onClose}>{w("skip")}</button>
      {error === "survey_revision_conflict" && onReload ? <button type="button" className="survey-secondary" disabled={busy} onClick={onReload}>{w("reload")}</button> : null}
    </div>
    <p className="survey-privacy">{w(local ? "localPrivacy" : "cloudPrivacy")} {w("privacy")}</p>
  </form>
}

/** One coordinator per signed-in account, or per local browser. Never waits in front of pairing. */
export function SurveyCoordinator({ account, eligible = false, productVersion = "unknown", localApiOrigin = "https://api.clawdline.com" }: {
  account?: { id: string; apiOrigin: string }; eligible?: boolean; productVersion?: string; localApiOrigin?: string;
}) {
  const local = useRef<LocalSurvey | null>(null)
  const [build, setBuild] = useState(productVersion)
  const [state, setState] = useState<SurveyState>(INITIAL)
  const [open, setOpen] = useState(false)
  const [edited, setEdited] = useState(false)
  const hide = () => { setOpen(false); setEdited(false) }
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [entry, setEntry] = useState<"onboarding" | "settings">("onboarding")
  const recovery = useRef<SurveyState | null>(null)
  const dialog = useRef<HTMLDialogElement>(null)
  const request = useRef<AbortController | null>(null)
  const pending = useRef<{ key: string; body: Record<string, unknown> } | null>(null)
  const active = useRef(true)
  const origin = account?.apiOrigin ?? localApiOrigin
  const path = account ? "/v1/onboarding/survey" : "/v1/onboarding/local"
  const persist = () => { try { if (local.current) localStorage.setItem(LOCAL_KEY, JSON.stringify(local.current)) } catch { /* Form remains usable when storage is disabled. */ } }
  useEffect(() => {
    active.current = true
    if (!account) { local.current = readLocal({ getItem: key => { try { return localStorage.getItem(key) } catch { return null } } }); setState(local.current.state); pending.current = local.current.pending ?? null; persist() }
    return () => { active.current = false; request.current?.abort() }
  }, [account?.id])
  useEffect(() => {
    if (!account) return
    if (!eligible) { hide(); return }
    const controller = new AbortController()
    void surveyRequest(origin, path, { signal: controller.signal }).then(saved => {
      if (controller.signal.aborted) return
      setState(saved)
      if (saved.status === "new" || saved.status === "shown") { setEntry("onboarding"); setOpen(true) }
    }).catch(() => { /* Survey availability must never block the console. Settings offers a retry. */ })
    return () => controller.abort()
  }, [account?.id, origin, eligible])
  const readCloud = async () => {
    const controller = new AbortController(); request.current?.abort(); request.current = controller
    setBusy(true); setError(null)
    try {
      const saved = await surveyRequest(origin, path, { signal: controller.signal })
      if (!controller.signal.aborted) { setState(saved); pending.current = null; setError(null); setOpen(true) }
    } catch { if (!controller.signal.aborted) setNotice(w("unavailable")) }
    finally { if (active.current && !controller.signal.aborted) setBusy(false) }
  }
  useEffect(() => {
    const onOpen = () => {
      setEntry("settings"); setNotice(null); setError(null)
      if (account) void readCloud()
      else setOpen(true)
    }
    addEventListener(OPEN_SURVEY, onOpen)
    return () => removeEventListener(OPEN_SURVEY, onOpen)
  }, [account?.id, origin])
  useEffect(() => {
    const node = dialog.current
    if (!node) return
    if (open && !node.open) { bindNativeModalBackdrop(node); node.showModal() }
    if (!open && node.open) node.close()
  }, [open])
  useEffect(() => {
    if (!open || account || !local.current) return
    local.current.shownAt ??= new Date().toISOString()
    if (local.current.state.status === "new") local.current.state.status = "shown"
    persist()
  }, [open, account?.id])
  // Record exposure only after the form is actually visible. StrictMode and other browsers deduplicate on the server.
  useEffect(() => {
    if (!open || !account || state.status !== "new") return
    const controller = new AbortController()
    void surveyRequest(origin, path, { body: { version: 1, revision: 0, action: "shown", meta: metadata() }, key: crypto.randomUUID(), signal: controller.signal })
      .then(saved => { if (!controller.signal.aborted) setState(saved) }).catch(() => {})
    return () => controller.abort()
  }, [open, account?.id, state.status])
  useEffect(() => {
    if (!open || productVersion !== "unknown") return
    const controller = new AbortController()
    void fetch(new URL("BUILD.json", document.baseURI), { cache: "no-store", signal: controller.signal })
      .then(async response => { if (!response.ok) return; const data = await response.json() as { stamp?: unknown }; if (!controller.signal.aborted && typeof data.stamp === "string" && /^[\w.+-]{1,80}$/.test(data.stamp)) setBuild(data.stamp) })
      .catch(() => {})
    return () => controller.abort()
  }, [open, productVersion])
  function metadata() { return { locale: currentCatalogTag(), entry, product_version: build } }
  const dismiss = () => {
    request.current?.abort(); setBusy(false); hide(); setNotice(null)
    if (account && state.status !== "submitted") {
      void surveyRequest(origin, path, { body: { version: 1, revision: state.revision, action: "skip", meta: metadata() }, key: crypto.randomUUID() }).then(saved => { if (active.current) setState(saved) }).catch(() => {})
    } else if (local.current) {
      local.current.dismissed = true
      local.current.skippedAt ??= new Date().toISOString()
      if (local.current.state.status !== "submitted") local.current.state.status = "skipped"
      persist()
    }
  }
  const submit = async (answers: Answers) => {
    if (busy) return
    const body: Record<string, unknown> = { version: 1, revision: state.revision, action: "submit", meta: metadata(), answers,
      ...(!account ? { respondent: local.current!.respondent, consent: true } : {}) }
    if (!pending.current || JSON.stringify(pending.current.body.answers) !== JSON.stringify(answers)) pending.current = { key: crypto.randomUUID(), body }
    if (local.current) { local.current.pending = pending.current; persist() }
    const attempt = pending.current
    const controller = new AbortController(); request.current?.abort(); request.current = controller
    setBusy(true); setError(null)
    try {
      const saved = await surveyRequest(origin, path, { ...attempt, local: !account, signal: controller.signal })
      if (controller.signal.aborted || !active.current) return
      setState(saved); hide(); setNotice(w("thanks")); pending.current = null
      if (local.current) { local.current.state = saved; local.current.dismissed = true; local.current.submittedAt ??= new Date().toISOString(); delete local.current.pending; persist() }
    } catch (err) {
      if (!controller.signal.aborted && active.current) {
        setError(err instanceof Error ? err.message : "survey_unavailable")
        recovery.current = err && typeof err === "object" && "current" in err ? (err as { current: SurveyState }).current : null
      }
    } finally { if (!controller.signal.aborted && active.current) setBusy(false) }
  }
  return <>
    {notice ? <div className="survey-notice" role="status"><span>{notice}</span><button type="button" onClick={() => setNotice(null)}>{w("close")}</button></div> : null}
    <dialog ref={dialog} className="survey-dialog" aria-labelledby="survey-title"
      data-modal-dismiss={String(!busy && !edited)} onChangeCapture={() => setEdited(true)}
      onCancel={e => { e.preventDefault(); if (!busy && !edited) hide() }}>
      {open ? <SurveyForm initial={state.answers ?? (validAnswers(pending.current?.body.answers) ? pending.current!.body.answers as Answers : null)} local={!account} busy={busy} error={error} onSubmit={submit} onClose={dismiss} onDismiss={hide} onReload={() => {
        if (account) void readCloud()
        else if (recovery.current) {
          setState(recovery.current); setError(null); pending.current = null
          if (local.current) { local.current.state = recovery.current; delete local.current.pending; persist() }
          recovery.current = null
        }
      }} /> : null}
    </dialog>
  </>
}
export function SurveySettingsLink() {
  return <div className="survey-settings"><strong>{w("settings")}</strong><button type="button" className="survey-secondary" onClick={() => dispatchEvent(new Event(OPEN_SURVEY))}>{w("edit")}</button></div>
}
