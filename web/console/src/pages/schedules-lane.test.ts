import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { laneOnScreen, laneOwesRead, scheduleRetryDelayMs, SCHEDULE_READ_DEADLINE_MS } from "./schedules-lane.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { HeldReads, READS_OUT_PER_TARGET } from "../poll.ts"

test("a section with a box and no hidden ancestor is on screen", () => {
  assert.equal(laneOnScreen({ laidOut: true, visibility: "visible" }), true)
})

test("the phone's hidden list pane takes the section off screen", () => {
  // responsive.css gives .pane-list `visibility: hidden` while the open
  // Session is the one showing, and visibility is inherited.
  assert.equal(laneOnScreen({ laidOut: true, visibility: "hidden" }), false)
})

test("a scroller taken out of the layout takes the section off screen", () => {
  assert.equal(laneOnScreen({ laidOut: false, visibility: "visible" }), false)
})

test("nothing drawn yet counts as on screen, so the first read is still made", () => {
  assert.equal(laneOnScreen(null), true)
})

const onScreen = { pageHidden: false, lastRefreshAt: null, now: 1_000_000, everyMs: 60_000, onScreen: () => true }

test("a lane that has never read owes a read", () => {
  assert.equal(laneOwesRead(onScreen), true)
})

test("a read a moment ago is left alone", () => {
  assert.equal(laneOwesRead({ ...onScreen, lastRefreshAt: onScreen.now - 59_999 }), false)
  assert.equal(laneOwesRead({ ...onScreen, lastRefreshAt: onScreen.now - 60_000 }), true)
})

test("nothing is read for a page nobody can see", () => {
  assert.equal(laneOwesRead({ ...onScreen, pageHidden: true }), false)
})

test("nothing is read while the section is off screen", () => {
  assert.equal(laneOwesRead({ ...onScreen, onScreen: () => false }), false)
})

test("the layout is only asked about once the reading is stale", () => {
  let asked = 0
  const box = () => {
    asked += 1
    return true
  }
  laneOwesRead({ ...onScreen, lastRefreshAt: onScreen.now - 1_000, onScreen: box })
  assert.equal(asked, 0, "a fresh reading answers without a style or layout read")
  laneOwesRead({ ...onScreen, lastRefreshAt: onScreen.now - 61_000, onScreen: box })
  assert.equal(asked, 1)
})

test("the lane alone reads again only two ticks after a failed first read", () => {
  // The first read starts a moment after the lane's interval does, so the
  // tick one minute in finds the failed read 59.9 s old and leaves it alone.
  assert.equal(laneOwesRead({ pageHidden: false, lastRefreshAt: 1_000_100, now: 1_060_000, everyMs: 60_000, onScreen: () => true }), false)
})

test("a failed schedule read is made again after 5, 10, 20, 40 seconds, then at the lane's pace", () => {
  assert.deepEqual([1, 2, 3, 4, 5, 6, 40].map((n) => scheduleRetryDelayMs(n)), [5_000, 10_000, 20_000, 40_000, 60_000, 60_000, 60_000])
  assert.equal(scheduleRetryDelayMs(0), 5_000)
})

// ---- the section's read loop, on one clock ----
//
// `schedules.tsx` needs a DOM, so its loop is restated here from the same
// parts it uses: a wait of SCHEDULE_READ_DEADLINE_MS on each read through
// `HeldReads`, `scheduleRetryDelayMs` after a failure, the lane's minute after
// an answer. What this proves is the parts' arithmetic, not the page's wiring.

function fakeClock() {
  let now = 0
  let seq = 0
  const timers = new Map<number, { at: number; fn: () => void }>()
  const flush = () => new Promise((r) => setImmediate(r))
  const set = (fn: () => void, ms: number): unknown => { timers.set(++seq, { at: now + ms, fn }); return seq }
  return {
    now: () => now,
    set,
    clear: (handle: unknown) => { timers.delete(handle as number) },
    async advance(ms: number) {
      const end = now + ms
      for (;;) {
        let next: number | null = null
        for (const [id, t] of timers) {
          if (t.at > end) continue
          if (next === null || t.at < timers.get(next)!.at) next = id
        }
        if (next === null) break
        const t = timers.get(next)!
        timers.delete(next)
        now = t.at
        t.fn()
        await flush()
      }
      now = end
      await flush()
    },
  }
}

function section(clock: ReturnType<typeof fakeClock>, machineRead: () => Promise<number>) {
  const reads = new HeldReads<number>({ deadlineMs: SCHEDULE_READ_DEADLINE_MS, now: clock.now, setTimer: clock.set, clearTimer: clock.clear })
  let out = 0, most = 0, started = 0, failed = 0
  let shown: number | null = null
  let state = "loading"
  let next: unknown = null
  const start = () => {
    started++
    most = Math.max(most, ++out)
    const done = () => { out-- }
    const p = machineRead()
    p.then(done, done)
    return p
  }
  const answered = (value: number) => {
    shown = value
    state = "ready"
    failed = 0
    clock.clear(next)
    next = clock.set(refresh, 60_000)
  }
  function refresh(): void {
    reads.read("machine", start, answered).then(answered, () => {
      state = "failed"
      next = clock.set(refresh, scheduleRetryDelayMs(++failed))
    })
  }
  refresh()
  return { shown: () => shown, state: () => state, most: () => most, started: () => started }
}

test("a machine whose schedules always take 25 s to read has them listed, not 'could not be read' for good", async () => {
  const clock = fakeClock()
  let n = 0
  const s = section(clock, () => new Promise<number>((resolve) => clock.set(() => resolve(++n), 25_000)))
  await clock.advance(21_000)
  assert.equal(s.state(), "failed", "the wait still ends at 20 s")
  await clock.advance(5_000)
  assert.equal(s.shown(), 1)
  assert.equal(s.state(), "ready")
  await clock.advance(274_000)
  assert.ok((s.shown() ?? 0) >= 3, "five minutes in, the list has kept being read: " + s.shown())
  assert.equal(s.most(), 1)
})

test("a machine that does not answer for five minutes has one schedule read out at a time, through every retry", async () => {
  // The relay ends a read nobody answered after its own minute.
  const clock = fakeClock()
  const s = section(clock, () => new Promise<number>((_, reject) => clock.set(() => reject(new Error("cloud_read_timeout")), 60_000)))
  await clock.advance(300_000)
  assert.ok(s.most() <= READS_OUT_PER_TARGET, "most schedule reads out at once: " + s.most() + ", started: " + s.started())
  assert.equal(s.shown(), null)
  assert.equal(s.state(), "failed")
})
