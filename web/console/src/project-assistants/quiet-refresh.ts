// @ts-expect-error -- Node's strip-types runner uses source extensions.
import {followSnapshotRoster, serialSnapshotRefresh} from "./snapshot-roster.ts"

/** A background list read that never queues behind itself.
 *
 * One request at a time: a timer or notice during a slow read coalesces into
 * one follow-up read instead of a second request. A hidden page does not read;
 * returning to the foreground reads at once. The timer is only a fallback for
 * the notices the caller forwards (a live roster change, an explicit action).
 * The returned function is that notice; it is ignored while the page is hidden. */
export function followQuietRefresh(load:()=>Promise<void>,signal:AbortSignal,refreshMs:number,document:EventTarget&{visibilityState:string},window:EventTarget):()=>void {
  const serial=serialSnapshotRefresh(()=>load(),signal)
  const trigger=followSnapshotRoster(()=>serial(),signal,refreshMs,document,window,{readOnEntry:true})
  return ()=>{if(!signal.aborted&&document.visibilityState!=="hidden")void trigger?.()}
}
