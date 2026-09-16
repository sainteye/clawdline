import type { Icon, SessionRow } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"
import { Transcript } from "./Transcript.js"
import { Composer } from "./Composer.js"
import { StatusLine } from "./StatusLine.js"

/**
 * Whether this transport can read a project's snippets — `snippetControls(api).read`
 * in `view/snippets-data.js`. This daemon has no snippets route, so the answer is
 * no, and the header and the `⋯` row both take the original's "no route" shape
 * from this one answer, as they take it from one function there: the mark is an
 * inert picture named after its folder, and the menu row is hidden and disabled.
 */
const SNIPPETS_READABLE = false

/**
 * The conversation, which is what this pane is for.
 *
 * In the original the transcript is the pane and the composer is under it, so a
 * person reads and answers without changing what they are looking at. That is
 * the arrangement being replicated, not a list with a detail popover.
 *
 * The header is `renderDetailHead` (`view/transcript.js`) and the markup is
 * `index.html`'s, element for element: the identity block is two buttons, the
 * mark (snippets) and the name (session info).
 *
 * `listUnknown` is the original's `listUnknown()` (`view/waits.js`): true while
 * the first list is still on its way, when the header says nothing rather than
 * "no session open". The list's loading state is not this file's to read, so the
 * page passes it; left out, the header behaves as if the list had arrived.
 */
export function Detail({
  row,
  onBack,
  onDid,
  listUnknown = false,
}: {
  row: SessionRow | null
  onBack: () => void
  onDid: () => void
  listUnknown?: boolean
}) {
  const T = L.strings
  // Which session is being closed, as `closingSelectionKey()` is there: the
  // header is "ending" only while the session it shows is the one going away.
  const [endingId, setEndingId] = useState<string | null>(null)
  const ending = !!row && endingId === row.id

  // No snippets route means no resolved project (`snippetProjectFor`), so the
  // mark is keyed by the session's own `cwd`.
  const mark = useMemo(() => markForSession(row), [row?.icon, row?.cwd])
  const markRef = useRef<HTMLCanvasElement>(null)
  const [drew, setDrew] = useState(false)
  useLayoutEffect(() => {
    setDrew(L.paintIcon(markRef.current, mark ?? undefined, 5))
  }, [mark])

  const snippetsSays = SNIPPETS_READABLE ? T.webSnippets : row ? projectLabel(row.cwd) : ""

  return (
    <section className="pane pane-detail" id="pane-detail">
      <div className="detail-head" id="detail-head" data-closing={ending ? "on" : "off"}>
        <button className="back" id="back" onClick={onBack} aria-label={T.webBackLabel} disabled={ending}>
          ‹ {T.webBack}
        </button>
        <div className="detail-identity-block">
          <button
            className="detail-mark-go"
            id="detail-snippets"
            type="button"
            aria-haspopup={SNIPPETS_READABLE ? "dialog" : undefined}
            data-mark={drew && mark ? (mark.generated ? "generated" : "registry") : "none"}
            hidden={!row}
            disabled={!row || ending || !SNIPPETS_READABLE}
            data-plain={SNIPPETS_READABLE ? "off" : "on"}
            title={snippetsSays}
            aria-label={snippetsSays || undefined}
          >
            <span className="detail-identity">
              {/* `coordinatorForSession` — this daemon knows no coordinator, so the crown stays hidden. */}
              <span className="clawdfather-crown" id="detail-clawdfather-crown" role="img" aria-label="Clawdfather" hidden />
              <canvas id="detail-mark" ref={markRef} width={0} height={0} />
            </span>
          </button>
          {/* Opens the session info sheet in the original. That sheet (`#info`) and
              its route are not in this console, so a press opens nothing; the button
              stays enabled because disabling it dims the whole title. */}
          <button
            className="detail-session"
            id="detail-info"
            type="button"
            aria-haspopup="dialog"
            disabled={!row || ending}
            title={T.webSessionInfo}
            aria-label={T.webSessionInfo}
          >
            <span className="who detail-who">
              <span className="name" id="detail-name" style={mark ? { color: L.accentTint(mark.accent) } : undefined}>
                {row ? row.label || row.tty || row.id : listUnknown ? "" : T.webNoSessionOpen}
              </span>
              <span className="sub" id="detail-sub">
                {detailSub(row)}
              </span>
            </span>
          </button>
        </div>
        <div className="tools">
          <Tools row={row} ending={ending} setEndingId={setEndingId} onDid={onDid} />
        </div>
      </div>

      <div className="scroller tx-scroll" id="tx-scroll">
        <div className="tx" id="tx">
          {row ? <Transcript id={row.id} /> : null}
        </div>
      </div>

      <Composer row={row} onDid={onDid} />
      <StatusLine row={row} />
    </section>
  )
}

/**
 * The line under the name, as `renderDetailHead` builds it: path, tty, and a
 * word for the three states a reader should not mistake for idle. No pane id.
 *
 * The original then adds the task that opened this session — its title, its
 * word, tokens and cost — from `taskOfChild` (`view/derive.js`). This console
 * never fills `S.tasks` (`bridge.publish` leaves it empty), so that lookup
 * answers nothing here and the part is absent, as it is there for a session no
 * task opened.
 */
function detailSub(row: SessionRow | null): string {
  if (!row) return ""
  const T = L.strings
  const sub: string[] = [L.path(row.cwd)]
  if (row.tty) sub.push(row.tty)
  if (row.state === "waiting") sub.push(T.sessionWaiting)
  else if (row.state === "working") sub.push(T.webStateWorking)
  else if (row.state === "unknown") sub.push(T.webStateUnreadable)
  return sub.join("  ·  ")
}

/**
 * The detail pane's controls, as the original has them.
 *
 * One chip and the `⋯` menu, and no more: the original has no interrupt button
 * here, so neither does this. The daemon can interrupt and the dashboard offers
 * it, but adding a control the screen being replicated does not have would make
 * this a different screen with the same paint.
 *
 * The menu is `index.html`'s two levels plus the two rows `user-messages.js` and
 * `snippets.js` insert before Git, switched the way `SessionActions.level` does
 * it. Rows whose destination this console does not have are disabled rather than
 * dropped: a menu that is missing rows is a menu somebody will assume they
 * imagined, and a disabled row says which part is not here.
 *
 * - Show on Mac, Session info, Live screen, Documents, Git changes: no route or
 *   no sheet in this console.
 * - My messages: needs no route, but its sheet (`#user-messages`) is not here.
 * - Snippets: hidden and disabled, which is the original's own shape for a
 *   transport with no snippets route (`syncRow` in `input/snippets.js`).
 * - commit, push: the daemon can send them, but the original sends them only
 *   through the `#action-confirm` sheet, which is not here, and sending without
 *   that step would be a weaker guard than the screen being replicated.
 * - Close session keeps the two-step confirmation this menu already had.
 */
function Tools({
  row,
  ending,
  setEndingId,
  onDid,
}: {
  row: SessionRow | null
  ending: boolean
  setEndingId: (id: string | null) => void
  onDid: () => void
}) {
  const T = L.strings
  const [open, setOpen] = useState(false)
  const [git, setGit] = useState(false)
  // `level()` first runs when the menu first opens; until then the main level
  // carries no aria-hidden, as the static markup has none.
  const [leveled, setLeveled] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const [said, setSaid] = useState<string | null>(null)

  const triggerRef = useRef<HTMLButtonElement>(null)
  const mainRef = useRef<HTMLDivElement>(null)
  const gitRef = useRef<HTMLDivElement>(null)
  const gitMoreRef = useRef<HTMLButtonElement>(null)
  // Focus moves after the level has re-rendered, since `inert` has to be gone first.
  const focusNext = useRef<"first" | "git-more" | "trigger" | null>(null)

  const items = () => {
    const level = git ? gitRef.current : mainRef.current
    return level ? [...level.querySelectorAll<HTMLButtonElement>("button:not(:disabled)")] : []
  }

  useEffect(() => {
    const want = focusNext.current
    if (!want) return
    focusNext.current = null
    const target =
      want === "first" ? items()[0] : want === "git-more" ? gitMoreRef.current : triggerRef.current
    target?.focus({ preventScroll: true })
  })

  const openMenu = () => {
    if (!row) return
    setGit(false)
    setLeveled(true)
    setOpen(true)
  }
  const closeMenu = (restore: boolean) => {
    if (!open) return
    setOpen(false)
    setGit(false)
    setConfirm(false)
    if (restore) focusNext.current = "trigger"
  }

  // A different session, or none, is a different menu.
  useEffect(() => {
    setOpen(false)
    setGit(false)
    setConfirm(false)
  }, [row?.id])

  // A press anywhere else closes it, and Escape closes it from the first level;
  // from the Git level Escape goes back one level instead (see `onMenuKey`).
  useEffect(() => {
    if (!open) return
    const onPointer = (ev: PointerEvent) => {
      if (ev.target instanceof Element && ev.target.closest(".detail-actions")) return
      closeMenu(false)
    }
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key !== "Escape" || git) return
      ev.preventDefault()
      ev.stopPropagation()
      closeMenu(true)
    }
    document.addEventListener("pointerdown", onPointer)
    document.addEventListener("keydown", onKey, true)
    return () => {
      document.removeEventListener("pointerdown", onPointer)
      document.removeEventListener("keydown", onKey, true)
    }
  })

  const onTriggerKey = (ev: ReactKeyboardEvent) => {
    if (ev.key !== "ArrowDown") return
    ev.preventDefault()
    ev.stopPropagation()
    openMenu()
    focusNext.current = "first"
  }

  const onMenuKey = (ev: ReactKeyboardEvent) => {
    if ((ev.key === "ArrowLeft" || ev.key === "Escape") && git) {
      ev.preventDefault()
      ev.stopPropagation()
      setGit(false)
      focusNext.current = "git-more"
      return
    }
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(ev.key)) return
    ev.preventDefault()
    ev.stopPropagation()
    const list = items()
    if (!list.length) return
    const at = list.indexOf(document.activeElement as HTMLButtonElement)
    const next =
      ev.key === "Home"
        ? 0
        : ev.key === "End"
          ? list.length - 1
          : ev.key === "ArrowDown"
            ? (at + 1 + list.length) % list.length
            : (at - 1 + list.length) % list.length
    list[next].focus({ preventScroll: true })
  }

  // As `SessionActions.end`: the menu closes first, the header shows the
  // session as ending while the request is out, and a refusal is said here.
  const end = async () => {
    if (!row || ending) return
    const id = row.id
    closeMenu(false)
    setEndingId(id)
    try {
      await client.close(id)
      setSaid(null)
      onDid()
    } catch (err) {
      setSaid(err instanceof RefusalError ? err.detail : String(err))
    } finally {
      setEndingId(null)
    }
  }

  return (
    <>
      {/* No focus route in this daemon, so the chip is always disabled. Its title
          is the write-on wording; the write flag lives with the composer. */}
      <button
        className="chip"
        id="tx-focus"
        type="button"
        hidden={!atMac()}
        disabled
        title={T.webShowOnMacTip}
      >
        <svg className="ico" viewBox="0 0 14 14" aria-hidden="true" focusable="false">
          <rect x="1.3" y="2.2" width="11.4" height="7.8" rx="1.3" fill="none" stroke="currentColor" strokeWidth="1.3" />
          <path d="M5 12.2h4M7 10v2.2" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
        </svg>
        <span id="tx-focus-label">{T.webShowOnMac}</span>
      </button>
      <div className="detail-actions">
        <button
          className="detail-more"
          id="detail-actions-trigger"
          type="button"
          ref={triggerRef}
          aria-haspopup="menu"
          aria-expanded={open}
          disabled={!row || ending}
          title={T.webSessionActions}
          aria-label={T.webSessionActions}
          onClick={() => (open ? closeMenu(false) : openMenu())}
          onKeyDown={onTriggerKey}
        >
          <svg viewBox="0 0 18 14" aria-hidden="true" focusable="false">
            <circle cx="3" cy="7" r="1.5" />
            <circle cx="9" cy="7" r="1.5" />
            <circle cx="15" cy="7" r="1.5" />
          </svg>
        </button>
        <div
          className="session-actions"
          id="session-actions"
          role="menu"
          hidden={!open}
          aria-label={T.webSessionActions}
          onKeyDown={onMenuKey}
        >
          <div className="session-action-stage">
            <div
              className="session-action-level"
              id="session-actions-main"
              ref={mainRef}
              data-place={git ? "left" : "current"}
              aria-hidden={leveled ? git : undefined}
              inert={git}
            >
              <button id="session-focus" type="button" role="menuitem" disabled>
                {T.webShowOnMac}
              </button>
              <button id="session-info" type="button" role="menuitem" disabled>
                {T.webSessionInfo}
              </button>
              <button id="session-screen" type="button" role="menuitem" disabled>
                {T.webSessionScreen}
              </button>
              <button id="session-documents" type="button" role="menuitem" disabled>
                {documentsMenuWord()}
              </button>
              <button id="session-user-messages" type="button" role="menuitem" disabled>
                {userMessagesTitle()}
              </button>
              <button id="session-snippets" type="button" role="menuitem" hidden={!SNIPPETS_READABLE} disabled>
                {T.webSnippets}
              </button>
              {/* "Git", "commit" and "push" are literal in the original's index.html, not catalog strings. */}
              <button
                id="session-git-more"
                type="button"
                role="menuitem"
                ref={gitMoreRef}
                aria-haspopup="menu"
                disabled={!row || ending}
                onClick={() => {
                  setGit(true)
                  focusNext.current = "first"
                }}
              >
                Git <span className="next" aria-hidden="true">›</span>
              </button>
              <button
                className="end"
                id="session-end"
                type="button"
                role="menuitem"
                disabled={!row || ending}
                onClick={() => (confirm ? void end() : setConfirm(true))}
              >
                {confirm ? T.webConfirm : T.webEndSession}
              </button>
            </div>
            <div
              className="session-action-level"
              id="session-actions-git"
              ref={gitRef}
              data-place={git ? "current" : "right"}
              aria-hidden={!git}
              inert={!git}
            >
              <button
                className="level-back"
                id="session-actions-back"
                type="button"
                role="menuitem"
                onClick={() => {
                  setGit(false)
                  focusNext.current = "git-more"
                }}
              >
                ‹ {T.webSessionActions}
              </button>
              <button id="session-git" type="button" role="menuitem" disabled>
                {T.webSessionGit}
              </button>
              <button id="session-commit" type="button" role="menuitem" data-action="commit" disabled>
                commit
              </button>
              <button id="session-push" type="button" role="menuitem" data-action="push" disabled>
                push
              </button>
            </div>
          </div>
        </div>
      </div>
      {said && (
        <span className="chip" title={said}>
          {said}
        </span>
      )}
    </>
  )
}

/** The original `atMac`, from `core/env.js`. */
const atMac = L.atMac

/** The page's language as the original modules read it. */
function pageLanguage(): string {
  return document.documentElement.lang || navigator.language || "en"
}

/**
 * Stand-in for `words(language).menu` in `view/documents.js`, which is not
 * copied: its `words()` is not exported, so taking the word would mean taking
 * the whole documents page. The word is that module's own table, not the
 * catalog.
 */
function documentsMenuWord(): string {
  return /^zh(?:-|$)/i.test(pageLanguage()) ? "文件" : "Documents"
}

/** `copyForUserMessages(lang).title`, from the copied `view/user-messages-data.js`. */
function userMessagesTitle(): string {
  return L.userMessagesCopy(document.documentElement.lang || "").title
}

type Mark = Icon & { generated?: boolean }

/** The original `markForSession` and `projectLabel`, from `view/project-mark.js`. */
function markForSession(session: SessionRow | null, projectKey = ""): Mark | null {
  return L.markForSession(session, projectKey)
}
const projectLabel = L.projectLabel
