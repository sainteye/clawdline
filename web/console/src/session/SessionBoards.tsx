import { createPortal } from "react-dom"
import { useCallback, useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from "react"
import { nextWord as t } from "../next-strings.js"
import { client, followsRelay, mayWriteThroughCurrentTransport } from "../client.js"
import { richText } from "../legacy/js/view/markdown.js"
import type { CloudClientHandle } from "../cloud/copied.js"
import { onStatusRefresh } from "../status-refresh.js"
import "./session-boards.css"

const path = "/v1/orchestrator/session-boards"

export type SessionBoardMember = { id: string; label: string }
type Actor = SessionBoardMember
export type SessionMessageBoard = { id: string; project: string; name: string; purpose: string; lead: string; summary: string; latest: number; archived_at?: number; members?: Actor[] }
type Board = SessionMessageBoard
type Post = { seq: number; author: Actor; text: string; reply_to?: number; at: number }
type Page = { board: Board; posts: Post[]; more: boolean }
type Cursor = { board?: string; before?: number; after?: number }
export type SessionBoardCommand = { action: string; board?: string; name?: string; text?: string; target?: string; members?: string[]; reply_to?: number }
type Command = SessionBoardCommand
type Receipt = { id: string; seq?: number }
type Entry = { actor?: Actor; render: (grouped: boolean, lead?: boolean) => ReactNode }
export type SessionBoardSource = {
  key: string
  writable: boolean
  read<T>(fields: Cursor, signal?: AbortSignal): Promise<T>
  change(command: Command, key: string): Promise<Receipt>
}

const local: SessionBoardSource = {
  key: "local",
  get writable() { return mayWriteThroughCurrentTransport() },
  async read(fields, signal) {
    const query = new URLSearchParams(Object.entries(fields).map(([k, v]) => [k, String(v)]))
    const response = await fetch(path + (query.size ? `?${query}` : ""), { signal, cache: "no-store" })
    if (!response.ok) throw await refusal(response)
    return response.json()
  },
  async change(command, key) {
    const response = await fetch(path, { method: "POST", headers: {
      "Content-Type": "application/json", "Idempotency-Key": key,
    }, body: JSON.stringify(command) })
    if (!response.ok) throw await refusal(response)
    const receipt = await response.json()
    invalidateSessionBoards(local.key)
    return receipt
  },
}

async function refusal(response: Response) {
  const answer = await response.json().catch(() => null)
  const code = typeof answer?.error === "string" ? answer.error : answer?.error?.code
  return Object.assign(new Error("Board request refused"), { code })
}

function boardErrorCode(error: unknown, fallback: string): string {
  const code = error && typeof error === "object" && "code" in error ? error.code : undefined
  return typeof code === "string" && /^[a-z0-9_-]+$/i.test(code) ? code : fallback
}

function writeProblem(code: string): string {
  switch (code) {
    case "session_board_closed": return t("sessionBoardArchived")
    case "session_board_full": return t("sessionBoardFull")
    case "session_board_denied": return t("sessionBoardDenied")
    case "session_board_invalid": return t("sessionBoardInvalid")
    default: return t("sessionBoardWriteError")
  }
}

export function cloudSessionBoardSource(machine: string, account: string, writable: boolean,
  current: () => CloudClientHandle | null): SessionBoardSource {
  function connection(word: string) {
    const c = current()
    if (!c || c.account !== account || !c.machineDescriptor?.(machine)?.machine?.commands?.includes(word)) {
      throw new Error("offline")
    }
    return c
  }
  return {
    key: JSON.stringify([account, machine]), writable,
    async read<T>(fields: Cursor, signal?: AbortSignal): Promise<T> {
      const word = "session-boards.read"
      const c = connection(word)
      if (!c._machineRequest) throw new Error("offline")
      const result = await c._machineRequest(machine, word, fields, "read", undefined, { signal })
      connection(word)
      return result as T
    },
    async change(command, key) {
      const word = "session-boards.change"
      const c = connection(word)
      if (!writable || !c._machineRequestAs) throw new Error("offline")
      const result = await c._machineRequestAs(key, machine, word, { command }, "action") as Receipt
      invalidateSessionBoards(JSON.stringify([account, machine]))
      return result
    },
  }
}

export function sessionBoardActor(assistant: string | undefined, conversation: string | undefined, label: string): Actor | undefined {
  return assistant && conversation ? { id: `${assistant}:${conversation}`, label } : undefined
}

export function useLocalSessionBoards(enabled: boolean): SessionBoardSource | undefined {
  const [ready, setReady] = useState(false)
  useEffect(() => {
    if (!enabled || followsRelay()) return
    let current = true
    void client.health().then(health => { if (current) setReady((health.api_level ?? 0) >= 87) }).catch(() => {})
    return () => { current = false }
  }, [enabled])
  return enabled && ready && !followsRelay() ? local : undefined
}

function cursorKey(source: SessionBoardSource, id: string) { return `clawdline.session-board.read:${source.key}:${id}` }

function currentCursor(source: SessionBoardSource, id: string) {
  try { return Number(localStorage.getItem(cursorKey(source, id))) || 0 } catch { return 0 }
}

export function SessionBoardRows({ source, rows, candidates }: {
  source?: SessionBoardSource; rows: Entry[]; candidates?: Actor[]
}) {
  if (!source) return rows.map(row => row.render(false))
  return <BoardRows key={source.key} source={source} rows={rows} candidates={candidates} />
}

const boardChanged = "clawdline:session-board-changed"
const boardIndexRead = "clawdline:session-board-index-read"

function invalidateSessionBoards(key: string) {
  window.dispatchEvent(new CustomEvent(boardChanged, { detail: key }))
}

function unreadFor(source: SessionBoardSource, board: Board) {
  return board.archived_at ? 0 : Math.max(0, board.latest - currentCursor(source, board.id))
}

export function SessionBoardLead() {
  return <span className="session-board-lead">
    <span className="clawdfather-crown" aria-hidden="true" />{t("sessionBoardLead")}
  </span>
}

function BoardGlyph() {
  return <svg aria-hidden="true" viewBox="0 0 24 24">
    <path d="M9 5H5.5A1.5 1.5 0 0 0 4 6.5v12A1.5 1.5 0 0 0 5.5 20h13a1.5 1.5 0 0 0 1.5-1.5v-12A1.5 1.5 0 0 0 18.5 5H15M9 4h6v3H9zM7.5 12h9m-9 4h6" />
  </svg>
}

function useBoardList(source: SessionBoardSource, watchMessages = false) {
  const [boards, setBoards] = useState<Board[]>([])
  const [problem, setProblem] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const inFlight = useRef<{ signal?: AbortSignal } | undefined>(undefined)
  const current = useRef(source)
  current.current = source
  const refresh = useCallback(async (signal?: AbortSignal) => {
    if (inFlight.current && !inFlight.current.signal?.aborted) return
    const flight = { signal }
    inFlight.current = flight
    try {
      const result = await current.current.read<{ boards: Board[] }>({}, signal)
      if (!signal?.aborted) {
        setBoards(result.boards)
        setProblem(false)
        const source = current.current
        const unread = result.boards.reduce((count, board) => count + unreadFor(source, board), 0)
        window.dispatchEvent(new CustomEvent(boardIndexRead, { detail: { key: source.key, unread } }))
      }
    } catch {
      if (!signal?.aborted) setProblem(true)
    } finally {
      if (inFlight.current === flight) inFlight.current = undefined
      if (!signal?.aborted) setLoaded(true)
    }
  }, [])
  useEffect(() => {
    const control = new AbortController()
    const read = () => { if (!document.hidden) void refresh(control.signal) }
    const changed = (e: Event) => {
      if ((e as CustomEvent<string>).detail === source.key) read()
    }
    read()
    const stopStatusRefresh = onStatusRefresh(read)
    window.addEventListener("focus", read)
    document.addEventListener("visibilitychange", read)
    window.addEventListener(boardChanged, changed)
    return () => {
      control.abort()
      stopStatusRefresh()
      document.removeEventListener("visibilitychange", read)
      window.removeEventListener("focus", read)
      window.removeEventListener(boardChanged, changed)
    }
  }, [refresh, source.key])
  const watch = watchMessages && boards.some(board => !board.archived_at)
  useEffect(() => {
    if (!watch) return
    const control = new AbortController()
    const timer = window.setInterval(() => {
      if (!document.hidden) void refresh(control.signal)
    }, 30_000)
    return () => { window.clearInterval(timer); control.abort() }
  }, [watch, refresh, source.key])
  return { boards, problem, loaded, refresh }
}

function BoardRows({ source, rows, candidates }: { source: SessionBoardSource; rows: Entry[]; candidates?: Actor[] }) {
  const list = useBoardList(source, true)
  const { boards, refresh } = list
  const [show, setShow] = useState<string>()
  const leadOptions = candidates ?? rows.flatMap(row => row.actor ? [row.actor] : [])
  const assigned = new Map<string, Board>()
  for (const b of boards) {
    if (b.archived_at) continue
    for (const member of b.members ?? []) if (!assigned.has(member.id)) assigned.set(member.id, b)
  }
  const grouped = new Map<string, Entry[]>()
  for (const row of rows) {
    const board = row.actor && assigned.get(row.actor.id)
    if (!board) continue
    const group = grouped.get(board.id) ?? []
    group.push(row)
    grouped.set(board.id, group)
  }
  const shown = new Set<string>()
  return <>
    {rows.map(row => {
      const b = row.actor && assigned.get(row.actor.id)
      if (!b) return row.render(false)
      if (shown.has(b.id)) return null
      shown.add(b.id)
      const unread = unreadFor(source, b)
      const members = grouped.get(b.id) ?? []
      const lead = members.find(entry => entry.actor?.id === b.lead)
      return <li key={b.id} className="session-board-group" role="presentation">
        <button type="button" onClick={() => setShow(b.id)}>
          <BoardGlyph />
          <strong>{b.name}</strong>
          {unread > 0 && <small>{t("sessionBoardUnread", { count: unread })}</small>}
        </button>
        <ul className="rows" role="group" aria-label={b.name}>{lead?.render(true, true)}{members.filter(row => row !== lead).map(row => row.render(true))}</ul>
      </li>
    })}
    {show !== undefined && createPortal(<BoardDialog source={source} initial={show} {...list}
      candidates={leadOptions} close={() => { setShow(undefined); void refresh() }} />, document.body)}
  </>
}

export type SessionBoardTarget = { machine?: string; source: SessionBoardSource; label: string; candidates: Actor[] }

export function SessionBoardDock({ targets }: { targets: readonly SessionBoardTarget[] }) {
  const [open, setOpen] = useState<string | null>(null)
  const button = useRef<HTMLButtonElement>(null)
  const [readings, setReadings] = useState<Record<string, number>>({})
  useEffect(() => {
    const read = (event: Event) => {
      const { key, unread } = (event as CustomEvent<{ key: string; unread: number }>).detail
      setReadings(current => current[key] === unread ? current : { ...current, [key]: unread })
    }
    window.addEventListener(boardIndexRead, read)
    return () => window.removeEventListener(boardIndexRead, read)
  }, [])
  const unread = targets.reduce((count, item) => count + (readings[item.source.key] ?? 0), 0)
  const label = unread ? `${t("sessionBoards")} · ${t("sessionBoardUnread", { count: unread })}` : t("sessionBoards")
  const close = () => {
    if (open) invalidateSessionBoards(open)
    setOpen(null)
    button.current?.focus()
  }
  const target = targets.find(t => t.source.key === open)
  useEffect(() => {
    if (open !== null && open !== "" && !target) setOpen(null)
  }, [open, target])
  if (!targets.length) return null
  return <>
    <button ref={button} type="button" className="session-board-dock" aria-haspopup="dialog"
      aria-label={label} title={label} data-unread={unread > 0 || undefined}
      onClick={() => setOpen(targets.length === 1 ? targets[0].source.key : "")}>
      <BoardGlyph />
      {unread > 0 && <span className="session-board-dock-dot" aria-hidden="true" />}
    </button>
    {target && createPortal(<StatusBoardDialog key={target.source.key} target={target} close={close} />, document.body)}
    {open === "" && createPortal(<BoardMachinePicker targets={targets} choose={setOpen} close={close} />, document.body)}
  </>
}

function BoardMachinePicker({ targets, choose, close }: {
  targets: readonly SessionBoardTarget[]; choose: (key: string) => void; close: () => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const heading = useId()
  useEffect(() => { dialog.current?.showModal() }, [])
  return <dialog ref={dialog} className="session-board-dialog" aria-labelledby={heading} onClose={close}>
    <header><h2 id={heading}>{t("sessionBoards")}</h2>
      <button type="button" onClick={() => dialog.current?.close()}>{t("sessionBoardClose")}</button>
    </header>
    <div className="session-board-body">
      <p>{t("cloudMachinesLede")}</p>
      <ul className="session-board-index">{targets.map(target => <li key={target.source.key}>
        <button type="button" onClick={() => choose(target.source.key)}>{target.label}</button>
      </li>)}</ul>
    </div>
  </dialog>
}

function StatusBoardDialog({ target, close }: { target: SessionBoardTarget; close: () => void }) {
  const list = useBoardList(target.source)
  return <BoardDialog source={target.source} initial="" candidates={target.candidates} close={close} {...list} />
}

function BoardDialog({ source, initial, boards, loaded, problem, candidates, refresh, close }: {
  source: SessionBoardSource; initial: string; boards: Board[]; loaded: boolean; problem: boolean
  candidates: Actor[]; refresh: () => Promise<void>; close: () => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const heading = useId()
  const [selected, select] = useState(initial)
  const [expanded, expand] = useState(false)
  const [creating, create] = useState(false)
  useEffect(() => { dialog.current?.showModal() }, [])
  return <dialog ref={dialog} className="session-board-dialog" data-expanded={expanded} aria-labelledby={heading} onClose={close}>
    <header>
      <h2 id={heading}>{t("sessionBoards")}</h2>
      <button type="button" className="session-board-expand" onClick={() => expand(!expanded)}>{t(expanded ? "sessionBoardRestore" : "sessionBoardExpand")}</button>
      <button type="button" onClick={() => dialog.current?.close()}>{t("sessionBoardClose")}</button>
    </header>
    {selected ? <>
      <nav><button type="button" onClick={() => { select(""); void refresh() }}>{t("sessionBoards")}</button></nav>
      <BoardMessages key={selected} source={source} id={selected} candidates={candidates} />
    </> : <div className="session-board-body" aria-busy={!loaded}>
      <div className="session-board-actions">
        <button type="button" onClick={() => void refresh()}>{t("sessionBoardRefresh")}</button>
        {source.writable && candidates.length > 0 && <button type="button" onClick={() => create(!creating)}>{t("sessionBoardCreate")}</button>}
      </div>
      {creating && source.writable && <BoardCreate source={source} candidates={candidates} onCreated={id => {
        create(false); select(id)
      }} />}
      {problem && <p role="alert">{t("sessionBoardReadError")}</p>}
      {loaded && boards.length === 0 && !problem && <p>{t("sessionBoardsEmpty")}</p>}
      <ul className="session-board-index">{boards.map(board => <li key={board.id}>
        <button type="button" onClick={() => select(board.id)}>
          <strong>{board.name}</strong>{board.archived_at ? <span>{t("sessionBoardArchived")}</span> : null}
          <span>{board.purpose}</span>
          {unreadFor(source, board) > 0 && <small>{t("sessionBoardUnread", { count: unreadFor(source, board) })}</small>}
        </button>
      </li>)}</ul>
    </div>}
  </dialog>
}

function useBoardChange(source: SessionBoardSource) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const flight = useRef(false)
  const retry = useRef<{ content: string; key: string } | null>(null)
  const send = async (command: Command, done: (value: Receipt) => void) => {
    if (flight.current || !source.writable) return
    flight.current = true
    setBusy(true); setError("")
    const content = JSON.stringify(command)
    const key = retry.current?.content === content ? retry.current.key : crypto.randomUUID()
    retry.current = { content, key }
    try {
      const r = await source.change(command, key)
      retry.current = null
      done(r)
    } catch (e) {
      setError(boardErrorCode(e, "session_board_write_unconfirmed"))
    } finally { flight.current = false; setBusy(false) }
  }
  return { busy, error, send }
}

function BoardCreate({ source, candidates, onCreated }: { source: SessionBoardSource; candidates: Actor[]; onCreated: (id: string) => void }) {
  const change = useBoardChange(source)
  return <form onSubmit={e => {
    e.preventDefault()
    const data = new FormData(e.currentTarget)
    void change.send({ action: "create", name: String(data.get("name")), text: String(data.get("purpose")),
      target: String(data.get("lead")) }, r => onCreated(r.id))
  }}>
    <label>{t("sessionBoardName")}<input name="name" required /></label>
    <label>{t("sessionBoardPurpose")}<textarea name="purpose" required rows={3} /></label>
    <label>{t("sessionBoardLead")}<select name="lead" aria-label={t("sessionBoardLead")} required><option value="">{t("sessionBoardChoose")}</option>
      {candidates.map(a => <option key={a.id} value={a.id}>{a.label}</option>)}
    </select></label>
    <button type="submit" disabled={change.busy}>{t("sessionBoardCreate")}</button>
    {change.error && <p role="alert">{writeProblem(change.error)} <code>{change.error}</code></p>}
  </form>
}

function BoardMessages({ source, id, candidates }: { source: SessionBoardSource; id: string; candidates: Actor[] }) {
  const [page, setPage] = useState<Page>()
  const [error, setError] = useState("")
  const [reading, setReading] = useState(false)
  const [compact] = useState(() => window.matchMedia("(max-width: 700px)").matches)
  const [unreadSince, setUnreadSince] = useState(() => currentCursor(source, id))
  const [draft, setDraft] = useState("")
  const [reply, setReply] = useState<number>()
  const [manage, setManage] = useState(false)
  const [archive, confirmArchive] = useState(false)
  const controller = useRef<AbortController | null>(null)
  const change = useBoardChange(source)
  const sourceRef = useRef(source)
  sourceRef.current = source
  const load = useCallback(async (before?: number) => {
    controller.current?.abort()
    const request = new AbortController()
    controller.current = request
    setReading(true)
    try {
      const next = await sourceRef.current.read<Page>(before ? { board: id, before } : { board: id }, request.signal)
      if (request.signal.aborted) return
      setPage(previous => before && previous ? { ...next, posts: [...next.posts, ...previous.posts] } : next)
      if (!before && next.posts.length > 0) {
        const current = sourceRef.current
        const cursor = currentCursor(current, id)
        const latest = next.posts.at(-1)?.seq ?? 0
        if (latest > cursor) {
          setUnreadSince(cursor)
          try { localStorage.setItem(cursorKey(current, id), String(latest)) } catch {}
          invalidateSessionBoards(current.key)
        }
      }
      setError("")
    } catch (e) {
      if (!request.signal.aborted) setError(boardErrorCode(e, "session_board_read_unavailable"))
    }
    finally { if (!request.signal.aborted) setReading(false) }
  }, [id])
  useEffect(() => { void load(); return () => controller.current?.abort() }, [load])
  const b = page?.board
  const allowed = source.writable && b && !b.archived_at
  const action = (cmd: Command, done?: () => void) => {
    void change.send({ ...cmd, board: id }, () => {
      done?.(); void load()
    })
  }
  const post = (e: FormEvent) => {
    e.preventDefault()
    if (draft.trim()) action({ action: "post", text: draft, reply_to: reply }, () => { setDraft(""); setReply(undefined) })
  }
  const lead = b?.members?.find(m => m.id === b.lead)
  const members = b?.members?.filter(member => member.id !== b.lead) ?? []
  const unread = b ? Math.max(0, b.latest - unreadSince) : 0
  return <div className="session-board-body" aria-busy={reading || change.busy}>
    {error && <p role="alert">{t("sessionBoardReadError")} <code>{error}</code></p>}
    {change.error && <p role="alert">{writeProblem(change.error)} <code>{change.error}</code></p>}
    {b && page && <>
      <h3 className="session-board-name">{b.name}</h3>
      <p className="session-board-purpose">{b.purpose}</p>
      {lead && <div className="session-board-lead-strip"><SessionBoardLead /> <strong>{lead.label}</strong></div>}
      <div className="session-board-actions">
        <button type="button" disabled={reading} onClick={() => void load()}>{t("sessionBoardRefresh")}</button>
        {allowed && <button type="button" aria-expanded={manage} onClick={() => setManage(!manage)}>{t("sessionBoardManage")}</button>}
        {b.archived_at && <span>{t("sessionBoardArchived")}</span>}
      </div>
      <div className="session-board-discussion">
      <div className="session-board-thread">
      {b.summary && <details className="session-board-summary" open><summary>{t("sessionBoardSummary")}</summary><p className="session-board-text">{b.summary}</p></details>}
      {allowed && manage && <div className="session-board-management">
        <form key={b.summary} onSubmit={e => { e.preventDefault(); action({ action: "summary", text: String(new FormData(e.currentTarget).get("summary")) }) }}>
          <label>{t("sessionBoardSummary")}<textarea name="summary" defaultValue={b.summary} rows={3} /></label>
          <button disabled={change.busy}>{t("sessionBoardSave")}</button>
        </form>
        <form onSubmit={e => { e.preventDefault(); action({ action: "transfer", target: String(new FormData(e.currentTarget).get("lead")) }) }}>
          <label>{t("sessionBoardTransfer")}<select name="lead" aria-label={t("sessionBoardTransfer")} required><option value="">{t("sessionBoardChoose")}</option>
            {b.members?.filter(m => m.id !== b.lead && candidates.some(c => c.id === m.id)).map(m => <option key={m.id} value={m.id}>{m.label}</option>)}
          </select></label>
          <button disabled={change.busy}>{t("sessionBoardTransfer")}</button>
        </form>
        {archive ? <div>
          <p>{t("sessionBoardArchiveAsk")}</p>
          <button type="button" disabled={change.busy} onClick={() => action({ action: "archive" }, () => confirmArchive(false))}>{t("sessionBoardArchive")}</button>
          <button type="button" onClick={() => confirmArchive(false)}>{t("sessionBoardCancel")}</button>
        </div> : <button type="button" onClick={() => confirmArchive(true)}>{t("sessionBoardArchive")}</button>}
      </div>}
      {unread > 0 && <p className="session-board-new" role="status">{t("sessionBoardUnread", { count: unread })}</p>}
      {!page.posts.length && <p>{t("sessionBoardNoPosts")}</p>}
      <ol className="session-board-posts" reversed>{[...page.posts].reverse().map(p => <li key={p.seq} data-unread={p.seq > unreadSince || undefined}>
        <div>
          <strong>{p.author.id === "person" ? t("sessionBoardPerson") : p.author.label}</strong>
          {p.author.id === b.lead && <SessionBoardLead />}
          <time dateTime={new Date(p.at * 1000).toISOString()}>{new Date(p.at * 1000).toLocaleString(document.documentElement.lang || undefined)}</time>
          <span>#{p.seq}</span>
        </div>
        {p.reply_to && <small>{t("sessionBoardReplyTo", { seq: p.reply_to })}</small>}
        <div className="session-board-text" dangerouslySetInnerHTML={{ __html: richText(p.text, { codeCopy: false }) }} />
        {allowed && <button type="button" onClick={() => setReply(p.seq)}>{t("sessionBoardReply")}</button>}
      </li>)}</ol>
      {page.more && <button type="button" disabled={reading} onClick={() => void load(page.posts[0]?.seq)}>{t("sessionBoardEarlier")}</button>}
      {allowed && <form onSubmit={post}>
        {reply && <div>{t("sessionBoardReplyTo", { seq: reply })} <button type="button" onClick={() => setReply(undefined)}>{t("sessionBoardCancel")}</button></div>}
        <label>{t("sessionBoardMessage")}<textarea value={draft} onChange={e => setDraft(e.currentTarget.value)} rows={3} required /></label>
        <button type="submit" disabled={change.busy || !draft.trim()}>{t("sessionBoardPost")}</button>
      </form>}
      </div>
      {b.members && <details className="session-board-members" open={!compact}>
        <summary>{t("sessionBoardMembers")} <span>{b.members.length}</span></summary>
        <ul>{(lead ? [lead, ...members] : members).map(member => <li key={member.id}>
          <span className="session-board-member-mark" aria-hidden="true"><BoardGlyph /></span>
          <strong>{member.label}</strong>
          {member.id === b.lead && <SessionBoardLead />}
        </li>)}</ul>
      </details>}
      </div>
    </>}
  </div>
}
