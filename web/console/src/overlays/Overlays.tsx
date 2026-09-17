import { Fragment, useEffect } from "react"
import * as L from "../legacy/bridge.js"
import { ActionConfirm, bindActionConfirm } from "./action-confirm.js"
import { bindInfo } from "./info.js"

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
    const unbind = [bindKeys(), bindInfo(), bindActionConfirm()]
    return () => unbind.forEach((off) => off())
  }, [])

  // `paintStatic`, again whenever the catalog has changed under a closed sheet.
  useEffect(() => {
    ActionConfirm.paint()
  })

  // The shortcuts card, `paintStatic`'s rows. The keys are symbols printed on a
  // keyboard and stay as they are; only the sentences come from the catalog,
  // with its `*emphasis*` and backticks.
  const rows: [string[], string][] = [
    [["↑", "↓"], T.webKeysMove],
    [["⏎"], T.webKeysOpen],
    [["/"], T.webKeysFilter],
    [["esc"], T.webKeysEscape],
    [["⌘K"], T.webKeysList],
    [["⌘J"], T.webKeysPane],
    [["⌘I"], T.webSessionInfo],
    [["g", "G"], T.webKeysEnds],
    [["r"], T.webKeysReverse],
    [["?"], T.webKeysThis],
  ]

  return (
    <>
      <div className="overlay" id="keys" hidden>
        <div className="sheet" role="dialog" aria-modal="true" aria-label={T.webKeysLabel}>
          <h2>{T.webKeysTitle}</h2>
          <dl>
            {rows.map(([keys, words], i) => (
              <Fragment key={i}>
                <dt>
                  {keys.map((key, k) => (
                    <Fragment key={k}>
                      {k ? " " : null}
                      <kbd>{key}</kbd>
                    </Fragment>
                  ))}
                </dt>
                <dd dangerouslySetInnerHTML={{ __html: L.wordsHTML(words) }} />
              </Fragment>
            ))}
          </dl>
          <div className="foot">{T.webKeysFoot}</div>
        </div>
      </div>

      {/* `aside#session-board` is the Board's related-work panel, which stays
          hidden where the Board is off, as it is on this daemon. */}
      <div className="overlay" id="info" hidden>
        <div className="sheet info-sheet" role="dialog" aria-modal="true" id="info-sheet" aria-label={T.webInfoTitle}>
          <h2 id="info-title">{T.webInfoTitle}</h2>
          <p className="say" id="info-say" hidden></p>
          <aside id="session-board" className="session-board" aria-label="Related Board work" hidden></aside>
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
          <h2 id="action-confirm-title">Run commit?</h2>
          <p className="say" id="action-confirm-say">
            This sends commit to the current session.
          </p>
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

/** `?`: the shortcuts card on and off. */
export function toggleKeys(): void {
  const keys = document.getElementById("keys")
  if (keys) keys.hidden = !keys.hidden
}

export function closeKeys(): void {
  const keys = document.getElementById("keys")
  if (keys) keys.hidden = true
}

/** A press on the dark part closes the card; a press on the card does not (`detail-actions.js`). */
function bindKeys(): () => void {
  const keys = document.getElementById("keys")
  const sheet = keys?.querySelector(".sheet")
  if (!keys || !sheet) return () => {}
  const onOverlay = () => closeKeys()
  const onSheet = (ev: Event) => ev.stopPropagation()
  keys.addEventListener("click", onOverlay)
  sheet.addEventListener("click", onSheet)
  return () => {
    keys.removeEventListener("click", onOverlay)
    sheet.removeEventListener("click", onSheet)
  }
}
