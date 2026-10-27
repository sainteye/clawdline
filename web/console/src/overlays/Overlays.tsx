import { macKeyboard, shortcutLabel } from "../shortcuts.js"
import { catalogWord } from "../catalog.js"
import { Fragment, useEffect } from "react"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { ActionConfirm, bindActionConfirm } from "./action-confirm.js"
import { bindInfo } from "./info.js"
import { bindRoleDetail } from "./role-detail.js"
import { ITermConsentDialog } from "../session/ITermConsentDialog.js"

/**
 * The three overlays and the toast, as `index.html` writes them: the keyboard
 * card (`div#keys`), the Session info card (`div#info`) and the confirmation
 * (`div#action-confirm`), then `div#toast` at the end of the body.
 *
 * Their elements are drawn here with fixed props, and what changes while the
 * page is open — `hidden`, the card's body, the sheet's sentence and buttons —
 * is written by the controllers in this directory, as the original's are. An
 * element React never re-renders is one it never fights over; the parts React
 * does draw are the words that arrive with the catalog.
 *
 * `App` opens and closes them: `Info`, `ActionConfirm`, and `toggleKeys` /
 * `closeKeys` below.
 */
export function Overlays() {
  const T = L.strings

  useEffect(() => {
    const unbind = [bindKeys(), bindInfo(), bindRoleDetail(), bindActionConfirm()]
    return () => unbind.forEach((off) => off())
  }, [])

  // `paintStatic`, again whenever the catalog has changed under a closed sheet.
  useEffect(() => {
    ActionConfirm.paint()
  })

  const mod = macKeyboard() ? "⌘" : "Ctrl+"
  const groups: { title: string; rows: [string[], string][] }[] = [
    { title: nextWord("shortcutNavigation"), rows: [
      [[shortcutLabel("sessions")], nextWord("shortcutSessions")],
      [[shortcutLabel("board")], nextWord("shortcutBoard")],
      [[shortcutLabel("sidebar")], nextWord("shortcutSidebar")],
      [[shortcutLabel("help"), "?"], nextWord("shortcutHelp")],
      [["Esc"], T.webKeysEscape],
    ] },
    { title: nextWord("shortcutActions"), rows: [
      [[shortcutLabel("start")], nextWord("shortcutStart")],
      [[shortcutLabel("task")], nextWord("shortcutTask")],
      [[shortcutLabel("voice")], nextWord("shortcutVoice")],
    ] },
    { title: nextWord("shortcutSession"), rows: [
      [[shortcutLabel("compose")], nextWord("shortcutCompose")],
      [[shortcutLabel("attention")], nextWord("shortcutAttention")],
      [["/"], T.webKeysFilter],
      [["↑", "↓"], T.webKeysMove],
      [["⏎"], T.webKeysOpen],
      [[mod + "J"], T.webKeysPane],
      [[mod + "I"], T.webSessionInfo],
      [["g", "G"], T.webKeysEnds],
      [["r"], T.webKeysReverse],
      [["F6"], nextWord("terminalKeysLeave")],
    ] },
    { title: nextWord("shortcutWriting"), rows: [
      [["⏎"], nextWord("shortcutSend")],
      [["Shift+⏎"], nextWord("shortcutNewline")],
    ] },
  ]

  return (
    <>
      <ITermConsentDialog />
      <div className="overlay" id="keys" hidden>
        <div className="sheet shortcuts-sheet" role="dialog" aria-modal="true" aria-labelledby="keys-title">
          <div className="shortcuts-head">
            <h2 id="keys-title">{nextWord("shortcutTitle")}</h2>
            <button className="chip" type="button" id="keys-close">{T.webClose}</button>
          </div>
          <div className="shortcuts-groups">
            {groups.map(({ title, rows }) => <section key={title} className="shortcuts-group">
              <h3>{title}</h3>
              <dl>
                {rows.map(([keys, words], i) => <Fragment key={i}>
                  <dt>{keys.map((key, k) => <Fragment key={k}>
                    {k ? " " : null}<kbd>{key}</kbd>
                  </Fragment>)}</dt>
                  <dd dangerouslySetInnerHTML={{ __html: L.wordsHTML(words) }} />
                </Fragment>)}
              </dl>
            </section>)}
          </div>
          <div className="foot">{nextWord("shortcutFoot")}</div>
        </div>
      </div>

      {/* `aside#session-board` is the Board's related-work panel, which stays
          hidden where the Board is off, as it is on this daemon. */}
      <div className="overlay" id="info" hidden>
        <div className="sheet info-sheet" role="dialog" aria-modal="true" id="info-sheet" aria-label={T.webInfoTitle}>
          <h2 id="info-title">{T.webInfoTitle}</h2>
          <p className="say" id="info-say" hidden></p>
          <aside id="session-board" className="session-board" aria-label={catalogWord("inline", "785626f3cb47")} hidden></aside>
          <div className="facts" id="info-body"></div>
          <p className="said" id="info-said" role="status" aria-live="polite"></p>
          <div className="buttons">
            <button className="chip" id="info-refresh" type="button">
              {T.webInfoRefresh}
            </button>
            <button className="chip" id="info-close" type="button">
              {T.webClose}
            </button>
          </div>
        </div>
      </div>

      <div className="overlay role-detail-overlay" id="role-detail" hidden>
        <div className="sheet role-detail-sheet" role="dialog" aria-modal="true" aria-labelledby="role-detail-title">
          <div className="role-detail-content" id="role-detail-content"></div>
          <div className="buttons">
            <button className="chip" id="role-detail-close" type="button">{T.webClose}</button>
          </div>
        </div>
      </div>

      {/* The title and sentence are the markup's English until the sheet first
          opens; the two buttons are written by `ActionConfirm`, never by React,
          because the busy state replaces what is inside the second one. */}
      <div className="overlay" id="action-confirm" hidden>
        <div
          className="sheet confirm-sheet"
          id="action-confirm-sheet"
          role="alertdialog"
          aria-modal="true"
          aria-labelledby="action-confirm-title"
          aria-describedby="action-confirm-say"
        >
          <h2 id="action-confirm-title">{catalogWord("inline", "81191ae00ea5")}</h2>
          <div className="say" id="action-confirm-say">{catalogWord("inline", "250f3493e97c")}
          </div>
          <div className="buttons">
            <button className="chip" id="action-confirm-cancel" type="button"></button>
            <button className="chip confirm-go" id="action-confirm-go" type="button"></button>
          </div>
        </div>
      </div>

      <div className="toast" id="toast" hidden></div>
    </>
  )
}

/** Whether a sheet or card is showing: `!els.<id>.hidden`. */
export function shown(id: string): boolean {
  return document.getElementById(id)?.hidden === false
}

let keysReturnFocus: HTMLElement | null = null

/** Open from either the keyboard or navigation, with a keyboard way out. */
export function toggleKeys(): void {
  const keys = document.getElementById("keys")
  if (!keys) return
  if (!keys.hidden) { closeKeys(); return }
  keysReturnFocus = document.activeElement as HTMLElement | null
  keys.hidden = false
  document.getElementById("keys-close")?.focus({ preventScroll: true })
}

export function closeKeys(): void {
  const keys = document.getElementById("keys")
  if (!keys || keys.hidden) return
  keys.hidden = true
  if (keysReturnFocus?.isConnected && keysReturnFocus.getClientRects().length) keysReturnFocus.focus({ preventScroll: true })
  keysReturnFocus = null
}

/** The sheet owns focus and its keys; the page behind it does not move. */
function bindKeys(): () => void {
  const keys = document.getElementById("keys")
  const sheet = keys?.querySelector(".sheet")
  if (!keys || !sheet) return () => {}
  // The sheet stops native bubbling before React's delegated click handler.
  // Bind its close button here too, so touch, mouse and keyboard activation agree.
  const close = document.getElementById("keys-close")
  const onOverlay = () => closeKeys()
  const onSheet = (ev: Event) => ev.stopPropagation()
  const onKey = (event: KeyboardEvent) => {
    if (keys.hidden || event.isComposing) return
    if (event.key === "Tab") {
      event.preventDefault()
      document.getElementById("keys-close")?.focus({ preventScroll: true })
    }
    if (event.key === "Escape") {
      event.preventDefault()
      event.stopPropagation()
      closeKeys()
    }
  }
  close?.addEventListener("click", closeKeys)
  keys.addEventListener("click", onOverlay)
  sheet.addEventListener("click", onSheet)
  document.addEventListener("keydown", onKey, true)
  return () => {
    close?.removeEventListener("click", closeKeys)
    keys.removeEventListener("click", onOverlay)
    sheet.removeEventListener("click", onSheet)
    document.removeEventListener("keydown", onKey, true)
  }
}
