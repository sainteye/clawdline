// The verification ledger's way into its copied module.
//
// `js/view/ledger.js` is the Swift app's, byte for byte. It is bound here as
// that app's `main.js` binds it: one table of the page's elements by id, and an
// environment whose one read is this daemon's route spelled as the original's
// `api` spells it.
//
// What differs, and why:
//
//   - the original's read is absent on the Cloud path and present on this one,
//     so `carries` is always true here. The module's own "this connection
//     cannot read the verification ledger" sentence stays in the build for the
//     day this console is served over one that cannot;
//   - a refusal on this daemon is `{ error: "code", detail }`, not
//     `{ error: { code, message } }`. `jsonFetch` reads both and, like the
//     original's, sets no `status`, so the sentences that would append one do
//     not.
import { T } from "./js/core/i18n.js"
import { bindLedgerPage } from "./js/view/ledger.js"

/** What `bindLedgerPage` hands back. */
export interface LedgerPage {
  enter(): Promise<void>
  leave(): void
  load(): Promise<void>
  openFeature(graphID: string): Promise<void>
  escape(): void
  state: { view: "list" | "detail"; graphID: string | null }
}

/** `main.js`'s element table for `bindLedgerPage`, in its order. */
export const LEDGER_ELEMENT_IDS = [
  "ledger",
  "ledger-list-view",
  "ledger-detail-view",
  "ledger-title",
  "ledger-lede",
  "ledger-count",
  "ledger-status",
  "ledger-unattributed",
  "ledger-rows",
  "ledger-back",
  "ledger-detail-title",
  "ledger-detail-status",
  "ledger-detail-rows",
] as const

type Coded = Error & { code?: string; body?: { code?: string } }

/** `net/fetch.js`'s `jsonFetch`, reading this daemon's refusal shape as well as the original's. */
async function jsonFetch(path: string): Promise<Record<string, unknown>> {
  let res: Response
  try {
    res = await fetch(path)
  } catch {
    const dead: Coded = new Error(T.webOffline)
    dead.code = "offline"
    throw dead
  }
  const body = await res.text()
  let data: Record<string, unknown> | null = null
  try {
    data = body ? JSON.parse(body) : null
  } catch {
    /* below */
  }
  if (!res.ok) {
    const raw = data?.error
    const err =
      typeof raw === "string"
        ? { code: raw, message: typeof data?.detail === "string" ? data.detail : raw }
        : raw && typeof raw === "object"
          ? (raw as { code?: string; message?: string })
          : { code: "http_" + res.status, message: res.statusText || T.webRequestFailed }
    const failed: Coded = new Error(err.message || err.code || "")
    failed.code = err.code
    throw failed
  }
  if (!data) throw new Error(T.webNotJSON)
  return data
}

/** `net/live.js`'s `verificationLedger`: the whole ledger, or one Feature. */
function verificationLedger(graphID?: string): Promise<Record<string, unknown>> {
  const query = graphID ? "?graph=" + encodeURIComponent(graphID) : ""
  return jsonFetch("/v1/orchestrator/usage/verification-ledger" + query)
}

/**
 * `static.js`'s two lines for this page.
 *
 * The drawer's row is painted by App with the rest of the drawer; these two are
 * the page's own, and `core/dom.js` has no `nav-ledger` in its element table,
 * so the original's paint of it wrote to nothing and the row keeps its markup's
 * English there. It does not here, because this console's drawer is React's.
 */
export function paintLedgerStatic(doc: Document): void {
  const title = doc.getElementById("ledger-title")
  if (title && T.webLedger) title.textContent = T.webLedger
  const lede = doc.getElementById("ledger-lede")
  if (lede && T.webLedgerLede) lede.textContent = T.webLedgerLede
}

/** Bind the page, as `main.js` binds it. */
export function bindLedger(doc: Document, navigate: (name: string) => void): LedgerPage {
  const elements: Record<string, HTMLElement | null> = {}
  for (const id of LEDGER_ELEMENT_IDS) elements[id] = doc.getElementById(id)
  return bindLedgerPage(elements, {
    document: doc,
    verificationLedger,
    // This daemon owns the read, so the page never draws the "open the app's
    // own address" sentence. The module keeps it for a transport that cannot.
    carries: () => true,
    navigate,
  }) as LedgerPage
}
