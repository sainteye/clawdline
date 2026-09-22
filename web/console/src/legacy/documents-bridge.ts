// The Documents page's way into its copied modules.
//
// `js/view/documents.js`, `js/view/document-render.js`, `js/net/document-links.js`
// and `js/net/cloud-crypto.js` are the Swift app's, byte for byte. They are
// bound here as that app's `main.js` binds them: one table of the page's
// elements by id, and an environment whose answers are the ones the original's
// local transport gives (`net/live.js`).
//
// What that transport gives, and so what this gives:
//
//   - `list` and `read` are `net/live.js`'s `documents()` and `document()`, the
//     same two fetches with the same two validators, refusing any machine but
//     this one before a request is made. The listing goes through
//     `localDocumentListing`, which is what turns this daemon's richer rows
//     into locators and drops the private address they carry.
//   - `shareOrigin` is null, as it is on the local path there: a share link is
//     the Cloud's canonical address for a document on a named Mac, and a page
//     served by the Mac itself has neither. `documentShareURL` then refuses,
//     the two buttons stay disabled, and their `title` says why — which is the
//     original's own behaviour on this transport, not a gap here.
//   - `navigate` moves the drawer's page, and never writes the fragment for
//     `documents`: a listing has no stable address of its own because it needs
//     a Session identity, and a selected document writes its complete fragment
//     through the share controls instead.
//   - `schedule`/`cancel` are left out, so the module's retry uses
//     `setTimeout`, as it does there.
//
// `documentIdentityForSession` is re-exported rather than restated: it is the
// one function that decides *which* Mac and Session a row means, and on a
// duplicate id it refuses rather than picking.
import { T } from "./js/core/i18n.js"
import { S } from "./js/core/state.js"
import { makeJSONFetch } from "@clawdline/core/refusal"
import { LOCAL_SESSION_MACHINE } from "./js/session/selection.js"
import {
  documentBytesAnswer,
  documentIdentityForSession as documentIdentityForSessionOriginal,
  documentLocatorFromHash as documentLocatorFromHashOriginal,
  localDocumentListing,
  normalizeDocumentIdentity,
  normalizeDocumentLocator,
} from "./js/net/document-links.js"
import { bindDocumentsPage as bindDocumentsPageOriginal } from "./js/view/documents.js"
import { nextWord } from "../next-strings.js"

/** The identity a listing is asked for: one Mac, one Session. */
export interface DocumentIdentity {
  machine: string
  session: string
}

/** One document's complete address, as `normalizeDocumentLocator` returns it. */
export interface DocumentLocator extends DocumentIdentity {
  scope: "project" | "task"
  task?: string
  path: string
}

/** What `bindDocumentsPage` hands back. */
export interface DocumentsPage {
  enter(): Promise<boolean> | boolean
  leave(): void
  hide(): void
  openSession(identity: unknown): boolean
  openSessionError(error: unknown): boolean
  openDirect(locator: unknown, error?: unknown): boolean
  transportChanged(): boolean
  paint(): void
  state(): {
    active: boolean
    held: boolean
    identity: DocumentIdentity | null
    locator: DocumentLocator | null
    hasAnswer: boolean
    displayTitle: string
  }
}

/** `main.js`'s element table for `bindDocumentsPage`, by the name it gives each slot. */
export const DOCUMENTS_ELEMENTS: Record<string, string> = {
  page: "documents-page",
  title: "documents-title",
  back: "documents-back",
  listBack: "document-list-back",
  status: "documents-status",
  listView: "documents-list-view",
  rows: "documents-rows",
  viewer: "document-viewer",
  documentTitle: "document-title",
  meta: "document-meta",
  body: "document-body",
  share: "document-share",
  copy: "document-copy",
  menu: "session-documents",
}

/** How the `⋯` menu asks for this page without importing it (`overlays/events.ts`'s idiom). */
export const OPEN_DOCUMENTS = "clawdline:open-documents"

/** One session's documents, named the way the row names it. */
export interface DocumentsRequest {
  /** The session row's id; the page resolves the identity from the published list. */
  id: string
}

/** `#session-documents`: open the Documents page on this session. */
export function requestDocuments(id: string): void {
  document.dispatchEvent(new CustomEvent<DocumentsRequest>(OPEN_DOCUMENTS, { detail: { id } }))
}

/**
 * The fleet as `bridge.ts`'s `publish` left it, which is what `main.js` passes
 * as `S.sessions`. The rows it holds carry `machine: "this-mac"`, so the
 * identity a row resolves to is this Mac's — the same answer the original gets
 * on its local transport, where the constant comes from the client too.
 */
export function publishedRows(): unknown[] {
  const rows = (S as Record<string, unknown>).sessions
  return Array.isArray(rows) ? rows : []
}

/**
 * `documentIdentityForSession` (`net/document-links.js`): the shared Session row
 * key, refusing rather than choosing when two Macs published the same id. The
 * transport kind is "live" here, which is what makes a row with no explicit
 * identity mean this Mac.
 */
export function documentIdentityForSession(rows: unknown[], id: string): DocumentIdentity {
  return (documentIdentityForSessionOriginal as (r: unknown[], i: string, k: string) => DocumentIdentity)(
    rows,
    id,
    "live",
  )
}

/** `documentLocatorFromHash`: a whole `#document=…` fragment, or null. */
export const documentLocatorFromHash = documentLocatorFromHashOriginal as (
  hash: string,
) => DocumentLocator | null

/** Whether a fragment is asking for a document at all (`documentIntent` in `input/route.js`). */
export function hasDocumentIntent(hash: string): boolean {
  return /(?:^|[#&])document=/.test(String(hash || ""))
}

/** `net/live.js`'s refusal for a document that belongs to another Mac. */
function wrongMachine(): Error & { code?: string } {
  const wrong: Error & { code?: string } = new Error("This document belongs to another machine.")
  wrong.code = "document_machine_mismatch"
  return wrong
}

/**
 * `net/live.js`'s `documents(value)`, and one thing more: a listing shorter
 * than what is there says so under the rows (limits N28).
 *
 * The daemon puts that in `X-Clawdline-Truncated` because the body is the
 * Swift app's one-key object, which `localDocumentListing` refuses to widen.
 * The note is this page's own element, beside the copied rows rather than in
 * them, so the copied module never paints over it; it is hidden with the list
 * when a document is open, and cleared by every listing that is not cut. The
 * Swift app cut the same listing and drew nothing.
 */
async function list(value: unknown): Promise<{ documents: DocumentLocator[] }> {
  const identity = (normalizeDocumentIdentity as (v: unknown) => DocumentIdentity)(value)
  if (identity.machine !== LOCAL_SESSION_MACHINE) throw wrongMachine()
  sayCut(null)
  const headers: Headers[] = []
  const body = await jsonFetch(
    "/v1/sessions/" + encodeURIComponent(identity.session) + "/documents",
    undefined,
    (response) => {
      headers.push(response.headers)
    },
  )
  const listing = (localDocumentListing as (b: unknown, i: DocumentIdentity) => { documents: DocumentLocator[] })(
    body,
    identity,
  )
  sayCut(headers[0]?.get("X-Clawdline-Truncated") ?? null)
  return listing
}

/** The note under the rows, made once beside them. */
let cutNote: HTMLElement | null = null

/** Say how the listing was cut, or nothing. */
function sayCut(header: string | null): void {
  if (!cutNote) return
  const said = header ? cutWords(header) : ""
  cutNote.textContent = said
  cutNote.hidden = !said
}

/** `listed=12; walked=4000; tasks=3` as sentences, the parts that are there. */
export function cutWords(header: string): string {
  const parts = new Map<string, number>()
  for (const part of header.split(";")) {
    const [name, raw] = part.split("=").map((x) => x.trim())
    const n = Number(raw)
    if (name && Number.isFinite(n) && n > 0) parts.set(name, n)
  }
  const said: string[] = []
  if (parts.has("listed")) said.push(nextWord("documentsListed", { count: parts.get("listed")! }))
  if (parts.has("tasks")) said.push(nextWord("documentsTasks", { count: parts.get("tasks")! }))
  if (parts.has("walked")) said.push(nextWord("documentsWalked", { entries: parts.get("walked")!.toLocaleString() }))
  return said.join(" ")
}

/** `net/live.js`'s `document(value)`: bytes, and the same validator the Cloud answer gets. */
async function read(value: unknown): Promise<unknown> {
  const locator = (normalizeDocumentLocator as (v: unknown) => DocumentLocator)(value)
  if (locator.machine !== LOCAL_SESSION_MACHINE) throw wrongMachine()
  let path = "/v1/sessions/" + encodeURIComponent(locator.session) + "/documents/" + locator.scope + "/"
  if (locator.scope === "task") path += encodeURIComponent(String(locator.task)) + "/"
  path += locator.path.split("/").map(encodeURIComponent).join("/")
  let response: Response
  try {
    response = await fetch(path, { cache: "no-store" })
  } catch {
    throw typed(words().webOffline, "offline")
  }
  const bytes = new Uint8Array(await response.arrayBuffer())
  if (!response.ok) {
    let body: Record<string, unknown> | null = null
    try {
      body = JSON.parse(new TextDecoder().decode(bytes))
    } catch {
      /* a refusal that is not JSON is still a refusal; its status names it */
    }
    const said = refusalOf(body, response)
    throw typed(said.message, said.code)
  }
  try {
    return (documentBytesAnswer as (l: unknown, m: string, b: Uint8Array) => unknown)(
      locator,
      response.headers.get("content-type") || "",
      bytes,
    )
  } catch (error) {
    ;(error as { code?: string }).code = "bad_payload"
    throw error
  }
}

/** `jsonFetch` (`net/live.js`), with this page's no-cache and bad-payload spelling. */
const jsonFetch = makeJSONFetch({
  words: {
    offline: words().webOffline,
    requestFailed: words().webRequestFailed,
    notJSON: words().webRequestFailed,
  },
  defaults: { cache: "no-store" },
  invalidJSONCode: "bad_payload",
  allowFalsyJSON: true,
})

/**
 * The refusal's own name, out of whichever envelope this refusal came in.
 *
 * This daemon writes `{"error": "not_found", "detail": "…"}` — a **string**
 * `error` beside a separate `detail` (`internal/transport/http/write.go`) —
 * and the Swift app wrote `{"error": {"code", "message"}}`. Every other bridge
 * here reads both (`git-bridge.ts`, `board-bridge.ts`, `projects-bridge.ts`,
 * `schedules-bridge.ts`, `start-bridge.ts`,
 * `timeline-bridge.ts`, `voice-bridge.ts`). This one read only the object, so
 * `error.code` on a string was `undefined` and every refusal the Documents page
 * ever showed arrived as `http_404`/`http_403`/`http_503` — none of which
 * `core/failure-text.js` has a sentence for, so `not_found`, `forbidden`,
 * `busy` and `store_unavailable` all ended as the same "The document could not
 * be read." The daemon had named it; this function is where the name was lost.
 */
function refusalOf(body: Record<string, unknown> | null, response: Response): { code: string; message: string } {
  const raw = body?.error
  if (typeof raw === "string" && raw) {
    return { code: raw, message: typeof body?.detail === "string" ? body.detail : raw }
  }
  if (raw && typeof raw === "object") {
    const nested = raw as { code?: string; message?: string }
    if (nested.code) return { code: nested.code, message: nested.message || nested.code }
  }
  return {
    code: "http_" + response.status,
    message: response.statusText || words().webRequestFailed,
  }
}

function typed(message: string, code: string): Error & { code: string } {
  const error = new Error(message) as Error & { code: string }
  error.code = code
  return error
}

function words(): Record<string, string> {
  return T as Record<string, string>
}

/**
 * Bind the page. `elements` are looked up in the document the caller names, as
 * `main.js` does with `byId`, so the section React drew is what gets filled.
 */
export function bindDocuments(doc: Document, navigate: (page: string) => void): DocumentsPage {
  const table: Record<string, Element | null> = {}
  for (const [slot, id] of Object.entries(DOCUMENTS_ELEMENTS)) table[slot] = doc.getElementById(id)
  const listView = table.listView
  if (listView && !cutNote) {
    cutNote = doc.createElement("p")
    cutNote.id = "documents-cut"
    cutNote.className = "documents-status"
    cutNote.setAttribute("role", "note")
    cutNote.hidden = true
    listView.appendChild(cutNote)
  }
  return (bindDocumentsPageOriginal as (e: unknown, s: unknown) => DocumentsPage)(table, {
    document: doc,
    language: () => doc.documentElement.lang || navigator.language || "en",
    list,
    read,
    // The local transport has no canonical Cloud origin, so no document here
    // can be shared across devices and both controls say so.
    shareOrigin: () => null,
    navigator: () => navigator,
    navigate: (name: string) => navigate(name),
  })
}
