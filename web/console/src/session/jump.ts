/*
 * When the transcript offers a way back to its newest end (`Transcript.tsx`,
 * `JumpToLatest`).
 *
 * A reader a little way up is reading and needs nothing; the button is for
 * the one who is a screen or more away, and so has lost where the
 * conversation is now. The one exception is news: something drawn at the
 * newest end while the reader was not there is offered as soon as they are
 * past the end at all, so a turn that lands below them is not missed.
 */

/** The same allowance `atBottom` gives: within this many pixels is at the end. */
export const JUMP_AT_END_PX = 40

export type ScrollBox = { scrollTop: number; scrollHeight: number; clientHeight: number }

/** How far the reader is from the newest end: the bottom, or the top when it reads newest first. */
export function fromNewest(box: ScrollBox, newestFirst: boolean): number {
  const d = newestFirst ? box.scrollTop : box.scrollHeight - box.clientHeight - box.scrollTop
  return Math.max(0, d)
}

/** Whether the reader counts as at the newest end. */
export function atNewest(box: ScrollBox, newestFirst: boolean): boolean {
  return fromNewest(box, newestFirst) <= JUMP_AT_END_PX
}

/** Whether the button is offered: a screen away, or past the end with something new there. */
export function jumpOffered(box: ScrollBox, newestFirst: boolean, fresh: boolean): boolean {
  const d = fromNewest(box, newestFirst)
  if (d <= JUMP_AT_END_PX) return false
  return fresh || d >= box.clientHeight
}
