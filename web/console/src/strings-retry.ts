/*
 * A second chance at the string catalog, and a third.
 *
 * The catalog is one fetch at boot and a failure was final: `loadStrings`
 * swallowed it and left the built-in English in place for the life of the
 * page. That is the right call for the first paint — a console that refused to
 * start over a translation would be worse than one that starts in English —
 * and the wrong call a second later, on the device where the first fetch is
 * most likely to lose. On 2026-09-20 a reader on 5G had this console in
 * Chinese at 16:06 and in English at 16:13 having done nothing but reopen it:
 * one fetch had lost, and nothing was ever going to ask again. Nor could they
 * have known that reloading was the cure.
 *
 * So the first attempt still decides the first paint, and the ones after it
 * run behind the page and put the words in when they arrive. The delays start
 * short, because a blip is usually over in seconds, and end a quarter of an
 * hour later rather than never, because a page added to a home screen outlives
 * the tunnel it was opened through.
 *
 * Nothing is imported at run time, so `node --test` loads this file as it is.
 */

/** How long to wait before each further attempt, in milliseconds. */
export const RETRY_DELAYS_MS: readonly number[] = [2_000, 5_000, 15_000, 60_000, 300_000, 900_000]

/** A pause. The default is the clock; a test hands in its own. */
export type Sleep = (ms: number) => Promise<void>

const pause: Sleep = (ms) => new Promise((done) => setTimeout(done, ms))

/**
 * `attempt` again after each delay, until one of them resolves.
 *
 * Resolves true when an attempt succeeded and `took` was called with what it
 * returned, false when every attempt failed. It never rejects: this runs
 * behind a page that is already usable, and the only thing a caller could do
 * with the rejection is try again, which is what this is.
 */
export async function retrying<T>(
  attempt: () => Promise<T>,
  took: (value: T) => void,
  options: { delays?: readonly number[]; sleep?: Sleep } = {},
): Promise<boolean> {
  const delays = options.delays ?? RETRY_DELAYS_MS
  const sleep = options.sleep ?? pause
  for (const delay of delays) {
    await sleep(delay)
    try {
      took(await attempt())
      return true
    } catch {
      /* the next delay */
    }
  }
  return false
}
