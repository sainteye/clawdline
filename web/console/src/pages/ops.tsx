import { useCallback, useEffect, useId, useMemo, useRef, useState, type ReactNode, type RefObject } from "react"
import type { SessionRow } from "@clawdline/contract"
import type { PageModule } from "./types.js"
import { RefusalError } from "@clawdline/core"
import { readProjectPlaces, readSessionsForWorkV2, directTodoActionV2, type ProjectPlace } from "./work/api.js"
import { listProjectDirectory, type TreeEntry } from "./projects/project-tree-api.js"
import { ProjectPicker } from "../ui/ProjectPicker.js"
import { ActionProgress } from "../ui/ActionProgress.js"
import { LoadingSkeleton } from "../LoadingSkeleton.js"
import { focusModalContainer } from "../modal-focus.js"
import { sessionName } from "../brand-mark.js"
import { richText } from "../legacy/js/view/markdown.js"
import {
  OpsError, createOps, deleteOps, listOps, readOpsEntry, readOpsSetup, requestOpsResolve, updateOpsEntry,
  type OpsCollection, type OpsEntry, type OpsList, type OpsSetup,
} from "./ops/ops-api.js"
import {
  IntentKeys, OPS_COLLECTION_NAME_BYTES, OPS_FILTERS, buildTree, checkFileName, checkLinkUrl, checkTitle, editable,
  fileNameOf, filterCounts, includeInGitDefault, markdownName, needsGitChoice, nextPosition, phoneRows,
  sameProjectSessions, sendProblem, statusView, displayName, awaitsAgent, changedStatus, typeIcon, typeWord, visibilityWord, type InputProblem, type OpsFilter, type OpsIcon,
} from "./ops/ops-model.js"
import { opsWord as W, type OpsWord } from "./ops/words.js"
import "../page-skeleton.css"
import "./ops/ops.css"

type Dialog = "markdown" | "file" | "link" | "collection" | null
const PHONE = "(max-width: 640px)"
const keys = new IntentKeys()

const filterWord: Record<OpsFilter, OpsWord> = {
  all: "filterAll", markdown: "filterMarkdown", notion: "filterNotion", google: "filterGoogle", web: "filterWeb", attention: "filterAttention",
}
const problemWord = (problem: InputProblem): OpsWord | null =>
  problem === "empty" ? "needsName" : problem === "tooLong" ? "tooLong" : problem === "slash" ? "slashName" : problem === "badUrl" ? "badUrl" : null

/** A refusal as one sentence the person can act on; the machine's detail is kept when it has one. */
function refusalText(error: unknown, fallback: OpsWord = "createRefused"): string {
  if (!(error instanceof OpsError)) return W("createUnknown")
  if (error.uncertain) return W(fallback === "saveRefused" ? "saveUnknown" : fallback === "moveRefused" ? "moveUnknown" : "createUnknown")
  if (error.code === "ops_name_taken") return W("nameTaken")
  if (error.code === "ops_bad_url") return W("badUrl")
  if (error.code === "ops_rename_case_only") return W("renameCaseOnly")
  if (error.code === "ops_rename_instruction_file") return W("renameInstructionFile")
  if (error.code === "ops_rename_unsupported") return W("renameUnsupported")
  if (error.code === "not_implemented" || error.status === 501) return W("machineTooOld")
  if (/limit|full/.test(error.code)) return W("limitReached")
  return error.message && error.message !== error.code ? `${W(fallback)} ${error.message}` : W(fallback)
}

/** Registered as console.ops_resolving_poll_seconds (internal/domain/capacity). */
export const OPS_RESOLVING_POLL_MS = 5_000

/** The name an entry is shown by in this interface's language. */
const entryName = (entry: OpsEntry) => displayName(entry, (word) => W(word))

const LAST_PLACE = "clawdline.ops.place"
function rememberedPlace(): string {
  try { return globalThis.localStorage?.getItem(LAST_PLACE) ?? "" } catch { return "" }
}
function rememberPlace(id: string): void {
  try { globalThis.localStorage?.setItem(LAST_PLACE, id) } catch { /* a private window keeps the choice for this page only */ }
}

/** The send route's refusal, said as what the person can do about it. */
function sendFailureText(cause: unknown): string {
  if (!(cause instanceof RefusalError)) return W("linkSavedUnsent")
  const problem = sendProblem(cause.code, cause.detail)
  if (problem === "asking") return W("sendAsking")
  if (problem === "busy") return W("sendBusy")
  return W("linkSavedUnsent")
}

function useMedia(query: string): boolean {
  const read = () => typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia(query).matches
  const [matches, setMatches] = useState(read)
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return
    const list = window.matchMedia(query)
    const change = () => setMatches(list.matches)
    change()
    list.addEventListener("change", change)
    return () => list.removeEventListener("change", change)
  }, [query])
  return matches
}

/** Escape closes, Tab stays inside, and focus goes back where it came from. */
function useDialog(open: boolean, ref: RefObject<HTMLElement | null>, close: () => void, returnTo?: RefObject<HTMLElement | null>) {
  const closeRef = useRef(close)
  closeRef.current = close
  useEffect(() => {
    if (!open) return
    const previous = document.activeElement as HTMLElement | null
    focusModalContainer(ref.current)
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); closeRef.current(); return }
      if (event.key !== "Tab") return
      const controls = Array.from(ref.current?.querySelectorAll<HTMLElement>("a[href], button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled)") ?? [])
      if (!controls.length) return
      const first = controls[0], last = controls[controls.length - 1]
      if (event.shiftKey && (document.activeElement === first || document.activeElement === ref.current)) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("keydown", onKey)
      const target = returnTo?.current ?? previous
      if (target?.isConnected) target.focus()
    }
  }, [open])
}

function TypeIcon({ icon }: { icon: OpsIcon }) {
  return <svg className={`ops-type-icon ops-type-${icon}`} viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
    {icon === "markdown" && <><rect x="3" y="5" width="18" height="14" rx="2" /><path d="M6.5 15V9l2.5 3 2.5-3v6M15.5 9v6m-2-2 2 2 2-2" /></>}
    {icon === "text" && <><path d="M7 3.5h7l4 4v13H7z" /><path d="M14 3.5v4h4M9.5 12h6M9.5 15.5h6" /></>}
    {icon === "file" && <><path d="M7 3.5h7l4 4v13H7z" /><path d="M14 3.5v4h4" /></>}
    {icon === "notion" && <><rect x="4" y="4" width="16" height="16" rx="2.5" /><path d="M9 16V8l6 8V8" /></>}
    {icon === "google" && <><rect x="5" y="3.5" width="14" height="17" rx="2" /><path d="M8.5 9h7M8.5 12.5h7M8.5 16h4" /></>}
    {icon === "web" && <><circle cx="12" cy="12" r="8.5" /><path d="M3.5 12h17M12 3.5c-2.4 2.3-3.6 5.1-3.6 8.5s1.2 6.2 3.6 8.5c2.4-2.3 3.6-5.1 3.6-8.5S14.4 5.8 12 3.5" /></>}
  </svg>
}

function Modal({ open, title, onClose, children, returnTo, wide = false }: {
  open: boolean; title: string; onClose: () => void; children: ReactNode; returnTo?: RefObject<HTMLElement | null>; wide?: boolean
}) {
  const ref = useRef<HTMLDivElement>(null)
  useDialog(open, ref, onClose, returnTo)
  if (!open) return null
  return <div className="ops-overlay" onPointerDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <div ref={ref} className={`ops-dialog${wide ? " ops-dialog-wide" : ""}`} role="dialog" aria-modal="true" aria-label={title}>
      <header><h2>{title}</h2><button type="button" className="ops-icon-button" onClick={onClose} aria-label={W("close")}>×</button></header>
      {children}
    </div>
  </div>
}

function dateText(seconds: number): string {
  if (!seconds) return ""
  try { return new Date(seconds * 1000).toLocaleDateString(document.documentElement.lang || undefined, { year: "numeric", month: "short", day: "numeric" }) }
  catch { return new Date(seconds * 1000).toISOString().slice(0, 10) }
}

/** A name shown as a heading that becomes a field in place: Enter saves, Esc cancels. */
function InlineName({ value, label, editLabel, onSave, className }: {
  value: string; label: string; editLabel: string; onSave: (next: string) => Promise<string | null>; className?: string
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(value)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const trigger = useRef<HTMLButtonElement>(null)
  const field = useRef<HTMLInputElement>(null)
  const hint = useId()
  useEffect(() => { if (!editing) setDraft(value) }, [value, editing])
  useEffect(() => { if (editing) { field.current?.focus(); field.current?.select() } }, [editing])
  const stop = () => { setEditing(false); setError(""); requestAnimationFrame(() => trigger.current?.focus()) }
  const save = async () => {
    const next = draft.trim()
    if (next === value) { stop(); return }
    setBusy(true); setError("")
    const problem = await onSave(next)
    setBusy(false)
    if (problem) setError(problem)
    else stop()
  }
  if (!editing) return <button ref={trigger} type="button" className={`ops-inline-name ${className ?? ""}`} onClick={() => setEditing(true)}
    aria-label={`${editLabel}: ${value}`}><span>{value}</span><span className="ops-inline-pencil" aria-hidden="true">✎</span></button>
  return <div className={`ops-inline-edit ${className ?? ""}`}>
    <label><span className="ops-visually-hidden">{label}</span>
      <input ref={field} value={draft} disabled={busy} aria-describedby={hint} aria-invalid={!!error || undefined}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter") { event.preventDefault(); void save() }
          if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); stop() }
        }} /></label>
    <small id={hint}>{busy ? W("saving") : W("renameHint")}</small>
    {error && <p role="alert" className="ops-error">{error}</p>}
  </div>
}

// ---------------------------------------------------------------- the page

function OpsPage({ shown }: { shown: boolean }) {
  const [places, setPlaces] = useState<ProjectPlace[]>([])
  const [place, setPlace] = useState<ProjectPlace | null>(null)
  const [list, setList] = useState<OpsList | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")
  const [selected, setSelected] = useState<string | null>(null)
  const [filter, setFilter] = useState<OpsFilter>("all")
  const [dialog, setDialog] = useState<Dialog>(null)
  const [menuOpen, setMenuOpen] = useState(false)
  const [sessions, setSessions] = useState<SessionRow[] | null>(null)
  const [sessionsFailed, setSessionsFailed] = useState(false)
  const [live, setLive] = useState("")
  const phone = useMedia(PHONE)
  const addButton = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const rows = useRef(new Map<string, HTMLButtonElement>())
  const backTo = useRef<string | null>(null)
  const request = useRef(0)
  const statuses = useRef(new Map<string, string>())
  const [notice, setNotice] = useState<{ id: string; text: string } | null>(null)

  const refresh = useCallback(async (target: ProjectPlace | null = place) => {
    if (!target) return null
    const ticket = ++request.current
    try {
      const answer = await listOps(target.id)
      if (ticket !== request.current) return null
      const changed = changedStatus(statuses.current, answer.entries)
      statuses.current = new Map(answer.entries.map((row) => [row.id, row.status]))
      if (changed.length) setLive(changed.map((row) => W("statusLive", { name: entryName(row), status: W(statusView(row).word) })).join(" "))
      setList(answer); setError("")
      return answer
    } catch (cause) {
      if (ticket !== request.current) return null
      setError(cause instanceof OpsError && (cause.status === 404 || cause.status === 501 || cause.code === "not_implemented") ? W("machineTooOld") : W("listUnavailable"))
      return null
    }
  }, [place])

  const readSessions = useCallback(async () => {
    try { const snapshot = await readSessionsForWorkV2(); setSessions(snapshot.sessions ?? []); setSessionsFailed(false) }
    catch {
      // refusal-ok: the Session list only offers who may read a link; any failure leaves "Save without AI", which the page says
      setSessionsFailed(true)
    }
  }, [])

  useEffect(() => {
    if (!shown) return
    let live = true
    setLoading(true)
    readProjectPlaces().then((answer) => {
      if (!live) return
      setPlaces(answer.places ?? [])
      setPlace((current) => answer.places.find((candidate) => candidate.id === (current?.id ?? rememberedPlace())) ?? answer.places[0] ?? null)
      if (!answer.places.length) setLoading(false)
    }).catch(() => { if (live) { setError(W("projectUnavailable")); setLoading(false) } })
    void readSessions()
    return () => { live = false }
  }, [shown])

  useEffect(() => {
    if (!shown || !place) return
    rememberPlace(place.id)
    setLoading(true); setList(null); setSelected(null)
    void refresh(place).finally(() => setLoading(false))
  }, [shown, place?.id])

  // The agent's answer to a resolve request arrives with no event: while a link
  // is being read, read the list again (console.ops_resolving_poll_seconds).
  const resolving = awaitsAgent(list?.entries ?? [])
  useEffect(() => {
    if (!shown || !place || !resolving) return
    const timer = setInterval(() => { if (!document.hidden) void refresh(place) }, OPS_RESOLVING_POLL_MS)
    return () => clearInterval(timer)
  }, [shown, place?.id, resolving, refresh])

  useEffect(() => {
    if (!menuOpen) return
    const outside = (event: PointerEvent) => { if (!menu.current?.contains(event.target as Node)) setMenuOpen(false) }
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") { event.preventDefault(); setMenuOpen(false); addButton.current?.focus() } }
    menu.current?.querySelector<HTMLButtonElement>("[role=menuitem]")?.focus()
    document.addEventListener("pointerdown", outside)
    document.addEventListener("keydown", escape)
    return () => { document.removeEventListener("pointerdown", outside); document.removeEventListener("keydown", escape) }
  }, [menuOpen])

  // Back on the phone returns focus to the row the person opened.
  useEffect(() => {
    if (selected || !backTo.current) return
    const id = backTo.current
    backTo.current = null
    requestAnimationFrame(() => rows.current.get(id)?.focus())
  }, [selected])

  const entries = list?.entries ?? []
  const entry = entries.find((row) => row.id === selected) ?? null
  const projectSessions = useMemo(() => sameProjectSessions(sessions ?? [], list?.project_key ?? ""), [sessions, list?.project_key])
  const tree = useMemo(() => buildTree(list?.collections ?? [], entries), [list])
  const counts = useMemo(() => filterCounts(entries), [list])

  const open = (id: string) => { setSelected(id); if (phone) backTo.current = null }
  const back = () => { backTo.current = selected; setSelected(null) }
  const opened = async (id: string | undefined, notice?: string) => {
    setDialog(null)
    const answer = await refresh()
    if (id && answer?.entries.some((row) => row.id === id)) { setNotice(notice ? { id, text: notice } : null); setSelected(id) }
  }
  const announce = (row: OpsEntry) => setLive(W("statusLive", { name: entryName(row), status: W(statusView(row).word) }))

  const addItems: { word: OpsWord; dialog: Exclude<Dialog, null> }[] = [
    { word: "newMarkdown", dialog: "markdown" }, { word: "addProjectFile", dialog: "file" },
    { word: "pasteLink", dialog: "link" }, { word: "newCollection", dialog: "collection" },
  ]
  const addControl = <div className="ops-add" ref={menu}>
    <button ref={addButton} type="button" className="ops-primary" aria-haspopup="menu" aria-expanded={menuOpen} disabled={!place || !list}
      onClick={() => setMenuOpen(!menuOpen)}>+ {W("add")}</button>
    {menuOpen && <div className="ops-add-menu" role="menu" aria-label={W("addMenu")}
      onKeyDown={(event) => {
        if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return
        event.preventDefault()
        const items = Array.from(menu.current?.querySelectorAll<HTMLButtonElement>("[role=menuitem]") ?? [])
        const at = items.indexOf(document.activeElement as HTMLButtonElement)
        items[(at + (event.key === "ArrowDown" ? 1 : items.length - 1)) % items.length]?.focus()
      }}>
      {addItems.map((item) => <button key={item.dialog} type="button" role="menuitem" onClick={() => { setMenuOpen(false); setDialog(item.dialog) }}>{W(item.word)}</button>)}
    </div>}
  </div>

  const reader = entry && place && list && <OpsReader key={entry.id} place={place} entry={entry} list={list} notice={notice?.id === entry.id ? notice.text : ""} sessions={projectSessions}
    allSessions={sessions ?? []} sessionsFailed={sessionsFailed} phone={phone} onBack={back}
    onChanged={async (next) => { const answer = await refresh(); const row = answer?.entries.find((r) => r.id === (next?.id ?? entry.id)); if (row) announce(row) }}
    onRemoved={async () => { setSelected(null); await refresh() }} />

  const body = () => {
    if (loading && !list) return <LoadingSkeleton id="OPS-01" kind="rows" label={W("loading")} />
    if (!place) return !loading && <div className="ops-empty"><p>{W("noProjects")}</p></div>
    if (!list) return null
    if (!entries.length && !list.collections.length) return <div className="ops-empty">
      <div className="ops-empty-mark" aria-hidden="true"><TypeIcon icon="markdown" /></div>
      <h2>{W("emptyTitle")}</h2><p>{W("emptyDetail")}</p>
      <div className="ops-empty-actions">{addItems.slice(0, 3).map((item) => <button key={item.dialog} type="button" onClick={() => setDialog(item.dialog)}>{W(item.word)}</button>)}</div>
    </div>
    if (phone) {
      if (entry) return reader
      const shownRows = phoneRows(entries, filter)
      return <>
        <div className="ops-chips" role="group" aria-label={W("filterLabel")}>
          {OPS_FILTERS.map((name) => <button key={name} type="button" aria-pressed={filter === name} onClick={() => setFilter(name)}>
            {W(filterWord[name])} <span className="ops-chip-count">{counts[name]}</span></button>)}
        </div>
        {shownRows.length ? <ul className="ops-phone-list">{shownRows.map((row) => {
          const status = statusView(row)
          return <li key={row.id}><button type="button" ref={(node) => { if (node) rows.current.set(row.id, node); else rows.current.delete(row.id) }}
            onClick={() => open(row.id)}>
            <TypeIcon icon={typeIcon(row.type)} />
            <span className="ops-row-copy"><strong>{entryName(row)}</strong>
              <small>{W(typeWord(row.type))} · {dateText(row.updated_at)}</small></span>
            {status.tone !== "ready" && <span className={`ops-badge ops-tone-${status.tone}`}>{W(status.word)}</span>}
            <span aria-hidden="true" className="ops-row-chevron">›</span>
          </button></li>
        })}</ul> : <p className="ops-muted">{W("filterEmpty")}</p>}
      </>
    }
    return <div className="ops-split">
      <nav className="ops-tree" aria-label={W("treeLabel")}>
        {tree.map((group) => <section key={group.key} className="ops-tree-group">
          <h3>{group.collection ? group.collection.name : W(group.key === "files" ? "filesGroup" : "linksGroup")}</h3>
          {group.entries.length ? <ul>{group.entries.map((row) => {
            const status = statusView(row)
            return <li key={row.id}><button type="button" aria-current={row.id === selected ? "true" : undefined} onClick={() => open(row.id)}>
              <TypeIcon icon={typeIcon(row.type)} /><span className="ops-tree-title">{entryName(row)}</span>
              {status.tone !== "ready" && <span className={`ops-dot ops-tone-${status.tone}`} title={W(status.word)}><span className="ops-visually-hidden">{W(status.word)}</span></span>}
            </button></li>
          })}</ul> : <p className="ops-muted ops-tree-empty">{W("emptyCollection")}</p>}
        </section>)}
      </nav>
      <div className="ops-reader-pane">{reader || <p className="ops-muted ops-choose">{W("chooseEntry")}</p>}</div>
    </div>
  }

  return <section className="page ops-page" id="ops" data-page-view="ops" hidden={!shown}>
    <header className="ops-head">
      <div><h1 tabIndex={-1}>{W("title")}</h1><p>{W("intro")}</p></div>
      <div className="ops-head-tools">
        <ProjectPicker label={W("projectLabel")} places={places.map((row) => ({ id: row.id, label: row.label || row.path, icon: row.icon }))}
          value={place?.id ?? ""} onChange={(id) => setPlace(places.find((row) => row.id === id) ?? null)} />
        <button type="button" onClick={() => { void refresh(); void readSessions() }} disabled={!place || loading}>{W("reload")}</button>
        {addControl}
      </div>
    </header>
    {error && <p role="alert" className="ops-error">{error}</p>}
    <p className="ops-visually-hidden" aria-live="polite">{live}</p>
    {body()}
    {place && list && <>
      <MarkdownDialog open={dialog === "markdown"} place={place} onClose={() => setDialog(null)} onCreated={(row) => void opened(row?.id)} returnTo={addButton} />
      <FileDialog open={dialog === "file"} place={place} onClose={() => setDialog(null)} onCreated={(row) => void opened(row?.id)} returnTo={addButton} />
      <LinkDialog open={dialog === "link"} place={place} sessions={projectSessions} sessionsFailed={sessionsFailed}
        onClose={() => setDialog(null)} onCreated={(row, notice) => void opened(row?.id, notice)} returnTo={addButton} />
      <CollectionDialog open={dialog === "collection"} place={place} onClose={() => setDialog(null)} onCreated={() => void opened(undefined)} returnTo={addButton} />
    </>}
  </section>
}

// ---------------------------------------------------------------- reader

function OpsReader({ notice, place, entry, list, sessions, allSessions, sessionsFailed, phone, onBack, onChanged, onRemoved }: {
  notice: string; place: ProjectPlace; entry: OpsEntry; list: OpsList; sessions: SessionRow[]; allSessions: SessionRow[]; sessionsFailed: boolean
  phone: boolean; onBack: () => void; onChanged: (entry?: OpsEntry) => Promise<void>; onRemoved: () => Promise<void>
}) {
  const [content, setContent] = useState<string | null>(null)
  const [version, setVersion] = useState("")
  const [reading, setReading] = useState(false)
  const [readError, setReadError] = useState("")
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState("")
  const [conflict, setConflict] = useState(false)
  const [busy, setBusy] = useState<"" | "save" | "latest" | "move" | "remove" | "send" | "resolve">("")
  const [message, setMessage] = useState(notice)
  const [confirmRemove, setConfirmRemove] = useState(false)
  const [terminal, setTerminal] = useState(() => entry.resolve?.terminal && sessions.some((row) => row.id === entry.resolve?.terminal) ? entry.resolve.terminal : sessions[0]?.id ?? "")
  const heading = useRef<HTMLHeadingElement>(null)
  const status = statusView(entry)
  const sessionLabel = (id: string) => { const row = allSessions.find((candidate) => candidate.id === id); return row ? sessionName(row) : id }

  const read = useCallback(async () => {
    if (entry.kind !== "file" || entry.status !== "ready") return
    setReading(true); setReadError("")
    try {
      const answer = await readOpsEntry(place.id, entry.id)
      setContent(answer.content ?? null); setVersion(answer.version ?? "")
    } catch { setReadError(W("contentUnavailable")) }
    finally { setReading(false) }
  }, [place.id, entry.id, entry.status])
  useEffect(() => { void read() }, [read])
  useEffect(() => { if (phone) heading.current?.focus() }, [])

  const patch = async (body: Parameters<typeof updateOpsEntry>[2], signature: string, fallback: OpsWord = "saveRefused"): Promise<string | null> => {
    const key = keys.key(signature)
    try { const answer = await updateOpsEntry(place.id, entry.id, body, key); keys.settle(signature); await onChanged(answer.entry); return null }
    catch (cause) { if (!(cause instanceof OpsError && cause.uncertain)) keys.settle(signature); return refusalText(cause, fallback) }
  }

  const rename = (next: string) => {
    const problem = problemWord(checkFileName(next))
    if (problem) return Promise.resolve(W(problem))
    return patch({ rename: next }, `rename:${entry.id}:${next}`)
  }
  const retitle = (next: string) => {
    const problem = problemWord(checkTitle(next))
    if (problem) return Promise.resolve(W(problem))
    return patch({ title: next }, `title:${entry.id}:${next}`)
  }

  const save = async () => {
    const signature = `save:${entry.id}:${version}:${draft.length}`
    setBusy("save"); setMessage("")
    try {
      const answer = await updateOpsEntry(place.id, entry.id, { content: draft, expected_version: version }, keys.key(signature))
      keys.settle(signature)
      setContent(draft); setVersion(answer.version ?? version); setEditing(false); setConflict(false); setMessage(W("saved"))
      await onChanged(answer.entry)
    } catch (cause) {
      if (cause instanceof OpsError && cause.code === "file_changed") { keys.settle(signature); setConflict(true) }
      else { if (!(cause instanceof OpsError && cause.uncertain)) keys.settle(signature); setMessage(refusalText(cause, "saveRefused")) }
    } finally { setBusy("") }
  }
  const loadLatest = async (keepDraft: boolean) => {
    setBusy("latest"); setMessage("")
    try {
      const answer = await readOpsEntry(place.id, entry.id)
      setContent(answer.content ?? null); setVersion(answer.version ?? "")
      setConflict(false)
      if (keepDraft) setMessage(W("latestLoaded"))
      else { setEditing(false); setDraft("") }
    } catch { setMessage(W("contentUnavailable")) }
    finally { setBusy("") }
  }

  const move = async (collection: string) => {
    setBusy("move"); setMessage("")
    const problem = await patch({ collection, position: nextPosition(list.entries, collection) }, `move:${entry.id}:${collection}`, "moveRefused")
    if (problem) setMessage(problem)
    setBusy("")
  }

  const remove = async () => {
    const signature = `delete:${entry.id}`
    setBusy("remove"); setMessage("")
    try { await deleteOps(place.id, entry.id, keys.key(signature)); keys.settle(signature); await onRemoved() }
    catch (cause) {
      if (cause instanceof OpsError && cause.uncertain) setMessage(W("removeUnknown"))
      else { keys.settle(signature); setMessage(refusalText(cause)) }
      setBusy("")
    }
  }

  const send = async () => {
    if (!entry.resolve) return
    setBusy("send"); setMessage("")
    try { await directTodoActionV2(entry.resolve.terminal, entry.resolve.todo, "send") }
    catch (cause) { setMessage(sendFailureText(cause)) }
    finally { setBusy(""); await onChanged() }
  }

  const askAgain = async () => {
    if (!terminal) { setMessage(W("chooseSessionFirst")); return }
    setBusy("resolve"); setMessage("")
    const problem = await askSession(place, entry.id, terminal)
    if (problem) setMessage(problem)
    setBusy("")
    await onChanged()
  }

  const sessionPicker = sessions.length ? <label className="ops-field ops-inline-field">{W("linkSession")}
    <select value={terminal} onChange={(event) => setTerminal(event.target.value)}>
      {sessions.map((row) => <option key={row.id} value={row.id}>{sessionName(row)}</option>)}
    </select></label> : <p className="ops-muted">{W(sessionsFailed ? "sessionsUnavailable" : "linkNoSessions")}</p>

  const original = entry.url && <a className="ops-link-button" href={entry.url} target="_blank" rel="noopener noreferrer">{W("openOriginal")} <span aria-hidden="true">↗</span></a>

  return <article className="ops-reader" aria-labelledby="ops-reader-title">
    {phone && <button type="button" className="ops-back" onClick={onBack}><span aria-hidden="true">‹</span> {W("back")}</button>}
    <header className="ops-reader-head">
      <TypeIcon icon={typeIcon(entry.type)} />
      <div className="ops-reader-names">
        <h2 id="ops-reader-title" ref={heading} tabIndex={-1}>
          <InlineName value={entryName(entry)} label={W("titleField")} editLabel={W("renameTitle")} onSave={retitle} className="ops-title-name" />
        </h2>
        {entry.kind === "file" && entry.path && <div className="ops-file-name">
          <InlineName value={fileNameOf(entry.path)} label={W("fileName")} editLabel={W("renameFile")} onSave={rename} />
          <small className="ops-path" title={entry.path}>{entry.path}</small>
        </div>}
        <p className="ops-meta">{W(typeWord(entry.type))} · {W("updated", { date: dateText(entry.updated_at) })}
          {status.tone !== "ready" && <> · <span className={`ops-badge ops-tone-${status.tone}`}>{W(status.word)}</span></>}</p>
      </div>
      <div className="ops-reader-tools">
        <label className="ops-field ops-inline-field">{W("collectionLabel")}
          <select value={list.collections.some((row) => row.id === entry.collection) ? entry.collection : ""} disabled={!!busy}
            onChange={(event) => void move(event.target.value)}>
            <option value="">{W("noCollection")}</option>
            {[...list.collections].sort((a, b) => a.position - b.position).map((row: OpsCollection) => <option key={row.id} value={row.id}>{row.name}</option>)}
          </select></label>
        {editable(entry) && !editing && content !== null && <button type="button" onClick={() => { setDraft(content); setEditing(true); setMessage("") }}>{W("edit")}</button>}
      </div>
    </header>

    {message && <p role="status" className="ops-message">{message}</p>}

    {entry.status === "missing" && <div className="ops-panel ops-panel-attention"><p>{W("missingDetail")}</p></div>}
    {entry.status === "unreadable" && <div className="ops-panel"><p>{W("unreadableDetail")}</p>{entry.access_note && <p className="ops-muted">{entry.access_note}</p>}</div>}

    {entry.status === "needs_access" && <div className="ops-panel ops-panel-attention">
      <h3>{W("needsAccessTitle")}</h3>
      {entry.access_note && <blockquote className="ops-note">{entry.access_note}</blockquote>}
      <h4>{W("needsAccessWays")}</h4>
      <ol><li>{W("needsAccessShare")}</li><li>{W("needsAccessConnect")}</li><li>{W("needsAccessPaste")}</li></ol>
      {sessionPicker}
      <div className="ops-actions">
        <button type="button" className="ops-primary" disabled={!!busy || !terminal} aria-busy={busy === "resolve"} onClick={() => void askAgain()}>
          <ActionProgress busy={busy === "resolve"} label={W("sending")}>{W("grantedRetry")}</ActionProgress></button>
        {original}
      </div>
    </div>}

    {entry.status === "resolving" && <div className="ops-panel">
      <p className="ops-resolve" aria-live="polite">{status.resolve ? W(status.resolve.word, { session: sessionLabel(entry.resolve!.terminal) }) : W("statusResolving")}</p>
      {status.actions.includes("retryResolve") && sessionPicker}
      <div className="ops-actions">
        {status.actions.includes("send") && <button type="button" className="ops-primary" disabled={!!busy} aria-busy={busy === "send"} onClick={() => void send()}>
          <ActionProgress busy={busy === "send"} label={W("sending")}>{W("send")}</ActionProgress></button>}
        {status.actions.includes("retryResolve") && <button type="button" className="ops-primary" disabled={!!busy || !terminal} aria-busy={busy === "resolve"} onClick={() => void askAgain()}>
          <ActionProgress busy={busy === "resolve"} label={W("sending")}>{W("retryResolve")}</ActionProgress></button>}
        {original}
      </div>
    </div>}

    {entry.kind === "link" && entry.status === "ready" && <div className="ops-panel">
      <p className="ops-url">{entry.url}</p><div className="ops-actions">{original}</div>
    </div>}

    {entry.kind === "file" && entry.status === "ready" && <div className="ops-content">
      {reading && <LoadingSkeleton id="OPS-02" kind="text" label={W("loading")} />}
      {readError && <p role="alert" className="ops-error">{readError} <button type="button" onClick={() => void read()}>{W("reload")}</button></p>}
      {editing ? <form className="ops-editor" onSubmit={(event) => { event.preventDefault(); void save() }}>
        <label className="ops-field">{W("editLabel")}<textarea value={draft} rows={18} onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => { if (event.key === "Escape") event.stopPropagation() }} /></label>
        {conflict && <div className="ops-panel ops-panel-attention" role="alert"><p>{W("fileChanged")}</p>
          <div className="ops-actions">
            <button type="button" disabled={!!busy} aria-busy={busy === "latest"} onClick={() => void loadLatest(true)}><ActionProgress busy={busy === "latest"}>{W("useLatest")}</ActionProgress></button>
            <button type="button" disabled={!!busy} onClick={() => void loadLatest(false)}>{W("discardDraft")}</button>
          </div></div>}
        <div className="ops-actions">
          <button type="submit" className="ops-primary" disabled={!!busy || conflict} aria-busy={busy === "save"}><ActionProgress busy={busy === "save"} label={W("saving")}>{W("save")}</ActionProgress></button>
          <button type="button" disabled={!!busy} onClick={() => { setEditing(false); setConflict(false) }}>{W("cancel")}</button>
        </div>
      </form> : content !== null && (entry.type === "markdown"
        ? <div className="ops-markdown" dangerouslySetInnerHTML={{ __html: richText(content, { codeCopy: false }) }} />
        : <pre className="ops-text">{content}</pre>)}
    </div>}

    <footer className="ops-reader-foot">
      {confirmRemove ? <div className="ops-confirm" role="group" aria-label={W("removeFromOps")}>
        <p>{W("removeConfirm", { name: entryName(entry) })}</p>
        <div className="ops-actions">
          <button type="button" className="ops-danger" disabled={!!busy} aria-busy={busy === "remove"} onClick={() => void remove()}>
            <ActionProgress busy={busy === "remove"} label={W("removing")}>{W("removeYes")}</ActionProgress></button>
          <button type="button" disabled={!!busy} onClick={() => setConfirmRemove(false)}>{W("cancel")}</button>
        </div>
      </div> : <button type="button" className={entry.status === "missing" ? "ops-danger" : "ops-quiet"} onClick={() => setConfirmRemove(true)}>{W("removeFromOps")}</button>}
    </footer>
  </article>
}

/**
 * Asks a Session to read a link: the machine creates the to-do (not sent),
 * then the existing send route delivers it. A refused or lost send leaves the
 * row `unsent`, which the reader offers to Send — never a second request.
 */
async function askSession(place: ProjectPlace, id: string, terminal: string): Promise<string | null> {
  const signature = `resolve:${id}:${terminal}`
  let todo: string
  try {
    const answer = await requestOpsResolve(place.id, id, terminal, keys.key(signature))
    keys.settle(signature)
    todo = answer.todo.id
  } catch (cause) {
    if (!(cause instanceof OpsError && cause.uncertain)) keys.settle(signature)
    return refusalText(cause)
  }
  try { await directTodoActionV2(terminal, todo, "send"); return null }
  catch (cause) { return sendFailureText(cause) }
}

// ---------------------------------------------------------------- dialogs

type DialogProps = { open: boolean; place: ProjectPlace; onClose: () => void; onCreated: (entry: OpsEntry | undefined, notice?: string) => void; returnTo: RefObject<HTMLElement | null> }

function MarkdownDialog({ open, place, onClose, onCreated, returnTo }: DialogProps) {
  const [name, setName] = useState("")
  const [content, setContent] = useState("")
  const [setup, setSetup] = useState<OpsSetup | null>(null)
  const [checking, setChecking] = useState(false)
  const [include, setInclude] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const ticket = useRef(0)

  const readSetup = async () => {
    const request = ++ticket.current
    setChecking(true); setError("")
    try {
      let answer = await readOpsSetup(place.id)
      if (needsGitChoice(answer)) answer = await readOpsSetup(place.id, true)
      if (request !== ticket.current) return
      setSetup(answer); setInclude(includeInGitDefault(answer.visibility))
    } catch { if (request === ticket.current) setError(W("setupUnavailable")) }
    finally { if (request === ticket.current) setChecking(false) }
  }
  useEffect(() => {
    if (!open) { ticket.current++; return }
    setName(""); setContent(""); setSetup(null); setError(""); setBusy(false)
    void readSetup()
  }, [open, place.id])

  const submit = async () => {
    const clean = markdownName(name)
    const problem = problemWord(checkFileName(clean))
    if (problem) { setError(W(problem)); return }
    if (!setup) { setError(W("setupUnavailable")); return }
    const git = needsGitChoice(setup) ? (include ? "include" as const : "exclude" as const) : undefined
    const signature = `markdown:${place.id}:${clean}`
    setBusy(true); setError("")
    try {
      const answer = await createOps(place.id, { kind: "markdown", name: clean, content, ...(git ? { git } : {}) }, keys.key(signature))
      keys.settle(signature); onCreated(answer.entry)
    } catch (cause) {
      if (cause instanceof OpsError && cause.code === "ops_git_choice_required") { keys.settle(signature); await readSetup(); setBusy(false); return }
      if (!(cause instanceof OpsError && cause.uncertain)) keys.settle(signature)
      setError(refusalText(cause)); setBusy(false)
    }
  }

  const gitChoice = setup && needsGitChoice(setup)
  return <Modal open={open} title={W("markdownDialog")} onClose={() => { if (!busy) onClose() }} returnTo={returnTo}>
    <form className="ops-form" onSubmit={(event) => { event.preventDefault(); void submit() }}>
      <label className="ops-field">{W("markdownName")}<input value={name} onChange={(event) => setName(event.target.value)} autoComplete="off" />
        <small>{W("markdownNameHint", { name: markdownName(name) || "…" })}</small></label>
      <label className="ops-field">{W("markdownContent")}<textarea value={content} rows={8} onChange={(event) => setContent(event.target.value)} /></label>
      {checking && <p role="status" className="ops-muted">{W("gitChecking")}</p>}
      {gitChoice && !checking && <div className="ops-panel" role="group" aria-label={W("includeInGit")}>
        <p>{W("gitQuestion")}</p>
        <p className={setup.visibility === "public" ? "ops-warning" : "ops-muted"}>{W(visibilityWord(setup.visibility))}</p>
        <label className="ops-check"><input type="checkbox" checked={include} onChange={(event) => setInclude(event.target.checked)} /> {W("includeInGit")}</label>
        <small className="ops-muted">{W("includeInGitHint")}</small>
      </div>}
      {error && <p role="alert" className="ops-error">{error}</p>}
      <div className="ops-actions">
        <button type="submit" className="ops-primary" disabled={busy || checking} aria-busy={busy}><ActionProgress busy={busy} label={W("saving")}>{W("createMarkdown")}</ActionProgress></button>
        <button type="button" disabled={busy} onClick={onClose}>{W("cancel")}</button>
        {error === W("setupUnavailable") && <button type="button" onClick={() => void readSetup()}>{W("reload")}</button>}
      </div>
    </form>
  </Modal>
}

function FileDialog({ open, place, onClose, onCreated, returnTo }: DialogProps) {
  const [directory, setDirectory] = useState("")
  const [listing, setListing] = useState<TreeEntry[]>([])
  const [truncated, setTruncated] = useState(false)
  const [loading, setLoading] = useState(false)
  const [picked, setPicked] = useState<TreeEntry | null>(null)
  const [title, setTitle] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const ticket = useRef(0)

  const browse = async (next: string) => {
    const request = ++ticket.current
    setLoading(true); setError(""); setPicked(null)
    try {
      const answer = await listProjectDirectory(place.id, next)
      if (request !== ticket.current) return
      setDirectory(answer.directory ?? next); setListing(answer.entries); setTruncated(answer.truncated)
    } catch { if (request === ticket.current) setError(W("fileListUnavailable")) }
    finally { if (request === ticket.current) setLoading(false) }
  }
  useEffect(() => {
    if (!open) { ticket.current++; return }
    setTitle(""); setBusy(false); void browse("")
  }, [open, place.id])

  const submit = async () => {
    if (!picked) return
    if (title.trim()) { const problem = problemWord(checkTitle(title)); if (problem) { setError(W(problem)); return } }
    const signature = `file:${place.id}:${picked.path}`
    setBusy(true); setError("")
    try {
      const answer = await createOps(place.id, { kind: "file", path: picked.path, ...(title.trim() ? { title: title.trim() } : {}) }, keys.key(signature))
      keys.settle(signature); onCreated(answer.entry)
    } catch (cause) {
      if (!(cause instanceof OpsError && cause.uncertain)) keys.settle(signature)
      setError(refusalText(cause)); setBusy(false)
    }
  }
  const parent = directory.includes("/") ? directory.slice(0, directory.lastIndexOf("/")) : ""
  const sorted = [...listing].filter((row) => row.kind === "directory" || row.kind === "file")
    .sort((a, b) => (a.kind === b.kind ? a.name.localeCompare(b.name) : a.kind === "directory" ? -1 : 1))

  return <Modal open={open} title={W("fileDialog")} onClose={() => { if (!busy) onClose() }} returnTo={returnTo} wide>
    <div className="ops-form">
      <div className="ops-browse-head">
        <button type="button" disabled={!directory || loading} onClick={() => void browse(parent)}><span aria-hidden="true">↑</span> {W("fileUp")}</button>
        <code className="ops-path">{directory || W("fileRoot")}</code>
      </div>
      {loading && <LoadingSkeleton id="OPS-03" kind="rows" label={W("loading")} />}
      {!loading && <ul className="ops-browse" aria-label={directory || W("fileRoot")}>
        {sorted.map((row) => <li key={row.path}>
          {row.kind === "directory"
            ? <button type="button" onClick={() => void browse(row.path)}><span aria-hidden="true">▸</span> {row.name}/</button>
            : <button type="button" aria-pressed={picked?.path === row.path} onClick={() => setPicked(row)}><TypeIcon icon={/\.(md|markdown)$/i.test(row.name) ? "markdown" : "file"} /> {row.name}</button>}
        </li>)}
        {!sorted.length && !error && <li className="ops-muted">{W("fileEmpty")}</li>}
      </ul>}
      {truncated && <p className="ops-muted">{W("fileTruncated")}</p>}
      {picked && <label className="ops-field">{W("linkName")}<input value={title} onChange={(event) => setTitle(event.target.value)} placeholder={picked.name.replace(/\.[^.]+$/, "")} /></label>}
      {error && <p role="alert" className="ops-error">{error}</p>}
      <div className="ops-actions">
        <button type="button" className="ops-primary" disabled={!picked || busy} aria-busy={busy} onClick={() => void submit()}><ActionProgress busy={busy} label={W("saving")}>{W("filePick")}</ActionProgress></button>
        <button type="button" disabled={busy} onClick={onClose}>{W("cancel")}</button>
      </div>
    </div>
  </Modal>
}

function LinkDialog({ open, place, sessions, sessionsFailed, onClose, onCreated, returnTo }: DialogProps & { sessions: SessionRow[]; sessionsFailed: boolean }) {
  const [url, setUrl] = useState("")
  const [title, setTitle] = useState("")
  const [terminal, setTerminal] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  useEffect(() => {
    if (!open) return
    setUrl(""); setTitle(""); setError(""); setBusy(false); setTerminal(sessions[0]?.id ?? "")
  }, [open])

  const submit = async () => {
    const urlProblem = checkLinkUrl(url)
    if (urlProblem) { setError(W(urlProblem === "tooLong" ? "tooLong" : "badUrl")); return }
    if (title.trim()) { const problem = problemWord(checkTitle(title)); if (problem) { setError(W(problem)); return } }
    const signature = `link:${place.id}:${url.trim()}`
    setBusy(true); setError("")
    let created: OpsEntry | undefined
    try {
      const answer = await createOps(place.id, { kind: "link", url: url.trim(), ...(title.trim() ? { title: title.trim() } : {}) }, keys.key(signature))
      keys.settle(signature); created = answer.entry
    } catch (cause) {
      if (!(cause instanceof OpsError && cause.uncertain)) keys.settle(signature)
      setError(refusalText(cause)); setBusy(false); return
    }
    // The link is saved whatever happens next; a failed request is shown on its page.
    const problem = terminal && created ? await askSession(place, created.id, terminal) : null
    onCreated(created, problem ?? undefined)
  }

  return <Modal open={open} title={W("linkDialog")} onClose={() => { if (!busy) onClose() }} returnTo={returnTo}>
    <form className="ops-form" onSubmit={(event) => { event.preventDefault(); void submit() }}>
      <label className="ops-field">{W("linkUrl")}<input type="url" inputMode="url" value={url} onChange={(event) => setUrl(event.target.value)} placeholder="https://" autoComplete="off" />
        <small>{W("linkUrlHint")}</small></label>
      <label className="ops-field">{W("linkName")}<input value={title} onChange={(event) => setTitle(event.target.value)} autoComplete="off" />
        <small>{W("linkNameHint")}</small></label>
      <label className="ops-field">{W("linkSession")}
        <select value={terminal} onChange={(event) => setTerminal(event.target.value)}>
          <option value="">{W("linkNoSession")}</option>
          {sessions.map((row) => <option key={row.id} value={row.id}>{sessionName(row)}</option>)}
        </select>
        <small>{sessions.length ? W("linkSessionHint") : W(sessionsFailed ? "sessionsUnavailable" : "linkNoSessions")}</small></label>
      {error && <p role="alert" className="ops-error">{error}</p>}
      <div className="ops-actions">
        <button type="submit" className="ops-primary" disabled={busy} aria-busy={busy}><ActionProgress busy={busy} label={W("saving")}>{W("saveLink")}</ActionProgress></button>
        <button type="button" disabled={busy} onClick={onClose}>{W("cancel")}</button>
      </div>
    </form>
  </Modal>
}

function CollectionDialog({ open, place, onClose, onCreated, returnTo }: DialogProps) {
  const [name, setName] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  useEffect(() => { if (open) { setName(""); setError(""); setBusy(false) } }, [open])
  const submit = async () => {
    const problem = problemWord(checkTitle(name, OPS_COLLECTION_NAME_BYTES))
    if (problem) { setError(W(problem)); return }
    const signature = `collection:${place.id}:${name.trim()}`
    setBusy(true); setError("")
    try { await createOps(place.id, { kind: "collection", name: name.trim() }, keys.key(signature)); keys.settle(signature); onCreated(undefined) }
    catch (cause) { if (!(cause instanceof OpsError && cause.uncertain)) keys.settle(signature); setError(refusalText(cause)); setBusy(false) }
  }
  return <Modal open={open} title={W("collectionDialog")} onClose={() => { if (!busy) onClose() }} returnTo={returnTo}>
    <form className="ops-form" onSubmit={(event) => { event.preventDefault(); void submit() }}>
      <label className="ops-field">{W("collectionName")}<input value={name} onChange={(event) => setName(event.target.value)} autoComplete="off" /></label>
      {error && <p role="alert" className="ops-error">{error}</p>}
      <div className="ops-actions">
        <button type="submit" className="ops-primary" disabled={busy} aria-busy={busy}><ActionProgress busy={busy} label={W("saving")}>{W("createCollection")}</ActionProgress></button>
        <button type="button" disabled={busy} onClick={onClose}>{W("cancel")}</button>
      </div>
    </form>
  </Modal>
}

export const page: PageModule = { id: "ops", Component: OpsPage }
