/*
 * Forgetting a machine: the one route that removes it from the account, and
 * the two questions a page has to answer afterwards.
 *
 * The control plane's `DELETE /v1/machines/:id` is a **revoke**, not a delete:
 * the row keeps its `revoked_at`, its credential hash is dropped, and the
 * account's revocation epoch moves. The machine's own token stops being
 * accepted, so its next heartbeat is a 401; the daemon reads a 401 at the
 * token endpoint or at the socket upgrade as terminal and stops rather than
 * redialling (`internal/adapters/cloud/backoff.go`, `transport.go`). It does
 * not re-register itself. It comes back only when somebody signs that machine
 * in again from the machine, and the approval matches an existing row by
 * public key **only while that row is not revoked** — so it comes back as a
 * new machine with a new id, spending a machine slot.
 *
 * The route answers three facts this page must not swallow: routing stopped,
 * key rotation is lazy, and the sentence that says what that costs. A device
 * that already holds the master secret can still open ciphertext it recorded
 * before the revoke. That belongs in front of the person, before they press
 * the button and again after it (docs/design-decisions.md: honesty goes where
 * it is read, not where it is logged), so it is carried out of here rather
 * than left in the response.
 *
 * The credential is this browser's account cookie. It is the same cookie
 * `cloud-boot.js` already sends to this origin (`credentials: "include"`,
 * `/v1/auth/session`), so this needs nothing new; the machine **list** on the
 * gate is not from this API at all — it is what the relay client decrypted —
 * which is why a forgotten machine does not disappear from it and is marked
 * here instead.
 *
 * Nothing is imported at run time, so `node --test` loads it as it is.
 */

/** What the route says it did, when it did it (`routes/machines.ts`). */
export const ROUTING_STOPPED = "stopped"
/** How it says content keys are rotated. Lazily: this is the honest part. */
export const ROTATION_LAZY = "lazy"

/**
 * What one attempt to forget a machine came to.
 *
 * The three failures are three different words on purpose, and this page never
 * blurs them: `refused` is the account saying no, `absent` is the account
 * having no such machine, `unreadable` is not knowing — no answer, or an
 * answer this page cannot make sense of. Only `absent` and `refused` are
 * facts; `unreadable` says so.
 */
export type ForgetOutcome =
  | {
      kind: "forgotten"
      /** When the control plane says it was revoked, or "" if it did not say. */
      revokedAt: string
      /** `routing`, `content_key_rotation` and `note`, as answered. */
      routing: string
      rotation: string
      note: string
    }
  | { kind: "refused"; status: number; code: string }
  | { kind: "absent"; status: number; code: string }
  | { kind: "unreadable"; status: number | null; code: string }

interface ApiBody {
  revoked_at?: unknown
  routing?: unknown
  content_key_rotation?: unknown
  note?: unknown
  error?: { code?: unknown; message?: unknown }
}

function word(value: unknown): string {
  return typeof value === "string" ? value : ""
}

/** The far end's own code for a refusal, or a stand-in that says where it came from. */
function code(body: ApiBody | null, status: number): string {
  const said = body && body.error ? word(body.error.code) : ""
  return said || "http_" + status
}

/**
 * Ask the account to forget `id`, with this browser's own cookie.
 *
 * `get` is injected so the tests drive a fake control plane; the browser
 * passes nothing and gets `fetch`. Nothing here retries: this is not an
 * idempotent read, and a second DELETE after an answer nobody could read would
 * be a second irreversible act on a guess.
 */
export async function forgetMachine(
  apiOrigin: string,
  id: string,
  get: typeof fetch = fetch,
): Promise<ForgetOutcome> {
  let res: Response
  try {
    res = await get(apiOrigin + "/v1/machines/" + encodeURIComponent(id), {
      method: "DELETE",
      credentials: "include",
    })
  } catch {
    // Nothing answered. The request may still have arrived, which is why this
    // is "not known" and not "did not happen".
    return { kind: "unreadable", status: null, code: "no_answer" }
  }
  let body: ApiBody | null = null
  try {
    body = (await res.json()) as ApiBody
  } catch {
    body = null
  }
  if (res.status === 401 || res.status === 403) {
    return { kind: "refused", status: res.status, code: code(body, res.status) }
  }
  if (res.status === 404) {
    return { kind: "absent", status: res.status, code: code(body, res.status) }
  }
  if (res.status !== 200 && res.status !== 204) {
    return { kind: "unreadable", status: res.status, code: code(body, res.status) }
  }
  // A 200 is the revoke having happened; what it answered about it may still
  // be unreadable, and the caller says our own sentence when it is.
  return {
    kind: "forgotten",
    revokedAt: word(body?.revoked_at),
    routing: word(body?.routing),
    rotation: word(body?.content_key_rotation),
    note: word(body?.note),
  }
}

/**
 * Whether the page's own sentence about what a revoke costs is still the one
 * the control plane is describing.
 *
 * The page says it in the person's language, before the button and after it.
 * That translation is only honest while the route keeps answering the two
 * words it was written against; if either changes, the caller shows the
 * route's own `note` instead of a sentence this build made up about a
 * behaviour it no longer knows.
 */
export function honestyIsOurs(outcome: ForgetOutcome): boolean {
  return (
    outcome.kind === "forgotten" &&
    outcome.routing === ROUTING_STOPPED &&
    outcome.rotation === ROTATION_LAZY
  )
}

/**
 * What a tab must stop doing with a machine it has just forgotten.
 *
 * The gate remembers the machine this tab chose (`sessionStorage`) and re-picks
 * it on the next load, and the console reads whichever machine is open. Neither
 * may keep pointing at a machine the account no longer routes to: the reader
 * would sit on a line the relay has stopped and say nothing about why, and the
 * remembered id would take the next load straight back into it.
 *
 * So this is asked once, with both facts, and it is the whole answer: drop the
 * memory when it names the forgotten machine, and go back to the list when the
 * machine being read is the one forgotten. In this build the list is only drawn
 * while nothing is being read, so `backToList` is the guard for the case rather
 * than the path anyone takes; it is here because "cannot happen" is not a
 * behaviour and the next screen that lists machines beside a console would make
 * it happen.
 */
export function afterForget(input: {
  forgotten: string
  remembered: string | null
  reading: string | null
}): { clearRemembered: boolean; backToList: boolean } {
  return {
    clearRemembered: !!input.forgotten && input.remembered === input.forgotten,
    backToList: !!input.forgotten && input.reading === input.forgotten,
  }
}
