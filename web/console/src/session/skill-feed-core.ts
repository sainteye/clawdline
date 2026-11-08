/*
 * The Skill-receipt feed every Session view shares, without React or the
 * client so a test can drive it (skill-feed.ts wires it to both).
 *
 * The still state is read only for the conversations on screen: the open
 * Session and the list rows that show a role. They go to the machine as one
 * `conversation=` filtered read of `/v1/squad/events` (split at
 * `MAX_SQUAD_EVENT_CONVERSATIONS`, paged by `after`), which the daemon answers
 * from its (conversation_id, seq) index. The live poll stays one head-based
 * read of the whole feed after the cursor, so a receipt for any Session is
 * seen once and plays only when it arrived live (skill-use.ts). A
 * conversation shown later is read the same way, up to the live cursor; it
 * never asks per row and never replays.
 *
 * A machine that ignores the filter answers the whole log: a page names a
 * conversation that was not asked for, or the relay says it read the
 * unfiltered word (`unfiltered`). The feed then holds the whole still state
 * up to the cursor, so it marks every shown and every later conversation
 * read and never asks again: such a machine costs one read of its log per
 * page life, as it did before the filter.
 *
 * An ID the machine would refuse (`squadEventIDValid`) is never sent, so one
 * cannot refuse the read for every other Session on screen. A failed still
 * read only backs off; it leaves no gap in the live cursor, so the next live
 * receipts still play.
 */
import type { SquadReceipt } from "../pages/squad/model.js"
// @ts-expect-error -- Node loads the source extension in focused behavior tests.
import { MAX_SQUAD_EVENT_CONVERSATIONS, squadEventIDValid } from "./skill-event-bounds.ts"
// @ts-expect-error -- Node loads the source extension in focused behavior tests.
import { SkillLedger, type SkillMoment } from "./skill-use.ts"

export const POLL_MS = 3000
export const MAX_BACKOFF_MS = 30000
/** Rows per page of the filtered still-state read; the daemon's page bound. */
export const STILL_PAGE_ROWS = 100
/** Rows per page of the live poll. */
export const LIVE_PAGE_ROWS = 50

export type FeedRead = (path: string) => Promise<{ status: number; value: unknown }>

export interface FeedClock {
  set(run: () => void, ms: number): unknown
  clear(handle: unknown): void
}

interface WireEvent { seq: number; receipt_id: string; snapshot_id: string; conversation_id: string; definition_id: string; scope_id: string; skill_id: string; skill_version: string; status: SquadReceipt["status"]; at: number }
/** `unfiltered` is the relay reader's word for a `conversation=` read it answered with the whole log. */
interface WirePage { events: WireEvent[]; next_after: number; has_more: boolean; unfiltered?: boolean }

function receiptOf(entry: WireEvent): SquadReceipt {
  return { seq: entry.seq, receiptId: entry.receipt_id, snapshotId: entry.snapshot_id, conversationId: entry.conversation_id,
    definitionId: entry.definition_id, scopeId: entry.scope_id, skillId: entry.skill_id, skillVersion: entry.skill_version, status: entry.status, at: entry.at }
}

function pageOf(value: unknown): WirePage | null {
  const page = value as Partial<WirePage> | null
  return page && Array.isArray(page.events) && typeof page.next_after === "number" ? page as WirePage : null
}

export class SkillFeed {
  ledger = new SkillLedger()
  /** Sequences that arrived live in this page's life. */
  readonly liveSeqs = new Set<number>()

  private readonly shown = new Map<string, number>()
  private loaded = new Set<string>()
  private generation = 0
  private running = false
  private busy = false
  private timer: unknown = null
  private live = true
  private delay = POLL_MS
  /** The machine answered a filtered read with its whole log: every conversation is read. */
  private unfiltered = false

  private readonly read: FeedRead
  private readonly changed: () => void
  private readonly moment: (moment: SkillMoment) => void
  private readonly clock: FeedClock

  constructor(read: FeedRead, changed: () => void, moment: (moment: SkillMoment) => void,
    clock: FeedClock = { set: (run, ms) => setTimeout(run, ms), clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>) }) {
    this.read = read; this.changed = changed; this.moment = moment; this.clock = clock
  }

  /** Marks a conversation on screen until the returned function is called. */
  show(conversation: string): () => void {
    this.shown.set(conversation, (this.shown.get(conversation) ?? 0) + 1)
    if (this.unfiltered) this.loaded.add(conversation)
    if (!this.loaded.has(conversation)) this.wake()
    let done = false
    return () => {
      if (done) return
      done = true
      const left = (this.shown.get(conversation) ?? 1) - 1
      if (left > 0) this.shown.set(conversation, left)
      else this.shown.delete(conversation)
    }
  }

  start(): void {
    if (this.running) return
    this.running = true
    this.generation++
    this.ledger = new SkillLedger()
    this.loaded = new Set()
    this.unfiltered = false
    this.live = true
    this.delay = POLL_MS
    void this.poll(this.generation)
  }

  stop(): void {
    this.running = false
    this.generation++
    this.busy = false
    if (this.timer !== null) this.clock.clear(this.timer)
    this.timer = null
  }

  /** A newly shown conversation is read now rather than at the next poll. */
  private wake(): void {
    if (!this.running || this.busy) return
    if (this.timer !== null) this.clock.clear(this.timer)
    this.timer = null
    void this.poll(this.generation)
  }

  private later(mine: number, ms: number): void {
    if (mine !== this.generation) return
    this.busy = false
    this.timer = this.clock.set(() => { this.timer = null; void this.poll(mine) }, ms)
  }

  private async poll(mine: number): Promise<void> {
    if (mine !== this.generation || this.busy) return
    this.busy = true
    try {
      if (!this.ledger.baselined) {
        const head = await this.read("/v1/squad/events/head")
        if (mine !== this.generation) return
        const seq = (head.value as { seq?: unknown } | null)?.seq
        // An older machine has no squad events: nothing to show, nothing to poll.
        if (typeof seq !== "number") {
          if (head.status === 404 || head.status === 501) { this.busy = false; return }
          throw new Error("head")
        }
        this.ledger.baseline(seq)
        // The live poll starts at the head; what came before is still state.
        this.ledger.advance(seq)
      }
      const pending: string[] = []
      for (const conversation of this.shown.keys()) {
        if (this.loaded.has(conversation)) continue
        // Nothing to ask for: the whole log is held, or the machine would refuse this ID.
        if (this.unfiltered || !squadEventIDValid(conversation)) { this.loaded.add(conversation); continue }
        if (pending.length < MAX_SQUAD_EVENT_CONVERSATIONS) pending.push(conversation)
      }
      if (pending.length) {
        try {
          await this.readStill(mine, pending)
        } catch {
          // No live receipt was skipped, so `live` stays as it was.
          if (mine !== this.generation) return
          this.delay = Math.min(this.delay * 2, MAX_BACKOFF_MS)
          this.later(mine, this.delay)
          return
        }
        if (mine !== this.generation) return
        for (const conversation of pending) this.loaded.add(conversation)
        if (this.unfiltered) for (const conversation of this.shown.keys()) this.loaded.add(conversation)
        this.delay = POLL_MS
        this.changed()
        this.later(mine, 0)
        return
      }
      const reply = await this.read("/v1/squad/events?after=" + this.ledger.after + "&limit=" + LIVE_PAGE_ROWS)
      if (mine !== this.generation) return
      const page = pageOf(reply.value)
      if (!page) throw new Error("events")
      let moved = false
      for (const entry of page.events) {
        const moment = this.ledger.take(receiptOf(entry), this.live)
        moved = true
        if (moment) {
          this.liveSeqs.add(moment.receipt.seq)
          try { this.moment(moment) } catch (error) { console.error(error) }
        }
      }
      this.ledger.advance(page.next_after)
      if (moved) this.changed()
      // Every page of a catch-up after a failure stays quiet, the last one too.
      if (!page.has_more) this.live = true
      this.delay = POLL_MS
      this.later(mine, page.has_more ? 0 : this.delay)
    } catch {
      if (mine !== this.generation) return
      this.live = false
      this.delay = Math.min(this.delay * 2, MAX_BACKOFF_MS)
      this.later(mine, this.delay)
    }
  }

  /**
   * The still state of `conversations` up to the live cursor, never played.
   * A receipt after the cursor is left for the live poll, which decides
   * whether it plays.
   */
  private async readStill(mine: number, conversations: string[]): Promise<void> {
    const until = this.ledger.after
    const asked = new Set(conversations)
    const query = conversations.map((conversation) => "conversation=" + encodeURIComponent(conversation)).join("&")
    let after = 0
    for (;;) {
      const reply = await this.read("/v1/squad/events?" + query + "&after=" + after + "&limit=" + STILL_PAGE_ROWS)
      if (mine !== this.generation) return
      const page = pageOf(reply.value)
      if (!page) throw new Error("events")
      if (page.unfiltered === true) this.unfiltered = true
      for (const entry of page.events) {
        if (!asked.has(entry.conversation_id)) this.unfiltered = true
        if (entry.seq > until) return
        this.ledger.record(receiptOf(entry))
      }
      if (!page.has_more || page.next_after <= after || page.next_after >= until) return
      after = page.next_after
    }
  }
}
