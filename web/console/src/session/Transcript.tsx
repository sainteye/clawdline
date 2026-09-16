import { useEffect, useLayoutEffect, useMemo, useRef, useState, type MouseEvent } from "react"
import type { SessionRow, TranscriptPage, Turn } from "@clawdline/contract"
import { client } from "../client.js"
import { usePoll } from "../useFleet.js"
import * as L from "../legacy/bridge.js"

/*
 * The transcript pane, drawn as `view/transcript.js` draws it.
 *
 * Every element, class and attribute below is the original's — `.entry`,
 * `.who > .speaker + time`, `.body`, `.entry.toolrow`, `.entry.folded > .pill`
 * — because `legacy/transcript.css` styles those names and no others. The
 * functions keep the original's names (`worthDrawing`, `runHTML`, `foldKey`…)
 * so each can be read beside the one it replicates.
 *
 * What this wire does not carry is not drawn, rather than drawn empty:
 * - A tool call's arguments and a tool's output. `Turn.text` is empty for both,
 *   so a call's subject is the original's own "…" and every result is dropped by
 *   `worthDrawing`, exactly as an empty result is dropped there.
 * - Questions (`askOf` needs the marked arguments), `fileChanges`, `plan`,
 *   `activity`, `artifacts`, peer/message/notice roles and the composer's
 *   optimistic entries. None of those can arrive here, so none of their blocks
 *   are written.
 */

/** Same as the original's `limit=200`, so both panes are reading the same stretch. */
const LIMIT = 200
const POLL_MS = 4000

/** One entry in the shape the original's wire has: tool calls and results under role "tool". */
interface Entry {
  role: string
  at: number
  text: string
  tool?: string
}

type Block = { kind: "entry"; rows: Entry[] } | { kind: "run"; rows: Entry[]; live: boolean }

export function Transcript({ id }: { id: string }) {
  // Keyed, so a different session starts from nothing: no previous session's
  // turns, error or opened folds are shown under the new name.
  return <TranscriptOf key={id} id={id} />
}

function TranscriptOf({ id }: { id: string }) {
  const read = useMemo(() => () => client.transcript(id, LIMIT), [id])
  const { data, error } = usePoll<TranscriptPage>(read, POLL_MS)
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const session = useSession(id)
  const entries = useMemo(() => (data ? toEntries(data.turns) : []), [data])
  const skeleton = useWait(!data && !error)

  // `liveSweepPhase`: the box carries the page-wide clock, so a newly live row
  // starts its sweep where one sweep across the page would be now.
  useLayoutEffect(() => {
    const box = document.getElementById("tx")
    if (!box || !entries.length) return
    const now = typeof performance !== "undefined" && performance.now ? performance.now() : Date.now()
    box.style.setProperty("--live-sweep-delay", -Math.round(now % LIVE_SWEEP_MS) + "ms")
  })

  const T = L.strings
  if (skeleton) return <Skeleton />
  if (!data && !error) return null
  // The original has no "evidence: none"; a transcript it could not read is its
  // `view.error`, and that is how it is drawn.
  const failed = error ?? (data?.evidence === "none" ? data.note || T.webTranscriptFailed : null)
  if (failed && !entries.length) return <div className="tx-note err">{failed}</div>
  const notice = failed ? <div className="tx-note err">{failed}</div> : null
  if (!entries.length) return <div className="tx-note">{T.noOutput}</div>

  const who = {
    user: T.webWhoYou,
    tool: T.webWhoTool,
    assistant: assistantName(session?.assistant),
  }
  const toggle = (key: string) =>
    setExpanded((was) => {
      const next = { ...was }
      if (next[key]) delete next[key]
      else next[key] = true
      return next
    })

  const blocks = blocksOf(entries.filter(worthDrawing), session?.state === "working")

  const toolRow = (e: Entry, live: boolean, n: number) => {
    const key = "e" + foldKey([e])
    const open = !!expanded[key]
    return (
      <div className="entry toolrow" data-role="tool" data-live={live ? "1" : undefined} key={key + ":" + n}>
        <div className="who">{who.tool}</div>
        <div className="body">
          <button
            type="button"
            className="toolline"
            data-fold={key}
            aria-expanded={open ? "true" : "false"}
            onClick={() => toggle(key)}
          >
            {e.tool ? <span className="toolname">{e.tool}</span> : <span className="caret">{open ? "⏷" : "⏵"}</span>}
            <span className="subject">{firstLine(e.text)}</span>
          </button>
          {open ? (
            <div className="toolbody" onClick={copyFrom} dangerouslySetInnerHTML={{ __html: L.richTextHTML(e.text) }} />
          ) : null}
        </div>
      </div>
    )
  }

  const run = (rows: Entry[], live: boolean, at: number) => {
    const names = rows.filter((e) => e.tool).map((e) => e.tool as string)
    const key = foldKey(rows)
    const last = rows.length - 1
    const each = (lit: boolean) => rows.map((e, n) => toolRow(e, lit && n === last, at + n))
    if (names.length < 2) return each(live)
    const open = !!expanded[key]
    const lit = live && !open
    return [
      <div className="entry folded" data-role="tool" data-live={lit ? "1" : undefined} key={key + ":" + at}>
        <div className="who">{who.tool}</div>
        <div className="body">
          <button
            type="button"
            className="pill"
            data-fold={key}
            aria-expanded={open ? "true" : "false"}
            onClick={() => toggle(key)}
          >
            <span className="caret">{open ? "⏷" : "⏵"}</span>
            <span className="steps">{L.fillString(T.webSteps, { n: names.length })}</span>
            {open ? null : <span className="what">{foldedRunDescription(names)}</span>}
          </button>
        </div>
      </div>,
      ...(open ? each(live) : []),
    ]
  }

  const entry = (e: Entry, at: number) => {
    const role = e.role === "user" ? "user" : "assistant"
    const record = role === "user" ? L.parseWorkflowRecord(e.text, role) : null
    let body = L.richTextHTML(record ? (record as { text: string }).text : e.text)
    if (record) {
      // The label is the original's own literal; the catalog has no key for it.
      body += L.workflowRecordHTML(record, {
        escape: esc,
        label: /^zh/i.test(document.documentElement.lang || "") ? "看板紀錄" : "Board record",
      })
    }
    const mark = role === "assistant" && assistantIcons() ? logoOf(session?.assistant) : ""
    return (
      <div className="entry" data-role={role} key={"m:" + at}>
        <div className="who">
          <span className="speaker" dangerouslySetInnerHTML={{ __html: mark + esc(who[role]) }} />
          {e.at ? <time data-at={e.at}>{clockOf(e.at)}</time> : null}
        </div>
        <div className="body" onClick={copyFrom} dangerouslySetInnerHTML={{ __html: body }} />
      </div>
    )
  }

  let at = 0
  const drawn = blocks.flatMap((block) => {
    const start = at
    at += block.rows.length
    return block.kind === "run" ? run(block.rows, block.live, start) : [entry(block.rows[0], start)]
  })
  return (
    <>
      {notice}
      {drawn}
    </>
  )
}

/**
 * The turns as the original's wire files them.
 *
 * This wire puts a call under the assistant and its result under the user,
 * each marked by `tool`; the original files both under "tool", a result with no
 * name. A message that said something *and* called a tool is two entries there,
 * the prose first, and is split the same way here.
 */
function toEntries(turns: Turn[]): Entry[] {
  const out: Entry[] = []
  for (const t of turns) {
    const at = Math.floor(Date.parse(t.at) / 1000) || 0
    if (t.text) out.push({ role: t.role, at, text: t.text })
    if (t.tool === "result") out.push({ role: "tool", at, text: "" })
    else if (t.tool) out.push({ role: "tool", at, text: "", tool: t.tool })
  }
  return out
}

/** `worthDrawing`: prose always, a named call always, other tool output only with a letter or digit in it. */
function worthDrawing(e: Entry): boolean {
  if (e.role !== "tool") return true
  if (e.tool) return true
  const text = String(e.text ?? "").trim()
  return text.length > 0 && /[\p{L}\p{N}]/u.test(text)
}

/** One message, or one whole run of tool calls, per block; the last run is live while the session works. */
function blocksOf(entries: Entry[], working: boolean): Block[] {
  const blocks: Block[] = []
  let liveAt = -1
  let i = 0
  while (i < entries.length) {
    if (entries[i].role !== "tool") {
      blocks.push({ kind: "entry", rows: [entries[i]] })
      i += 1
      continue
    }
    const rows: Entry[] = []
    while (i < entries.length && entries[i].role === "tool") rows.push(entries[i++])
    liveAt = blocks.length
    blocks.push({ kind: "run", rows, live: false })
  }
  const last = blocks[blocks.length - 1]
  if (last && last.kind === "run" && liveAt === blocks.length - 1 && working) last.live = true
  return blocks
}

/** The live sweep's period in `transcript.css`. */
const LIVE_SWEEP_MS = 2200

function firstLine(text: string): string {
  const line = String(text ?? "").split("\n")[0].replace(/\s+/g, " ").trim()
  return line || "…"
}

/** `foldedRunDescription`: the provider once, shell commands counted, other names as they are. The original's English. */
function foldedRunDescription(names: string[]): string {
  const unique = (values: string[]) => values.filter((v, i) => values.indexOf(v) === i)
  let providers: string[] = []
  let shellCount = 0
  let others: string[] = []
  for (const name of names) {
    const match = /^mcp__(.+?)__/.exec(name)
    if (match) providers.push(match[1])
    else if (name === "Bash") shellCount += 1
    else others.push(name)
  }
  const parts: string[] = []
  providers = unique(providers)
  others = unique(others)
  if (providers.length) parts.push("Called " + providers.join(" · "))
  if (shellCount) parts.push("ran " + shellCount + " shell command" + (shellCount === 1 ? "" : "s"))
  if (others.length) parts.push(others.join(" · "))
  return parts.join(", ")
}

/** `foldKey`: FNV-1a over name and text, so an opened run stays open across redraws. */
function foldKey(run: Entry[]): string {
  let hash = 0x811c9dc5
  for (const e of run) {
    const text = (e.tool || "") + "" + (e.text || "")
    for (let i = 0; i < text.length; i++) hash = Math.imul(hash ^ text.charCodeAt(i), 0x01000193)
  }
  return (hash >>> 0).toString(36)
}

/** `clockOf` from `core/util.js`, which the bridge does not export. */
function clockOf(unix: number): string {
  if (!unix) return ""
  const d = new Date(unix * 1000)
  const age = Date.now() / 1000 - unix
  if (age < 60) return L.strings.webJustNow
  if (age < 3600) return L.fillString(L.strings.webMinutesAgo, { n: Math.round(age / 60) })
  const h = d.getHours()
  const m = d.getMinutes()
  return (h < 10 ? "0" : "") + h + ":" + (m < 10 ? "0" : "") + m
}

/** `esc` from `core/esc.js`, which the bridge does not export. */
function esc(s: unknown): string {
  return String(s ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;")
}

/** `assistantName` from `core/pixels.js`, which the bridge does not export. */
function assistantName(kind: string | undefined): string {
  return kind === "claude" ? "claude" : kind === "codex" ? "codex" : "assistant"
}

/** `assistantLogo`, taken from the bridge's logo-plus-name markup since the bridge has no logo on its own. */
function logoOf(kind: string | undefined): string {
  const html = L.whoHTML(kind)
  const cut = html.lastIndexOf("<span>")
  return cut < 0 ? "" : html.slice(0, cut)
}

/** `S.assistantIcons`: `storedBool("clawdline.assistant-icons", true)`. */
function assistantIcons(): boolean {
  try {
    const value = localStorage.getItem("clawdline.assistant-icons")
    return value === null ? true : value === "1"
  } catch {
    return true
  }
}

/** The transcript's one delegated click: the copy button inside a rendered code block. */
function copyFrom(ev: MouseEvent<HTMLElement>) {
  const copy = (ev.target as Element).closest?.("button.codecopy")
  if (copy) L.copyCode(copy.getAttribute("data-code-copy") ?? "")
}

/**
 * The open session's row: the published list first, and the daemon once if a
 * filter has hidden it. The speaker's name and mark come from its assistant,
 * and the live sweep from its state.
 */
function useSession(id: string): SessionRow | undefined {
  const shown = L.orderedRows().find((r) => r.id === id)
  const [fetched, setFetched] = useState<SessionRow | undefined>(undefined)
  const missing = !shown
  useEffect(() => {
    if (!missing) return
    let alive = true
    client
      .sessions()
      .then((snap) => {
        if (alive) setFetched(snap.sessions.find((r) => r.id === id))
      })
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [id, missing])
  return shown ?? fetched
}

/**
 * `Waits.tx`: nothing for the first 150ms, and once the skeleton is up it stays
 * for at least 320ms, so a fast answer never strobes.
 */
function useWait(waiting: boolean): boolean {
  const [visible, setVisible] = useState(false)
  const shown = useRef(0)
  useEffect(() => {
    if (waiting) {
      if (visible) return
      const timer = setTimeout(() => {
        shown.current = Date.now()
        setVisible(true)
      }, 150)
      return () => clearTimeout(timer)
    }
    if (!visible) return
    const left = 320 - (Date.now() - shown.current)
    if (left <= 0) {
      setVisible(false)
      return
    }
    const timer = setTimeout(() => setVisible(false), left)
    return () => clearTimeout(timer)
  }, [waiting, visible])
  return visible
}

/** `txSkeleton`. */
function Skeleton() {
  const shapes = [[88, 54], [96, 78, 43], [71], [92, 61, 34]]
  return (
    <div className="skel" role="status" aria-label={L.strings.webReading}>
      {shapes.map((widths, i) => (
        <div className="entry skel-entry" key={i}>
          <div className="who">
            <span className="bar" />
          </div>
          <div className="body">
            {widths.map((w, j) => (
              <span className="bar" style={{ width: w + "%" }} key={j} />
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}
