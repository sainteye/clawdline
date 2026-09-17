import { useEffect } from "react"
import * as L from "../legacy/bridge.js"
import { Start } from "./Start.js"

/**
 * `div#starting`, as `index.html` writes it: the line under the header between
 * a tab being opened and its session turning up in the list. It is shown,
 * written and hidden by `Start` (./Start.tsx), as the original's is by
 * `input/start.js`; React draws it once, with the one word `static.js` writes
 * into it, and its × is the way to let go of the wait.
 */
export function Starting() {
  const T = L.strings
  useEffect(() => {
    const x = document.getElementById("starting-close")
    if (!x) return
    const onClose = () => Start.dismiss()
    x.addEventListener("click", onClose)
    return () => x.removeEventListener("click", onClose)
  }, [])
  return (
    <div className="starting" id="starting" hidden role="status" aria-live="polite">
      <canvas id="starting-spin"></canvas>
      <span id="starting-say"></span>
      <button className="x" id="starting-close" type="button" aria-label={T.webClose}>
        ×
      </button>
    </div>
  )
}
