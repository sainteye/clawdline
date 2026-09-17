import type { Icon, SessionRow } from "@clawdline/contract"
import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type RefObject,
} from "react"
import { requestConfirm, requestInfo, useClosingId } from "../overlays/index.js"
import { toast, toastFailure } from "../overlays/toast.js"
import * as L from "../legacy/bridge.js"
import { requestDocuments } from "../legacy/documents-bridge.js"
import { requestUserMessages } from "../legacy/user-messages-bridge.js"
import { askFocus } from "../legacy/screen-bridge.js"
import { Transcript } from "./Transcript.js"
import { GitPanel } from "./GitPanel.js"
import { Composer } from "./Composer.js"
import { ScreenPanel } from "./ScreenPanel.js"
import { StatusLine } from "./StatusLine.js"
import { UserMessages } from "./UserMessages.js"

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
  // Which session is being closed is the confirmation sheet's to know; the
  // head reads it so it can show that session as ending.
  const closingId = useClosingId()
  const ending = !!row && closingId === row.id
  // Which panel is over the transcript. `#screen-panel` is the only one this
  // console has; `pane-detail`'s `data-panel` is what the stylesheet reads, and
  // it is deleted rather than set to a word when nothing is open, as
  // `Terminal.close` does.
  const [screenOpen, setScreenOpen] = useState(false)
  const actionsTrigger = useRef<HTMLButtonElement>(null)
  // A different session, or none, closes the panel: it is that session's
  // terminal that was being watched (`followTerminal`).
  useEffect(() => {
    setScreenOpen(false)
  }, [row?.id])

  // `els["pane-detail"].dataset.panel` (`input/git-panel.js`): which panel has
  // the transcript's space. There is never more than one, and the copied
  // stylesheet steps the transcript, the composer and the rest aside from this
  // one attribute.
  const [gitOpen, setGitOpen] = useState(false)
  // `GitPanel.close(restore)`: focus goes back to the `⋯` trigger, and only
  // when it is still something a person can be on.
  const closeGit = (restore: boolean) => {
    setGitOpen(false)
    if (!restore) return
    const trigger = document.getElementById("detail-actions-trigger") as HTMLButtonElement | null
    if (trigger && !trigger.disabled) trigger.focus({ preventScroll: true })
  }

  // No snippets route means no resolved project (`snippetProjectFor`), so the
  // mark is keyed by the session's own `cwd`.
  const mark = useMemo(() => markForSession(row), [row?.icon, row?.cwd])
  const markRef = useRef<HTMLCanvasElement>(null)
  const [drew, setDrew] = useState(false)
  useLayoutEffect(() => {
    setDrew(L.paintIcon(markRef.current, mark ?? undefined, 5))
  }, [mark])

  const snippetsSays = SNIPPETS_READABLE ? T.webSnippets : row ? projectLabel(row.cwd) : ""

  // `renderTranscript`: with nothing open the pane is the home screen, and it
  // is blank rather than that while the list is still on its way — a pane that
  // says "pick a session" and then opens one on its own has changed its mind in
  // front of the reader.
  const home = !row && !listUnknown

  return (
    <section
      className="pane pane-detail"
      id="pane-detail"
      data-panel={screenOpen ? "screen" : gitOpen ? "git" : undefined}
    >
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
              {/* `coordinatorForSession`: shown on the registered Clawdfather's session. */}
              <span
                className="clawdfather-crown"
                id="detail-clawdfather-crown"
                role="img"
                aria-label="Clawdfather"
                hidden={!L.coordinatorForSession(row)}
              />
              <canvas id="detail-mark" ref={markRef} width={0} height={0} />
            </span>
          </button>
          <button
            className="detail-session"
            id="detail-info"
            type="button"
            aria-haspopup="dialog"
            disabled={!row || ending}
            onClick={requestInfo}
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
          <Tools
            row={row}
            ending={ending}
            onDid={onDid}
            triggerRef={actionsTrigger}
            onScreen={() => setScreenOpen(true)}
            onOpenGit={() => setGitOpen(true)}
          />
        </div>
      </div>

      <ScreenPanel
        row={row}
        open={screenOpen}
        onClose={(restore) => {
          setScreenOpen(false)
          if (restore && !actionsTrigger.current?.disabled) {
            actionsTrigger.current?.focus({ preventScroll: true })
          }
        }}
      />

      <GitPanel row={row} open={gitOpen} onClose={closeGit} />

      <div className={home ? "scroller tx-scroll home" : "scroller tx-scroll"} id="tx-scroll">
        <div className={home ? "tx home" : "tx"} id="tx">
          {row ? <Transcript id={row.id} /> : home ? <HomeHero /> : null}
        </div>
      </div>

      <Composer row={row} onDid={onDid} />
      <StatusLine row={row} />
      {/* `input/user-messages.js` puts its overlay on the body at import; this
          one is drawn into the body from here, because the `⋯` row that opens
          it is this component's. */}
      <UserMessages row={row} />
    </section>
  )
}

/** The home screen `renderTranscript` writes into `#tx` when no session is open. */
function HomeHero() {
  const T = L.strings
  return (
    <section className="home-hero" aria-labelledby="home-hero-title">
      <div className="copy">
        <span className="rule" aria-hidden="true"></span>
        <h1 id="home-hero-title">{T.webNoSessionOpen}</h1>
        <p>{T.webPickSession}</p>
      </div>
    </section>
  )
}

/**
 * The line under the name, as `renderDetailHead` builds it: path, tty, a word
 * for the three states a reader should not mistake for idle, and then the task
 * that opened this session — its title, its word, tokens and cost — from
 * `taskOfChild` (`view/derive.js`). Every known task, not only the ones still
 * shaping the list: "what was this for" has an answer long after the row went
 * back to normal. No pane id.
 */
function detailSub(row: SessionRow | null): string {
  if (!row) return ""
  const T = L.strings
  const sub: string[] = [L.path(row.cwd)]
  if (row.tty) sub.push(row.tty)
  if (row.state === "waiting") sub.push(T.sessionWaiting)
  else if (row.state === "working") sub.push(T.webStateWorking)
  else if (row.state === "unknown") sub.push(T.webStateUnreadable)
  const task = L.taskOfChild(row.id)
  if (task) {
    if (task.title) sub.push(task.title)
    sub.push(L.taskWord(task))
    const used = task.usage
    if (used?.total) sub.push("↓ " + L.agentTokens(used.total))
    // Only when there is a figure. Codex is billed by the plan rather than the
    // token, so there is no cost, and a "$0.0000" beside real work would read as
    // a measurement.
    if (typeof used?.costUsd === "number") sub.push("$" + used.costUsd.toFixed(4))
  }
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
 * - Show on Mac and Live screen are routes this daemon now owns: the chip and
 *   the row both POST `/focus`, and Live screen opens `#screen-panel`.
 * - Documents opens the Documents page on this session (`pages/documents.tsx`),
 *   and My messages opens its sheet (`session/UserMessages.tsx`), both through
 *   an event, as `main.js` opens them from its own listener on these two rows.
 * - Session info opens the info sheet.
 * - Snippets: hidden and disabled, which is the original's own shape for a
 *   transport with no snippets route (`syncRow` in `input/snippets.js`).
 * - Git changes opens the panel over the transcript, as `#session-git` does
 *   there. commit and push are ordinary prompts behind the `#action-confirm`
 *   sheet — the daemon runs no git for them; the word is typed into the
 *   session, which is what the original's `data-action` rows do.
 * - Close session opens the confirmation sheet, as the original does; a
 *   refusal is reported by the toast there, not by this menu.
 */
function Tools({
  row,
  ending,
  onDid,
  triggerRef,
  onScreen,
  onOpenGit,
}: {
  row: SessionRow | null
  ending: boolean
  onDid: () => void
  triggerRef: RefObject<HTMLButtonElement | null>
  onScreen: () => void
  onOpenGit: () => void
}) {
  const T = L.strings
  const [open, setOpen] = useState(false)
  const [git, setGit] = useState(false)
  // `level()` first runs when the menu first opens; until then the main level
  // carries no aria-hidden, as the static markup has none.
  const [leveled, setLeveled] = useState(false)

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
    if (restore) focusNext.current = "trigger"
  }

  // A different session, or none, is a different menu.
  useEffect(() => {
    setOpen(false)
    setGit(false)
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

  /**
   * `SessionActions.focusMac` and the `#tx-focus` listener, which are the same
   * call from two places.
   *
   * The toast is what the original says on success, and it says exactly what
   * happened: the Mac was *asked* to bring that terminal forward. Whether a
   * window is now in front of somebody is the emulator's answer and this end
   * never sees it — under `tmux -CC` iTerm2 is asked after the route has
   * already replied, and Ghostty, Terminal.app and the rest are selected inside
   * tmux and raise nothing.
   */
  const focusMac = () => {
    if (!row) return
    const mine = row.id
    askFocus(mine).then(
      () => {
        if (mine === row.id) toast(T.webShowOnMacAsked)
      },
      (e) => {
        if (mine === row.id) toastFailure(e, T.webRequestFailed)
      },
    )
  }

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

  return (
    <>
      {/* **Hidden unless this page is being read on the Mac itself.** The chip
          brings a session's terminal to the front over there; pressed from a
          phone it does something real and entirely invisible to the person
          pressing it. The test is where the page was *loaded from*, not the
          screen width: an iPad with a keyboard is not at the Mac either, and a
          narrow window on the Mac still is.

          Its title is the write-on wording. The original swaps it for
          `webShowOnMacOff` on a `write: false` health, which this daemon's
          health does not carry — the same gap the composer has, and a refusal
          arrives in the toast rather than in the title. */}
      <button
        className="chip"
        id="tx-focus"
        type="button"
        hidden={!atMac()}
        disabled={!row || ending}
        title={T.webShowOnMacTip}
        onClick={focusMac}
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
              <button
                id="session-focus"
                type="button"
                role="menuitem"
                disabled={!row || ending}
                onClick={() => {
                  closeMenu(false)
                  focusMac()
                }}
              >
                {T.webShowOnMac}
              </button>
              <button
                id="session-info"
                type="button"
                role="menuitem"
                disabled={!row || ending}
                onClick={() => {
                  closeMenu(false)
                  requestInfo()
                }}
              >
                {T.webSessionInfo}
              </button>
              {/* The original writes no `disabled` on this row and neither does
                  this: reading a screen needs no send capability, and the menu
                  it sits in cannot be opened without a session. */}
              <button
                id="session-screen"
                type="button"
                role="menuitem"
                onClick={() => {
                  if (!row) return
                  closeMenu(false)
                  onScreen()
                }}
              >
                {T.webSessionScreen}
              </button>
              <button
                id="session-documents"
                type="button"
                role="menuitem"
                disabled={!row || ending}
                onClick={() => {
                  if (!row) return
                  closeMenu(false)
                  requestDocuments(row.id)
                }}
              >
                {documentsMenuWord()}
              </button>
              <button
                id="session-user-messages"
                type="button"
                role="menuitem"
                disabled={!row || ending}
                onClick={() => {
                  if (!row) return
                  closeMenu(false)
                  requestUserMessages()
                }}
              >
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
                onClick={() => {
                  if (!row) return
                  closeMenu(false)
                  requestConfirm({ kind: "end", id: row.id, opener: triggerRef.current })
                }}
              >
                {T.webEndSession}
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
              {/* A read: nothing is sent, so there is no confirmation to cross.
                  The menu closes and the panel opens over the transcript. */}
              <button
                id="session-git"
                type="button"
                role="menuitem"
                disabled={!row || ending}
                onClick={() => {
                  closeMenu(false)
                  onOpenGit()
                }}
              >
                {T.webSessionGit}
              </button>
              {/* `data-action` in the original, read by one listener on the
                  menu. Both are sends, so both cross the confirmation sheet,
                  which is what types the word into the session. */}
              <button
                id="session-commit"
                type="button"
                role="menuitem"
                data-action="commit"
                disabled={!row || ending}
                onClick={() => {
                  if (!row) return
                  closeMenu(false)
                  requestConfirm({ kind: "commit", id: row.id, opener: triggerRef.current })
                }}
              >
                commit
              </button>
              <button
                id="session-push"
                type="button"
                role="menuitem"
                data-action="push"
                disabled={!row || ending}
                onClick={() => {
                  if (!row) return
                  closeMenu(false)
                  requestConfirm({ kind: "push", id: row.id, opener: triggerRef.current })
                }}
              >
                push
              </button>
            </div>
          </div>
        </div>
      </div>
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
