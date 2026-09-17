/**
 * The native shell's half of the input bar, as the page sees it.
 *
 * The bar is a window that a page cannot make for itself: borderless, above
 * everything, summoned by a key pressed in another application, dismissed
 * without a title bar to close. Everything *inside* it — the card, the rows,
 * the words, which session is selected, what a key does — is this page's, so
 * that a Linux or Windows shell is a window and nothing else. The list of what
 * each side owns is `docs/shell-bridge.md`; what the other platforms cannot do
 * is `docs/cross-platform.md`.
 *
 * The shape is `pages/settings/shell.ts`'s, deliberately: one message handler
 * the page posts to, and window events carrying the shell's answers, because a
 * `WKScriptMessageHandler` has no reply. A shell built on another webview
 * (WebKitGTK's `window.webkit.messageHandlers` is the same name; WebView2 has
 * `chrome.webview.postMessage`) implements the same two directions.
 *
 * Outside a shell — this page opened in a browser — every call below is a
 * no-op that answers false, and the bar still draws and still sends. That is
 * how it is developed, and it is why none of the page's own behaviour is
 * behind one of these.
 */

/** What the page asks the shell to do. One flat kind per message. */
export type BarRequest =
  /** The card is this tall now; make the window that tall. Sent on every change. */
  | { kind: "height"; height: number }
  /** Put the bar away, as Escape and a finished send do (`Controller.hide`). */
  | { kind: "hide" }
  /** The page has drawn; the window may be shown. Sent once per load. */
  | { kind: "ready" }

/**
 * What the shell tells the page. Delivered as `CustomEvent` detail on `window`.
 *
 * `shown` is the moment the window is summoned, which for this page is what a
 * fresh `Controller.show()` is: the text stays, the list closes, the keyboard
 * goes to the box. It arrives on every summon, including the ones where the
 * page was already loaded and hidden behind an ordered-out window.
 */
export interface BarShellState {
  /**
   * Whether the shell will select the terminal's own tab when the bar's
   * selection moves — the Swift app's `follow_target`.
   *
   * The page does the following itself, over `POST /v1/sessions/<id>/focus`;
   * this only says whether it should. Off unless a shell says otherwise: this
   * daemon's focus route raises the terminal application as well as selecting
   * the pane, and raising it would take the keyboard away from the bar that
   * asked. See `docs/cross-platform.md`.
   */
  follow: boolean
  /** The combination that summons the bar, as the shell registered it, for the hint row. Empty for none. */
  hotkey: string
}

export const BAR_SHOWN_EVENT = "clawdline-bar-shown"
export const BAR_HIDDEN_EVENT = "clawdline-bar-hidden"
export const BAR_STATE_EVENT = "clawdline-bar-state"

type Handler = { postMessage: (body: unknown) => void }

// Only the handler table, and deliberately not `__clawdlineShell`: that name is
// already declared by `pages/settings/shell.ts`, where it carries the native
// settings window's words, and two `declare global` blocks widening one
// property with different shapes is a type error rather than a union. The bar
// needs no injected data — everything it draws it fetches — so the presence of
// the handler is the whole test.
declare global {
  interface Window {
    webkit?: { messageHandlers?: Record<string, Handler | undefined> }
  }
}

function handler(): Handler | null {
  return window.webkit?.messageHandlers?.shellBar ?? null
}

/** Whether there is a shell around this page at all. */
export function inShell(): boolean {
  return handler() !== null
}

/** Ask the shell for something. False when there is no shell to ask. */
export function askShell(request: BarRequest): boolean {
  const h = handler()
  if (!h) return false
  try {
    h.postMessage(request)
    return true
  } catch {
    return false
  }
}

/**
 * Tell the shell how tall the card is.
 *
 * This is the one number the window's size depends on, and the page owns it
 * because the page owns the layout: the card grows when the text wraps past one
 * line and when the list opens, and only the page knows when that happened. The
 * shell keeps the width (`Config.width`, 720 in the Swift app) because the
 * width is where the window is on the screen, which is the shell's business.
 *
 * Rounded up to a whole pixel, and only sent when it changed: a resize is a
 * window server round trip, and a fractional height would make one on every
 * keystroke.
 *
 * **Remembered only once it has actually gone.** Recording the number before
 * knowing whether the message arrived means a height that was never delivered
 * is never offered again — and the window keeps a size the card stopped having.
 * Measured here: with no shell attached the first toggles filled this in, and a
 * shell that arrived afterwards was told nothing.
 */
let lastHeight = -1
export function reportHeight(height: number): void {
  const px = Math.ceil(height)
  if (px <= 0 || px === lastHeight) return
  if (askShell({ kind: "height", height: px })) lastHeight = px
}

/** `Controller.hide`. In a browser there is nothing to hide, and the bar stays. */
export function hideBar(): boolean {
  return askShell({ kind: "hide" })
}
