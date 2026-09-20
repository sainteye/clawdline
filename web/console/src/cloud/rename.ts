/*
 * Renaming a machine: the one route that changes what the account calls it,
 * and the two things a page has to be honest about afterwards.
 *
 * The control plane's `PATCH /v1/machines/:id` takes `{"name": …}` and answers
 * `{"ok": true}` (`api/src/routes/machines.ts`). Three things about that
 * answer are carried out of here rather than reduced to a boolean:
 *
 * 1. **It does not echo the stored name.** So "it is called X now" is this
 *    page saying what it asked for, against a 200 that said only "ok" — which
 *    is why the name is carried back out of this module rather than read from
 *    the response, and why an answer that is not a plain `ok` is not treated
 *    as one.
 * 2. **An empty name is accepted and stores nothing** (`optionalString` gives
 *    null, and the route's `if (name)` skips the write). A page that sent one
 *    would get a 200 for a change that did not happen, so nothing empty is
 *    sent from here at all — that refusal is `blank`, and it never reaches the
 *    network.
 * 3. **The machine list this console draws is not this API's.** It is what the
 *    relay client decrypted from what the machines themselves published, the
 *    same reason a forgotten machine stays on it (`forget.ts`). So a rename
 *    that worked shows the old name until that machine reports in again, and
 *    the page says so instead of redrawing a row it has no new answer for.
 *
 * The credential is this browser's account cookie, as `forget.ts` uses. The
 * console served by the daemon has no such cookie and no way to get one, so
 * renaming is the hosted console's; nothing here is reachable from the other.
 *
 * Nothing is imported at run time, so `node --test` loads it as it is.
 */

/**
 * The longest name the control plane will store: `optionalString(input,
 * "name", 120)` in `routes/machines.ts`. It is checked here so that too long
 * is refused in words a person can act on, rather than as a `bad_field` from
 * somewhere else.
 */
export const NAME_MAX = 120

/**
 * What one attempt to rename a machine came to.
 *
 * The failures are different words on purpose, as forgetting one's are:
 * `blank` is this page refusing to send a change that would not be one,
 * `too_long` is the account's own bound, `refused` is the account saying no,
 * `absent` is the account having no such machine, and `unreadable` is not
 * knowing — no answer, or an answer this page cannot make sense of. Only
 * `refused` and `absent` are facts about the account; `unreadable` says it is
 * not one.
 */
export type RenameOutcome =
  | { kind: "renamed"; name: string }
  | { kind: "blank" }
  | { kind: "too_long"; max: number }
  | { kind: "refused"; status: number; code: string }
  | { kind: "absent"; status: number; code: string }
  | { kind: "unreadable"; status: number | null; code: string }

interface ApiBody {
  ok?: unknown
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
 * Ask the account to call `id` by `name`, with this browser's own cookie.
 *
 * The name is trimmed the way the route trims it before storing, so what this
 * page reports back is what the account now holds and not what was typed
 * around it. `get` is injected so the tests drive a fake control plane; the
 * browser passes nothing and gets `fetch`.
 *
 * Nothing retries. A PATCH whose answer nobody could read may still have
 * landed, and a second one sent on that guess would overwrite whatever another
 * browser did in between.
 */
export async function renameMachine(
  apiOrigin: string,
  id: string,
  name: string,
  get: typeof fetch = fetch,
): Promise<RenameOutcome> {
  const wanted = name.trim()
  if (!wanted) return { kind: "blank" }
  if (wanted.length > NAME_MAX) return { kind: "too_long", max: NAME_MAX }
  let res: Response
  try {
    res = await get(apiOrigin + "/v1/machines/" + encodeURIComponent(id), {
      method: "PATCH",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ name: wanted }),
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
  if (res.status !== 200) {
    return { kind: "unreadable", status: res.status, code: code(body, res.status) }
  }
  // A 200 whose body is not the `{ok: true}` this was written against is a
  // route answering something else; saying "it is called that now" over it
  // would be this page inventing the part the response never carried.
  if (body?.ok !== true) {
    return { kind: "unreadable", status: res.status, code: "unreadable_answer" }
  }
  return { kind: "renamed", name: wanted }
}
