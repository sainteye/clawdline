/**
 * Waits, one event-loop turn at a time, until `done()` holds or `ms` of real time pass.
 *
 * For tests whose work finishes off the event loop (Web Crypto sealing, verifying and
 * digesting): a fixed number of turns or a fixed delay is enough on a quiet machine and
 * not on a loaded CI runner. The bound is the real clock, `Date.now()`, and each turn is
 * a `setImmediate`; tests that mock `setTimeout` with `t.mock.timers` mock neither. It
 * does not fail at the deadline: the assertion that follows says what did not happen.
 * An async `done` can do a round of work, such as answering requests, before it checks.
 */
export async function until(done: () => boolean | Promise<boolean>, ms = 5_000): Promise<void> {
  const deadline = Date.now() + ms
  while (!await done() && Date.now() < deadline) await new Promise<void>((resolve) => setImmediate(resolve))
}

/** Reports whether `promise` has settled, for `until` to wait on. */
export function settled(promise: Promise<unknown>): () => boolean {
  let done = false
  promise.then(() => { done = true }, () => { done = true })
  return () => done
}
