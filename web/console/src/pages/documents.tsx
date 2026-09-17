import { useEffect, useLayoutEffect, useRef } from "react"
import type { PageModule } from "./types.js"
import {
  OPEN_DOCUMENTS,
  bindDocuments,
  documentIdentityForSession,
  publishedRows,
  type DocumentsPage,
  type DocumentsRequest,
} from "../legacy/documents-bridge.js"
import sectionMarkup from "./documents/section.html?raw"

/**
 * The Documents page: `section#documents-page` in the Swift app's `index.html`,
 * driven by its `view/documents.js`.
 *
 * The fragment beside this file is that section's markup between its own tags
 * (index.html lines 551–574), whitespace included, and the copied module fills
 * it: its words, the listing, one document, the two share controls and the
 * status line. React owns the section element and nothing inside it.
 *
 * What the original's `main.js` does for this page is done here: bind once,
 * `enter` on arrival and `leave` on departure, the keyboard landing on the
 * heading; and `#session-documents` in the `⋯` menu resolves the row's identity
 * with `documentIdentityForSession` and opens the page on it, where a refusal —
 * two Macs publishing one id, a row with no identity — is drawn by
 * `openSessionError` rather than swallowed.
 *
 * **`bindDocumentRoute` is deliberately not wired**, which is a decision rather
 * than a gap. That route exists for a `#document=…` address, and on this
 * transport no such address can be made: `documentShareURL` refuses every
 * locator whose machine is `this-mac`, which is why Share and Copy link sit
 * disabled here exactly as they do on the original's local page. The only
 * fragments left are ones typed by hand, and the page router that would have to
 * recognise them is `App`'s, which this task does not change. `openDirect` is
 * bound and working, so wiring it later is one clause in that router.
 *
 * `transportChanged` is not wired either: it exists there for a Cloud
 * connection that comes back, and this page has one transport that is either
 * answering or not. The module's own retry — `held` and `scheduleRetry` — is
 * still what handles a refusal it can wait out.
 */
function DocumentsPageView({ shown }: { shown: boolean }) {
  const page = useRef<DocumentsPage | null>(null)
  const was = useRef(false)

  useLayoutEffect(() => {
    if (!page.current) page.current = bindDocuments(document, navigate)
  }, [])

  // `byId("session-documents").addEventListener` in `main.js`, moved to an
  // event because the menu row is drawn by a component that does not own this
  // page. The menu closes itself before this runs, as `SessionActions.close()`
  // does there.
  useEffect(() => {
    const open = (ev: Event) => {
      const bound = page.current
      const id = (ev as CustomEvent<DocumentsRequest>).detail?.id
      if (!bound || !id) return
      let identity
      try {
        identity = documentIdentityForSession(publishedRows(), id)
      } catch (error) {
        bound.openSessionError(error)
        return
      }
      bound.openSession(identity)
    }
    document.addEventListener(OPEN_DOCUMENTS, open)
    return () => document.removeEventListener(OPEN_DOCUMENTS, open)
  }, [])

  useLayoutEffect(() => {
    if (shown === was.current) return
    was.current = shown
    if (!shown) {
      page.current?.leave()
      return
    }
    // The words arrive in App's effect, which runs after this one on a cold
    // start at `#page=documents`; the page is still covered (`booting`) until
    // then, and `enter` paints its heading from them, so arrival waits.
    const arrive = () => {
      if (!was.current) return
      void page.current?.enter()
      document.getElementById("documents-title")?.focus({ preventScroll: true })
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

  return (
    <section
      className="page documents-page"
      id="documents-page"
      data-page-view="documents"
      aria-labelledby="documents-title"
      hidden={!shown}
      dangerouslySetInnerHTML={{ __html: sectionMarkup }}
    />
  )
}

/**
 * The page router, as the drawer moves: its row for that page, clicked.
 *
 * Documents has no drawer row — it has none in the original either, where it is
 * reached from the session menu and from a document address — so the address is
 * how this page is asked for. `App`'s router follows `#page=` on every change
 * and switches without writing it back, which is `Pages.go(name, {hash:false})`
 * arriving the other way round.
 */
function navigate(name: string): void {
  const row = document.querySelector<HTMLButtonElement>(`#sidebar [data-page-to="${name}"]`)
  if (row && !row.disabled) {
    row.click()
    return
  }
  const wanted = "#page=" + encodeURIComponent(name)
  if (location.hash === wanted) return
  location.hash = wanted
}

export const page: PageModule = { id: "documents", Component: DocumentsPageView }
