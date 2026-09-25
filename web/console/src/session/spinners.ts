import * as L from "../legacy/bridge.js"

/**
 * Every "on its way" line's spinner, on the page's one clock: the transcript's
 * pending cards and working line (`Transcript.tsx`) and a waiting card's answer
 * (`Waiting.tsx`).
 *
 * One list, because the clock keeps one (`setOptimisticSpinners` replaces it
 * on every call): two places registering their own would each take the other's
 * spinners off the clock. Each is painted once now, so it has its size before
 * the clock's next tick.
 */
export function turnPendingSpinners(): void {
  const spinners = [
    ...document.querySelectorAll<HTMLCanvasElement>("#tx .entry.pending canvas.spin, #tx .tx-working canvas.spin, #waiting .pending-state canvas.spin"),
  ]
  for (const canvas of spinners) L.paintSpinner(canvas)
  L.registerPendingSpinners(spinners)
}
