// The sessions a reboot took away, offered back (docs/session-restore.md).
//
// After a restart the session list is empty, and the conversations that were
// open are still on disk: the daemon recorded which ones they were and answers
// `GET /v1/sessions/restorable`. This file is the part of the offer that is
// decided rather than drawn — whether a card shows and in which shape, what a
// press sends, and what each row says after it — so `node --test` can hold it
// without a browser. `Restore.tsx` draws it.
//
// Nothing is imported at run time, so `node --test` loads it as it is. The
// words come in as a function, as `next-strings.ts` spells them.
import type {
  DismissRestorableAnswer,
  RestorableSession,
  RestorableSessions,
  RestoreCode,
  RestoreResult,
  RestoreSessionsAnswer,
} from "@clawdline/contract"

/** The three routes, as the daemon serves them and `cloud/relay-writer.ts` carries them. */
export const RESTORE_ROUTES = {
  list: "/v1/sessions/restorable",
  restore: "/v1/sessions/restorable/restore",
  dismiss: "/v1/sessions/restorable/dismiss",
} as const

/**
 * Where the offer stands, or that it does not.
 *
 * `hero` takes the empty list's place; `compact` sits above rows the person
 * already has — somebody who opened a new session first after a reboot still
 * has the old ones to get back. Nothing is offered when the machine cannot
 * tell a reboot from a restart (`available: false`), when there is nothing on
 * offer, or before the list has been read at all: an unread offer is not an
 * empty one, and it is not drawn as either.
 */
export type OfferShape = "hero" | "compact" | null
/**
 * `list` is what the session list is showing: `home` its empty state with no
 * filter typed (the home hero), `rows` anything else it has settled on, and
 * `loading` its skeleton, over which nothing is offered yet.
 */
export function offerShape(offer: RestorableSessions | null, list: "home" | "rows" | "loading"): OfferShape {
  if (list === "loading" || !offer || !offer.available || !offer.sessions?.length) return null
  return list === "home" ? "hero" : "compact"
}

/**
 * What a restore press sends: the ticked conversations, in the order the
 * sheet lists them, each once. An empty selection sends nothing — the route
 * refuses an empty list, and the sheet's button is off for it.
 */
export function restoreBody(sessions: readonly RestorableSession[], ticked: ReadonlySet<string>): { conversations: string[] } | null {
  const conversations: string[] = []
  for (const s of sessions) {
    if (ticked.has(s.conversation_id) && !conversations.includes(s.conversation_id)) conversations.push(s.conversation_id)
  }
  return conversations.length ? { conversations } : null
}

/**
 * How many conversations one restore may name: the daemon's
 * `sessions.restore_batch` (docs/limits.md N49), which refuses a longer list
 * whole with `restore_batch_too_large`. Up to 200 can be on offer, so a press
 * over more than this goes as several requests rather than as one refusal.
 */
export const RESTORE_BATCH = 20

/**
 * One press's requests. The press mints one key; a press that needs more than
 * one request names each after it (`<key>.2`, …), so every request is still
 * one the press owns and a retry of the same press is the same requests.
 */
export function restoreBatches(body: { conversations: string[] }, key: string): { conversations: string[]; key: string }[] {
  const out: { conversations: string[]; key: string }[] = []
  for (let i = 0; i < body.conversations.length; i += RESTORE_BATCH) {
    const n = out.length + 1
    out.push({ conversations: body.conversations.slice(i, i + RESTORE_BATCH), key: n === 1 ? key : `${key}.${n}` })
  }
  return out
}

/** Every conversation ticked: how the sheet opens. */
export function allTicked(sessions: readonly RestorableSession[]): Set<string> {
  return new Set(sessions.map((s) => s.conversation_id))
}

/** One row's state in the sheet after a press. */
export type RowOutcome =
  | { kind: "idle" }
  | { kind: "opening" }
  | { kind: "opened" }
  | { kind: "failed"; code: RestoreCode | ""; message: string }

/**
 * Each row's outcome from the machine's answer. A row the press named and the
 * answer does not mention is `failed` with no code: it was asked for and
 * nothing says it opened, and saying nothing would read as success.
 */
export function outcomes(
  named: readonly string[],
  answer: Pick<RestoreSessionsAnswer, "results"> | null,
): Map<string, RowOutcome> {
  const out = new Map<string, RowOutcome>()
  const results = new Map<string, RestoreResult>()
  for (const r of answer?.results ?? []) results.set(r.conversation_id, r)
  for (const id of named) {
    const r = results.get(id)
    if (r?.ok) out.set(id, { kind: "opened" })
    else out.set(id, { kind: "failed", code: r?.code ?? "", message: r?.message ?? "" })
  }
  return out
}

/** Every row the press named opened: the sheet may close on its own. */
export function allOpened(named: readonly string[], rows: ReadonlyMap<string, RowOutcome>): boolean {
  return named.length > 0 && named.every((id) => rows.get(id)?.kind === "opened")
}

/** The words the sheet needs, keyed as `next-strings.ts` keys them. */
export type RestoreWordKey =
  | "restoreOpened"
  | "restoreOpening"
  | "restoreFailedNotRestorable"
  | "restoreFailedPlaceUnavailable"
  | "restoreFailedNotFound"
  | "restoreFailedOpen"
  | "restoreFailedBusy"
  | "restoreFailedUnknown"
  | "restoreFailedSaid"

const CODE_WORD: Readonly<Record<RestoreCode, RestoreWordKey>> = {
  not_restorable: "restoreFailedNotRestorable",
  place_unavailable: "restoreFailedPlaceUnavailable",
  conversation_not_found: "restoreFailedNotFound",
  open_failed: "restoreFailedOpen",
  over_capacity: "restoreFailedBusy",
}

/**
 * What a row says under its name after a press, or "" before one.
 *
 * A failure says the sentence for its code; `open_failed` is the one whose
 * reason is the terminal's own, so its message is said after it. A code this
 * bundle does not know says the machine's message, or that it did not open.
 */
export function outcomeLine(outcome: RowOutcome | undefined, word: (key: RestoreWordKey, holes?: Record<string, string>) => string): string {
  if (!outcome) return ""
  switch (outcome.kind) {
    case "idle":
      return ""
    case "opening":
      return word("restoreOpening")
    case "opened":
      return word("restoreOpened")
    case "failed": {
      const key = outcome.code ? CODE_WORD[outcome.code] : undefined
      if (!key) return outcome.message ? word("restoreFailedSaid", { why: outcome.message }) : word("restoreFailedUnknown")
      const said = word(key)
      return outcome.code === "open_failed" && outcome.message ? `${said} ${outcome.message}` : said
    }
  }
}

/** The name a row goes by: its title, else its folder's label, else the folder. */
export function rowName(s: RestorableSession): string {
  return s.title.trim() || s.place_label.trim() || s.cwd
}

/** The assistant as the start sheet names it. */
export function assistantName(assistant: string): string {
  if (assistant === "claude") return "Claude Code"
  if (assistant === "codex") return "Codex"
  return assistant
}

/** "last seen" as a relative time, in the page's language. */
export function lastSeenRelative(at: number, now: number, locale?: string): string {
  if (!Number.isFinite(at) || at <= 0) return ""
  const seconds = Math.min(0, at - now / 1000)
  const absolute = Math.abs(seconds)
  const unit: Intl.RelativeTimeFormatUnit = absolute < 90 * 60 ? "minute" : absolute < 36 * 3600 ? "hour" : "day"
  const size = unit === "minute" ? 60 : unit === "hour" ? 3600 : 86400
  try {
    return new Intl.RelativeTimeFormat(locale, { numeric: "auto" }).format(Math.round(seconds / size), unit)
  } catch {
    // refusal-ok: a browser refusing a locale is not a restore refusal.
    return new Date(at * 1000).toLocaleString()
  }
}

/** A refusal from one of the three routes, kept whole: the code is what is branched on. */
export class RestoreRefusal extends Error {
  readonly code: string
  readonly status: number
  constructor(status: number, code: string, detail: string) {
    super(detail || code)
    this.name = "RestoreRefusal"
    this.code = code
    this.status = status
  }
}

type Fetch = (input: string, init?: RequestInit) => Promise<Response>

/**
 * One request and its answer. Both refusal spellings are read — the route's
 * flat `{error, message|detail}` and the gate's nested `{error: {code,
 * message}}` — because the gate in front of every route answers the nested
 * one. A fetch that threw is `offline`, which says nothing about the machine.
 */
async function ask<T>(fetch: Fetch, path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, init)
  } catch (error) {
    throw new RestoreRefusal(0, "offline", error instanceof Error ? error.message : String(error))
  }
  const text = await res.text()
  let body: unknown = null
  try {
    body = text ? JSON.parse(text) : null
  } catch {
    body = null
  }
  if (res.ok && body && typeof body === "object") return body as T
  const b = (body ?? {}) as { error?: unknown; message?: unknown; detail?: unknown }
  const nested = b.error && typeof b.error === "object" ? (b.error as { code?: unknown; message?: unknown }) : null
  const code = typeof b.error === "string" ? b.error : typeof nested?.code === "string" ? nested.code : `http_${res.status}`
  const detail = typeof b.detail === "string" ? b.detail : typeof b.message === "string" ? b.message : typeof nested?.message === "string" ? nested.message : ""
  throw new RestoreRefusal(res.status, code, detail)
}

/** What the machine has on offer. */
export function readOffer(fetch: Fetch, signal?: AbortSignal): Promise<RestorableSessions> {
  return ask<RestorableSessions>(fetch, RESTORE_ROUTES.list, { method: "GET", cache: "no-store", signal })
}

/** Reopen the named conversations, under the press's one key. */
export function sendRestore(fetch: Fetch, body: { conversations: string[] }, key: string): Promise<RestoreSessionsAnswer> {
  return ask<RestoreSessionsAnswer>(fetch, RESTORE_ROUTES.restore, {
    method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": key },
    body: JSON.stringify(body),
  })
}

/** Dismiss every conversation on offer, under the press's one key. */
export function sendDismissAll(fetch: Fetch, key: string): Promise<DismissRestorableAnswer> {
  return ask<DismissRestorableAnswer>(fetch, RESTORE_ROUTES.dismiss, {
    method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": key },
    body: "{}",
  })
}
