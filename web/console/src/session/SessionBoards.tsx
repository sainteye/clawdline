import { ChevronIcon } from "../ui/ChevronIcon.js"
import { createPortal } from "react-dom"
import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type FormEvent, type MouseEvent, type PointerEvent, type ReactNode } from "react"
import { nextWord as t } from "../next-strings.js"
import { client, followsRelay, mayWriteThroughCurrentTransport } from "../client.js"
import { richText } from "../legacy/js/view/markdown.js"
import type { CloudClientHandle } from "../cloud/copied.js"
import { onStatusRefresh } from "../status-refresh.js"
import { LoadingSkeleton } from "../LoadingSkeleton.js"
import { useDockMotion } from "../ui/dock-motion.js"
import type { Icon } from "@clawdline/contract"
import { sessionBoardAvatar, SessionBoardMark } from "./board-avatars.js"
import { usePersonas } from "./PersonaBot.js"
import { visualBranches } from "./order.js"
import "./session-boards.css"

const path = "/v1/orchestrator/session-boards"

export type SessionBoardMember = {
  id: string; label: string; persona?: string; projectIcon?: Icon; presence?: "listed" | "not_listed"
}
type Actor = SessionBoardMember
export type SessionMessageBoard = { id: string; project: string; name: string; purpose: string; lead: string; summary: string; latest: number; archived_at?: number; members?: Actor[] }
type Board = SessionMessageBoard
type Post = { seq: number; author: Actor; text: string; reply_to?: number; at: number }
type Page = { board: Board; posts: Post[]; more: boolean }
type Cursor = { board?: string; before?: number; after?: number }
export type SessionBoardCommand = { action: string; board?: string; name?: string; text?: string; target?: string; members?: string[]; reply_to?: number }
type Command = SessionBoardCommand
type Receipt = { id: string; seq?: number }
type BoardRow = { lead: boolean; avatar: Icon | null; depth: number; branchThrough: boolean; ancestorThrough: boolean }
type Entry = { id: string; parent?: string; actor?: Actor; render: (board?: BoardRow) => ReactNode }
export type SessionBoardSource = {
  key: string
  machine?: string
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
    key: JSON.stringify([account, machine]), machine, writable,
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

export function sessionBoardActor(assistant: string | undefined, conversation: string | undefined, label: string,
  persona?: string, projectIcon?: Icon): Actor | undefined {
  return assistant && conversation ? { id: `${assistant}:${conversation}`, label, persona, projectIcon } : undefined
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
  if (!source) return rows.map(row => row.render())
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
  return <span className="session-board-lead">{t("sessionBoardLead")}</span>
}

function BoardChevron({ direction }: { direction: "back" | "forward" | "down" }) {
  return <svg className="session-board-chevron" data-direction={direction} aria-hidden="true" viewBox="0 0 24 24">
    <path d="m9 6 6 6-6 6" />
  </svg>
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

function boardProjectIcon(board: Board, candidates: ReadonlyMap<string, Actor>) {
  return candidates.get(board.lead)?.projectIcon ??
    board.members?.map(member => candidates.get(member.id)?.projectIcon).find(Boolean)
}

function BoardRows({ source, rows, candidates }: { source: SessionBoardSource; rows: Entry[]; candidates?: Actor[] }) {
  const list = useBoardList(source, true)
  const { boards, refresh } = list
  const [show, setShow] = useState<string>()
  const leadOptions = candidates ?? rows.flatMap(row => row.actor ? [row.actor] : [])
  const candidateMap = new Map(leadOptions.map(actor => [actor.id, actor]))
  const personas = usePersonas(source.machine)
  const assigned = new Map<string, Board>()
  for (const b of boards) {
    if (b.archived_at) continue
    for (const member of b.members ?? []) if (!assigned.has(member.id)) assigned.set(member.id, b)
  }
  const grouped = new Map<string, { entry: Entry; depth: number }[]>()
  const placement = new Map<string, { board: Board; depth: number }>()
  for (const row of rows) {
    const parent = row.parent ? placement.get(row.parent) : undefined
    const board = (row.actor && assigned.get(row.actor.id)) ?? parent?.board
    if (!board) continue
    const depth = parent?.board === board && row.actor?.id !== board.lead ? parent.depth + 1 : 0
    placement.set(row.id, { board, depth })
    const group = grouped.get(board.id) ?? []
    group.push({ entry: row, depth })
    grouped.set(board.id, group)
  }
  const shown = new Set<string>()
  return <>
    {rows.map(row => {
      const b = placement.get(row.id)?.board
      if (!b) return row.render()
      if (shown.has(b.id)) return null
      shown.add(b.id)
      const unread = unreadFor(source, b)
      const members = grouped.get(b.id) ?? []
      const leadAt = members.findIndex(({ entry }) => entry.actor?.id === b.lead)
      // Keep the Lead's child Sessions directly underneath it when moving it.
      const nextRoot = members.findIndex(({ depth }, index) => index > leadAt && depth === 0)
      const ordered = leadAt > 0
        ? [...members.slice(leadAt, nextRoot < 0 ? undefined : nextRoot),
          ...members.slice(0, leadAt), ...(nextRoot < 0 ? [] : members.slice(nextRoot))]
        : members
      const depths = new Map(ordered.map(({ entry, depth }) => [entry.id, depth]))
      const branches = visualBranches(ordered.map(({ entry }) => entry), depths)
      const enrolled = new Map(b.members?.map(member => [member.id, member]))
      const icon = boardProjectIcon(b, candidateMap)
      return <li key={b.id} className="session-board-group" role="presentation">
        <button type="button" onClick={() => { setShow(b.id); void refresh() }}>
          {icon ? <SessionBoardMark icon={icon} project /> : <BoardGlyph />}
          <strong>{b.name}</strong>
          {unread > 0 && <small>{t("sessionBoardUnread", { count: unread })}</small>}
        </button>
        <ul className="rows" role="group" aria-label={b.name}>{ordered.map(({ entry, depth }) => {
          const actor = entry.actor
          const lead = actor?.id === b.lead
          return entry.render({ lead, depth,
            avatar: sessionBoardAvatar(actor?.id ?? entry.id, actor?.persona || (actor && enrolled.get(actor.id)?.persona), lead, personas),
            branchThrough: branches.through.has(entry.id), ancestorThrough: branches.ancestorThrough.has(entry.id) })
        })}</ul>
      </li>
    })}
    {show !== undefined && createPortal(<BoardDialog source={source} initial={show} {...list}
      candidates={leadOptions} close={() => { setShow(undefined); void refresh() }} />, document.body)}
  </>
}

export type SessionBoardTarget = { machine?: string; source: SessionBoardSource; label: string; candidates: Actor[] }

function isBoardBackdrop(event: MouseEvent<HTMLDialogElement>) {
  if (event.target !== event.currentTarget) return false
  const { left, right, top, bottom } = event.currentTarget.getBoundingClientRect()
  return event.clientX < left || event.clientX > right || event.clientY < top || event.clientY > bottom
}

function useBoardBackdrop(close: () => void) {
  const pressedOutside = useRef(false)
  return {
    onPointerDown(event: PointerEvent<HTMLDialogElement>) {
      pressedOutside.current = event.isPrimary && event.button === 0 && isBoardBackdrop(event)
    },
    onPointerCancel() { pressedOutside.current = false },
    onClick(event: MouseEvent<HTMLDialogElement>) {
      // The native dialog receives backdrop clicks too. A drag from inside
      // can also end there, so require both ends to be outside.
      const dismiss = pressedOutside.current && isBoardBackdrop(event)
      pressedOutside.current = false
      if (dismiss) close()
    },
  }
}

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
    {target && createPortal(<StatusBoardDialog key={target.source.key} target={target} close={close} opener={button.current} />, document.body)}
    {open === "" && createPortal(<BoardMachinePicker targets={targets} choose={setOpen} close={close} opener={button.current} />, document.body)}
  </>
}

function BoardMachinePicker({ targets, choose, close, opener }: {
  targets: readonly SessionBoardTarget[]; choose: (key: string) => void; close: () => void; opener: HTMLElement | null
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const heading = useId()
  const motion = useDockMotion(dialog)
  const dismiss = () => motion.close(() => dialog.current?.close())
  const backdrop = useBoardBackdrop(dismiss)
  useEffect(() => { dialog.current?.showModal(); motion.capture(opener); motion.open() }, [])
  return <dialog ref={dialog} className="session-board-dialog" aria-labelledby={heading} onClose={close} onCancel={event => { event.preventDefault(); dismiss() }} {...backdrop}>
    <header><h2 id={heading}>{t("sessionBoards")}</h2>
      <button type="button" onClick={dismiss}>{t("sessionBoardClose")}</button>
    </header>
    <div className="session-board-body">
      <p>{t("cloudMachinesLede")}</p>
      <ul className="session-board-index">{targets.map(target => <li key={target.source.key}>
        <button type="button" onClick={() => choose(target.source.key)}>{target.label}</button>
      </li>)}</ul>
    </div>
  </dialog>
}

function StatusBoardDialog({ target, close, opener }: { target: SessionBoardTarget; close: () => void; opener: HTMLElement | null }) {
  const list = useBoardList(target.source, true)
  return <BoardDialog source={target.source} initial="" candidates={target.candidates} close={close} opener={opener} {...list} />
}

function BoardRefresh({ refresh, disabled }: { refresh: () => void; disabled?: boolean }) {
  return <button type="button" className="session-board-icon-button" aria-label={t("sessionBoardRefresh")}
    title={t("sessionBoardRefresh")} onClick={refresh} disabled={disabled}>
    <svg aria-hidden="true" viewBox="0 0 24 24"><path d="M19 7a8 8 0 1 0 1 10M19 3v4h-4" /></svg>
  </button>
}

function BoardDialog({ source, initial, boards, loaded, problem, candidates, refresh, close, opener }: {
  source: SessionBoardSource; initial: string; boards: Board[]; loaded: boolean; problem: boolean
  candidates: Actor[]; refresh: () => Promise<void>; close: () => void; opener?: HTMLElement | null
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const heading = useId()
  const title = useRef<HTMLHeadingElement>(null)
  const motion = useDockMotion(dialog)
  const dismiss = () => {
    motion.capture(opener ?? document.querySelector<HTMLButtonElement>(".session-board-dock"), 0)
    motion.close(() => dialog.current?.close())
  }
  const backdrop = useBoardBackdrop(() => {
    const fields = dialog.current?.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>("form input[name=name], form textarea")
    const changed = Array.from(fields ?? []).some(field => field.name === "summary" ? field.value !== field.defaultValue : !!field.value)
    if (!changed && !dialog.current?.querySelector('[aria-busy="true"]')) dismiss()
  })
  const [selected, select] = useState(initial)
  const [opened, setOpened] = useState<Board>()
  const indexedBoard = boards.find(board => board.id === selected)
  const selectedBoard = opened?.id === selected ? opened : indexedBoard
  const candidateMap = new Map(candidates.map(actor => [actor.id, actor]))
  const projectIcon = selectedBoard && boardProjectIcon(selectedBoard, candidateMap)
  const view = useRef<HTMLDivElement>(null)
  const switchBoard = (id: string) => {
    select(id)
    void refresh()
    if (view.current) view.current.scrollTop = 0
  }
  const [expanded, expand] = useState(false)
  const [creating, create] = useState(false)
  useEffect(() => { dialog.current?.showModal(); motion.capture(opener); motion.open() }, [])
  useEffect(() => {
    if (dialog.current?.open) title.current?.focus({ preventScroll: true })
  }, [selected])
  const headingAction = t(initial ? "sessionBoardClose" : "sessionBoardBack")
  return <dialog ref={dialog} className="session-board-dialog session-board-reader" data-expanded={expanded} data-board={Boolean(selected) || undefined} aria-labelledby={heading} onClose={close} onCancel={event => { event.preventDefault(); dismiss() }} {...backdrop}>
    <header>
      {selected && <button type="button" className="session-board-icon-button" aria-label={headingAction}
        title={headingAction} onClick={initial ? dismiss : () => switchBoard("")}>
        <BoardChevron direction={initial ? "down" : "back"} />
      </button>}
      <div className="session-board-heading">
        <h2 id={heading} ref={title} tabIndex={-1}>{projectIcon && <SessionBoardMark icon={projectIcon} project />}{selectedBoard?.name ?? t("sessionBoards")}</h2>
        {selectedBoard && <BoardPurpose key={selectedBoard.id} text={selectedBoard.purpose} />}
      </div>
      <button type="button" className="session-board-expand" onClick={() => expand(!expanded)}>{t(expanded ? "sessionBoardRestore" : "sessionBoardExpand")}</button>
      <button type="button" onClick={dismiss}>{t("sessionBoardClose")}</button>
    </header>
    <div className="session-board-scroll" ref={view}>
    {selected ? <BoardMessages key={selected} source={source} id={selected} indexedBoard={indexedBoard}
      refreshIndex={refresh} candidates={candidateMap} onRead={setOpened} />
    : <div className="session-board-body">
      <div className="session-board-actions session-board-index-actions">
        {source.writable && candidates.length > 0 && <button type="button" className="session-board-primary"
          aria-expanded={creating} onClick={() => create(!creating)}>{t("sessionBoardCreate")}</button>}
        <BoardRefresh refresh={() => void refresh()} />
      </div>
      {creating && source.writable && <BoardCreate source={source} candidates={candidates} onCreated={id => {
        create(false); switchBoard(id)
      }} />}
      {problem && <p role="alert">{t("sessionBoardReadError")}</p>}
      {!loaded && <LoadingSkeleton id="SK-36" label={t("sessionBoardsLoading")} />}
      {loaded && boards.length === 0 && !problem && <p>{t("sessionBoardsEmpty")}</p>}
      <ul className="session-board-index">{boards.map(board => {
        const unread = unreadFor(source, board)
        return <li key={board.id}>
          <button type="button" className="session-board-index-card" onClick={() => switchBoard(board.id)}>
            <span className="session-board-index-mark"><BoardGlyph /></span>
            <span className="session-board-index-content">
              <strong>{board.name}</strong>
              <span className="session-board-index-purpose">{board.purpose}</span>
              <span className="session-board-index-meta">
                {board.archived_at ? t("sessionBoardArchived") : t("sessionBoardMemberCount", { count: board.members?.length ?? 0 })}
                {unread > 0 && <small>{t("sessionBoardUnread", { count: unread })}</small>}
              </span>
            </span>
            <BoardChevron direction="forward" />
          </button>
        </li>
      })}</ul>
    </div>}
    </div>
  </dialog>
}

function BoardPurpose({ text }: { text: string }) {
  const measure = useRef<HTMLSpanElement>(null)
  const [clipped, setClipped] = useState(false)
  useLayoutEffect(() => {
    const line = measure.current
    const area = line?.parentElement
    if (!line || !area) return
    const check = () => setClipped(text.trim().includes("\n") || line.getBoundingClientRect().width > area.clientWidth)
    check()
    const observer = new ResizeObserver(check)
    observer.observe(area)
    observer.observe(line)
    return () => observer.disconnect()
  }, [text])
  return <div className="session-board-heading-purpose">
    <span className="session-board-purpose-measure" aria-hidden="true"><span ref={measure}>{text}</span></span>
    {clipped ? <details>
      <summary><span>{text}</span><BoardChevron direction="down" /></summary>
    </details> : <p>{text}</p>}
  </div>
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

function BoardMessages({ source, id, indexedBoard, refreshIndex, candidates, onRead }: {
  source: SessionBoardSource; id: string; indexedBoard?: Board; refreshIndex: () => Promise<void>
  candidates: ReadonlyMap<string, Actor>; onRead: (board: Board) => void
}) {
  const [page, setPage] = useState<Page>()
  const [error, setError] = useState("")
  const [reading, setReading] = useState(false)
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
    setError("")
    try {
      const next = await sourceRef.current.read<Page>(before ? { board: id, before } : { board: id }, request.signal)
      if (request.signal.aborted) return
      setPage(previous => before && previous ? { ...next, posts: [...next.posts, ...previous.posts] } : next)
      onRead(next.board)
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
    } catch (e) {
      if (!request.signal.aborted) setError(boardErrorCode(e, "session_board_read_unavailable"))
    }
    finally { if (!request.signal.aborted) setReading(false) }
  }, [id, onRead])
  useEffect(() => { void load(); return () => controller.current?.abort() }, [load])
  const b = page?.board
  const personas = usePersonas(source.machine)
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
  // The index refreshes even while the reader stays open on an older message page.
  const roster = indexedBoard ?? b
  const lead = roster?.members?.find(member => member.id === roster.lead)
  const members = roster?.members?.filter(member => member.id !== roster.lead) ?? []
  const rosterMembers = roster?.members ?? []
  const total = rosterMembers.length
  const listed = rosterMembers.filter(member => member.presence === "listed").length
  const count = listed < total && rosterMembers.every(member => member.presence)
    ? t("sessionBoardMembersInList", { listed, total }) : total
  const roleByActor = new Map(roster?.members?.map(member => [member.id, member.persona]))
  const labelFor = (actor: Actor) => candidates.get(actor.id)?.label ?? actor.label
  const avatar = (actor: Actor) => {
    const isLead = actor.id === roster?.lead
    const persona = candidates.get(actor.id)?.persona || roleByActor.get(actor.id) || actor.persona
    return <SessionBoardMark lead={isLead}
      icon={sessionBoardAvatar(actor.id, persona, isLead, personas)} />
  }
  const unread = b ? Math.max(0, b.latest - unreadSince) : 0
  return <div className="session-board-body" aria-busy={Boolean(page) && (reading || change.busy)}>
    {!page && !error && <LoadingSkeleton id="SK-37" kind="cards" label={t("sessionBoardMessagesLoading")} />}
    {error && <p role="alert">{t("sessionBoardReadError")} <code>{error}</code></p>}
    {!page && error && <BoardRefresh refresh={() => void load()} />}
    {change.error && <p role="alert">{writeProblem(change.error)} <code>{change.error}</code></p>}
    {b && page && <>
      <div className="session-board-overview">
        {lead && <div className="session-board-lead-strip">{avatar(lead)}<SessionBoardLead /><strong>{labelFor(lead)}</strong></div>}
        <div className="session-board-actions">
          <BoardRefresh refresh={() => { void refreshIndex(); void load() }} disabled={reading} />
          {allowed && <button type="button" aria-expanded={manage} onClick={() => setManage(!manage)}>{t("sessionBoardManage")}</button>}
          {b.archived_at && <span>{t("sessionBoardArchived")}</span>}
        </div>
      </div>
      <div className="session-board-disclosures">
        {b.summary && <details className="session-board-summary"><summary><ChevronIcon direction="right" className="disclosure-chevron" />{t("sessionBoardSummary")}</summary><p className="session-board-text">{b.summary}</p></details>}
        {roster?.members && <details className="session-board-members">
          <summary><ChevronIcon direction="right" className="disclosure-chevron" />{t("sessionBoardMembers")} <span>{count}</span></summary>
          <ul>{(lead ? [lead, ...members] : members).map(member => <li key={member.id} data-not-listed={member.presence === "not_listed" || undefined}>
            {avatar(member)}
            <strong>{labelFor(member)}</strong>
            {member.id === roster.lead && <SessionBoardLead />}
            {member.presence === "not_listed" && <small className="session-board-member-presence">{t("sessionBoardMemberNotListed")}</small>}
          </li>)}</ul>
        </details>}
      </div>
      {allowed && manage && <div className="session-board-management">
        <form key={b.summary} onSubmit={e => { e.preventDefault(); action({ action: "summary", text: String(new FormData(e.currentTarget).get("summary")) }) }}>
          <label>{t("sessionBoardSummary")}<textarea name="summary" defaultValue={b.summary} rows={3} /></label>
          <button disabled={change.busy}>{t("sessionBoardSave")}</button>
        </form>
        <form onSubmit={e => { e.preventDefault(); action({ action: "transfer", target: String(new FormData(e.currentTarget).get("lead")) }) }}>
          <label>{t("sessionBoardTransfer")}<select name="lead" aria-label={t("sessionBoardTransfer")} required><option value="">{t("sessionBoardChoose")}</option>
            {members.filter(member => candidates.has(member.id)).map(member => <option key={member.id} value={member.id}>{labelFor(member)}</option>)}
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
        <div className="session-board-post-meta">
          {p.author.id !== "person" && avatar(p.author)}
          <strong>{p.author.id === "person" ? t("sessionBoardPerson") : labelFor(p.author)}</strong>
          {p.author.id === roster?.lead && <SessionBoardLead />}
          <time dateTime={new Date(p.at * 1000).toISOString()}>
            {new Date(p.at * 1000).toLocaleString(document.documentElement.lang || undefined, { dateStyle: "short", timeStyle: "short" })}
          </time>
          <span>#{p.seq}</span>
        </div>
        {p.reply_to && <small>{t("sessionBoardReplyTo", { seq: p.reply_to })}</small>}
        <BoardPostText text={p.text} reply={allowed ? () => setReply(p.seq) : undefined} />
      </li>)}</ol>
      {page.more && <button type="button" disabled={reading} onClick={() => void load(page.posts[0]?.seq)}>{t("sessionBoardEarlier")}</button>}
      {allowed && <form onSubmit={post}>
        {reply && <div>{t("sessionBoardReplyTo", { seq: reply })} <button type="button" onClick={() => setReply(undefined)}>{t("sessionBoardCancel")}</button></div>}
        <label>{t("sessionBoardMessage")}<textarea value={draft} onChange={e => setDraft(e.currentTarget.value)} rows={3} required /></label>
        <button type="submit" disabled={change.busy || !draft.trim()}>{t("sessionBoardPost")}</button>
      </form>}
    </>}
  </div>
}

// A collapsed note shows plain text so its hidden links cannot take focus.
function BoardPostText({ text, reply }: { text: string; reply?: () => void }) {
  const [expanded, expand] = useState(false)
  const [clipped, setClipped] = useState(false)
  const content = useId()
  const excerpt = useRef<HTMLParagraphElement>(null)
  const { html, preview } = useMemo(() => {
    const html = richText(text, { codeCopy: false })
    const template = document.createElement("template")
    template.innerHTML = html
    const preview = Array.from(template.content.childNodes, node => node.textContent ?? "")
      .join(" ").replace(/\s+/g, " ").trim() || text
    return { html, preview }
  }, [text])
  const hasFormatting = preview !== text.replace(/\s+/g, " ").trim()
  useLayoutEffect(() => {
    const element = excerpt.current
    if (!element || hasFormatting) return
    const check = () => setClipped(element.scrollHeight > element.clientHeight + 1)
    check()
    const observer = new ResizeObserver(check)
    observer.observe(element)
    return () => observer.disconnect()
  }, [expanded, hasFormatting, preview])
  const canExpand = expanded || hasFormatting || clipped
  return <>
    {expanded
      ? <div id={content} className="session-board-text" dangerouslySetInnerHTML={{ __html: html }} />
      : <p id={content} ref={excerpt} className="session-board-note-preview">{preview}</p>}
    {(canExpand || reply) && <div className="session-board-post-actions">
      {canExpand && <button type="button" className="session-board-note-control" aria-expanded={expanded} aria-controls={content}
        onClick={() => expand(!expanded)}>
        <BoardChevron direction="down" />{t(expanded ? "sessionBoardHidePost" : "sessionBoardShowPost")}
      </button>}
      {reply && <button className="session-board-reply" type="button" onClick={reply}>{t("sessionBoardReply")}</button>}
    </div>}
  </>
}
