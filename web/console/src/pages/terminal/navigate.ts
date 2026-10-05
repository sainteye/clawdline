import { requestPage } from "../../overlays/index.js"
import { sessionsPageHash } from "../../page-route.js"

/** Tells the Session page that its terminal address changed. */
export const TERMINAL_ROUTE = "clawdline:terminal-route"

/** The history entry a phone pushed for a terminal it opened, which closing it steps back over. */
const STEPPED = "clawdlineTerminalStep"

const phone = () => window.matchMedia("(max-width: 899px)").matches

function show(address: string, push: boolean): void {
  try {
    if (push) history.pushState({ ...(history.state ?? {}), [STEPPED]: true }, "", address)
    else history.replaceState(history.state, "", address)
  } catch {
    location.hash = address
  }
  requestPage({ page: "sessions", hash: false })
  document.dispatchEvent(new CustomEvent(TERMINAL_ROUTE))
}

/**
 * Show terminals where Sessions are: the Session page's terminal list, and the
 * terminal named here in its second column — beside the list on a desk, a
 * whole screen on a phone, as a Session opens. A project alone is the list;
 * the list names every terminal's project, so it no longer has a page of its own.
 *
 * On a phone opening one is a step with its own history entry, so the back
 * gesture returns to the list; on a desk the address is replaced, because
 * moving between terminals there is where you are and not a step you took.
 */
export function openTerminalPage(project: string, terminal = ""): void {
  creating = ""
  const open = terminal.trim() !== "" && project.trim() !== ""
  show(sessionsPageHash(true, open ? { project, terminal } : undefined), open && phone() && !history.state?.[STEPPED])
}

/** Put the terminal beside the list away, leaving the list. */
export function closeTerminalPane(): void {
  creating = ""
  if (history.state?.[STEPPED]) {
    history.back()
    return
  }
  show(sessionsPageHash(true), false)
}

/**
 * A terminal being opened in a project on the console Clawdline Cloud serves,
 * which can only ask for one on the machine's own channel once the second
 * column has connected to it. Held here, not in the address: an address read
 * again (a reload, a copied link) must never open another shell.
 */
let creating = ""

/** Open a new terminal in this project in the second column (hosted). */
export function openNewTerminal(project: string): void {
  if (!project.trim()) return
  const stepped = phone() && !history.state?.[STEPPED]
  show(sessionsPageHash(true), stepped)
  creating = project
  document.dispatchEvent(new CustomEvent(TERMINAL_ROUTE))
}

/** The project a new terminal is being opened in, or "". */
export function newTerminalProject(): string {
  return creating
}

/** The back gesture left the second column: a new terminal asked for there is no longer wanted. */
export function forgetNewTerminal(): void {
  creating = ""
}
