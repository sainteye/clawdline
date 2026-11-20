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
 * Only a pane's first fill hides the conversation behind a skeleton. Once the
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
