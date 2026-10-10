/*
 * Whether the schedules section is on somebody's screen, and whether its lane
 * owes a read.
 *
 * The section lives at the foot of the session list (`Sessions.tsx`,
 * `cloud/FleetSessionList.tsx`), and on a phone the list and the open Session
 * are one screen each: `legacy/responsive.css` gives `.pane-list` a
 * `visibility: hidden` while the Session is the one showing, and the list's own
 * scroller leaves the layout altogether in terminal mode. The rows this lane
 * draws are then on nobody's screen, and a Session left open for three minutes
 * was still reading `/v1/orchestrator/schedules` three times and `/v1/places`
 * once for them.
 *
 * The decision asks the stylesheet rather than repeating its breakpoint: a
 * `max-width` written here as well as there is a number that goes out of date
 * in one of the two places. It is asked of the element the section sits in
 * rather than of the section itself, because the section is `hidden` until its
 * first answer has something to draw — a lane that waited for the section to
 * be on screen would never make the read that unhides it.
 *
 * Nothing here is imported at run time, so `node --test` loads this file as it
 * is; `schedules.tsx` holds the lane.
 */

/** The parts of a laid-out page this decision reads, so a test can hand in its own. */
export interface LaneBox {
  /** The element has a box in the layout: false under a `display: none`. */
  laidOut: boolean
  /** The computed `visibility`, which is inherited, so an ancestor's counts. */
  visibility: string
}

/**
 * Whether a read made now would be drawn for somebody.
 *
 * A page with nothing drawn yet — no section in the document — is treated as
 * on screen: the first read is what draws it, and withholding that would be a
 * lane that never starts.
 */
export function laneOnScreen(box: LaneBox | null): boolean {
  if (!box) return true
  if (!box.laidOut) return false
  return box.visibility !== "hidden"
}

/**
 * Whether the lane owes a read now: the page is in view, the section is on
 * screen, and what it holds is at least `everyMs` old. A read a moment ago is
 * left alone, which is the rule the page's own `visibilitychange` has always
 * followed, and the staleness is checked before the layout is, so a window
 * being dragged does not ask the browser for a style on every event.
 */
export function laneOwesRead(o: {
  pageHidden: boolean
  lastRefreshAt: number | null
  now: number
  everyMs: number
  onScreen: () => boolean
}): boolean {
  if (o.pageHidden) return false
  if (o.lastRefreshAt !== null && o.now - o.lastRefreshAt < o.everyMs) return false
  return o.onScreen()
}
