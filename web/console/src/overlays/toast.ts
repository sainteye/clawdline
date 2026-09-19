import { failureSentence } from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"

/**
 * `toast` and `toastFailure` (`core/util.js`), against the `div#toast` that
 * `Overlays` renders. The copied `util.js` has the same function, but it
 * reaches the element through `core/dom.js`, which looks every id up once at
 * import — before React has drawn any of them — so it holds `null` here.
 *
 * The element is drawn once with fixed props and written only from here, as
 * the original writes it. The failure-opener half is absent: nothing in this
 * console registers a status sheet (`setFailureOpener`), so a failure toast is
 * one that cannot be pressed, as it is there when none is registered.
 */
let timer: number | undefined

export function toast(text: string, bad = false): void {
  const el = document.getElementById("toast")
  if (!el) return
  el.textContent = text
  el.className = "toast" + (bad ? " err" : "")
  el.onclick = null
  el.hidden = false
  window.clearTimeout(timer)
  timer = window.setTimeout(() => {
    el.hidden = true
  }, 3200)
}

export function toastFailure(error: unknown, fallback: string): void {
  toast(failureSentence(error, { sentence: ownSentence(error), fallback }), true)
}

/**
 * The one code the copied catalog has no sentence for because the Swift app
 * never said it: a request this console cannot carry over Clawdline Cloud
 * (`cloud/relay-writer.ts`). Its tag still names the code.
 */
function ownSentence(error: unknown): string {
  const code = (error as { code?: unknown } | null)?.code
  return code === "cloud_not_carried" ? nextWord("cloudNotCarried") : ""
}
