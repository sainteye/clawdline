/** Registered as console.transcript_fill_pages: one automatic fill, including a manual restart. */
export const HISTORY_FILL_PAGES = 32

/** A fill survives polling and resize; only an explicit request starts another batch. */
export class HistoryFill {
  reads = 0

  needed({ cursor, contentHeight, viewportHeight, loading, failed }: {
    cursor?: number
    contentHeight: number
    viewportHeight: number
    loading: boolean
    failed: boolean
  }): boolean {
    return !!cursor && viewportHeight > 0 && contentHeight <= viewportHeight &&
      !loading && !failed && this.reads < HISTORY_FILL_PAGES
  }

  begin(automatic: boolean): void {
    if (!automatic) this.reads = 0
    this.reads++
  }
}

/** A repeated or forward cursor cannot safely extend the history. */
export function historyAdvanced(before: number, nextBefore?: number): boolean {
  return !nextBefore || nextBefore > 0 && nextBefore < before
}

/**
 * The first readable entries replace the opening skeleton. Once the
 * conversation has been shown, later reads (load earlier, an automatic fill
 * after a resize, a restarted history) load in place under the inline "loading
 * earlier" control, so the page never flashes or drops the reader's focus.
 */
export function firstFillCover(): (filling: boolean) => boolean {
  let shown = false
  return (filling) => {
    if (!filling) shown = true
    return filling && !shown
  }
}

/**
 * Where the scroll box goes after an older page is drawn above the reader, so
 * the message they were reading stays where it was. A rubber-band past the top
 * reads as a negative offset; the place held is the top edge, not the bounce.
 */
export function heldReadingTop(anchor: { top: number; height: number }, height: number, newestFirst: boolean): number {
  const top = Math.max(0, anchor.top)
  return newestFirst ? top : top + height - anchor.height
}

/**
 * Writes that place. A finger fling, or the bounce at the top, keeps running
 * across a plain `scrollTop` write on iOS and lands where the fling was
 * heading: the first message of the page just loaded. After a touch, the box
 * stops scrolling for one frame, which ends the fling, and the place is
 * written again once scrolling is back. A mouse or trackpad never needs it,
 * and hiding a classic scrollbar for a frame would reflow the conversation.
 */
export function holdReadingPlace(el: HTMLElement, top: number, touched: boolean): void {
  el.scrollTop = top
  if (!touched) return
  el.style.overflowY = "hidden"
  requestAnimationFrame(() => {
    el.style.overflowY = ""
    el.scrollTop = top
  })
}

/** Registered as console.transcript_fling_gap_milliseconds: wheel events closer than this are one fling. */
export const FLING_GAP_MS = 120
/** Registered as console.transcript_fling_hold_milliseconds: the longest a fling is held after a page lands. */
export const FLING_HOLD_MS = 3000

/**
 * A trackpad or mouse fling to the top keeps sending wheel events after the
 * finger leaves: the older page lands, the reading place is restored, and the
 * rest of the fling would carry the reader up into the page just loaded. The
 * fling that was running when the page landed is swallowed until it pauses,
 * turns around, or runs past FLING_HOLD_MS; a slow scroll has no fling to hold.
 */
export class FlingGuard {
  private last = -Infinity
  private until = -Infinity

  /** The reading place was restored at `at`. */
  held(at: number): void {
    this.until = at - this.last < FLING_GAP_MS ? at + FLING_HOLD_MS : -Infinity
  }

  /** A wheel event at `at`; true when it belongs to the held fling and must not scroll. */
  swallow(at: number, towardOlder: boolean): boolean {
    const held = at < this.until && at - this.last < FLING_GAP_MS && towardOlder
    this.last = at
    if (!held) this.until = -Infinity
    return held
  }
}
