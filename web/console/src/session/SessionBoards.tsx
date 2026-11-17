import { useCallback, useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from "react"
import { nextWord as t } from "../next-strings.js"
import { client, followsRelay, mayWriteThroughCurrentTransport } from "../client.js"
import { richText } from "../legacy/js/view/markdown.js"
import type { CloudClientHandle } from "../cloud/copied.js"
import "./session-boards.css"

const path = "/v1/orchestrator/session-boards"

type Actor = { id: string; label: string }
type Board = { id: string; project: string; name: string; purpose: string; lead: string; summary: string; latest: number; archived_at?: number; members?: Actor[] }
type Post = { seq: number; author: Actor; text: string; reply_to?: number; at: number }
type Page = { board: Board; posts: Post[]; more: boolean }
type Cursor = { board?: string; before?: number; after?: number }
type Command = { action: string; board?: string; name?: string; text?: string; target?: string; reply_to?: number }
type Receipt = { id: string; seq?: number }
type Entry = { actor?: Actor; render: (grouped: boolean) => ReactNode }
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
    return response.json()
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
      return await c._machineRequestAs(key, machine, word, { command }, "action") as Receipt
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

function BoardRows({ source, rows, candidates }: { source: SessionBoardSource; rows: Entry[]; candidates?: Actor[] }) {
  const [boards, setBoards] = useState<Board[]>([])
  const [show, setShow] = useState<string>()
  const [problem, setProblem] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const current = useRef(source)
  current.current = source
  const refresh = useCallback(async (signal?: AbortSignal) => {
    try {
      const result = await current.current.read<{ boards: Board[] }>({}, signal)
      if (!signal?.aborted) { setBoards(result.boards); setProblem(false) }
    } catch {
      if (!signal?.aborted) setProblem(true)
    } finally {
      if (!signal?.aborted) setLoaded(true)
    }
  }, [])
  useEffect(() => {
    const control = new AbortController()
    const read = () => { if (!document.hidden) void refresh(control.signal) }
    read()
    window.addEventListener("focus", read)
    return () => { control.abort(); window.removeEventListener("focus", read) }
  }, [refresh])
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
    <li className="session-boards-entry" role="presentation">
      <button type="button" className="chip" onClick={() => { setShow(""); void refresh() }}>{t("sessionBoards")}</button>
    </li>
    {rows.map(row => {
      const b = row.actor && assigned.get(row.actor.id)
      if (!b) return row.render(false)
      if (shown.has(b.id)) return null
      shown.add(b.id)
      const unread = Math.max(0, b.latest - currentCursor(source, b.id))
      const lead = b.members?.find(m => m.id === b.lead)
      return <li key={b.id} className="session-board-group" role="presentation">
        <button type="button" onClick={() => setShow(b.id)}>
          <strong>{b.name}</strong>
          {lead && <span>{t("sessionBoardLead")} · {lead.label}</span>}
          {unread > 0 && <small>{t("sessionBoardUnread", { count: unread })}</small>}
        </button>
        <ul className="rows" role="group" aria-label={b.name}>{grouped.get(b.id)?.map(r => r.render(true))}</ul>
      </li>
    })}
    {show !== undefined && <BoardDialog source={source} initial={show} boards={boards} loaded={loaded} problem={problem}
      candidates={leadOptions} refresh={refresh} close={() => { setShow(undefined); void refresh() }} />}
  </>
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
      <BoardMessages key={selected} source={source} id={selected} candidates={candidates} changed={refresh} />
    </> : <div className="session-board-body" aria-busy={!loaded}>
      <div className="session-board-actions">
        <button type="button" onClick={() => void refresh()}>{t("sessionBoardRefresh")}</button>
        {source.writable && candidates.length > 0 && <button type="button" onClick={() => create(!creating)}>{t("sessionBoardCreate")}</button>}
      </div>
      {creating && source.writable && <BoardCreate source={source} candidates={candidates} onCreated={id => {
        create(false); select(id); void refresh()
      }} />}
      {problem && <p role="alert">{t("sessionBoardReadError")}</p>}
      {loaded && boards.length === 0 && !problem && <p>{t("sessionBoardsEmpty")}</p>}
      <ul className="session-board-index">{boards.map(board => <li key={board.id}>
        <button type="button" onClick={() => select(board.id)}>
          <strong>{board.name}</strong>{board.archived_at ? <span>{t("sessionBoardArchived")}</span> : null}
          <span>{board.purpose}</span>
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

function BoardMessages({ source, id, changed, candidates }: { source: SessionBoardSource; id: string; changed: () => Promise<void>; candidates: Actor[] }) {
  const [page, setPage] = useState<Page>()
  const [error, setError] = useState("")
  const [reading, setReading] = useState(false)
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
        try { localStorage.setItem(cursorKey(sourceRef.current, id), String(next.posts.at(-1)?.seq ?? 0)) } catch {}
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
      done?.(); void load(); void changed()
    })
  }
  const post = (e: FormEvent) => {
    e.preventDefault()
    if (draft.trim()) action({ action: "post", text: draft, reply_to: reply }, () => { setDraft(""); setReply(undefined) })
  }
  const lead = b?.members?.find(m => m.id === b.lead)
  return <div className="session-board-body" aria-busy={reading || change.busy}>
    <div className="session-board-actions"><button type="button" disabled={reading} onClick={() => void load()}>{t("sessionBoardRefresh")}</button>
      {allowed && <button type="button" onClick={() => setManage(!manage)}>{t("sessionBoardManage")}</button>}</div>
    {error && <p role="alert">{t("sessionBoardReadError")} <code>{error}</code></p>}
    {change.error && <p role="alert">{writeProblem(change.error)} <code>{change.error}</code></p>}
    {b && page && <>
      <h3>{b.name}</h3>
      <p className="session-board-text">{b.purpose}</p>
      <p>{t("sessionBoardLead")} · {lead?.label}</p>
      {b.archived_at && <p>{t("sessionBoardArchived")}</p>}
      {b.members && <p>{t("sessionBoardMembers")} · {b.members.map(m => m.label).join(" · ")}</p>}
      {b.summary && <aside><strong>{t("sessionBoardSummary")}</strong><p className="session-board-text">{b.summary}</p></aside>}
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
      {page.more && <button type="button" disabled={reading} onClick={() => void load(page.posts[0]?.seq)}>{t("sessionBoardEarlier")}</button>}
      {!page.posts.length && <p>{t("sessionBoardNoPosts")}</p>}
      <ol className="session-board-posts">{page.posts.map(p => <li key={p.seq}>
        <div>
          <strong>{p.author.id === "person" ? t("sessionBoardPerson") : p.author.label}</strong>
          <time dateTime={new Date(p.at * 1000).toISOString()}>{new Date(p.at * 1000).toLocaleString(document.documentElement.lang || undefined)}</time>
          <span>#{p.seq}</span>
        </div>
        {p.reply_to && <small>{t("sessionBoardReplyTo", { seq: p.reply_to })}</small>}
        <div className="session-board-text" dangerouslySetInnerHTML={{ __html: richText(p.text, { codeCopy: false }) }} />
        {allowed && <button type="button" onClick={() => setReply(p.seq)}>{t("sessionBoardReply")}</button>}
      </li>)}</ol>
      {allowed && <form onSubmit={post}>
        {reply && <div>{t("sessionBoardReplyTo", { seq: reply })} <button type="button" onClick={() => setReply(undefined)}>{t("sessionBoardCancel")}</button></div>}
        <label>{t("sessionBoardMessage")}<textarea value={draft} onChange={e => setDraft(e.currentTarget.value)} rows={3} required /></label>
        <button type="submit" disabled={change.busy || !draft.trim()}>{t("sessionBoardPost")}</button>
      </form>}
    </>}
  </div>
}
