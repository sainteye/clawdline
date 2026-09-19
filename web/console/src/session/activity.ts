/*
 * When each session last moved, as this page saw it.
 *
 * The list's order puts the session that moved most recently first within its
 * state, and the row on the wire carries no time that follows a session's
 * activity. Measured against a live daemon before this was written:
 * `closeability.activity_generation` is the Swift app's per-terminal turn
 * counter, read from its store, and did not change on any row while those
 * sessions worked — and a counter of one terminal says nothing against another's.
 * `closeability.observed_at` is the time of the reading, the same on every row.
 * `work_since` is absent unless a session declared its state. `shells[].at`
 * moves only while a background command prints, and falls back when one ends.
 *
 * What does change when a session does something is the row itself: its state,
 * a new question on a waiting one, a background command printing or ending, a
 * declaration. So this page keeps its own clock: the moment it first saw a
 * row's movement differ from the last time it looked, and "now" for every row
 * that is working, which is moving by definition.
 *
 * What that costs is said here rather than discovered: a row this browser has
 * never watched move has no time (zero, after every row that has one) until it
 * does. The clock is kept in `localStorage`, so a reload, or reopening the page
 * on the same browser, carries on from where it was rather than starting over;
 * another browser starts empty.
 */
import type { SessionRow } from "@clawdline/contract"

/** Where the clock is kept between page loads. Either call may throw; neither is needed for the list to work. */
export interface ActivityStore {
  read(): string | null
  write(value: string): void
}

/** How long a row that has not been seen is remembered. */
const FORGET_MS = 7 * 24 * 60 * 60 * 1000
/** The most rows remembered; the ones seen longest ago go first. */
const KEPT = 400

interface Seen {
  /** `movementOf` when last looked at. */
  sig: string
  /** When the row was last seen moving, in milliseconds; 0 when never. */
  at: number
  /** When the row was last in a list at all, for forgetting it. */
  seen: number
}

/**
 * Everything on a row that changes because the session did something, and
 * nothing that changes on its own: not the reading's time, not the working
 * line's own clock (a working row is moving whatever its line says).
 */
export function movementOf(row: SessionRow): string {
  let shell = 0
  for (const s of row.shells ?? []) shell = Math.max(shell, s.at || 0)
  return [
    row.state,
    row.work_state,
    row.work_since ?? 0,
    row.closeability?.activity_generation ?? 0,
    shell,
    // On a waiting row `line` is the menu's revision: a new question is movement.
    row.state === "waiting" ? row.line ?? "" : "",
  ].join("\u0001")
}

export class ActivityClock {
  private rows = new Map<string, Seen>()
  /** Whether a list has been looked at yet: a row that turns up after that is new, and new is movement. */
  private primed = false
  // A plain field rather than a constructor parameter property, which is not
  // syntax `node --test` can strip.
  private readonly store: ActivityStore | null

  constructor(store: ActivityStore | null = null) {
    this.store = store
    this.load()
  }

  /** When this row last moved, in milliseconds; 0 when this page has never seen it move. */
  movedAt(id: string): number {
    return this.rows.get(id)?.at ?? 0
  }

  /** Look at a list of rows at time `now` (milliseconds). */
  observe(rows: readonly SessionRow[], now: number): void {
    let changed = false
    for (const row of rows) {
      const sig = movementOf(row)
      const was = this.rows.get(row.id)
      if (row.state === "working") {
        // Every working row gets the same `now` in one look, so they tie on
        // time and keep their order by title.
        if (!was || was.sig !== sig) changed = true
        this.rows.set(row.id, { sig, at: now, seen: now })
      } else if (!was) {
        // The first list after a load holds rows whose history this page did
        // not see; a row that turns up in a later one has just appeared.
        this.rows.set(row.id, { sig, at: this.primed ? now : 0, seen: now })
        changed = true
      } else if (was.sig !== sig) {
        this.rows.set(row.id, { sig, at: now, seen: now })
        changed = true
      } else {
        was.seen = now
      }
    }
    if (rows.length) this.primed = true
    // Written when something moved, not on every look: a working row's time
    // changes on every draw, and what is kept is only a starting point.
    if (changed) this.save(now)
  }

  private load(): void {
    let raw: string | null = null
    try {
      raw = this.store?.read() ?? null
    } catch {
      return
    }
    if (!raw) return
    try {
      const parsed: unknown = JSON.parse(raw)
      if (!parsed || typeof parsed !== "object") return
      const rows = (parsed as { rows?: unknown }).rows
      if (!rows || typeof rows !== "object") return
      for (const [id, value] of Object.entries(rows)) {
        if (!Array.isArray(value) || value.length !== 3) continue
        const [sig, at, seen] = value as unknown[]
        if (typeof sig !== "string" || typeof at !== "number" || typeof seen !== "number") continue
        this.rows.set(id, { sig, at, seen })
      }
      // Rows remembered from before are rows whose history is known, so a
      // list that brings them back is not a first look.
      if (this.rows.size) this.primed = true
    } catch {
      /* somebody else's value, or a broken one: start empty */
    }
  }

  private save(now: number): void {
    if (!this.store) return
    const kept = [...this.rows.entries()]
      .filter(([, row]) => now - row.seen < FORGET_MS)
      .sort((a, b) => b[1].seen - a[1].seen)
      .slice(0, KEPT)
    this.rows = new Map(kept)
    const rows: Record<string, [string, number, number]> = {}
    for (const [id, row] of kept) rows[id] = [row.sig, row.at, row.seen]
    try {
      this.store.write(JSON.stringify({ v: 1, rows }))
    } catch {
      /* a private window or full storage: the clock still runs for this page */
    }
  }
}
