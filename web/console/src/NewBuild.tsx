import { useEffect, useState } from "react"
import { isDifferentBuild } from "./build-freshness.js"
import { nextWord } from "./next-strings.js"
import "./build-freshness.css"

/**
 * The line that appears when this page is no longer the build being served,
 * and the reload that answers it (`build-freshness.ts` says why).
 *
 * **It asks rather than reloads.** A reload here would land on somebody
 * halfway through writing a message, and what they would lose is exactly what
 * this feature exists to protect: their words reaching the Mac. So the page
 * says what it has found and the press is theirs.
 *
 * **It asks the server rarely, and only while somebody is here.** A console
 * left open on a desk for a week should not be a request every minute, and a
 * page in the background should not ask at all — the answer would be stale by
 * the time it was looked at. So: once on coming back to the page, and once an
 * hour while it is in front of somebody. A build usually lands while nobody is
 * looking, which is when the first of those two catches it.
 *
 * Once it has found one it stops asking. The answer cannot change back, and
 * the line is already on screen.
 */
export function NewBuild() {
  const [stale, setStale] = useState(false)

  useEffect(() => {
    if (stale) return
    let dropped = false
    let asked = 0

    const look = async () => {
      // Not while the page is in the background, and not twice in a minute:
      // `visibilitychange` and the clock can land together.
      if (document.visibilityState !== "visible") return
      const now = Date.now()
      if (now - asked < 60_000) return
      asked = now
      let served: string
      try {
        const res = await fetch(document.baseURI, { cache: "reload", credentials: "include" })
        if (!res.ok) return
        served = await res.text()
      } catch {
        return /* offline, or the tunnel is down: not an answer */
      }
      if (dropped) return
      const running = [...document.querySelectorAll("script[src], link[href]")].map(
        (el) => el.getAttribute("src") || el.getAttribute("href") || "",
      )
      if (isDifferentBuild(served, running)) setStale(true)
    }

    const wake = () => void look()
    document.addEventListener("visibilitychange", wake)
    const clock = window.setInterval(wake, 3_600_000)
    return () => {
      dropped = true
      document.removeEventListener("visibilitychange", wake)
      window.clearInterval(clock)
    }
  }, [stale])

  return (
    <div className="new-build" role="status" hidden={!stale}>
      <span>{nextWord("newBuild")}</span>
      <button type="button" className="go" onClick={() => location.reload()}>
        {nextWord("newBuildReload")}
      </button>
    </div>
  )
}
