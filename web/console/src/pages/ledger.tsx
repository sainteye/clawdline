import { useLayoutEffect, useRef } from "react"
import type { PageModule } from "./types.js"
import { bindLedger, paintLedgerStatic, type LedgerPage } from "../legacy/ledger-bridge.js"
import { ActionConfirm, Info, shown as overlayShown } from "../overlays/index.js"
import sectionMarkup from "./ledger/section.html?raw"

/**
 * The verification ledger: `section#ledger` in the Swift app's `index.html`,
 * drawn by its `view/ledger.js`.
 *
 * The fragment beside this file is that section's markup between its own tags,
 * whitespace included, and the copied module fills it — the Feature cards, the
 * block that names no Feature, the three-state figures and a Feature's findings
 * are the original's own DOM. React owns the section element and nothing inside
 * it, and never re-renders inside it.
 *
 * What the original's `main.js` does for this page is done here: bind once,
 * `enter` on arrival (the list, every time), `leave` on departure, the keyboard
 * lands on the heading, and `static.js`'s two lines are painted.
 *
 * Escape: in the original, `input/keys.js` gives this page its turn after the
 * drawer and the keyboard card, and an open Feature is given back before the
 * page is left. App's key handler leaves the page directly, so the page's turn
 * is taken here, ahead of it, and only when nothing App would close first is
 * open.
 */
function LedgerPageView({ shown }: { shown: boolean }) {
  const page = useRef<LedgerPage | null>(null)
  const was = useRef(false)
  const painted = useRef(false)

  useLayoutEffect(() => {
    if (!page.current) page.current = bindLedger(document, navigate)
  }, [])

  useLayoutEffect(() => {
    if (shown === was.current) return
    was.current = shown
    if (!shown) {
      page.current?.leave()
      return
    }
    // The words arrive in App's effect, which runs after this one on a cold
    // start at `#page=ledger`; the page is still covered (`booting`) until
    // then, so arrival waits for them as the original's does.
    const arrive = () => {
      if (!was.current) return
      if (!painted.current) {
        painted.current = true
        paintLedgerStatic(document)
      }
      void page.current?.enter()
      document.getElementById("ledger-title")?.focus({ preventScroll: true })
    }
    const root = document.documentElement
    if (!root.classList.contains("booting")) {
      arrive()
      return
    }
    const watch = new MutationObserver(() => {
      if (root.classList.contains("booting")) return
      watch.disconnect()
      arrive()
    })
    watch.observe(root, { attributes: true, attributeFilter: ["class"] })
    return () => watch.disconnect()
  }, [shown])

  useLayoutEffect(() => {
    if (!shown) return
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key !== "Escape" || ev.metaKey || ev.ctrlKey) return
      if (page.current?.state.view !== "detail") return
      if (ActionConfirm.isOpen() || Info.isOpen() || overlayShown("keys")) return
      if (!(document.getElementById("sidebar")?.hidden ?? true)) return
      if (document.querySelector("dialog[open]")) return
      ev.preventDefault()
      ev.stopImmediatePropagation()
      page.current?.escape()
    }
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [shown])

  return (
    <section
      className="page ledger"
      id="ledger"
      data-page-view="ledger"
      aria-labelledby="ledger-title"
      hidden={!shown}
      dangerouslySetInnerHTML={{ __html: sectionMarkup }}
    />
  )
}

/** The page router, as the drawer moves: its row for that page, clicked. */
function navigate(name: string): void {
  const row = document.querySelector<HTMLButtonElement>(`#sidebar [data-page-to="${name}"]`)
  if (row && !row.disabled) row.click()
}

export const page: PageModule = { id: "ledger", Component: LedgerPageView }
