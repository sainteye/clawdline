import { useEffect, useLayoutEffect, useMemo, useRef, useState, type MouseEvent, type ReactElement } from "react"
import type {
  SessionRow,
  TranscriptActivity,
  TranscriptAction,
  TranscriptEntry,
  TranscriptFileChange,
  TranscriptPage,
  TranscriptPlanStep,
} from "@clawdline/contract"
import { client } from "../client.js"
import { usePoll } from "../useFleet.js"
import * as L from "../legacy/bridge.js"

/*
 * The transcript pane, drawn as `view/transcript.js` draws it.
 *
 * Every element, class and attribute below is the original's — `.entry`,
 * `.who > .speaker + time`, `.body`, `.entry.toolrow`, `.entry.folded > .pill`,
 * `.entry.ask`, `.entry.patch`, `.entry.plan`, `.entry.activity`,
 * `.entry.clawdline-notice` — because `legacy/transcript.css` styles those
 * names and no others. The functions keep the original's names (`worthDrawing`,
 * `runHTML`, `askOf`, `patchHTML`…) so each can be read beside the one it
 * replicates, and the wire is the original's too: `entries` with `role`,
 * `text`, `tool`, `at`, `fileChanges`, `plan`, `activity`, `notice`, `source`.
 *
 * Not drawn, because this daemon does not send it yet: image artifacts (the
 * original's `artifactTilesHTML`) and the composer's optimistic entries.
 */

/** Same as the original's `limit=200`, so both panes are reading the same stretch. */
const LIMIT = 200
const POLL_MS = 4000

/** The mark an `AskUserQuestion` call's text starts with. */
const ASK_MARK = "\u0001ask\u0001"

interface Question {
  header: string
  text: string
  multi: boolean
  options: { label: string; note: string }[]
}

/** One entry as the wire has it, with its questions read once. */
type Entry = TranscriptEntry & { ask?: Question[] | null }

type Block =
  | { kind: "entry"; rows: Entry[] }
  | { kind: "run"; rows: Entry[]; live: boolean }
  | { kind: "explored"; rows: Entry[] }
  | { kind: "ask"; rows: Entry[] }

type Toggle = (key: string, defaultOpen?: boolean) => void

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
  const entries = useMemo<Entry[]>(() => (data ? data.entries.map((e) => ({ ...e })) : []), [data])
  const skeleton = useWait(!data && !error)

  // `settleTranscript`: a read whose signature is new is drawn, and if the
  // reader was at the bottom before it was drawn they are put back there. A
  // reader who scrolled up stays where they are. A fresh session starts empty,
  // which is the bottom, so it opens on its newest entry.
  const drawnSignature = skeleton || !data ? undefined : data.signature || null
  const shownSignature = useRef<string | null | undefined>(undefined)
  const stick = useRef(false)
  if (drawnSignature !== undefined && (drawnSignature === null || drawnSignature !== shownSignature.current)) {
    stick.current = atBottom()
  }
  useLayoutEffect(() => {
    if (drawnSignature === undefined) return
    if (drawnSignature !== null && drawnSignature === shownSignature.current) return
    shownSignature.current = drawnSignature
    if (stick.current) toBottom()
  }, [data, drawnSignature])

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

  const who: Record<string, string> = {
    user: T.webWhoYou,
    assistant: L.assistantDisplayName(session?.assistant),
    peer: "Claude ↔",
    message: "Clawdline ↔",
    notice: "Clawdline",
    tool: T.webWhoTool,
  }
  // `toggleFold`: a fold that is open by default is remembered only when closed.
  const toggle: Toggle = (key, defaultOpen = false) =>
    setExpanded((was) => {
      const open = defaultOpen ? was[key] !== false : !!was[key]
      const next = { ...was }
      if (!defaultOpen && open) delete next[key]
      else next[key] = !open
      return next
    })
  const view: View = { who, expanded, toggle, assistant: session?.assistant }

  const blocks = blocksOf(entries.filter(worthDrawing), session?.state === "working")
  let at = 0
  const drawn = blocks.flatMap((block) => {
    const start = at
    at += block.rows.length
    if (block.kind === "run") return runHTML(view, block.rows, block.live, start)
    if (block.kind === "explored") return exploredRunHTML(view, block.rows, start)
    if (block.kind === "ask") return [askHTML(view, block.rows[0], start)]
    return [entryHTML(view, block.rows[0], start)]
  })
  return (
    <>
      {notice}
      {drawn}
    </>
  )
}

interface View {
  who: Record<string, string>
  expanded: Record<string, boolean>
  toggle: Toggle
  assistant: string | undefined
}

/** `worthDrawing`: prose always, a named call always, other tool output only with a letter or digit in it. */
function worthDrawing(e: Entry): boolean {
  if (e.role !== "tool") return true
  if (e.tool) return true
  const text = String(e.text ?? "").trim()
  return text.length > 0 && /[\p{L}\p{N}]/u.test(text)
}

/**
 * One message, one question, one structured card, or one whole run of tool
 * calls per block; the last run is live while the session works.
 */
function blocksOf(entries: Entry[], working: boolean): Block[] {
  const blocks: Block[] = []
  let liveAt = -1
  let i = 0
  const plainTool = (e: Entry) =>
    e.role === "tool" && !askOf(e) && !fileChangesOf(e) && !planOf(e) && !activityOf(e)
  while (i < entries.length) {
    if (entries[i].role !== "tool") {
      blocks.push({ kind: "entry", rows: [entries[i]] })
      i += 1
      continue
    }
    // Adjacent Explored items read as one compact run.
    if (activityOf(entries[i])?.kind === "explored") {
      const rows: Entry[] = []
      while (i < entries.length && entries[i].role === "tool" && activityOf(entries[i])?.kind === "explored") {
        rows.push(entries[i++])
      }
      blocks.push({ kind: "explored", rows })
      continue
    }
    // A patch, a plan or a called tool is output worth reading, not machinery.
    if (fileChangesOf(entries[i]) || planOf(entries[i]) || activityOf(entries[i])) {
      blocks.push({ kind: "entry", rows: [entries[i++]] })
      continue
    }
    // A question breaks the run around it: it is addressed to the reader.
    if (askOf(entries[i])) {
      blocks.push({ kind: "ask", rows: [entries[i++]] })
      continue
    }
    const rows: Entry[] = []
    while (i < entries.length && plainTool(entries[i])) rows.push(entries[i++])
    liveAt = blocks.length
    blocks.push({ kind: "run", rows, live: false })
  }
  const last = blocks[blocks.length - 1]
  if (last && last.kind === "run" && liveAt === blocks.length - 1 && working) last.live = true
  return blocks
}

/* --------------------------------------------------------------------------
   Tool calls
   ------------------------------------------------------------------------ */

/** `toolRowHTML`: one call or result as a line that opens when pressed. */
function toolRowHTML(v: View, e: Entry, live: boolean, n: number) {
  const key = "e" + foldKey([e])
  const open = !!v.expanded[key]
  return (
    <div className="entry toolrow" data-role="tool" data-live={live ? "1" : undefined} key={key + ":" + n}>
      <div className="who">{v.who.tool}</div>
      <div className="body">
        <button
          type="button"
          className="toolline"
          data-fold={key}
          aria-expanded={open ? "true" : "false"}
          onClick={() => v.toggle(key)}
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

/** `runHTML`: two or more calls fold behind one pill. */
function runHTML(v: View, rows: Entry[], live: boolean, at: number) {
  const names = rows.filter((e) => e.tool).map((e) => e.tool as string)
  const key = foldKey(rows)
  const last = rows.length - 1
  const each = (lit: boolean) => rows.map((e, n) => toolRowHTML(v, e, lit && n === last, at + n))
  if (names.length < 2) return each(live)
  const open = !!v.expanded[key]
  return [foldHTML(v, key, names, open, live && !open, at), ...(open ? each(live) : [])]
}

/** `foldHTML`: the line a folded run leaves behind, and the handle that opens it. */
function foldHTML(v: View, key: string, names: string[], open: boolean, live: boolean, at: number) {
  return (
    <div className="entry folded" data-role="tool" data-live={live ? "1" : undefined} key={key + ":" + at}>
      <div className="who">{v.who.tool}</div>
      <div className="body">
        <button
          type="button"
          className="pill"
          data-fold={key}
          aria-expanded={open ? "true" : "false"}
          onClick={() => v.toggle(key)}
        >
          <span className="caret">{open ? "⏷" : "⏵"}</span>
          <span className="steps">{L.fillString(L.strings.webSteps, { n: names.length })}</span>
          {open ? null : <span className="what">{foldedRunDescription(names)}</span>}
        </button>
      </div>
    </div>
  )
}

/** The live sweep's period in `transcript.css`. */
const LIVE_SWEEP_MS = 2200

/** `atBottom`: within 40px of the end counts, so a reader need not land on the last pixel. */
function atBottom(): boolean {
  const el = document.getElementById("tx-scroll")
  if (!el) return false
  return el.scrollTop + el.clientHeight >= el.scrollHeight - 40
}

/** `toBottom`. */
function toBottom(): void {
  const el = document.getElementById("tx-scroll")
  if (el) el.scrollTop = el.scrollHeight
}

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

/**
 * `foldKey`: FNV-1a over name and text — and a patch's or an activity's
 * payload — so an opened run stays open across redraws.
 */
function foldKey(run: Entry[]): string {
  let hash = 0x811c9dc5
  for (const e of run) {
    let text = (e.tool || "") + "\u0001" + (e.text || "")
    if (Array.isArray(e.fileChanges)) text += "\u0001" + JSON.stringify(e.fileChanges)
    if (e.activity && typeof e.activity === "object") text += "\u0001" + JSON.stringify(e.activity)
    for (let i = 0; i < text.length; i++) hash = Math.imul(hash ^ text.charCodeAt(i), 0x01000193)
  }
  return (hash >>> 0).toString(36)
}

/* --------------------------------------------------------------------------
   A question Claude stopped to ask
   ------------------------------------------------------------------------ */

/** `askOf`: the questions in a marked call, `[]` when the mark does not parse, null otherwise. */
function askOf(e: Entry): Question[] | null {
  if (!e || e.role !== "tool" || !e.tool) return null
  if (e.ask === undefined) e.ask = parseAsk(e.text)
  return e.ask
}

function parseAsk(text: string): Question[] | null {
  const s = String(text ?? "")
  if (s.slice(0, ASK_MARK.length) !== ASK_MARK) return null
  let rows: unknown = null
  try {
    rows = JSON.parse(s.slice(ASK_MARK.length))
  } catch {
    rows = null
  }
  if (!Array.isArray(rows) || !rows.length) return []
  return rows.map((raw) => {
    const row = (raw || {}) as Record<string, unknown>
    const list = Array.isArray(row.o) ? row.o : []
    const options = []
    for (const item of list) {
      const o = (item || {}) as Record<string, unknown>
      if (typeof o.l !== "string" || !o.l) continue
      options.push({ label: o.l, note: typeof o.d === "string" ? o.d : "" })
    }
    return {
      header: typeof row.h === "string" ? row.h : "",
      text: typeof row.q === "string" ? row.q : "",
      multi: row.m === true,
      options,
    }
  })
}

/** `askHTML`: a question, drawn in full and never folded. */
function askHTML(v: View, e: Entry, at: number) {
  const T = L.strings
  const esc = L.escapeHTML
  const body = (askOf(e) || [])
    .map((q) => {
      let head = ""
      if (q.header) head += '<div class="askhead">' + esc(q.header) + "</div>"
      if (q.multi) head += '<div class="askany">' + esc(T.webAskAny) + "</div>"
      const asked = q.text ? '<div class="askq">' + L.inlineMdHTML(q.text) + "</div>" : ""
      const options = q.options
        .map(
          (o, n) =>
            '<li class="askopt"><span class="n">' +
            (n + 1) +
            "</span>" +
            '<span class="what"><b>' +
            L.inlineMdHTML(o.label) +
            "</b>" +
            (o.note ? '<span class="note">' + L.inlineMdHTML(o.note) + "</span>" : "") +
            "</span></li>",
        )
        .join("")
      return head + asked + (options ? '<ol class="askopts">' + options + "</ol>" : "")
    })
    .join("")
  return (
    <div className="entry ask" data-role="ask" key={"ask:" + at}>
      {whoHTML(v, "assistant", e.at)}
      <div
        className="body"
        dangerouslySetInnerHTML={{
          __html: '<div class="askbox"><div class="asktag">' + esc(T.webAskLabel) + "</div>" + body + "</div>",
        }}
      />
    </div>
  )
}

/* --------------------------------------------------------------------------
   Messages
   ------------------------------------------------------------------------ */

/** `whoHTML`: the speaker, with the assistant's mark when this browser opted into it. */
function whoHTML(v: View, role: string, at: number | undefined) {
  const mark = role === "assistant" && L.assistantIconsOn() ? L.assistantLogoHTML(v.assistant) : ""
  return (
    <div className="who">
      <span className="speaker" dangerouslySetInnerHTML={{ __html: mark + L.escapeHTML(v.who[role]) }} />
      {at ? <time data-at={at}>{L.clock(at)}</time> : null}
    </div>
  )
}

/** `entryHTML`: everything that is not a run of calls or a question. */
function entryHTML(v: View, e: Entry, at: number): ReactElement {
  if (fileChangesOf(e)) return patchHTML(v, e, at)
  if (planOf(e)) return planHTML(v, e, at)
  const activity = activityOf(e)
  if (activity) {
    return activity.kind === "explored" ? <>{exploredRunHTML(v, [e], at)}</> : activityCardHTML(v, e, at)
  }
  if (e.role === "notice") return noticeHTML(v, e, at)
  const role = v.who[e.role] ? e.role : "assistant"
  const esc = L.escapeHTML
  if (role === "message") {
    const assistant = String(e.sourceAssistant || "")
    const meta = assistant
      ? L.assistantLogoHTML(assistant) + "<span>" + esc(L.assistantDisplayName(assistant)) + "</span>"
      : ""
    const card =
      '<div class="message-card"><div class="message-source"><span>' +
      esc(String(e.source || "session")) +
      "</span>" +
      meta +
      "</div><div>" +
      L.richTextHTML(e.text) +
      "</div></div>"
    return (
      <div className="entry" data-role="message" key={"m:" + at}>
        {whoHTML(v, "message", e.at)}
        <div className="body" onClick={copyFrom} dangerouslySetInnerHTML={{ __html: card }} />
      </div>
    )
  }
  if (role === "peer") {
    const mode = e.sourceMode ? ' title="' + esc(String(e.sourceMode)) + '"' : ""
    const card =
      '<div class="peer-card"><div class="peer-source"' +
      mode +
      ">" +
      esc(String(e.source || "session")) +
      "</div><div>" +
      L.richTextHTML(e.text) +
      "</div></div>"
    return (
      <div className="entry" data-role="peer" key={"m:" + at}>
        {whoHTML(v, "peer", e.at)}
        <div className="body" onClick={copyFrom} dangerouslySetInnerHTML={{ __html: card }} />
      </div>
    )
  }
  const record = role === "user" ? L.parseWorkflowRecord(e.text, role) : null
  let body = (e.tool ? '<span class="toolname">' + esc(e.tool) + "</span>" : "") +
    L.richTextHTML(record ? (record as { text: string }).text : e.text)
  if (record) {
    // The label is the original's own literal; the catalog has no key for it.
    body += L.workflowRecordHTML(record, {
      escape: esc,
      label: /^zh/i.test(document.documentElement.lang || "") ? "看板紀錄" : "Board record",
    })
  }
  return (
    <div className="entry" data-role={role} key={"m:" + at}>
      {whoHTML(v, role, e.at)}
      <div className="body" onClick={copyFrom} dangerouslySetInnerHTML={{ __html: body }} />
    </div>
  )
}

/**
 * `noticeHTML`: a Clawdline notice as a card. Every field is text, and none
 * goes through the Markdown renderer. A notice this page cannot place is drawn
 * as its words.
 */
function noticeHTML(v: View, e: Entry, at: number): ReactElement {
  const n = e.notice
  if (!n || typeof n.kind !== "string" || ((n.kind === "task_finished" || n.kind === "workspace_overlap") && !n.task)) {
    return entryHTML(v, { ...e, role: "assistant", notice: undefined }, at)
  }
  const T = L.strings
  const esc = L.escapeHTML
  const fill = L.fillString
  const task = n.task || { id: "", title: "" }
  const identity = task.title || task.id || T.webNoticeTask
  let title = T.webNoticeFinished
  let tone = "neutral"
  let detail = ""
  const pathList = (paths: string[] | undefined) =>
    '<ul class="notice-overlaps">' +
    (paths || []).map((p) => '<li><code class="notice-path">' + esc(String(p)) + "</code></li>").join("") +
    "</ul>"

  if (n.kind === "task_finished") {
    const states: Record<string, [string, string]> = {
      success: [T.webNoticeCompleted, "success"],
      failure: [T.webNoticeFailed, "failure"],
      timeout: [T.webNoticeTimedOut, "timeout"],
      cancelled: [T.webNoticeCancelled, "neutral"],
      spawn_failed: [T.webNoticeCouldNotStart, "failure"],
    }
    const state = (n.state && states[n.state]) || [T.webNoticeFinished, "neutral"]
    title = state[0]
    tone = state[1]
    detail = '<div class="notice-task">' + esc(identity) + "</div>"
    if (n.result_path) detail += '<code class="notice-path">' + esc(n.result_path) + "</code>"
    // The decoder requires `outstanding` on this kind, so an absent one is 0.
    const outstanding = n.outstanding ?? 0
    if (n.audience === "parent" && Number.isSafeInteger(outstanding)) {
      const siblings =
        outstanding === 0
          ? T.webNoticeNoSiblings
          : fill(outstanding === 1 ? T.webNoticeOneSibling : T.webNoticeManySiblings, { n: outstanding })
      detail += '<div class="notice-meta">' + esc(siblings) + "</div>"
    }
    if (n.claims_released === true && n.child_may_still_write === true) {
      detail += '<div class="notice-warning">' + esc(T.webNoticeClaimsReleased) + "</div>"
    }
  } else if (n.kind === "workspace_overlap") {
    title = T.webNoticeWorkspaceOverlap
    tone = "overlap"
    detail = '<div class="notice-task">' + esc(identity) + "</div>"
    detail +=
      '<ul class="notice-overlaps">' +
      (n.overlaps || [])
        .map((row) => {
          const other = row.task || { id: "", title: "" }
          const name = other.title || other.id || T.webNoticeTask
          return (
            "<li><span>" +
            esc(name) +
            "</span>" +
            (row.path ? '<code class="notice-path">' + esc(row.path) + "</code>" : "") +
            "</li>"
          )
        })
        .join("") +
      "</ul>"
  } else if (n.kind === "file_wait_request") {
    title = T.webNoticeFileWaitRequested
    tone = "overlap"
    detail = '<code class="notice-path">' + esc(String(n.repository || "")) + "</code>" + pathList(n.paths)
    if (n.reason) detail += '<div class="notice-meta">' + esc(n.reason) + "</div>"
    if (n.release_condition) detail += '<div class="notice-warning">' + esc(n.release_condition) + "</div>"
  } else if (n.kind === "file_wait_release") {
    title = T.webNoticeFileWaitReleased
    tone = "success"
    detail = '<code class="notice-path">' + esc(String(n.repository || "")) + "</code>" + pathList(n.paths)
    if (n.commit) detail += '<code class="notice-path">' + esc(n.commit) + "</code>"
    if (n.note) detail += '<div class="notice-meta">' + esc(n.note) + "</div>"
    detail += '<div class="notice-warning">' + esc(T.webNoticeRecheckGit) + "</div>"
  } else if (n.kind === "handoff_receipt") {
    const pickedUp = n.state === "picked_up"
    title = pickedUp ? T.webNoticeHandoffPickedUp : T.webNoticeHandoffNeedsDelivery
    tone = pickedUp ? "success" : "failure"
    detail = '<div class="notice-task">' + esc(String(n.title || n.handoff_id || T.webNoticeTask)) + "</div>"
    if (n.assistant) detail += '<div class="notice-meta">' + esc(n.assistant) + "</div>"
    if (n.project_dir) detail += '<code class="notice-path">' + esc(n.project_dir) + "</code>"
  } else {
    return entryHTML(v, { ...e, role: "assistant", notice: undefined }, at)
  }
  return (
    <div className="entry clawdline-notice" data-role="notice" data-tone={tone} key={"n:" + at}>
      {whoHTML(v, "notice", e.at)}
      <div
        className="body"
        dangerouslySetInnerHTML={{
          __html: '<div class="notice-card"><div class="notice-title">' + esc(title) + "</div>" + detail + "</div>",
        }}
      />
    </div>
  )
}

/* --------------------------------------------------------------------------
   Structured tool output
   ------------------------------------------------------------------------ */

function fileChangesOf(e: Entry): TranscriptFileChange[] | null {
  if (!e || e.role !== "tool" || !Array.isArray(e.fileChanges)) return null
  const changes = e.fileChanges.filter((c) => c && typeof c.path === "string" && c.path.length > 0)
  return changes.length ? changes : null
}

const PLAN_STATUSES: Record<string, true> = { pending: true, inProgress: true, completed: true }

function planOf(e: Entry): TranscriptPlanStep[] | null {
  if (!e || e.role !== "tool" || !Array.isArray(e.plan)) return null
  const steps = e.plan.filter(
    (s) => s && typeof s.step === "string" && s.step.length > 0 && Object.prototype.hasOwnProperty.call(PLAN_STATUSES, s.status),
  )
  return steps.length ? steps : null
}

function activityOf(e: Entry): TranscriptActivity | null {
  if (!e || e.role !== "tool" || !e.activity || typeof e.activity !== "object") return null
  const kind = e.activity.kind
  if (kind !== "called" && kind !== "explored") return null
  if (kind === "explored" && !Array.isArray(e.activity.actions)) return null
  return e.activity
}

/** `planHTML`. */
function planHTML(v: View, e: Entry, at: number) {
  const esc = L.escapeHTML
  const steps = planOf(e) || []
  const card =
    '<section class="plan-card"><header>Updated Plan</header><ol>' +
    steps
      .map((step) => {
        const mark = step.status === "completed" ? "✓" : step.status === "inProgress" ? "●" : "○"
        return (
          '<li data-status="' + esc(step.status) + '"><span class="plan-mark">' + mark + "</span><span>" + esc(step.step) + "</span></li>"
        )
      })
      .join("") +
    "</ol></section>"
  return (
    <div className="entry plan" data-role="plan" key={"plan:" + at}>
      <div className="who">{v.who.tool}</div>
      <div className="body" dangerouslySetInnerHTML={{ __html: card }} />
    </div>
  )
}

/** `durationText`. A negative duration is this wire's "the record did not say". */
function durationText(ms: number | undefined): string {
  if (typeof ms !== "number" || ms < 0 || !isFinite(ms)) return ""
  if (ms < 1000) return ms + "ms"
  const seconds = ms / 1000
  return (seconds < 10 ? String(Math.round(seconds * 100) / 100) : String(Math.round(seconds * 10) / 10)) + "s"
}

function activityActionHTML(action: TranscriptAction): string {
  if (!action || typeof action !== "object") return ""
  const esc = L.escapeHTML
  const label = action.kind === "search" ? "Search" : action.kind === "read" ? "Read" : ""
  if (!label) return ""
  const subject =
    action.kind === "search"
      ? String(action.query || action.command || "")
      : String(action.name || action.path || action.command || "")
  const where = action.path && action.path !== subject ? '<span class="activity-path">' + esc(action.path) + "</span>" : ""
  return (
    '<li><span class="activity-verb">' +
    label +
    '</span><span class="activity-detail"><span class="activity-subject">' +
    esc(subject) +
    "</span>" +
    where +
    "</span></li>"
  )
}

/** Only identical, adjacent rows collapse. */
function activityActionsHTML(actions: TranscriptAction[]): string {
  const rows: string[] = []
  let previous: string | null = null
  for (const action of actions) {
    const row = activityActionHTML(action)
    if (!row || row === previous) continue
    rows.push(row)
    previous = row
  }
  return rows.join("")
}

/** `activityCardHTML`. */
function activityCardHTML(v: View, e: Entry, at: number) {
  const esc = L.escapeHTML
  const activity = activityOf(e) || ({} as Partial<TranscriptActivity>)
  const kind = activity.kind === "explored" ? "explored" : "called"
  const title = kind === "called" ? String(activity.title || e.text || e.tool || "") : ""
  const meta = [activity.status, durationText(activity.durationMs)].filter((value) => !!value)
  const actions = kind === "explored" ? activityActionsHTML(activity.actions || []) : ""
  const result =
    kind === "called" && typeof activity.result === "string" && activity.result.length
      ? '<pre class="activity-result">' + esc(activity.result) + "</pre>"
      : ""
  const card =
    '<section class="activity-card"><header><span class="activity-kind">' +
    (kind === "called" ? "Called" : "Explored") +
    "</span>" +
    (title ? '<span class="activity-title">' + esc(title) + "</span>" : "") +
    '<span class="activity-meta">' +
    esc(meta.join(" · ")) +
    "</span></header>" +
    (actions ? "<ul>" + actions + "</ul>" : "") +
    result +
    "</section>"
  return (
    <div className="entry activity" data-role={kind} key={"act:" + at}>
      <div className="who">{v.who.tool}</div>
      <div className="body" dangerouslySetInnerHTML={{ __html: card }} />
    </div>
  )
}

/** `exploredRunHTML`: read and search commands behind one pill, each a card when opened. */
function exploredRunHTML(v: View, run: Entry[], at: number) {
  const names: string[] = []
  for (const e of run) {
    const count = activityOf(e)?.actions?.length ?? 0
    for (let i = 0; i < count; i++) names.push(e.tool || "shell")
  }
  if (!names.length) for (const e of run) names.push(e.tool || "shell")
  const key = "activity-" + foldKey(run)
  const open = !!v.expanded[key]
  return [foldHTML(v, key, names, open, false, at), ...(open ? run.map((e, n) => activityCardHTML(v, e, at + n)) : [])]
}

function patchPath(path: string | undefined): string {
  return String(path || "").replace(/^\/Users\/[^/]+/, "~")
}

interface DiffLine {
  kind: string
  old: number | ""
  new: number | ""
  text: string
}

/** `unifiedDiffLines`: numbered lines, with only real additions and deletions coloured. */
function unifiedDiffLines(text: string): DiffLine[] {
  const source = String(text || "").split("\n")
  if (source.length && source[source.length - 1] === "") source.pop()
  const out: DiffLine[] = []
  let oldLine: number | null = null
  let newLine: number | null = null
  for (const line of source) {
    const hunk = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/)
    if (hunk) {
      oldLine = Number(hunk[1])
      newLine = Number(hunk[2])
      out.push({ kind: "hunk", old: "", new: "", text: line })
    } else if (
      line.indexOf("--- ") === 0 ||
      line.indexOf("+++ ") === 0 ||
      line.indexOf("diff ") === 0 ||
      line.indexOf("index ") === 0 ||
      line.indexOf("\\ No newline") === 0
    ) {
      out.push({ kind: "meta", old: "", new: "", text: line })
    } else if (line.charAt(0) === "-" && oldLine !== null) {
      out.push({ kind: "del", old: oldLine++, new: "", text: line.slice(1) })
    } else if (line.charAt(0) === "+" && newLine !== null) {
      out.push({ kind: "add", old: "", new: newLine++, text: line.slice(1) })
    } else if (oldLine !== null && newLine !== null) {
      out.push({ kind: "context", old: oldLine++, new: newLine++, text: line.charAt(0) === " " ? line.slice(1) : line })
    } else {
      out.push({ kind: "meta", old: "", new: "", text: line })
    }
  }
  return out
}

/** `contentLines`: an added or deleted file, every line on the side that exists. */
function contentLines(text: string | undefined, kind: string): DiffLine[] {
  const source = String(text == null ? "" : text).split("\n")
  if (source.length && source[source.length - 1] === "") source.pop()
  const side = kind === "delete" ? "del" : "add"
  return source.map((line, i) => ({
    kind: side,
    old: side === "del" ? i + 1 : "",
    new: side === "add" ? i + 1 : "",
    text: line,
  }))
}

function linesOfChange(change: TranscriptFileChange): DiffLine[] {
  if (typeof change.unifiedDiff === "string") return unifiedDiffLines(change.unifiedDiff)
  if (typeof change.content === "string") return contentLines(change.content, change.kind)
  return []
}

function diffLineHTML(line: DiffLine): string {
  const esc = L.escapeHTML
  return (
    '<div class="diff-line ' +
    esc(line.kind) +
    '"><span class="old">' +
    esc(line.old) +
    '</span><span class="new">' +
    esc(line.new) +
    "</span><code>" +
    esc(line.text) +
    "</code></div>"
  )
}

/** Enough rows to show an edit's shape without it taking the whole phone screen. */
const PATCH_DETAIL_LINE_LIMIT = 18

function fileChangeHTML(change: TranscriptFileChange, lines: DiffLine[], showLines: boolean): string {
  const esc = L.escapeHTML
  const additions = lines.filter((l) => l.kind === "add").length
  const deletions = lines.filter((l) => l.kind === "del").length
  const moved = change.movePath ? '<span class="diff-move">→ ' + esc(patchPath(change.movePath)) + "</span>" : ""
  const stats =
    (additions ? '<span class="add">+' + additions + "</span>" : "") +
    (deletions ? '<span class="del">−' + deletions + "</span>" : "")
  return (
    '<section class="diff-file" data-kind="' +
    esc(change.kind || "change") +
    '"><header><span class="diff-path">' +
    esc(patchPath(change.path)) +
    "</span>" +
    moved +
    '<span class="diff-kind">' +
    esc(change.kind || "change") +
    '</span><span class="diff-stats">' +
    stats +
    "</span></header>" +
    (showLines && lines.length
      ? '<div class="diff-scroll"><div class="diff-lines">' + lines.map(diffLineHTML).join("") + "</div></div>"
      : "") +
    "</section>"
  )
}

/** `patchHTML`: an edit, open by default when it is short. */
function patchHTML(v: View, e: Entry, at: number) {
  const views = (fileChangesOf(e) || []).map((change) => ({ change, lines: linesOfChange(change) }))
  const key = "p" + foldKey([e])
  const lineCount = views.reduce((total, x) => total + x.lines.length, 0)
  const defaultOpen = lineCount <= PATCH_DETAIL_LINE_LIMIT
  const open = defaultOpen ? v.expanded[key] !== false : !!v.expanded[key]
  return (
    <div className="entry patch" data-role="patch" data-detail={open ? "open" : "collapsed"} key={"p:" + at}>
      <div className="who">{v.who.tool}</div>
      <div className="body">
        <button
          type="button"
          className="patch-title"
          data-fold={key}
          data-default-open={defaultOpen ? "1" : "0"}
          aria-expanded={open ? "true" : "false"}
          onClick={() => v.toggle(key, defaultOpen)}
        >
          <span className="caret">{open ? "⏷" : "⏵"}</span>
          <span className="toolname">{e.tool}</span>
          <span className="subject">{e.text || ""}</span>
        </button>
        <div
          className="patch-body"
          dangerouslySetInnerHTML={{ __html: views.map((x) => fileChangeHTML(x.change, x.lines, open)).join("") }}
        />
      </div>
    </div>
  )
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
