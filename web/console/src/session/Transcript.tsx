import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type MouseEvent,
  type ReactElement,
} from "react"
import type {
  SessionAgent,
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
import { ArtifactTiles, artifactTilesHTML, artifactsKey } from "../legacy/images-bridge.js"
import { byteWords, nextWord } from "../next-strings.js"
import type { PendingSend } from "./pending.js"
import { pendingFailureCanRetry, pendingFailureSentence } from "./pending-copy.js"
import { sitsUnsubmitted, waitsOnQuestion } from "./outcome.js"
import { INTERRUPTED } from "./persist.js"
import { look, pendingSends, resend } from "./send.js"
import { turnPendingSpinners } from "./spinners.js"
import "./pending.css"
import "./working-line.css"
import { conversationNotStarted } from "./readiness.js"
import { transcriptShow } from "./transcript-trouble.js"
import { agentReportIdentity } from "./agent-report.js"
import "./agent-report.css"

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
 * A provider hand-back adds `role: agent`; its `source` joins the same agent
 * id the work tree shows, rather than being drawn as the person.
 *
 * Pictures are the original's too: an assistant turn or a Clawdline message
 * with `artifacts` carries `artifactTilesHTML`'s static tiles, numbered into one
 * queue in the order they are drawn, and `legacy/images-bridge.ts` connects them
 * after the draw with the original's own `transcript-images.js`.
 *
 * The composer's pending turns are the original's `.entry.pending`, at the
 * newest end, one per message this page sent and has not read back yet
 * (`pending.ts`, which says where they differ from the original's).
 */

/** Same as the original's `limit=200`, so both panes are reading the same stretch. */
const LIMIT = 200
const POLL_MS = 4000
/**
 * While a message is on its way the next read is the one that replaces its
 * card, so it comes sooner (`followPendingTranscript` in the original).
 */
const FOLLOW_MS = 1000

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

export function Transcript({
  id,
  agentId,
  onAgent,
}: {
  id: string
  agentId?: string
  onAgent?: (id: string) => void
}) {
  // Keyed, so a different session starts from nothing: no previous session's
  // turns, error or opened folds are shown under the new name.
  return <TranscriptOf key={`${id}:${agentId ?? ""}`} id={id} agentId={agentId} onAgent={onAgent} />
}

function TranscriptOf({ id, agentId, onAgent }: { id: string; agentId?: string; onAgent?: (id: string) => void }) {
  const read = useMemo(
    () => () => agentId ? client.agentTranscript(id, agentId, LIMIT) : client.transcript(id, LIMIT),
    [id, agentId],
  )
  useSyncExternalStore(pendingSends.subscribe, pendingSends.getVersion)
  const cards = agentId ? [] : pendingSends.of(id)
  const following = cards.some((card) => card.state !== "failed")
  const poll = usePoll<TranscriptPage>(read, following ? FOLLOW_MS : POLL_MS)
  const { data, error } = poll
  // A read nobody answered waits out its quiet stretch before it is news
  // (`transcript-trouble.ts`); until then the skeleton, or the entries already
  // read, stay as they are.
  const show = transcriptShow({
    hasData: !!data,
    failureKind: poll.failureKind,
    failures: poll.failures,
    failingForMs: poll.failingSince === null ? 0 : Date.now() - poll.failingSince,
    noRecord: data?.evidence === "none" && !!data.note,
  })
  // Each read settles the cards it confirms before it is painted, so the turn
  // and the card standing for it are never on screen together.
  useLayoutEffect(() => {
    if (data && !agentId) pendingSends.reconcile(id, data.entries, Date.now())
  }, [data, id, agentId])
  // The cards' spinners turn on the page's one clock, drawn once now so they
  // have their size before the clock's next tick.
  useLayoutEffect(turnPendingSpinners)
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const session = useSession(id)
  // The list row's spinner and live line, repeated under the conversation's
  // newest end, where the reader's eye already is. Only the session's own
  // transcript: a provider subagent's page is not what the row's state is about.
  const working = !agentId && !!session && L.workState(session).state === "working"
  const entries = useMemo<Entry[]>(() => (data ? data.entries.map((e) => ({ ...e })) : []), [data])
  const skeleton = useWait(show === "loading")
  // `S.newestFirst` and `S.assistantIcons`, read on every draw as the original
  // reads them, and a draw of their own when either changes.
  const newestFirst = useSyncExternalStore(L.subscribeSettings, L.settingsNewestFirst)
  const icons = useSyncExternalStore(L.subscribeSettings, L.settingsAssistantIcons)

  // `artifactRenderQueue`: every picture this draw shows, in the order it is
  // drawn, rebuilt on every draw. The tiles are connected after the draw.
  const tiles = useRef<ArtifactTiles | null>(null)
  tiles.current ??= new ArtifactTiles()
  const queue = useRef<NonNullable<TranscriptEntry["artifacts"]>>([])
  queue.current = []
  useEffect(() => {
    tiles.current?.settle(document.getElementById("tx"), queue.current, id)
  })
  useEffect(() => () => tiles.current?.release(), [])

  // `settleTranscript`: a read whose signature is new is drawn, and if the
  // reader was at the bottom before it was drawn they are put back there. A
  // reader who scrolled up stays where they are. A fresh session starts empty,
  // which is the bottom, so it opens at the bottom — its newest entry, or its
  // oldest when it reads newest first, as there.
  //
  // "Where they are" is the number: the original replaces every node, which
  // leaves the browser's scroll anchoring nothing to hold, so a turn arriving
  // above the reader pushes what they were reading down. React keeps the
  // nodes and the browser would hold them still, so the number is put back.
  // A card arriving, changing or going is a new draw too: a reader at the
  // bottom when they pressed Send stays there to watch it.
  const pendingSignature = cards.map((card) => card.token + ":" + card.state).join(",")
  const drawnSignature =
    skeleton || !data ? undefined : data.signature ? data.signature + "|" + pendingSignature + (working ? "|working" : "") : null
  const shownSignature = useRef<string | null | undefined>(undefined)
  const stick = useRef(false)
  const held = useRef(0)
  if (drawnSignature !== undefined && (drawnSignature === null || drawnSignature !== shownSignature.current)) {
    stick.current = atBottom()
    held.current = scrollTop()
  }
  useLayoutEffect(() => {
    if (drawnSignature === undefined) return
    if (drawnSignature !== null && drawnSignature === shownSignature.current) return
    shownSignature.current = drawnSignature
    if (stick.current) toBottom()
    else toScrollTop(held.current)
  }, [data, drawnSignature])

  // `toggleOrder`: the transcript is drawn the other way round and goes back to
  // its top — whoever turned it over, the settings row or `r`.
  const drawnOrder = useRef(newestFirst)
  useLayoutEffect(() => {
    if (drawnOrder.current === newestFirst) return
    drawnOrder.current = newestFirst
    toScrollTop(0)
  }, [newestFirst])

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
  // Newest end: the bottom, or the top when the transcript reads newest first.
  const cardsDrawn = (newestFirst ? [...cards].reverse() : cards).map(pendingHTML)
  const live = working ? <WorkingLine key="working" line={session?.line ?? ""} /> : null
  // The working line is newer than any card: the turn it stands for is the
  // one answering them.
  const pending = newestFirst ? [live, ...cardsDrawn] : [...cardsDrawn, live]
  if (show === "loading") return cardsDrawn.length || live ? <>{pending}</> : null
  // The daemon's note is diagnostic English. It is useful evidence in the
  // disclosure below, but never the main sentence in a translated interface.
  // `no_record` is not a failure at all: the provider has not created its
  // first conversation record yet.
  const technical = show !== "failure" ? "" : poll.failureKind ? String(error) : data?.note || ""
  const notStarted = conversationNotStarted(session)
  if (notStarted && !entries.length) {
    return (
      <>
        <div className="tx-note">{nextWord("sessionNotStarted")}</div>
        {technicalDetails(technical)}
        {pending}
      </>
    )
  }
  const failed = show === "failure" ? readFailed(poll.reading, poll.retry) : null
  if (failed && !entries.length) {
    return (
      <>
        {failed}
        {technicalDetails(technical)}
        {pending}
      </>
    )
  }
  const notice = failed ? (
    <>
      {failed}
      {technicalDetails(technical)}
    </>
  ) : null
  if (!entries.length) {
    // A window that ran out before reaching a single entry is not a
    // conversation with nothing in it.
    return (
      <>
        {cutNote(data)}
        <div className="tx-note">{agentId ? T.agentEmpty : T.noOutput}</div>
        {pending}
      </>
    )
  }

  const who: Record<string, string> = {
    user: T.webWhoYou,
    assistant: L.assistantDisplayName(session?.assistant),
    agent: T.webAgents,
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
  const blocks = blocksOf(entries.filter(worthDrawing), session?.state === "working")
  // Slots are handed out in the order the blocks are drawn, which is the
  // original's: it reverses the blocks before rendering them.
  const slots = new Map<Entry, number>()
  for (const block of newestFirst ? [...blocks].reverse() : blocks) {
    const e = block.rows[0]
    if (block.kind !== "entry" || !showsPictures(who, e)) continue
    slots.set(e, queue.current.length)
    queue.current.push(...(e.artifacts ?? []))
  }
  const view: View = {
    who,
    expanded,
    toggle,
    assistant: session?.assistant,
    agents: session?.agents ?? [],
    onAgent,
    icons,
    slots,
  }

  let at = 0
  const drawn = blocks.map((block) => {
    const start = at
    at += block.rows.length
    if (block.kind === "run") return runHTML(view, block.rows, block.live, start)
    if (block.kind === "explored") return exploredRunHTML(view, block.rows, start)
    if (block.kind === "ask") return [askHTML(view, block.rows[0], start)]
    return [entryHTML(view, block.rows[0], start)]
  })
  // Reversed a block at a time, so a run of calls stays one thing in its own
  // order whichever way round the transcript is read. Keys are counted from
  // the oldest entry either way, so turning it over moves rows, not rebuilds them.
  if (newestFirst) drawn.reverse()
  // The oldest end says when there is more before it (limits N17). The Swift
  // app's page draws nothing here and reads as if the conversation began at
  // its first entry; this is a deliberate difference, in the copied note's
  // class, at whichever end is the oldest.
  const cut = cutNote(data)
  return (
    <>
      {notice}
      {newestFirst && pending}
      {cut && !newestFirst && cut}
      {drawn.flat()}
      {cut && newestFirst && cut}
      {!newestFirst && pending}
    </>
  )
}

/**
 * The row's working state, drawn where the conversation ends: the same pixel
 * spinner on the page's one clock (`turnPendingSpinners` registers it) and the
 * same live line the list row carries, so a reader inside the session does not
 * have to look back at the list to know a turn is still going. The line falls
 * back to the state's own word when the provider has said nothing yet.
 */
function WorkingLine({ line }: { line: string }) {
  return (
    <div className="tx-working" role="status">
      <canvas className="spin"></canvas>
      <span className="line">{line || L.strings.webStateWorking}</span>
    </div>
  )
}

/**
 * `webTranscriptFailed`, and beside it the way to ask again now rather than at
 * the next poll. While that read is out the button says so and waits; the
 * sentence stays until an answer replaces it.
 */
function readFailed(reading: boolean, retry: () => void): ReactElement {
  return (
    <div className="tx-note err tx-failed" role="alert">
      <span>{L.strings.webTranscriptFailed}</span>
      <button type="button" className="go" disabled={reading} onClick={retry}>
        {nextWord(reading ? "transcriptRetrying" : "transcriptRetry")}
      </button>
    </div>
  )
}

/** Internal producer text stays available without becoming the screen's claim. */
function technicalDetails(detail: string): ReactElement | null {
  if (!detail) return null
  return (
    <details className="tx-note">
      <summary>{nextWord("transcriptTechnicalDetails")}</summary>
      <code>{detail}</code>
    </details>
  )
}

/**
 * `.entry.pending`, as `view/transcript.js` draws a turn the Mac has not
 * written yet: the words, the pictures counted, and one line saying where the
 * send has got to. A send that failed says so on that line, in the catalog's
 * words, beside "try again" and a close button, and keeps its words. A send
 * that may have gone — nothing answered, or the answer was lost — says it does
 * not know, beside "look" (F3).
 */
function pendingHTML(card: PendingSend): ReactElement {
  const T = L.strings
  const esc = L.escapeHTML
  const n = card.pictures.length
  let body = L.richTextHTML(card.text)
  if (n) {
    body +=
      '<div class="pending-images">' +
      esc(L.fillString(n === 1 ? T.webAttachedImage : T.webAttachedImages, { n })) +
      "</div>"
  }
  const close = esc(T.webClose)
  const dismiss =
    '<button type="button" class="dismiss" data-pending-dismiss="' +
    esc(card.token) +
    '" aria-label="' +
    close +
    '" title="' +
    close +
    '">×</button>'
  // A card put back from this browser's store without its pictures (F4). The
  // words are here to read; sending them again is not offered, because the
  // pictures cannot go with them and the same words under a new request would
  // be the message twice.
  const partial = card.partial ? '<span class="pending-partial">' + esc(nextWord("sendKeptWords")) + "</span>" : ""
  if (card.state === "failed") {
    body +=
      '<div class="pending-state" role="alert"><span>' +
      // A dialog on the session's screen refused the words before a byte was
      // typed: the card names the question as what to deal with, and "try
      // again" sends the same words once it is answered.
      esc(
        waitsOnQuestion(card.failure)
          ? nextWord("sendAsking", { code: card.failure })
          : pendingFailureSentence(card.failure),
      ) +
      "</span>" +
      (card.partial
        ? partial
        : pendingFailureCanRetry(card.failure)
          ? '<button type="button" class="go" data-pending-retry="' + esc(card.token) + '">' + esc(T.webPlanRetry) + "</button>"
          : "") +
      dismiss +
      "</div>"
  } else if (card.state === "unknown" && sitsUnsubmitted(card.failure)) {
    // The Mac typed the words and held Enter back: they are in the session's
    // input line. Neither a look nor "send again" helps — the transcript has
    // no turn for them, and a second send types them twice — so the card says
    // where they are and offers only to close it.
    body +=
      '<div class="pending-state" role="alert"><span>' +
      esc(nextWord("sendUnsubmitted", { code: card.failure })) +
      "</span>" +
      partial +
      dismiss +
      "</div>"
  } else if (card.state === "unknown" && card.checking) {
    body +=
      '<div class="pending-state" role="status"><canvas class="spin"></canvas><span>' +
      esc(nextWord("sendLooking")) +
      "</span></div>"
  } else if (card.state === "unknown") {
    // F3: the words may be on the Mac. The card says it does not know and
    // offers a look, not "try again"; only a look that read the transcript
    // and found no turn offers sending — under the card's one request, which
    // the Mac answers with the first attempt's answer if that one landed.
    //
    // A card the page was still sending when it was reloaded says that
    // (`sendInterrupted`): the request went with the page, so nothing answered
    // and nothing here knows. Looking is the same press as for any other
    // unknown card.
    const said = card.absent ? "sendAbsent" : card.failure === INTERRUPTED ? "sendInterrupted" : "sendUnknown"
    body +=
      '<div class="pending-state" role="alert"><span>' +
      esc(nextWord(said, { code: card.failure })) +
      "</span>" +
      partial +
      (card.absent && !card.partial
        ? '<button type="button" class="go" data-pending-retry="' +
          esc(card.token) +
          '" title="' +
          esc(nextWord("sendAgainTip")) +
          '">' +
          esc(nextWord("sendAgain")) +
          "</button>"
        : '<button type="button" class="go" data-pending-look="' +
          esc(card.token) +
          '" title="' +
          esc(nextWord("sendLookTip")) +
          '">' +
          esc(nextWord("sendLook")) +
          "</button>") +
      dismiss +
      "</div>"
  } else {
    body +=
      '<div class="pending-state" role="status"><canvas class="spin"></canvas><span>' +
      esc(card.state === "accepted" ? T.webPromptAccepted : T.webSending) +
      "</span></div>"
  }
  const at = Math.floor(card.sentAt / 1000)
  return (
    <div className="entry pending" data-role="user" data-send={card.state} key={"pending:" + card.token}>
      <div className="who">
        <span className="speaker">{T.webWhoYou}</span>
        <time data-at={at}>{L.clock(at)}</time>
      </div>
      <div className="body" onClick={pendingAction} dangerouslySetInnerHTML={{ __html: body }} />
    </div>
  )
}

/** A press inside a pending card: try again, look, close it, or a code block's copy button. */
function pendingAction(ev: MouseEvent<HTMLElement>) {
  const target = ev.target as Element
  const retry = target.closest?.("[data-pending-retry]")
  if (retry) {
    void resend(retry.getAttribute("data-pending-retry") ?? "")
    return
  }
  const looked = target.closest?.("[data-pending-look]")
  if (looked) {
    void look(looked.getAttribute("data-pending-look") ?? "")
    return
  }
  const close = target.closest?.("[data-pending-dismiss]")
  if (close) {
    pendingSends.dismiss(close.getAttribute("data-pending-dismiss") ?? "")
    return
  }
  copyFrom(ev)
}

/**
 * What the page says about the conversation before its first entry: that the
 * read window ran out before the page was full (`unread`, this daemon's own),
 * or that older entries did not fit in one page's bytes (`truncation`, the
 * Swift app's key, which its page never drew). Null when the page begins where
 * the conversation does.
 */
function cutNote(page: TranscriptPage | null | undefined): ReactElement | null {
  if (!page) return null
  const said: string[] = []
  if (page.unread && page.unread.bytes > 0) {
    said.push(nextWord("olderNotRead", { window: byteWords(page.unread.windowBytes), bytes: byteWords(page.unread.bytes) }))
  }
  if (page.truncation && page.truncation.entriesOmittedCount > 0) {
    said.push(nextWord("olderLeftOut", {
      count: page.truncation.entriesOmittedCount,
      budget: byteWords(page.truncation.budgetBytes),
    }))
  }
  if (!said.length) return null
  return (
    <div key="tx-cut" className="tx-note" data-cut={page.unread ? "unread" : "truncation"} role="note">
      {said.join(" ")}
    </div>
  )
}

interface View {
  who: Record<string, string>
  expanded: Record<string, boolean>
  toggle: Toggle
  assistant: string | undefined
  agents: SessionAgent[]
  onAgent: ((id: string) => void) | undefined
  /** `S.assistantIcons` as this draw read it. */
  icons: boolean
  /** Where each entry's pictures start in this draw's queue. */
  slots: Map<Entry, number>
}

/** Whether `entryHTML` draws this entry's pictures: an assistant turn or a Clawdline message that has some. */
function showsPictures(who: Record<string, string>, e: Entry): boolean {
  if (!e.artifacts?.length || fileChangesOf(e) || planOf(e) || activityOf(e) || e.role === "notice") return false
  const role = who[e.role] ? e.role : "assistant"
  return role === "message" || role === "assistant"
}

/** This entry's tiles, or nothing. */
function tilesHTML(v: View, e: Entry): string {
  const first = v.slots.get(e)
  return first === undefined ? "" : artifactTilesHTML(e.artifacts, first)
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

function scrollTop(): number {
  return document.getElementById("tx-scroll")?.scrollTop ?? 0
}

function toScrollTop(top: number): void {
  const el = document.getElementById("tx-scroll")
  if (el) el.scrollTop = top
}

/**
 * `toggleOrder` (`input/keys.js`), for `r`: the transcript turns over and goes
 * back to its top. The settings row does the same through its own setter; the
 * transcript follows the value, not the press.
 */
export function toggleOrder(): void {
  L.setSettingsNewestFirst(!L.settingsNewestFirst())
  toScrollTop(0)
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
  const mark = role === "assistant" && v.icons ? L.assistantLogoHTML(v.assistant) : ""
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
      "</div>" +
      tilesHTML(v, e) +
      "</div>"
    return (
      <div className="entry" data-role="message" key={"m:" + at}>
        {whoHTML(v, "message", e.at)}
        <div
          className="body"
          key={artifactsKey(e.artifacts)}
          onClick={copyFrom}
          dangerouslySetInnerHTML={{ __html: card }}
        />
      </div>
    )
  }
  if (role === "agent") {
    const identity = agentReportIdentity(e.source, v.agents, v.assistant, document.documentElement.lang || "")
    const source = identity.known ? (
      <>
        <button
          type="button"
          title={identity.id}
          onClick={(event) => {
            event.stopPropagation()
            if (identity.id) v.onAgent?.(identity.id)
          }}
        >
          {identity.label}
        </button>
        <span className="agent-report-id">{identity.id}</span>
      </>
    ) : <span className="agent-report-unknown">{identity.detail}</span>
    return (
      <div className="entry" data-role="agent" key={"m:" + at}>
        {whoHTML(v, "agent", e.at)}
        <div className="body agent-report-card" onClick={copyFrom}>
          <div className="agent-report-source">{source}</div>
          <div dangerouslySetInnerHTML={{ __html: L.richTextHTML(e.text) }} />
        </div>
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
  // An assistant turn carries pictures when it wrote an image marker into its
  // own reply; the tiles are the same field-free markup the message card uses.
  if (role === "assistant") body += tilesHTML(v, e)
  return (
    <div className="entry" data-role={role} key={"m:" + at}>
      {whoHTML(v, role, e.at)}
      <div
        className="body"
        key={role === "assistant" ? artifactsKey(e.artifacts) : undefined}
        onClick={copyFrom}
        dangerouslySetInnerHTML={{ __html: body }}
      />
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
