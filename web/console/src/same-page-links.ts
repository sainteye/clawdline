/*
 * A link to this console, pressed inside the Home Screen app, stays in it.
 *
 * Every renderer here writes links with `target="_blank"`: the transcript's
 * Markdown (`legacy/js/view/markdown.js`, all three kinds of link), a
 * terminal's OSC 8 links (`legacy/screen-bridge.ts`), the info sheet, the
 * board. Their reason is good and still holds: the panel is somebody's
 * session, and following a link in place loses the screen they were reading.
 *
 * In a browser tab that costs nothing, because the new tab is the same
 * browser with the same storage. **In a standalone window it is not the same
 * browser.** An iPhone opens `_blank` in Safari even when the address is this
 * app's own, and Safari keeps its storage apart from the Home Screen app: no
 * sign-in, no device key, no machine keys. A link to this console opened there
 * is a console that cannot read anything — and a machine's pairing link
 * (`https://<app>/#pair=…`) opened there pairs Safari, which the person never
 * uses, and leaves the app they do use unpaired.
 *
 * So the rule is narrow on purpose, and one place decides it for every
 * renderer rather than each renderer deciding it again:
 *
 * - only in a standalone window (`isStandaloneWebApp`, the install screen's
 *   own test), because only there does the new window lose the storage;
 * - only a link to **this page** — same origin *and* same path, so what
 *   differs is the fragment or the query. An image or a download on this
 *   origin is not this console; followed in place it would leave a window
 *   that has no Back button showing a bare file;
 * - only an ordinary press: a modifier or another button is somebody asking
 *   for the other window, and a renderer that already handled the press keeps
 *   it.
 *
 * `legacy/js/view/markdown.js` is a byte-for-byte copy and is not changed;
 * this listens above it, in the capture phase, because the sheets that carry
 * links stop their clicks from bubbling.
 */
import { isStandaloneWebApp } from "./legacy/js/net/cloud-onboarding.js"

interface Place {
  href: string
  origin: string
  pathname: string
}

/**
 * The address `href` names, when it is this page with another fragment or
 * query; null for anything else.
 */
export function samePageTarget(href: string | null | undefined, here: Place): URL | null {
  if (typeof href !== "string" || !href) return null
  let url: URL
  try {
    url = new URL(href, here.href)
  } catch {
    return null
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") return null
  if (url.origin !== here.origin || url.pathname !== here.pathname) return null
  return url
}

interface Anchor {
  getAttribute(name: string): string | null
  hasAttribute(name: string): boolean
}

interface Press {
  defaultPrevented: boolean
  button: number
  metaKey: boolean
  ctrlKey: boolean
  shiftKey: boolean
  altKey: boolean
  target: unknown
  preventDefault(): void
}

interface Scope {
  location: Place & { assign(url: string): void }
  navigator?: unknown
  matchMedia?: unknown
}

/** The link a press landed in, if it is one that would open another window. */
function newWindowAnchor(target: unknown): Anchor | null {
  const start = target as { closest?: (selector: string) => Anchor | null } | null
  if (!start || typeof start.closest !== "function") return null
  const anchor = start.closest("a[href]")
  if (!anchor) return null
  const where = (anchor.getAttribute("target") || "").trim().toLowerCase()
  if (!where || where === "_self" || where === "_top" || where === "_parent") return null
  if (anchor.hasAttribute("download")) return null
  return anchor
}

/**
 * Follow one press in place, when it is a press this rule is for. Answers
 * whether it did, so a caller can tell the two apart.
 */
export function followInPlace(event: Press, scope: Scope): boolean {
  if (event.defaultPrevented || event.button !== 0) return false
  if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return false
  const anchor = newWindowAnchor(event.target)
  if (!anchor) return false
  if (!isStandaloneWebApp(scope)) return false
  const url = samePageTarget(anchor.getAttribute("href"), scope.location)
  if (!url) return false
  event.preventDefault()
  scope.location.assign(url.href)
  return true
}

/** Listen for the whole page, once. Answers how to stop. */
export function keepSamePageLinksHere(scope: Window = window): () => void {
  const listen = (event: MouseEvent) => {
    followInPlace(event, scope)
  }
  scope.addEventListener("click", listen, true)
  return () => scope.removeEventListener("click", listen, true)
}
