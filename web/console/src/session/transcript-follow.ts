/*
 * When the transcript pane reads, and how much.
 *
 * It used to read the newest 200 entries every 4 seconds, and every second
 * while a card was on its way, whether or not anything had been written. The
 * session's own row already says when its record last grew (`activity.at`) and
 * what it is doing (`state`), and the row arrives on the stream; so the pane
 * reads when one of those changes, asks only for what was appended since its
 * last read (`after=`, the page's `nextAfter`), and keeps a slower read of the
 * whole page as a safety net.
 *
 * - **Change.** The open row's `activity.at` or `state` moved: read what was
 *   appended, now, and once more `TRAIL_MS` after the last such change — the
 *   time is in whole seconds, so a second write in the same second as the one
 *   read moves nothing.
 * - **Safety.** Every `SAFETY_MS` the whole page is read: it catches what no
 *   row said, and it is what refreshes what an incremental read never revisits
 *   (a picture that expired, a row whose `activity` is unknown).
 * - **A card on its way.** Every `FOLLOW_MS` for the first `FOLLOW_FAST_MS`,
 *   then every `FOLLOW_LATER_MS`, for at most `FOLLOW_WINDOW_MS`
 *   after the newest send, then back to the safety pace; the card's own states
 *   and lifetime are `pending.ts`'s and do not change.
 * - **No row time.** A row with no `activity.at` cannot say when it grew, so
 *   it is read on the old 4-second pace.
 * - **Failure.** A failed read waits `BACKOFF.firstMs`, doubling, up to
 *   `BACKOFF.maxMs`.
 *
 * A daemon whose page carries no `nextAfter` is read whole every time, as
 * before. Nothing here is imported at run time, so `node --test` loads it.
 *
 * The bounds are registered in `internal/domain/capacity`
 * (`console.transcript_*`).
 */
import type { TranscriptPage } from "@clawdline/contract"

/** The whole page, read on this pace whatever the row says. */
export const SAFETY_MS = 30_000
/** While a card is on its way. */
export const FOLLOW_MS = 2_000
/** How long after the newest send the following pace holds. */
export const FOLLOW_WINDOW_MS = 90_000
/**
 * The first stretch after a send, read every `FOLLOW_MS`; the rest of the
 * window is read every `FOLLOW_LATER_MS`. A reply usually starts inside it;
 * one that has not is a turn at work, whose row moves as it writes. At two
 * seconds the whole window was 38 reads in 75 s on a phone.
 */
export const FOLLOW_FAST_MS = 10_000
export const FOLLOW_LATER_MS = 4_000
/** A row that cannot say when its record grew is read on the old pace. */
export const BLIND_MS = 4_000
/** One more read this long after the last change the row showed. */
export const TRAIL_MS = 2_000
export const BACKOFF = { firstMs: 2_000, maxMs: 30_000 }
/**
 * Entries one held page may grow to by appended reads before the next read is
 * a whole one, which starts it again at the newest `LIMIT`. The route's own
 * ceiling for one page.
 */
export const MERGED_ROWS_LIMIT = 1_000

/** The pace of the pane's own timer. */
export function transcriptPace(o: { following: boolean; newestSendAt: number; now: number; rowTimed: boolean }): number {
  if (followingNow(o)) return o.now - o.newestSendAt < FOLLOW_FAST_MS ? FOLLOW_MS : FOLLOW_LATER_MS
  return o.rowTimed ? SAFETY_MS : BLIND_MS
}

/** Whether the pane is inside the window after its newest send. */
export function followingNow(o: { following: boolean; newestSendAt: number; now: number }): boolean {
  return o.following && o.now - o.newestSendAt < FOLLOW_WINDOW_MS
}

/**
 * Whether a read may ask only for what was appended: there is a held page that
 * said where it ended, and this read is not the safety read of the whole page.
 * The fast reads while a card is on its way are appended reads too.
 */
export function appendedReadFrom(held: TranscriptPage | null, why: string, fast: boolean): number | null {
  if (!held || held.evidence !== "transcript" || typeof held.nextAfter !== "number") return null
  if (why === "timer" && !fast) return null
  if (why === "start" || why === "retry") return null
  return held.nextAfter
}

/**
 * The held page with an appended read added at its newest end, or null when the
 * pair cannot be joined and the page must be read whole: the appended read is
 * not a transcript page, says no new end, or would grow the page past
 * `MERGED_ROWS_LIMIT`. An appended read with nothing in it returns the held
 * page itself, so nothing is drawn again.
 */
export function joinAppended(held: TranscriptPage, appended: TranscriptPage): TranscriptPage | null {
  if (appended.evidence !== "transcript" || typeof appended.nextAfter !== "number") return null
  if (appended.entries.length === 0 && appended.nextAfter === held.nextAfter && appended.signature === held.signature) {
    return held
  }
  const fresh = appended.entries.slice(overlap(held.entries, appended.entries))
  if (held.entries.length + fresh.length > MERGED_ROWS_LIMIT) return null
  return {
    ...held,
    entries: fresh.length ? [...held.entries, ...fresh] : held.entries,
    signature: appended.signature,
    nextAfter: appended.nextAfter,
  }
}

type Row = TranscriptPage["entries"][number]

/**
 * How many of the appended entries the held page already ends with. A whole
 * read can include a last row the writer had not finished with a newline yet,
 * which the daemon's `nextAfter` leaves out and the next appended read returns
 * again; it is the same row, and is shown once. The comparison is
 * `history.ts`'s `joinTranscript`'s.
 */
function overlap(held: readonly Row[], appended: readonly Row[]): number {
  const same = (a: Row, b: Row) =>
    a.role === b.role && a.text === b.text && a.tool === b.tool && a.at === b.at && a.source === b.source
  for (let n = Math.min(held.length, appended.length); n > 0; n--) {
    let matches = true
    for (let i = 0; i < n && matches; i++) matches = same(held[held.length - n + i], appended[i])
    if (matches) return n
  }
  return 0
}
