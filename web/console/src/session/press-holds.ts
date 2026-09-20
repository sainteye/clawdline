/*
 * A waiting card's press, from the tap until the question it answered has gone.
 *
 * While a press is held the card's options are shut: they answer one question,
 * and a second tap would be a stray digit in whatever question comes next.
 * `Waiting.tsx` draws from this; the rules are here, where `node --test` can
 * hold them, because each is about a digit that may already be on the Mac.
 *
 * - **A press belongs to its session.** Reading another session and coming
 *   back does not drop it: the request is still on its way, and dropping the
 *   hold opened the options under it (review F1(b)).
 * - **Only a refusal that proves nothing was typed opens the options again**
 *   (`outcome.ts`). Any other failure — nothing answered, the answer lost —
 *   leaves the press `unknown`: it may have answered, and the next question may
 *   already be up on the Mac while this card still draws the old one. The
 *   options stay shut, saying so, until the row moves — or until the person
 *   chooses to answer again (`release`), which the Mac then checks against its
 *   screen like any press (F1(a), F3).
 * - **An answered press holds for `PRESS_SETTLED_MS`** and then gives the card
 *   back to the row, as the original did.
 * - **A press that only ticks a box does not hold at all once it has landed.**
 *   A multi-select's rows toggle and nothing is sent until Submit, so the
 *   question does not move and a second tap is not a stray digit in the next
 *   one — it is the second box, which is the whole point of a multi-select.
 *   Holding one for ten seconds locked every row and Submit with it, so the
 *   answer could never be more than one box (2026-09-20). The page this one
 *   replicates had no hold and redrew the rows live the moment the tick came
 *   back; this keeps that, and keeps the hold for the presses that do answer.
 * - **Every press is its own request.** Its id is the Idempotency-Key, so a
 *   press retried after its answer was lost is the same request to the Mac.
 *
 * Nothing is imported at run time, so `node --test` loads it as it is.
 */
import type { Outcome } from "./outcome.js"

/** How long an answered press holds its question once the machine said yes. */
export const PRESS_SETTLED_MS = 10_000

export interface Press {
  readonly session: string
  /** `menuKey` of the question pressed at: the hold lasts while it is up. */
  readonly menu: string
  /**
   * This press ticks a box rather than answering the question. `menuKey` does
   * not read a row's tick — deliberately, so that folding or waving away a
   * card survives one — so a ticking press cannot be released by the question
   * changing, and is released by landing instead.
   */
  readonly ticks: boolean
  /** The press's one request id. */
  readonly request: string
  state: "sending" | "sent" | "unknown"
  /** Long enough on its way to say so. */
  shown: boolean
  /** When it was pressed, then when the machine said yes. */
  at: number
}

export class PressHolds {
  private holds = new Map<string, Press>()
  private mint: () => string

  constructor(mint?: () => string) {
    let n = 0
    this.mint = mint ?? (() => {
      const c = globalThis.crypto
      if (typeof c?.randomUUID === "function") {
        try {
          return c.randomUUID()
        } catch {
          /* below */
        }
      }
      n += 1
      const bytes = new Uint8Array(8)
      c.getRandomValues(bytes)
      return "press-" + n + "-" + Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")
    })
  }

  /**
   * The press holding `session`'s card, given what the row says now. A hold
   * whose question has gone — the session stopped waiting, or asks something
   * else — is released here, and so is an answered one past its allowance.
   */
  current(session: string, menu: string | null, waiting: boolean, now: number): Press | null {
    const press = this.holds.get(session)
    if (!press) return null
    const landed = press.state === "sent"
    if (!waiting || press.menu !== menu || (landed && (press.ticks || now - press.at > PRESS_SETTLED_MS))) {
      this.holds.delete(session)
      return null
    }
    return press
  }

  /** A tap: the options go dead until this press is over. */
  start(session: string, menu: string, now: number, ticks = false): Press {
    const press: Press = { session, menu, ticks, request: this.mint(), state: "sending", shown: false, at: now }
    this.holds.set(session, press)
    return press
  }

  /** The machine said yes. A late answer for a press that is no longer held changes nothing. */
  settled(press: Press, now: number): void {
    if (this.holds.get(press.session) !== press) return
    press.state = "sent"
    press.at = now
  }

  /** It failed: open the options again only if it provably typed nothing. */
  failed(press: Press, outcome: Outcome): void {
    if (this.holds.get(press.session) !== press) return
    if (outcome === "not_done") this.holds.delete(press.session)
    else press.state = "unknown"
  }

  /** "Choose again", on a press that may have landed: the person's decision. */
  release(session: string): void {
    this.holds.delete(session)
  }
}

/** What one press came to: the daemon's answer, or a refusal (`status` null when nothing answered). */
export type Pressed =
  | { ok: true; body: unknown }
  | { ok: false; status: number | null; code: string; message: string; outcome?: unknown }

/**
 * `net/live.js`'s `key`: one press for a session's menu, as the daemon's own
 * route takes it. `expect` names the question it was chosen for
 * (`fingerprint.ts`); `request` is the press's own id, sent as its
 * Idempotency-Key, so the same press asked again — its answer lost — is the
 * same request to the daemon's receipt rather than a second digit.
 */
export async function postPress(doFetch: typeof fetch, id: string, key: string, expect: string, request: string): Promise<Pressed> {
  const body: { key: string; expect?: string } = { key: String(key) }
  if (expect) body.expect = expect
  let res: Response
  try {
    res = await doFetch("/v1/sessions/" + encodeURIComponent(id) + "/key", {
      method: "POST",
      headers: { "Content-Type": "application/json", "Idempotency-Key": request },
      body: JSON.stringify(body),
    })
  } catch {
    return { ok: false, status: null, code: "offline", message: "" }
  }
  const text = await res.text()
  let data: Record<string, unknown> | null = null
  try {
    data = text ? (JSON.parse(text) as Record<string, unknown>) : null
  } catch {
    /* below */
  }
  if (res.ok) return data ? { ok: true, body: data } : { ok: false, status: res.status, code: "not_json", message: "" }
  const raw = data?.error
  if (typeof raw === "string") {
    return { ok: false, status: res.status, code: raw, message: typeof data?.detail === "string" ? data.detail : raw, outcome: data?.outcome }
  }
  if (raw && typeof raw === "object") {
    const e = raw as { code?: unknown; message?: unknown; outcome?: unknown }
    const code = typeof e.code === "string" ? e.code : "http_" + res.status
    return { ok: false, status: res.status, code, message: typeof e.message === "string" ? e.message : code, outcome: e.outcome }
  }
  return { ok: false, status: res.status, code: "http_" + res.status, message: res.statusText }
}
