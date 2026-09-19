/*
 * A card's words, sent, sent again, and looked for.
 *
 * `send.ts` wires this to the daemon; the decisions are here, where
 * `node --test` can hold them, because each of them is about a message that may
 * already be on the Mac:
 *
 * - **Every attempt of a card is one request.** The card's `request` goes as
 *   the Idempotency-Key, and the Mac answers a second attempt with the first
 *   one's answer rather than typing the words again (F2). Across Clawdline
 *   Cloud it is the command's `request` too (`cloud/relay-writer.ts`).
 * - **A failure is `failed` only when it proves nothing was typed**
 *   (`outcome.ts`); otherwise the card is `unknown` and says so (F3).
 * - **"Try again" reads the transcript first, and a read that fails sends
 *   nothing.** That read fails exactly when the Mac is busy or far away — when
 *   the first attempt is most likely to have landed after all — so the card
 *   goes to `unknown` instead of typing the words a second time (F2).
 * - **An `unknown` card is looked at, not retried.** Looking reads the
 *   transcript: the turn there settles the card; a read that shows no turn
 *   marks it `absent`, and only then is sending offered — under the same
 *   request, so an attempt still queued on the Mac is not doubled.
 *
 * Nothing is imported at run time, so `node --test` loads it as it is.
 */
import type { PendingSend, PendingSends, SeenTurn } from "./pending.js"
import type { FailedWrite, Outcome } from "./outcome.js"

/** What posting one attempt came to: taken, or a refusal (`status` null when nothing answered). */
export type Posted = { ok: true } | { ok: false; status: number | null; code: string; outcome?: unknown }

export interface SenderDeps {
  cards: PendingSends
  now(): number
  /** Post one attempt of the card, under `card.request`. A throw is an attempt nothing answered. */
  post(card: PendingSend): Promise<Posted>
  /** The session's transcript read fresh from the daemon, or null when it could not be read. */
  readBack(session: string): Promise<SeenTurn[] | null>
  /** `outcome.ts` `outcomeOf`, handed in so that this file imports nothing at run time. */
  outcomeOf(failure: FailedWrite): Outcome
}

/** The code a card shows when its transcript could not be read to check it. */
export const READ_BACK_FAILED = "read_back_failed"

/**
 * How long one attempt is waited for before the page stops waiting. Past the
 * daemon's own bounds — its lane wait and its send, forty-five seconds with
 * pictures — and past the Cloud seam's minute; an attempt given up on here is
 * `unknown`, never `failed`.
 */
const POST_WAIT_MS = 75_000

/**
 * One attempt, as the daemon's own route takes it: `POST
 * /v1/sessions/<id>/send` under the card's one Idempotency-Key. The body is the
 * same bytes for every attempt of the card, because the key names the route
 * and the body together, and a key reused with other bytes is refused.
 */
export async function postCard(doFetch: typeof fetch, url: (path: string) => string, card: PendingSend): Promise<Posted> {
  const body: { text?: string; images?: string[] } = {}
  if (card.text) body.text = card.text
  if (card.pictures.length) body.images = [...card.pictures]
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), POST_WAIT_MS)
  let res: Response
  try {
    res = await doFetch(url("/v1/sessions/" + encodeURIComponent(card.session) + "/send"), {
      method: "POST",
      headers: { "Content-Type": "application/json", "Idempotency-Key": card.request },
      body: JSON.stringify(body),
      credentials: "same-origin",
      signal: controller.signal,
    })
  } catch {
    return { ok: false, status: null, code: "offline" }
  } finally {
    clearTimeout(timer)
  }
  if (res.ok) return { ok: true }
  return { ok: false, ...(await refusalOf(res)) }
}

/**
 * A refusal in either of this daemon's spellings — flat `{error, detail,
 * outcome}` or nested `{error: {code, message, outcome}}` — as code, sentence,
 * status and the far seam's `outcome`.
 */
export async function refusalOf(res: Response): Promise<{ status: number; code: string; message: string; outcome?: unknown }> {
  let parsed: Record<string, unknown> | null = null
  try {
    parsed = (await res.json()) as Record<string, unknown>
  } catch {
    /* a body that is not a refusal */
  }
  const error = parsed?.error
  const nested = error && typeof error === "object" ? (error as Record<string, unknown>) : null
  const code = typeof error === "string" ? error : typeof nested?.code === "string" ? nested.code : "http_" + res.status
  const message =
    typeof parsed?.detail === "string" ? parsed.detail : typeof nested?.message === "string" ? nested.message : code
  const outcome = nested ? nested.outcome : parsed?.outcome
  return { status: res.status, code, message, ...(outcome === undefined ? {} : { outcome }) }
}

/**
 * The session's transcript as the daemon has it now, or null when it could
 * not be read. `no-store` asks past any answer held from before: the Cloud
 * seam reuses transcripts, and waits out a read already on its way rather than
 * sharing it (`relay-reader.ts`).
 */
export async function readTranscript(doFetch: typeof fetch, url: (path: string) => string, session: string): Promise<SeenTurn[] | null> {
  try {
    const res = await doFetch(url(`/v1/transcript?session=${encodeURIComponent(session)}&limit=200`), {
      cache: "no-store",
      credentials: "same-origin",
    })
    if (!res.ok) return null
    const page = (await res.json()) as { entries?: unknown }
    return Array.isArray(page.entries) ? (page.entries as SeenTurn[]) : null
  } catch {
    return null
  }
}

export class Sender {
  private readonly deps: SenderDeps

  constructor(deps: SenderDeps) {
    this.deps = deps
  }

  /**
   * Post one attempt. Resolves with the refusal's code, or "" when the daemon
   * took the words; the card has been told either way.
   */
  async deliver(card: PendingSend): Promise<string> {
    let posted: Posted
    try {
      posted = await this.deps.post(card)
    } catch {
      posted = { ok: false, status: null, code: "offline" }
    }
    const { cards, now } = this.deps
    if (posted.ok) {
      cards.accepted(card.token, now())
      return ""
    }
    if (this.deps.outcomeOf({ status: posted.status, code: posted.code, said: posted.outcome }) === "not_done") {
      cards.failed(card.token, posted.code)
    } else {
      cards.uncertain(card.token, posted.code)
    }
    return posted.code
  }

  /**
   * "Try again" on a card that failed, or on an unknown one whose transcript
   * showed no turn: read the transcript fresh, and post only if it could be
   * read and does not hold the words.
   */
  async resend(token: string): Promise<void> {
    const { cards, now } = this.deps
    const card = cards.retrying(token)
    if (!card) return
    const turns = await this.readBack(card.session)
    if (!turns) {
      cards.uncertain(token, READ_BACK_FAILED)
      return
    }
    cards.reconcile(card.session, turns, now())
    const again = cards.resend(token, now())
    if (again) await this.deliver(again)
  }

  /** "Look" at an unknown card: read the transcript fresh; never post. */
  async look(token: string): Promise<void> {
    const { cards, now } = this.deps
    const card = cards.looking(token)
    if (!card) return
    const code = card.failure
    const turns = await this.readBack(card.session)
    if (!turns) {
      cards.uncertain(token, code, false)
      return
    }
    cards.reconcile(card.session, turns, now())
    // Still here: the transcript was read and holds no turn for it.
    if (cards.card(token)) cards.uncertain(token, code, true)
  }

  private async readBack(session: string): Promise<SeenTurn[] | null> {
    try {
      return await this.deps.readBack(session)
    } catch {
      return null
    }
  }
}
