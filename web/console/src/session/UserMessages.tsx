import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent,
  type ReactElement,
} from "react"
import { createPortal } from "react-dom"
import type { SessionRow, TranscriptEntry, TranscriptPage } from "@clawdline/contract"
import { client } from "../client.js"
import { usePoll } from "../useFleet.js"
import * as L from "../legacy/bridge.js"
import {
  OPEN_USER_MESSAGES,
  filterUserMessages,
  userMessageEntries,
  userMessagePosition,
  userMessagesCopy,
} from "../legacy/user-messages-bridge.js"

/**
 * "My messages": every turn the person wrote in this session, newest first, and
 * a way back to the one they pick.
 *
 * The original is `input/user-messages.js`, which builds its own small DOM
 * island on the body and reuses two things the page already has: the transcript
 * entry renderer and the `.overlay`/`.sheet` furniture. The markup below is
 * that module's, string for string — the sheet, its head, the search field, the
 * list and the one chip — and `legacy/user-messages.css` is the copied
 * stylesheet that names it.
 *
 * Its three data functions are the copied `view/user-messages-data.js`, which
 * this repository already had: `userMessageEntries`, `filterUserMessages` and
 * `userMessagePosition`. Nothing about which turns are shown, in what order, or
 * which row one of them is in the transcript is decided here.
 *
 * Two deliberate differences from the original, both because this console holds
 * its transcript differently:
 *
 *   - The original reads `S.tx.entries`, the copy the transcript pane is
 *     already drawing, and redraws on `clawdline:rendered`. This console's
 *     transcript keeps its entries in its own component, so the sheet reads the
 *     same route while it is open and stops when it closes. The turns are the
 *     same turns; what is different is that an open sheet costs one more poll.
 *   - `Optimistic.entries` is empty here, because this console has no
 *     optimistic composer entry yet (see `session/Transcript.tsx`). The pending
 *     half of all three functions is passed as the empty list rather than
 *     dropped, so it starts working the moment there is one.
 */
export function UserMessages({ row }: { row: SessionRow | null }) {
  const [open, setOpen] = useState(false)

  // The row the sheet was opened on. A different session, or none, closes it —
  // `clawdline:rendered`'s check that the open selection is still the one this
  // sheet belongs to.
  const openedOn = useRef<string | null>(null)
  useEffect(() => {
    if (!open) return
    if (!row || row.id !== openedOn.current) setOpen(false)
  }, [open, row])

  useEffect(() => {
    const ask = () => {
      if (!row) return
      openedOn.current = row.id
      setOpen(true)
    }
    document.addEventListener(OPEN_USER_MESSAGES, ask)
    return () => document.removeEventListener(OPEN_USER_MESSAGES, ask)
  }, [row])

  return createPortal(
    <div className="overlay" id="user-messages" hidden={!open} onClick={() => setOpen(false)}>
      {open && row ? <Sheet row={row} onClose={() => setOpen(false)} /> : null}
    </div>,
    document.body,
  )
}

/** Same as the transcript pane's, so both panes read the same stretch. */
const LIMIT = 200
const POLL_MS = 4000

function Sheet({ row, onClose }: { row: SessionRow; onClose: () => void }) {
  const words = userMessagesCopy(document.documentElement.lang || "")
  const T = L.strings
  // The field is the original's: a plain input whose value is read when it
  // changes, not one React writes back. A controlled input is drawn with a
  // `value` attribute the original's markup does not have, which a node-by-node
  // comparison of the two sheets sees; and there is nothing to control, because
  // the sheet is built when it opens and `open()` there clears the field for
  // the same reason.
  const [query, setQuery] = useState("")
  const search = useRef<HTMLInputElement>(null)
  const list = useRef<HTMLDivElement>(null)
  const read = useMemo(() => () => client.transcript(row.id, LIMIT), [row.id])
  const { data, pending } = usePoll<TranscriptPage>(read, POLL_MS)
  const entries = useMemo(() => (data ? data.entries : []), [data])
  const newestFirst = L.settingsNewestFirst()

  // `open()`: the field takes the keyboard, without the page jumping to it.
  useEffect(() => {
    search.current?.focus({ preventScroll: true })
  }, [])

  // `draw()` puts the list back at its top; it is a new list each time the
  // query changes, not a scrolled one.
  useEffect(() => {
    if (list.current) list.current.scrollTop = 0
  }, [query])

  const all = userMessageEntries(entries, [])
  const shown = filterUserMessages(all, query)
  // "You have not sent a message in this session yet" is a statement, and it is
  // not true while the first read is still on its way. The original never has
  // this moment — it opens on the copy the transcript pane already drew — so
  // the sentence waits for an answer rather than filling the gap this one read
  // makes.
  const waiting = pending && !data

  const jumpTo = (entry: TranscriptEntry) => {
    const position = userMessagePosition(entries, [], entry, newestFirst)
    onClose()
    if (position < 0) return
    const rows = document.querySelectorAll<HTMLElement>('#tx .entry[data-role="user"]')
    const target = rows[position]
    if (!target) return
    target.scrollIntoView({ block: "center", behavior: reduced() ? "auto" : "smooth" })
    const previous = document.querySelector("#tx .user-message-target")
    if (previous) previous.classList.remove("user-message-target")
    // Restart the small locator pulse when the same row is chosen twice.
    void target.offsetWidth
    target.classList.add("user-message-target")
    window.clearTimeout(pulseTimer)
    pulseTimer = window.setTimeout(() => target.classList.remove("user-message-target"), 1800)
  }

  const picked = (ev: MouseEvent<HTMLElement> | ReactKeyboardEvent<HTMLElement>) => {
    const node = (ev.target as Element).closest?.('.entry[data-role="user"]') as HTMLElement | null
    if (!node || !list.current?.contains(node)) return null
    return node
  }

  return (
    <div
      className="sheet user-messages-sheet"
      id="user-messages-sheet"
      role="dialog"
      aria-modal="true"
      aria-labelledby="user-messages-title"
      onClick={(ev) => ev.stopPropagation()}
      onKeyDown={(ev) => {
        if (ev.key !== "Escape") return
        ev.preventDefault()
        ev.stopPropagation()
        onClose()
      }}
    >
      <div className="user-messages-head">
        <h2 id="user-messages-title">{words.title}</h2>
        <input
          className="user-messages-search"
          id="user-messages-search"
          type="search"
          autoComplete="off"
          enterKeyHint="search"
          ref={search}
          placeholder={words.search}
          aria-label={words.search}
          onChange={(ev) => setQuery(ev.target.value)}
        />
      </div>
      <div
        className="user-message-list"
        id="user-message-list"
        ref={list}
        onClick={(ev) => {
          // Markdown links keep doing what they say. A tap anywhere else on the
          // message returns to it.
          if ((ev.target as Element).closest?.("a")) return
          const node = picked(ev)
          if (!node) return
          jumpTo(shown[[...(list.current?.children ?? [])].indexOf(node)])
        }}
        onKeyDown={(ev) => {
          if (ev.key !== "Enter" && ev.key !== " ") return
          const node = picked(ev)
          if (!node || ev.target !== node) return
          ev.preventDefault()
          jumpTo(shown[[...(list.current?.children ?? [])].indexOf(node)])
        }}
      >
        {shown.length ? (
          shown.map((entry, n) => <UserEntry key={n} entry={entry} you={T.webWhoYou} />)
        ) : waiting ? null : (
          <p className="user-messages-empty">{all.length ? words.noMatches : words.empty}</p>
        )}
      </div>
      <div className="buttons">
        <button className="chip" id="user-messages-close" type="button" onClick={onClose}>
          {T.webClose}
        </button>
      </div>
    </div>
  )
}

/**
 * One turn, as `entryHTML` draws a `user` row: the speaker and its time, then
 * the Markdown body and a Board record when the text is one.
 *
 * It is restated here rather than imported because `session/Transcript.tsx`
 * keeps its `entryHTML` to itself and this task does not change that file. The
 * three parts are its user branch line for line, so both panes put the same
 * classes in front of the same stylesheet — which is what
 * `.user-message-list .entry[data-role="user"]` is written against.
 */
function UserEntry({ entry, you }: { entry: TranscriptEntry; you: string }): ReactElement {
  const esc = L.escapeHTML
  const record = L.parseWorkflowRecord(entry.text, "user")
  let body = L.richTextHTML(record ? (record as { text: string }).text : entry.text)
  if (record) {
    // The label is the original's own literal; the catalog has no key for it.
    body += L.workflowRecordHTML(record, {
      escape: esc,
      label: /^zh/i.test(document.documentElement.lang || "") ? "看板紀錄" : "Board record",
    })
  }
  return (
    <div className="entry" data-role="user" tabIndex={0} role="button">
      <div className="who">
        <span className="speaker" dangerouslySetInnerHTML={{ __html: esc(you) }} />
        {entry.at ? <time data-at={entry.at}>{L.clock(entry.at)}</time> : null}
      </div>
      <div className="body" dangerouslySetInnerHTML={{ __html: body }} />
    </div>
  )
}

/** `reduced` (`core/env.js`), asked each time as the page asks it. */
function reduced(): boolean {
  return !!window.matchMedia?.("(prefers-reduced-motion: reduce)").matches
}

let pulseTimer = 0
