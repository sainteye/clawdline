import { useSyncExternalStore } from "react"

/**
 * How the rest of the page opens an overlay, without importing the page.
 *
 * The original calls `Info.open()` and `ActionConfirm.open("end")` straight
 * from its listeners (`input/action-confirm.js`). Here the controls live in
 * components that do not own the overlays, so they announce the press on the
 * document and `App` answers it. `dispatchEvent` runs its listeners before it
 * returns, so the overlay opens and takes focus inside the press — which is
 * what keeps a phone's keyboard and focus rules on its side.
 */
export const OPEN_INFO = "clawdline:open-info"
export const OPEN_CONFIRM = "clawdline:open-confirm"
export const GO_PAGE = "clawdline:go-page"

export interface PageRequest {
  /** The page to show, by the name `Pages.knows` knows it by. */
  page: string
  /** Whether the address is written; `false` is `Pages.go(name, {hash:false})`. */
  hash?: boolean
}

export interface ConfirmRequest {
  /** "end" closes the session; any other word is a command sent into it. */
  kind: string
  /** The session; the open one when absent. */
  id?: string
  /** Where focus goes back on Cancel; the `⋯` trigger when absent. */
  opener?: HTMLElement | null
}

/** Open the Session info card for the open session (`#detail-info`, `#session-info`, `#status-line-open`). */
export function requestInfo(): void {
  document.dispatchEvent(new CustomEvent(OPEN_INFO))
}

/**
 * Move to a page without going through the address (`Pages.go`).
 *
 * **The address is not a way to ask for a page.** `main.js` passes
 * `{hash: false}` for the Documents page and `core/pages.js` then switches the
 * page and writes nothing, and that is not a preference: writing
 * `location.hash` is a same-document navigation, which fires `popstate`, and
 * this page's `popstate` listener is the phone's back gesture
 * (`input/action-confirm.js`) — so on a phone the address write closed the
 * detail, `closeDetail` replaced the address it had just been given, and the
 * `hashchange` that followed read no page at all and went back to the list.
 * That was the ⋯ menu's "文件" doing nothing at all at 760 wide while working
 * at 900. A page a component asks for is asked for here instead.
 */
export function requestPage(request: PageRequest): void {
  document.dispatchEvent(new CustomEvent<PageRequest>(GO_PAGE, { detail: request }))
}

/** Open the confirmation for one action (`#session-end` is `requestConfirm({ kind: "end" })`). */
export function requestConfirm(request: ConfirmRequest): void {
  document.dispatchEvent(new CustomEvent<ConfirmRequest>(OPEN_CONFIRM, { detail: request }))
}

/**
 * Which session is being closed, as `closingSelectionKey()` is there: the
 * detail head says "closing" (`data-closing="on"`) only while the session it
 * shows is the one going away. The confirmation owns the close now, so it
 * owns this too, and the header reads it with `useClosingId`.
 */
let closingId: string | null = null
const watchers = new Set<() => void>()

export function setClosingId(id: string | null): void {
  if (closingId === id) return
  closingId = id
  for (const watch of watchers) watch()
}

export function getClosingId(): string | null {
  return closingId
}

export function useClosingId(): string | null {
  return useSyncExternalStore(
    (watch) => {
      watchers.add(watch)
      return () => {
        watchers.delete(watch)
      }
    },
    getClosingId,
  )
}
