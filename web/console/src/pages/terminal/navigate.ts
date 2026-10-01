import { requestPage } from "../../overlays/index.js"
import { terminalPageHash, type TerminalFrom } from "../../page-route.js"

/** Tells a terminal page already on screen that its address changed. */
export const TERMINAL_ROUTE = "clawdline:terminal-route"

/**
 * Go to the terminal page: a project's terminals, or one of them. The address
 * is replaced, as every page move here is (App's `writeHash`), and the page is
 * told directly, because `replaceState` fires no `hashchange`.
 */
export function openTerminalPage(project: string, terminal = "", from: TerminalFrom = ""): void {
  const address = terminalPageHash(project, terminal, from)
  try {
    if (from === "sessions") history.pushState(history.state, "", address)
    else history.replaceState(history.state, "", address)
  } catch {
    location.hash = address
  }
  requestPage({ page: "terminal", hash: false })
  document.dispatchEvent(new CustomEvent(TERMINAL_ROUTE))
}
