import type { Snippet, SnippetAnswer, SnippetControls } from "../legacy/snippets-bridge.js"

/**
 * The five snippet routes as this sheet reads and writes them
 * (internal/transport/http/snippets.go). The shapes are the wire's, field for
 * field; nothing here is derived.
 *
 * Every write carries an Idempotency-Key, minted once per decision: a retry
 * after the connection dropped reuses it, so the daemon answers the first
 * attempt's outcome rather than making a second snippet (D03).
 *
 * `fetch` is looked up per request, as `client.ts` does, because a console
 * reading a machine through Clawdline Cloud has its own `fetch` installed over
 * this origin's `/v1/…` (`cloud/install.ts`). All five are carried there now
 * (`cloud/carry.ts`), and a refusal still arrives as this daemon's own typed
 * one rather than as the static host's page coming back as a body that is not
 * JSON.
 *
 * **Every one of them names the session it was made from.** The daemon on this
 * machine's own network reads `?session=` on the list and ignores it on the
 * four writes; the relay needs it on all five, because a snippet belongs to a
 * machine and the session is what says which machine that is
 * (`cloud/relay-writer.ts`, `snippetIdentity`). Sending it always is one rule
 * rather than two, and the local routes are unchanged by it.
 */

/** A refusal or a failure, with the code the sheet says it by. */
export class SnippetFailure extends Error {
  readonly code: string
  readonly status: number | null
  readonly detail: string
  constructor(code: string, detail: string, status: number | null) {
    super(`${code}: ${detail}`)
    this.name = "SnippetFailure"
    this.code = code
    this.status = status
    this.detail = detail
  }
}

/**
 * The code out of a refusal body, in either envelope this daemon writes.
 *
 * Two shapes reach the browser and both are this daemon's: the flat
 * `{"error":"code","detail":"…"}` its own routes and the relay write, and the
 * nested `{"error":{"code":"…","message":"…"}}` the door writes, because the
 * gate answers in the Swift app's envelope. A reader that knew only one of them
 * would show "that did not work" for exactly the refusals about permission.
 */
function codeOf(parsed: unknown, status: number): SnippetFailure {
  const body = parsed as { error?: unknown; detail?: unknown } | null
  const flat = body && typeof body.error === "string" ? body.error : ""
  if (flat) {
    return new SnippetFailure(flat, typeof body?.detail === "string" ? body.detail : "", status)
  }
  const nested = body && typeof body.error === "object" && body.error !== null ? (body.error as Record<string, unknown>) : null
  const code = nested && typeof nested.code === "string" ? nested.code : ""
  const message = nested && typeof nested.message === "string" ? nested.message : ""
  if (code) return new SnippetFailure(code, message, status)
  return new SnippetFailure("unexpected_error", "", status)
}

const TIMEOUT_MS = 15_000

async function call<T>(path: string, init: RequestInit = {}): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), TIMEOUT_MS)
  let res: Response
  try {
    res = await globalThis.fetch(path, { credentials: "same-origin", ...init, signal: controller.signal })
  } catch {
    // Nobody answered. That is not a refusal and must not be dressed as one:
    // `offline` has its own sentence in the catalog.
    throw new SnippetFailure("offline", "", null)
  } finally {
    clearTimeout(timer)
  }
  const text = await res.text()
  let parsed: unknown = null
  try {
    parsed = text ? JSON.parse(text) : null
  } catch {
    throw new SnippetFailure("unexpected_error", "", res.status)
  }
  if (!res.ok) throw codeOf(parsed, res.status)
  return parsed as T
}

/**
 * A fresh Idempotency-Key. `crypto.randomUUID` exists only in a secure context,
 * and a paired phone on this Mac's own network reaches the page over plain http;
 * `getRandomValues` is there in both.
 */
function mintKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return "web-" + Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")
}

function writing(method: string, body?: unknown): RequestInit {
  const init: RequestInit = {
    method,
    headers: { "Content-Type": "application/json", "Idempotency-Key": mintKey() },
  }
  if (body !== undefined) init.body = JSON.stringify(body)
  return init
}

/** One session's two groups, and the project the machine resolved for it. */
export const readSnippets = (sessionRowID: string): Promise<SnippetAnswer> =>
  call<SnippetAnswer>("/v1/snippets?session=" + encodeURIComponent(sessionRowID))

/** `?session=<row>`, which every one of these carries; see the note above. */
const on = (sessionRowID: string) => "?session=" + encodeURIComponent(sessionRowID)

export const createSnippet = (sessionRowID: string, body: Record<string, string>): Promise<Snippet> =>
  call<Snippet>("/v1/snippets" + on(sessionRowID), writing("POST", body))

export const updateSnippet = (sessionRowID: string, id: string, patch: Record<string, string>): Promise<Snippet> =>
  call<Snippet>("/v1/snippets/" + encodeURIComponent(id) + on(sessionRowID), writing("PATCH", patch))

export const deleteSnippet = (sessionRowID: string, id: string): Promise<{ ok: boolean; deleted: string }> =>
  call<{ ok: boolean; deleted: string }>("/v1/snippets/" + encodeURIComponent(id) + on(sessionRowID), writing("DELETE"))

export const orderSnippets = (
  sessionRowID: string,
  body: { scope: string; project?: string; order: string[] },
): Promise<{ ok: boolean }> => call<{ ok: boolean }>("/v1/snippets/order" + on(sessionRowID), writing("POST", body))

/**
 * Which of the five routes this console has, which is all of them: they are
 * served by the daemon this page came from.
 *
 * `snippetControls` over there asks a transport object whether each is a
 * function, because the Swift page's relay client carries only the reading half.
 * Here the same question is answered once, as a constant, so the sheet's six
 * call sites still ask it in one place — and a transport that turns out not to
 * carry a write says so in the answer to that write, by name. That is still
 * the shape over the relay: a copied client older than the four snippet words
 * answers `cloud_not_carried` for the one that is missing rather than making
 * the whole sheet read-only.
 */
export const SNIPPET_CONTROLS: SnippetControls = {
  read: true,
  create: true,
  update: true,
  remove: true,
  order: true,
}
