import { useEffect, useRef, useState, type ReactNode } from "react"
import type * as W from "@clawdline/contract"
import { BotAvatar } from "./BotAvatar"
import { BotMessageMarkdown } from "./BotMessageMarkdown"
import { charterPlaybooks, playbookReaders, readsCurrentAt, type CharterApi, type CharterPlaybookDraft, type CharterProject } from "./charter-api"
import { assignmentChange, changedPlaybooks, firstSentence, playbookReasonKey, playbookState, unreadByActivity, type ChangedPlaybook, type PlaybookWrite } from "./charter-playbooks"
import type { DiffLine } from "./charter-text"
import { charterFormat, charterNames, charterWord } from "./charter-words"
import type { Words } from "./words"

// The charter's playbooks: a section of cards under the charter's sections,
// and a side panel (a full-screen page on a phone) that shows one playbook,
// who it is assigned to and who has read it, and edits, reassigns, creates or
// deletes it. Every write is a new charter version through the same write
// the rest of the page uses, so a stale one rereads and says so.

type Doc = W.ProjectAssistantCharterResponse
type Reader = W.ProjectAssistantCharterReader
type Playbook = W.ProjectAssistantCharterPlaybook
export type BotAppearanceOf = (botID: string) => W.ProjectAssistantAppearance | undefined
type Write = (action: (signal: AbortSignal) => Promise<unknown>, done: (next: Doc | null) => string, onConflict?: (current: Doc | null, field: string) => string) => Promise<boolean>
type Notice = { text: string; tone: "ok" | "error" } | null

/** What the panel shows: one playbook, its editor, or a new one. */
export type PlaybookView = { mode: "detail"; id: string } | { mode: "edit"; id: string; title: string; text: string } | { mode: "new"; title: string; text: string; bots: string[] }

// A Bot whose appearance is not in the roster yet still gets the outline avatar.
const fallbackAppearance = { shape: "blob", color: "" } as W.ProjectAssistantAppearance

export interface PlaybooksInput {
  api: CharterApi; project: CharterProject; doc: Doc | null; readers: readonly Reader[]; busy: boolean; narrow: boolean
  write: Write; notice: Notice; open: boolean; onOpen: () => void; onClose: () => void
  appearance?: BotAppearanceOf; refreshReaders: () => void; w: Words
}

/** The section and its panel, or nothing from a machine older than API 127. */
export function usePlaybooks(input: PlaybooksInput): { section: ReactNode; panel: ReactNode } {
  const { api, project, doc, readers, busy, narrow, write, notice, open, onOpen, onClose, appearance, refreshReaders, w } = input
  const [view, setView] = useState<PlaybookView | null>(null)
  if (!doc || doc.version < 1 || !api.savePlaybook || !api.removePlaybook) return { section: null, panel: null }
  const avatar = (botID: string) => appearance?.(botID) ?? fallbackAppearance
  const groups = playbookReaders(doc, { contract_version: "", machine_id: project.machine_id, project_id: project.project_id, version: doc.version, readers: [...readers] })
  const show = (next: PlaybookView) => { setView(next); onOpen(); refreshReaders() }
  const save = async (draft: CharterPlaybookDraft, kind: PlaybookWrite) => {
    const known = new Set(charterPlaybooks(doc).map(item => item.id))
    let saved = draft.id ?? ""
    const ok = await write(signal => api.savePlaybook!(project, { expected_version: doc.version, playbook: draft, reason: charterFormat(playbookReasonKey(kind), draft.title) }, signal),
      next => { saved ||= (next && charterPlaybooks(next).find(item => !known.has(item.id))?.id) || ""; return charterFormat("saved", next?.version ?? doc.version + 1) },
      current => charterFormat("conflictEdit", current?.version ?? doc.version))
    if (ok && saved) setView({ mode: "detail", id: saved })
    return ok
  }
  const remove = async (playbook: Playbook) => {
    const ok = await write(signal => api.removePlaybook!(project, { expected_version: doc.version, id: playbook.id }, signal),
      next => charterFormat("playbookRemoved", playbook.title, next?.version ?? doc.version + 1), current => charterFormat("conflictVersion", current?.version ?? doc.version))
    if (ok) { setView(null); onClose() }
  }
  const section = <section className="pa-charter-playbooks" aria-label={charterWord("playbooks")}>
    <header className="pa-charter-playbooks-head">
      <div><h3>{charterWord("playbooks")}</h3><p>{charterWord("playbooksHint")}</p></div>
      <button type="button" className="pa-pill" disabled={busy} onClick={() => show({ mode: "new", title: "", text: "", bots: [] })}>{charterWord("playbookAdd")}</button>
    </header>
    {!groups.length && <p className="pa-charter-hint">{charterWord("playbooksEmpty")}</p>}
    {groups.length > 0 && <ul className="pa-charter-playbook-grid">
      {groups.map(group => <li key={group.playbook.id}>
        <PlaybookCard playbook={group.playbook} assigned={readers.filter(r => group.playbook.bot_ids.includes(r.bot_id))} state={playbookState(group)} avatar={avatar}
          selected={open && view?.mode !== "new" && (view as { id?: string } | null)?.id === group.playbook.id} onOpen={() => show({ mode: "detail", id: group.playbook.id })}/>
      </li>)}
    </ul>}
  </section>
  const playbook = view && view.mode !== "new" ? charterPlaybooks(doc).find(item => item.id === view.id) : undefined
  const panel = <aside hidden={!open} inert={!open ? true : undefined} className="pa-info-panel pa-side-inspector pa-charter-playbook" aria-label={playbook ? charterFormat("playbookOpen", playbook.title) : charterWord("playbookNew")}
    onKeyDown={event => { if (event.key === "Escape" && !event.defaultPrevented) { event.stopPropagation(); onClose() } }}>
    {open && view && <PlaybookPanel view={view} playbook={playbook} doc={doc} readers={readers} busy={busy} narrow={narrow} notice={notice} avatar={avatar} w={w}
      onView={setView} onClose={onClose} onSave={save} onRemove={remove}/>}
  </aside>
  return { section, panel }
}

function PlaybookCard({ playbook, assigned, state, avatar, selected, onOpen }: {
  playbook: Playbook; assigned: readonly Reader[]; state: ReturnType<typeof playbookState>; avatar: (botID: string) => W.ProjectAssistantAppearance; selected: boolean; onOpen: () => void
}) {
  return <button type="button" className="pa-charter-playbook-card" data-state={state} aria-current={selected || undefined} title={charterFormat("playbookOpen", playbook.title)} onClick={onOpen}>
    <strong className="pa-charter-playbook-name">{playbook.title}</strong>
    <span className="pa-charter-playbook-first">{firstSentence(playbook.text) || charterWord("playbookEmptyText")}</span>
    <span className="pa-charter-playbook-foot">
      {assigned.length > 0 && <span className="pa-charter-playbook-bots">{assigned.map(reader => <span key={reader.bot_id} className="pa-charter-playbook-bot">
        <BotAvatar appearance={avatar(reader.bot_id)} size={22}/><span>{reader.name}</span>
      </span>)}</span>}
      <PlaybookStateLabel state={state}/>
    </span>
  </button>
}

function PlaybookStateLabel({ state }: { state: ReturnType<typeof playbookState> }) {
  const dot = state === "current" ? "●" : state === "unread" ? "○" : ""
  const text = charterWord(state === "current" ? "playbookCurrent" : state === "unread" ? "playbookUnread" : "playbookUnassigned")
  return <span className="pa-charter-playbook-state" data-state={state}>{dot && <span aria-hidden="true">{dot} </span>}{text}</span>
}

function PlaybookPanel({ view, playbook, doc, readers, busy, narrow, notice, avatar, w, onView, onClose, onSave, onRemove }: {
  view: PlaybookView; playbook: Playbook | undefined; doc: Doc; readers: readonly Reader[]; busy: boolean; narrow: boolean; notice: Notice
  avatar: (botID: string) => W.ProjectAssistantAppearance; w: Words
  onView: (view: PlaybookView) => void; onClose: () => void; onSave: (draft: CharterPlaybookDraft, kind: PlaybookWrite) => Promise<boolean>; onRemove: (playbook: Playbook) => Promise<void>
}) {
  const body = useRef<HTMLDivElement>(null)
  useEffect(() => { if (view.mode === "detail") body.current?.querySelector<HTMLElement>(".pa-charter-history-close")?.focus({ preventScroll: true }) }, [view.mode])
  const next = doc.version + 1
  if (view.mode === "new") return <PlaybookEditor key="new" title={view.title} text={view.text} bots={view.bots} chooseBots readers={readers} next={next} busy={busy} full={narrow} notice={notice} avatar={avatar}
    heading={charterWord("playbookNew")} onCancel={onClose} onSave={(title, text, bots) => onSave({ title, text, bot_ids: bots }, "add")}/>
  if (!playbook) return <div ref={body} className="pa-charter-history-body">
    <PanelHead title={charterWord("playbooks")} onClose={onClose}/>
    <p className="pa-charter-hint">{charterWord("playbookMissing")}</p>
  </div>
  if (view.mode === "edit") return <PlaybookEditor key={`edit-${playbook.id}`} title={view.title} text={view.text} bots={playbook.bot_ids} readers={readers} next={next} busy={busy} full={narrow} notice={notice} avatar={avatar}
    heading={charterWord("playbookEditTitle")} onCancel={() => onView({ mode: "detail", id: playbook.id })}
    onSave={(title, text) => onSave({ id: playbook.id, title, text, bot_ids: playbook.bot_ids }, "edit")}/>
  return <div ref={body} className="pa-charter-history-body pa-charter-playbook-body">
    <PanelHead title={playbook.title} onClose={onClose}/>
    {notice?.text && <p className="pa-charter-panel-notice" data-tone={notice.tone} role="status">{notice.text}</p>}
    <PlaybookDetail playbook={playbook} doc={doc} readers={readers} busy={busy} narrow={narrow} avatar={avatar} w={w}
      onEdit={() => onView({ mode: "edit", id: playbook.id, title: playbook.title, text: playbook.text })}
      onAssign={bots => onSave({ id: playbook.id, title: playbook.title, text: playbook.text, bot_ids: bots }, "assign")} onRemove={() => onRemove(playbook)}/>
  </div>
}

function PanelHead({ title, onClose }: { title: string; onClose: () => void }) {
  return <header className="pa-charter-history-head">
    <h3>{title}</h3>
    <button type="button" className="pa-circle pa-charter-history-close" onClick={onClose} aria-label={charterWord("playbookClose")} title={charterWord("playbookClose")}>×</button>
  </header>
}

function PlaybookDetail({ playbook, doc, readers, busy, narrow, avatar, w, onEdit, onAssign, onRemove }: {
  playbook: Playbook; doc: Doc; readers: readonly Reader[]; busy: boolean; narrow: boolean; avatar: (botID: string) => W.ProjectAssistantAppearance; w: Words
  onEdit: () => void; onAssign: (bots: string[]) => Promise<boolean>; onRemove: () => Promise<void>
}) {
  const [assigning, setAssigning] = useState<string[] | null>(null)
  const [removing, setRemoving] = useState(false)
  const assigned = readers.filter(reader => playbook.bot_ids.includes(reader.bot_id))
  const unread = unreadByActivity(assigned.filter(reader => !readsCurrentAt(doc.version, reader)))
  const names = (list: readonly Reader[]) => charterNames(list.map(reader => reader.name))
  const saveAssignment = async () => { if (assigning && await onAssign(assigning)) setAssigning(null) }
  return <>
    {(unread.working.length > 0 || unread.idle.length > 0) && <div className="pa-charter-playbook-reminder" role="status">
      {unread.working.length > 0 && <p>{charterFormat("playbookReminderWorking", names(unread.working))}</p>}
      {unread.idle.length > 0 && <p>{charterFormat("playbookReminderIdle", names(unread.idle))}</p>}
    </div>}
    <section className="pa-charter-playbook-given" aria-label={charterWord("playbookGivenTo")}>
      <h4>{charterWord("playbookGivenTo")}</h4>
      {!assigned.length && <p className="pa-charter-hint">{charterWord("playbookNoBots")}</p>}
      <ul className="pa-charter-playbook-readers">{assigned.map(reader => <PlaybookReaderRow key={reader.bot_id} reader={reader} current={readsCurrentAt(doc.version, reader)} appearance={avatar(reader.bot_id)}/>)}</ul>
      {!assigning && <button type="button" className="pa-pill pa-charter-playbook-assign-open" disabled={busy} onClick={() => setAssigning([...playbook.bot_ids])}>{charterWord("playbookAssign")}</button>}
      {assigning && <AssignSheet readers={readers} before={playbook.bot_ids} chosen={assigning} next={doc.version + 1} busy={busy} sheet={narrow} avatar={avatar}
        onChange={setAssigning} onCancel={() => setAssigning(null)} onSave={() => void saveAssignment()}/>}
    </section>
    <article className="pa-charter-section pa-charter-playbook-text" aria-label={playbook.title}>
      {playbook.text.trim() ? <BotMessageMarkdown text={playbook.text} w={w}/> : <p className="pa-charter-hint">{charterWord("playbookEmptyText")}</p>}
      <button type="button" className="pa-charter-edit" disabled={busy} aria-label={charterWord("playbookEditTitle")} title={charterWord("playbookEditTitle")} onClick={onEdit}>
        <svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M10.5 2.5l3 3L5.5 13.5H2.5v-3z"/><path d="M9 4l3 3"/></svg>
      </button>
    </article>
    {removing
      ? <div className="pa-charter-remove-confirm" role="group" aria-label={charterWord("playbookRemove")}>
        <p><strong>{charterFormat("playbookRemoveTitle", playbook.title)}</strong></p>
        <p>{assigned.length ? charterFormat("playbookRemoveBody", names(assigned)) : charterWord("playbookRemoveBodyNone")}</p>
        <div className="pa-charter-actions">
          <button type="button" onClick={() => setRemoving(false)}>{charterWord("cancel")}</button>
          <button type="button" className="pa-charter-danger" disabled={busy} onClick={() => void onRemove()}>{charterWord("playbookRemoveConfirm")}</button>
        </div>
      </div>
      : <div className="pa-charter-remove"><button type="button" disabled={busy} onClick={() => setRemoving(true)}>{charterWord("playbookRemove")}</button></div>}
  </>
}

function PlaybookReaderRow({ reader, current, appearance }: { reader: Reader; current: boolean; appearance: W.ProjectAssistantAppearance }) {
  const working = reader.running || reader.queued
  return <li className="pa-charter-playbook-reader" data-current={current || undefined}>
    <BotAvatar appearance={appearance} size={32} mood={reader.running ? "working" : "idle"}/>
    <span className="pa-charter-playbook-reader-name"><strong>{reader.name}</strong>
      <small><span aria-hidden="true">{current ? "● " : "○ "}</span>{reader.version > 0 ? charterFormat("playbookReadVersion", reader.version) : charterWord("playbookNeverRead")}</small>
    </span>
    <span className="pa-charter-playbook-activity" data-working={working || undefined}>{charterWord(working ? "playbookWorking" : "playbookIdle")}</span>
  </li>
}

/** The Project's visible Bots as checkboxes. */
function BotChecklist({ readers, chosen, avatar, onChange }: { readers: readonly Reader[]; chosen: readonly string[]; avatar: (botID: string) => W.ProjectAssistantAppearance; onChange: (bots: string[]) => void }) {
  return <ul className="pa-charter-assign-list">{readers.map(reader => <li key={reader.bot_id}>
    <label className="pa-charter-assign-row">
      <input type="checkbox" checked={chosen.includes(reader.bot_id)} onChange={event => onChange(event.target.checked ? [...chosen, reader.bot_id] : chosen.filter(id => id !== reader.bot_id))}/>
      <BotAvatar appearance={avatar(reader.bot_id)} size={28}/><span>{reader.name}</span>
    </label>
  </li>)}</ul>
}

function AssignSheet({ readers, before, chosen, next, busy, sheet, avatar, onChange, onCancel, onSave }: {
  readers: readonly Reader[]; before: readonly string[]; chosen: string[]; next: number; busy: boolean; sheet: boolean; avatar: (botID: string) => W.ProjectAssistantAppearance
  onChange: (bots: string[]) => void; onCancel: () => void; onSave: () => void
}) {
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => { box.current?.querySelector<HTMLElement>("input")?.focus({ preventScroll: !sheet }) }, [])
  const change = assignmentChange(before, chosen)
  const named = (ids: readonly string[]) => charterNames(readers.filter(reader => ids.includes(reader.bot_id)).map(reader => reader.name))
  return <>
    {sheet && <div className="pa-charter-sheet-backdrop" aria-hidden="true" onClick={onCancel}/>}
    <div ref={box} className={`pa-charter-assign${sheet ? " pa-charter-assign-sheet" : ""}`} role={sheet ? "dialog" : "group"} aria-modal={sheet || undefined} aria-label={charterWord("playbookAssignTitle")}
      onKeyDown={event => { if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onCancel() } }}>
      <strong className="pa-charter-assign-title">{charterWord("playbookAssignTitle")}</strong>
      <BotChecklist readers={readers} chosen={chosen} avatar={avatar} onChange={onChange}/>
      <div className="pa-charter-after">
        <strong>{charterWord("afterSave")}</strong>
        {change.added.length > 0 && <p>{charterFormat("playbookAssignAdded", named(change.added))}</p>}
        {change.removed.length > 0 && <p>{charterFormat("playbookAssignRemoved", named(change.removed))}</p>}
        {!change.added.length && !change.removed.length && <p>{charterWord("playbookAssignNone")}</p>}
      </div>
      <div className="pa-charter-actions">
        <button type="button" onClick={onCancel}>{charterWord("cancel")}</button>
        <button type="button" className="pa-charter-primary" disabled={busy || (!change.added.length && !change.removed.length)} onClick={onSave}>{charterFormat("saveAs", next)}</button>
      </div>
    </div>
  </>
}

function PlaybookEditor({ title: initialTitle, text: initialText, bots: initialBots, chooseBots = false, readers, next, busy, full, notice, avatar, heading, onCancel, onSave }: {
  title: string; text: string; bots: readonly string[]; chooseBots?: boolean; readers: readonly Reader[]; next: number; busy: boolean; full: boolean; notice: Notice
  avatar: (botID: string) => W.ProjectAssistantAppearance; heading: string; onCancel: () => void; onSave: (title: string, text: string, bots: string[]) => Promise<boolean>
}) {
  const [title, setTitle] = useState(initialTitle)
  const [text, setText] = useState(initialText)
  const [bots, setBots] = useState<string[]>([...initialBots])
  const [missing, setMissing] = useState(false)
  const field = useRef<HTMLInputElement>(null), area = useRef<HTMLTextAreaElement>(null)
  useEffect(() => { (initialTitle ? area : field).current?.focus({ preventScroll: !full }) }, [])
  const assigned = readers.filter(reader => bots.includes(reader.bot_id))
  const waiting = assigned.filter(reader => !reader.running), running = assigned.filter(reader => reader.running)
  const save = () => {
    const name = title.replace(/[\r\n]+/g, " ").trim()
    if (!name) { setMissing(true); field.current?.focus(); return }
    void onSave(name, text, bots)
  }
  return <div className={`pa-charter-editor pa-charter-playbook-editor${full ? " pa-charter-editor-full" : ""}`} role={full ? "dialog" : "group"} aria-modal={full || undefined} aria-label={heading}
    onKeyDown={event => { if (event.key === "Escape" && !event.nativeEvent.isComposing) { event.preventDefault(); event.stopPropagation(); onCancel() } }}>
    {full ? <header className="pa-charter-editor-bar">
      <button type="button" onClick={onCancel}>{charterWord("cancel")}</button>
      <strong>{heading}</strong>
      <button type="button" className="pa-charter-primary" disabled={busy} onClick={save}>{charterWord("saveShort")}</button>
    </header> : <h3 className="pa-charter-playbook-editor-head">{heading}</h3>}
    <div className="pa-charter-editor-body">
      {notice?.text && <p className="pa-charter-panel-notice" data-tone={notice.tone} role="status">{notice.text}</p>}
      <input ref={field} className="pa-charter-title" type="text" value={title} aria-label={charterWord("playbookTitleLabel")} placeholder={charterWord("playbookTitlePlaceholder")} aria-invalid={missing || undefined}
        onChange={event => { setTitle(event.target.value); setMissing(false) }} onKeyDown={event => { if (event.key === "Enter" && !event.nativeEvent.isComposing) { event.preventDefault(); area.current?.focus() } }}/>
      {missing && <p className="pa-charter-hint" role="alert">{charterWord("playbookNeedTitle")}</p>}
      <textarea ref={area} value={text} aria-label={charterWord("playbookTextLabel")} onChange={event => setText(event.target.value)} rows={Math.min(16, Math.max(6, text.split("\n").length + 1))}/>
      <p className="pa-charter-hint">{charterWord("editHint")}</p>
      {chooseBots && <div className="pa-charter-assign">
        <strong className="pa-charter-assign-title">{charterWord("playbookAssignTitle")}</strong>
        <BotChecklist readers={readers} chosen={bots} avatar={avatar} onChange={setBots}/>
      </div>}
      <div className="pa-charter-after">
        <strong>{charterWord("afterSave")}</strong>
        {waiting.length > 0 && <p>{charterFormat("afterNext", charterNames(waiting.map(r => r.name)))}</p>}
        {running.length > 0 && <p>{charterFormat("afterRunning", charterNames(running.map(r => r.name)))}</p>}
        {!assigned.length && <p>{charterWord("playbookAfterNone")}</p>}
      </div>
      {!full && <div className="pa-charter-actions">
        <button type="button" onClick={onCancel}>{charterWord("cancel")}</button>
        <button type="button" className="pa-charter-primary" disabled={busy} onClick={save}>{charterFormat("saveAs", next)}</button>
      </div>}
    </div>
  </div>
}

/** The playbooks one history version changed, each a diff group headed by its title. */
export function PlaybookDiffs({ before, after, nameOf }: { before: readonly Playbook[]; after: readonly Playbook[]; nameOf: (botID: string) => string }) {
  return <>{changedPlaybooks(before, after).map(change => <PlaybookDiff key={change.id} change={change} nameOf={nameOf}/>)}</>
}

/** The diff lines that stand for a renamed playbook and a changed assignment, before its text lines. */
export function playbookDiffLines(change: ChangedPlaybook, nameOf: (botID: string) => string): DiffLine[] {
  const given = (ids: readonly string[]) => {
    const names = ids.map(nameOf).filter(Boolean)
    return charterFormat("historyPlaybookGivenTo", names.length ? charterNames(names) : charterWord("historyPlaybookNobody"))
  }
  const lines: DiffLine[] = []
  if (change.renamedFrom !== undefined) lines.push({ kind: "same", text: charterFormat("historyPlaybookRenamed", change.renamedFrom, change.title) })
  if (change.assigned && change.kind !== "added") lines.push({ kind: "removed", text: given(change.assigned.before) })
  if (change.assigned && change.kind !== "removed") lines.push({ kind: "added", text: given(change.assigned.after) })
  return [...lines, ...change.lines]
}

function PlaybookDiff({ change, nameOf }: { change: ChangedPlaybook; nameOf: (botID: string) => string }) {
  const heading = charterFormat(change.kind === "added" ? "reasonPlaybookAdd" : change.kind === "removed" ? "reasonPlaybookRemove" : "reasonPlaybookEdit", change.title)
  return <div className="pa-charter-diff pa-charter-playbook-diff" role="group" aria-label={heading}>
    <div className="pa-charter-diff-head">{heading}</div>
    {playbookDiffLines(change, nameOf).map((line, i) => <div key={i} className="pa-charter-diff-line" data-kind={line.kind}>
      <span className="pa-charter-diff-mark">{line.kind === "removed" ? charterWord("diffRemoved") : line.kind === "added" ? charterWord("diffAdded") : ""}</span>
      <span>{line.text || " "}</span>
    </div>)}
  </div>
}
