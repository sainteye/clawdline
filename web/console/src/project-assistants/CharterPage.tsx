import { useCallback, useEffect, useRef, useState } from "react"
import { BotMessageMarkdown } from "./BotMessageMarkdown"
import type * as W from "@clawdline/contract"
import { charterRefusal, charterRefusalCode, shownReaders, type CharterApi, type CharterProject } from "./charter-api"
import { changedCharterSections, charterSections, replaceCharterSection, shownCharterSection, type ChangedSection } from "./charter-text"
import { charterFormat, charterNames, charterProblem, charterWord } from "./charter-words"
import type { Words } from "./words"
import "./charter.css"

// One Project's team charter where a chat would be: the sections as cards,
// each editable in place; the lead's pending proposals as diffs; and, in a
// side panel, the versions with a restore. Every read and write goes through
// the CharterApi adapter, so the screen is the same over Cloud.

type CharterDocument = W.ProjectAssistantCharterResponse
type CharterReader = W.ProjectAssistantCharterReader
type CharterProposal = W.ProjectAssistantCharterProposal
type CharterVersion = W.ProjectAssistantCharterVersion
type Notice = { text: string; tone: "ok" | "error" }
type Editing = { index: number; draft: string }

const refreshMs = 10_000

/** One Project's charter, its readers and proposals, and the writes that change them. */
function useCharterDocument(api: CharterApi, project: CharterProject, onChanged: () => void) {
  const [doc, setDoc] = useState<CharterDocument | null>(null)
  const [loadError, setLoadError] = useState("")
  const [readers, setReaders] = useState<CharterReader[]>([])
  const [proposals, setProposals] = useState<CharterProposal[]>([])
  const [notice, setNotice] = useState<Notice | null>(null)
  const [busy, setBusy] = useState(false)
  const life = useRef(new AbortController())
  useEffect(() => { const control = new AbortController(); life.current = control; return () => control.abort() }, [project.machine_id, project.project_id])
  const reload = useCallback(async () => {
    const signal = life.current.signal
    try {
      const [next, who, pending] = await Promise.all([api.read(project, 0, signal), api.readers(project, signal), api.proposals(project, signal)])
      if (signal.aborted) return null
      setDoc(next); setReaders(shownReaders(who)); setProposals(pending.proposals); setLoadError("")
      return next
    } catch (error) {
      if (!signal.aborted) setLoadError(charterProblem(error))
      return null
    }
  }, [api, project.machine_id, project.project_id])
  useEffect(() => {
    setDoc(null); setNotice(null)
    void reload()
    const timer = window.setInterval(() => { if (document.visibilityState === "visible") void reload() }, refreshMs)
    return () => window.clearInterval(timer)
  }, [reload])
  /** Runs one write; afterwards the page rereads, and a stale one says what moved. */
  const write = async (action: (signal: AbortSignal) => Promise<unknown>, done: (next: CharterDocument | null) => string, onConflict?: (current: CharterDocument | null, field: string) => string) => {
    setBusy(true); setNotice(null)
    try {
      await action(life.current.signal)
      const next = await reload()
      onChanged()
      setNotice({ text: done(next), tone: "ok" })
      return true
    } catch (error) {
      if (life.current.signal.aborted) return false
      if (charterRefusalCode(error) !== "version_conflict") { setNotice({ text: charterProblem(error), tone: "error" }); return false }
      const current = await reload()
      onChanged()
      const field = (charterRefusal(error) as { field?: string }).field ?? ""
      setNotice({ text: onConflict ? onConflict(current, field) : charterProblem(error, current?.version), tone: "error" })
      return false
    } finally { if (!life.current.signal.aborted) setBusy(false) }
  }
  // Who is working changes by the minute; the after-save box reads it as an edit opens.
  const refreshReaders = () => {
    const signal = life.current.signal
    api.readers(project, signal).then(answer => { if (!signal.aborted) setReaders(shownReaders(answer)) }, () => {})
  }
  return { doc, loadError, readers, proposals, notice, busy, reload, write, refreshReaders }
}

export function CharterPage({ api, project, projectName, narrow, showBack, onBack, onChanged, w, language }: {
  api: CharterApi; project: CharterProject; projectName: string; narrow: boolean; showBack: boolean
  onBack: () => void; onChanged: () => void; w: Words; language: string
}) {
  const { doc, loadError, readers, proposals, notice, busy, reload, write, refreshReaders } = useCharterDocument(api, project, onChanged)
  const [editing, setEditing] = useState<Editing | null>(null)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [request, setRequest] = useState("")
  const [retouch, setRetouch] = useState<string | null>(null)
  const composer = useRef<HTMLTextAreaElement>(null)
  const historyButton = useRef<HTMLButtonElement>(null)

  const sections = doc ? charterSections(doc.text) : []
  const shown = sections.map((section, index) => ({ section, index })).filter(({ section }) => shownCharterSection(section))
  const next = (doc?.version ?? 0) + 1
  const behind = doc ? readers.filter(r => r.version !== doc.version) : []
  const status = !doc || !readers.length ? "" : behind.length
    ? charterFormat("statusSome", readers.length - behind.length, doc.version, charterNames(behind.map(r => r.name)))
    : charterFormat("statusAll", readers.length, doc.version)
  const proposal = proposals[0]
  const botName = (id: string) => readers.find(r => r.bot_id === id)?.name ?? charterWord("lead")
  const proposed: ChangedSection[] = doc && proposal ? changedCharterSections(doc.text, proposal.text) : []
  const proposedHeadings = new Set(proposed.map(change => change.heading))
  const version = (current: CharterDocument | null) => current?.version ?? doc?.version ?? 0

  const saveSection = async () => {
    if (!doc || !editing) return
    const section = sections[editing.index]
    const text = replaceCharterSection(doc.text, editing.index, editing.draft)
    if (text === doc.text) { setEditing(null); return }
    const ok = await write(signal => api.save(project, { expected_version: doc.version, text, reason: charterFormat("reasonEdit", section?.heading || charterWord("intro")) }, signal),
      saved => charterFormat("saved", version(saved)), current => charterFormat("conflictEdit", version(current)))
    if (ok) setEditing(null)
  }
  const apply = (item: CharterProposal) => doc && void write(signal => api.applyProposal(project, item.id, { expected_version: doc.version }, signal),
    saved => charterFormat("applied", version(saved)),
    (current, field) => field === "charter_proposal_base" ? charterFormat("conflictProposal", item.base_version, version(current)) : charterFormat("conflictVersion", version(current)))
  const dismiss = (item: CharterProposal) => void write(async signal => { await api.dismissProposal(project, item.id, signal); if (retouch === item.id) setRetouch(null) }, () => charterWord("dismissed"))
  const sendRequest = async () => {
    const text = request.trim()
    if (!text || busy) return
    if (await write(signal => api.requestChange(project, { text }, signal), () => charterWord("requested"))) { setRequest(""); setRetouch(null) }
  }
  const startRetouch = (item: CharterProposal) => setRetouch(item.id)
  useEffect(() => { if (retouch) composer.current?.focus() }, [retouch])
  const startEdit = (index: number, text: string) => { setEditing({ index, draft: text.replace(/\n+$/, "") }); refreshReaders() }
  const closeHistory = () => { setHistoryOpen(false); requestAnimationFrame(() => historyButton.current?.focus({ preventScroll: true })) }

  const editedSection = editing ? shown.findIndex(item => item.index === editing.index) + 1 : 0
  const editor = editing && doc && <SectionEditor key={editing.index} n={editedSection} editing={editing} readers={readers} next={next} busy={busy} full={narrow}
    onDraft={draft => setEditing({ ...editing, draft })} onCancel={() => setEditing(null)} onSave={() => void saveSection()}/>

  return <section className={`pa-chat-layout pa-chat-surface pa-charter${historyOpen ? " pa-info-open" : ""}`} aria-label={charterWord("title")}>
    <div className="pa-charter-main" inert={narrow && historyOpen ? true : undefined}>
      <header className="pa-chat-header pa-charter-header">
        <button type="button" className="pa-circle pa-chat-back" data-show-back={showBack || undefined} onClick={onBack} aria-label={w.back} title={w.back}>‹</button>
        <h2 className="pa-chat-heading"><span className="pa-charter-pill"><CharterIcon size={14}/><span>{[charterWord("title"), projectName, doc ? charterFormat("version", doc.version) : ""].filter(Boolean).join(" · ")}</span></span></h2>
      </header>
      <div className="pa-charter-scroll">
        {loadError && !doc && <div className="pa-charter-problem" role="alert"><p>{loadError}</p><button type="button" className="pa-pill" onClick={() => void reload()}>{w.retry}</button></div>}
        {!doc && !loadError && <p className="pa-charter-loading" role="status">{w.loading}</p>}
        {doc && <>
          <div className="pa-charter-status">
            <p>{status}</p>
            <button ref={historyButton} type="button" className="pa-pill pa-charter-history-open" aria-expanded={historyOpen} onClick={() => historyOpen ? closeHistory() : setHistoryOpen(true)}>{charterWord("history")}</button>
          </div>
          {notice && <p className="pa-charter-notice" data-tone={notice.tone} role={notice.tone === "error" ? "alert" : "status"}>{notice.text}</p>}
          {proposal && <ProposalCard proposal={proposal} author={botName(proposal.bot_id)} changes={proposed} next={next} busy={busy} stale={proposal.stale || proposal.base_version !== doc.version}
            onApply={() => apply(proposal)} onDismiss={() => dismiss(proposal)} onRetouch={() => startRetouch(proposal)}/>}
          {shown.map(({ section, index }, position) => editing?.index === index && !narrow
            ? <div key={index}>{editor}</div>
            : <article key={index} className="pa-charter-section" data-proposed={proposedHeadings.has(section.heading) || undefined} aria-label={section.heading || charterWord("intro")}>
              <BotMessageMarkdown text={section.text} w={w}/>
              <button type="button" className="pa-charter-edit" disabled={busy} aria-label={charterFormat("editSection", position + 1)} title={charterFormat("editSection", position + 1)}
                onClick={() => startEdit(index, section.text)}>
                {narrow ? <span>{charterWord("editShort")}</span> : <PencilIcon/>}
              </button>
            </article>)}
        </>}
      </div>
      {doc && <form className="pa-charter-request" onSubmit={event => { event.preventDefault(); void sendRequest() }}>
        {retouch && <p className="pa-charter-retouch">{charterWord("retouchHint")}<button type="button" onClick={() => setRetouch(null)} aria-label={charterWord("retouchCancel")}>×</button></p>}
        <div className="pa-composer">
          <textarea ref={composer} rows={1} value={request} aria-label={charterWord("requestLabel")} placeholder={charterWord("requestPlaceholder")} disabled={busy}
            onChange={event => setRequest(event.target.value)}
            onKeyDown={event => { if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); void sendRequest() } }}/>
          <button type="submit" className="pa-send" disabled={busy || !request.trim()} aria-label={charterWord("requestSend")} title={charterWord("requestSend")}>
            <svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M8 13V3M3.5 7.5 8 3l4.5 4.5"/></svg>
          </button>
        </div>
      </form>}
    </div>
    {narrow && editor}
    {doc && <aside hidden={!historyOpen} inert={!historyOpen ? true : undefined} className="pa-info-panel pa-side-inspector pa-charter-history" aria-label={charterWord("history")}
      onKeyDown={event => { if (event.key === "Escape" && !event.defaultPrevented) { event.stopPropagation(); closeHistory() } }}>
      {historyOpen && <CharterHistory key={doc.version} api={api} project={project} doc={doc} busy={busy} language={language} onClose={closeHistory}
        onRestore={from => void write(signal => api.restore(project, { expected_version: doc.version, version: from }, signal),
          saved => charterFormat("restored", from, version(saved)))}/>}
    </aside>}
  </section>
}

function SectionEditor({ n, editing, readers, next, busy, full, onDraft, onCancel, onSave }: {
  n: number; editing: Editing; readers: readonly CharterReader[]; next: number; busy: boolean; full: boolean
  onDraft: (draft: string) => void; onCancel: () => void; onSave: () => void
}) {
  const area = useRef<HTMLTextAreaElement>(null)
  useEffect(() => { area.current?.focus({ preventScroll: !full }) }, [])
  const waiting = readers.filter(r => !r.running), running = readers.filter(r => r.running)
  const title = charterFormat("editSection", n)
  return <div className={`pa-charter-editor${full ? " pa-charter-editor-full" : ""}`} role={full ? "dialog" : undefined} aria-modal={full || undefined} aria-label={title}
    onKeyDown={event => { if (event.key === "Escape" && !event.nativeEvent.isComposing) { event.stopPropagation(); onCancel() } }}>
    {full && <header className="pa-charter-editor-bar">
      <button type="button" onClick={onCancel}>{charterWord("cancel")}</button>
      <strong>{title}</strong>
      <button type="button" className="pa-charter-primary" disabled={busy} onClick={onSave}>{charterWord("saveShort")}</button>
    </header>}
    <div className="pa-charter-editor-body">
      <textarea ref={area} value={editing.draft} aria-label={charterFormat("sectionText", n)} onChange={event => onDraft(event.target.value)} rows={Math.min(16, Math.max(5, editing.draft.split("\n").length + 1))}/>
      <p className="pa-charter-hint">{charterWord("editHint")}</p>
      {readers.length > 0 && <div className="pa-charter-after">
        <strong>{charterWord("afterSave")}</strong>
        {waiting.length > 0 && <p>{charterFormat("afterNext", charterNames(waiting.map(r => r.name)))}</p>}
        {running.length > 0 && <p>{charterFormat("afterRunning", charterNames(running.map(r => r.name)))}</p>}
      </div>}
      {!full && <div className="pa-charter-actions">
        <button type="button" onClick={onCancel}>{charterWord("cancel")}</button>
        <button type="button" className="pa-charter-primary" disabled={busy} onClick={onSave}>{charterFormat("saveAs", next)}</button>
      </div>}
    </div>
  </div>
}

function ProposalCard({ proposal, author, changes, next, busy, stale, onApply, onDismiss, onRetouch }: {
  proposal: CharterProposal; author: string; changes: readonly ChangedSection[]; next: number; busy: boolean; stale: boolean
  onApply: () => void; onDismiss: () => void; onRetouch: () => void
}) {
  return <article className="pa-charter-proposal" aria-label={charterFormat("proposalFrom", author)}>
    <p className="pa-charter-proposal-title"><strong>{charterFormat("proposalFrom", author)}</strong></p>
    {proposal.summary && <p className="pa-charter-proposal-why">{proposal.summary}</p>}
    {stale && <p className="pa-charter-proposal-stale">{charterFormat("proposalStale", proposal.base_version)}</p>}
    {changes.map((change, i) => <CharterDiff key={i} change={change}/>)}
    {!changes.length && <p className="pa-charter-hint">{charterWord("proposalNoChange")}</p>}
    <p className="pa-charter-hint">{charterWord("proposalUntilApply")}</p>
    <div className="pa-charter-actions">
      <button type="button" className="pa-charter-primary" disabled={busy} onClick={onApply}>{charterFormat("apply", next)}</button>
      <button type="button" disabled={busy} onClick={onDismiss}>{charterWord("dismiss")}</button>
      <button type="button" disabled={busy} onClick={onRetouch}>{charterWord("retouch")}</button>
    </div>
  </article>
}

function CharterDiff({ change }: { change: ChangedSection }) {
  return <div className="pa-charter-diff" role="group" aria-label={change.heading || charterWord("intro")}>
    {change.lines.map((line, i) => <div key={i} className="pa-charter-diff-line" data-kind={line.kind}>
      <span className="pa-charter-diff-mark">{line.kind === "removed" ? charterWord("diffRemoved") : line.kind === "added" ? charterWord("diffAdded") : ""}</span>
      <span>{line.text || " "}</span>
    </div>)}
  </div>
}

function CharterHistory({ api, project, doc, busy, language, onClose, onRestore }: {
  api: CharterApi; project: CharterProject; doc: CharterDocument; busy: boolean; language: string
  onClose: () => void; onRestore: (version: number) => void
}) {
  const [selected, setSelected] = useState(doc.version)
  const [old, setOld] = useState<{ text: string; previous_text: string } | null>(null)
  const [error, setError] = useState("")
  const panel = useRef<HTMLDivElement>(null)
  useEffect(() => { panel.current?.querySelector<HTMLElement>(".pa-charter-history-close")?.focus({ preventScroll: true }) }, [])
  useEffect(() => {
    const control = new AbortController()
    setOld(null); setError("")
    const text = (version: number) => version < 1 ? Promise.resolve("") : api.read(project, version, control.signal).then(answer => answer.selected?.text ?? "")
    Promise.all([text(selected), text(selected - 1)]).then(([now, before]) => { if (!control.signal.aborted) setOld({ text: now, previous_text: before }) },
      problem => { if (!control.signal.aborted) setError(charterProblem(problem)) })
    return () => control.abort()
  }, [api, project.machine_id, project.project_id, selected, doc.version])
  const date = new Intl.DateTimeFormat(language, { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" })
  const changes = old ? changedCharterSections(old.previous_text, old.text) : []
  return <div ref={panel} className="pa-charter-history-body">
    <header className="pa-charter-history-head">
      <h3>{charterWord("history")}</h3>
      <button type="button" className="pa-circle pa-charter-history-close" onClick={onClose} aria-label={charterWord("historyClose")} title={charterWord("historyClose")}>×</button>
    </header>
    <ol className="pa-charter-versions">
      {doc.history.map(item => <li key={item.version}>
        <button type="button" aria-pressed={item.version === selected} onClick={() => setSelected(item.version)}>
          <strong>{charterFormat("version", item.version)}{item.version === doc.version && <small className="pa-charter-current">{charterWord("current")}</small>}</strong>
          <span>{versionReason(item)}</span>
          <small>{[charterWord(`author_${item.author}`), safeDate(date, item.created_at)].filter(Boolean).join(" · ")}</small>
        </button>
      </li>)}
    </ol>
    <section className="pa-charter-history-diff" aria-label={charterFormat("historyDiff", selected)}>
      <h4>{selected > 1 ? charterFormat("historyDiff", selected) : charterWord("historyFirst")}</h4>
      {error && <p role="alert">{error}</p>}
      {old && changes.map((change, i) => <CharterDiff key={i} change={change}/>)}
      {old && !changes.length && <p className="pa-charter-hint">{charterWord("historySame")}</p>}
      {selected !== doc.version && <button type="button" className="pa-charter-primary" disabled={busy || !old} onClick={() => onRestore(selected)}>{charterFormat("restore", selected)}</button>}
    </section>
  </div>
}

function versionReason(item: CharterVersion): string {
  switch (item.author) {
    case "template": return item.reason || charterWord("reasonCreated")
    // The source names a restore "Restored version N".
    case "restore": { const from = /^Restored version ([1-9][0-9]*)$/.exec(item.reason)?.[1]; return from ? charterFormat("reasonRestored", from) : item.reason || charterWord("reasonRestoredOld") }
    case "lead_proposal": return item.reason ? charterFormat("reasonProposal", item.reason) : charterWord("reasonProposalPlain")
    default: return item.reason || charterWord("reasonEdited")
  }
}

function safeDate(format: Intl.DateTimeFormat, at: string) {
  const time = Date.parse(at)
  return Number.isFinite(time) ? format.format(time) : ""
}

export function CharterIcon({ size = 20 }: { size?: number }) {
  return <svg viewBox="0 0 20 20" width={size} height={size} fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M5 2.5h7l3.5 3.5v11.5h-10.5z"/><path d="M12 2.5V6h3.5M7.5 9.5h5M7.5 12.5h5M7.5 15h3"/>
  </svg>
}

function PencilIcon() {
  return <svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M10.5 2.5l3 3L5.5 13.5H2.5v-3z"/><path d="M9 4l3 3"/></svg>
}
