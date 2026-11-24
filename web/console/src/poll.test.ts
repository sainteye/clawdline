import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { HELD_READ_LOST_MS, HeldReads, Poller, READS_OUT_PER_TARGET, type PollState } from "./poll.ts"

/** A poller whose reads are settled by hand and whose timer is one slot. */
function harness() {
  const reads: { resolve: (v: number) => void; reject: (e: unknown) => void }[] = []
  let timer: (() => void) | null = null
  let timersSet = 0
  let state: PollState<number> | null = null
  const poller = new Poller<number>({
    read: () => new Promise<number>((resolve, reject) => reads.push({ resolve, reject })),
    intervalMs: 4000,
    classify: () => "unanswered",
    onChange: (s) => (state = s),
    now: () => 1000,
    setTimer: (fn) => {
      timersSet++
      timer = fn
      return fn
    },
    clearTimer: () => (timer = null),
  })
  const settle = () => new Promise((r) => setImmediate(r))
  return {
    poller,
    reads,
    settle,
    state: () => state!,
    timer: () => timer,
    timersSet: () => timersSet,
  }
}

test("a retry while a read is out does not start a second read", async () => {
  const h = harness()
  h.poller.start()
  assert.equal(h.reads.length, 1)
  assert.equal(h.state().reading, true)
  h.poller.retry()
  h.poller.retry()
  assert.equal(h.reads.length, 1, "one read out, however often retry is pressed")
  h.reads[0].reject(new Error("timeout"))
  await h.settle()
  assert.equal(h.state().reading, false)
  assert.equal(h.state().failures, 1)
  assert.equal(h.state().failingSince, 1000)
  h.poller.stop()
})

test("a retry cancels the scheduled read and asks now", async () => {
  const h = harness()
  h.poller.start()
  h.reads[0].reject(new Error("timeout"))
  await h.settle()
  assert.ok(h.timer(), "the next read is scheduled")
  h.poller.retry()
  assert.equal(h.timer(), null, "and cancelled by the retry")
  assert.equal(h.reads.length, 2)
  h.reads[1].resolve(7)
  await h.settle()
  assert.equal(h.state().data, 7)
  assert.equal(h.state().failures, 0, "a success ends the run")
  assert.equal(h.state().failingSince, null)
  assert.equal(h.state().failureKind, null)
  assert.equal(h.timersSet(), 2, "one timer per settled read, none left over")
  h.poller.stop()
})

test("a stopped poller neither reads on retry nor schedules", async () => {
  const h = harness()
  h.poller.start()
  h.poller.stop()
  h.reads[0].resolve(1)
  await h.settle()
  assert.equal(h.timer(), null)
  h.poller.retry()
  assert.equal(h.reads.length, 1)
})

/** The harness with a page that can be hidden, and the waits it asked for. */
function visibleHarness(options: { backoff?: { firstMs: number; maxMs: number } } = {}) {
  const reads: { why: string; resolve: (v: number) => void; reject: (e: unknown) => void }[] = []
  let timer: (() => void) | null = null
  const waits: number[] = []
  let hidden = false
  const heard = new Set<() => void>()
  let state: PollState<number> | null = null
  const poller = new Poller<number>({
    read: (why) => new Promise<number>((resolve, reject) => reads.push({ why, resolve, reject })),
    intervalMs: 30000,
    classify: () => "unanswered",
    onChange: (s) => (state = s),
    visibility: {
      hidden: () => hidden,
      subscribe(fn) {
        heard.add(fn)
        return () => heard.delete(fn)
      },
    },
    backoff: options.backoff,
    setTimer: (fn, ms) => {
      waits.push(ms)
      timer = fn
      return fn
    },
    clearTimer: () => (timer = null),
  })
  return {
    poller,
    reads,
    waits,
    heard,
    state: () => state!,
    timer: () => timer,
    setHidden(next: boolean) {
      hidden = next
      for (const fn of heard) fn()
    },
    /** Hidden without the event: the timer can still fire first. */
    setHiddenQuietly(next: boolean) {
      hidden = next
    },
    settle: () => new Promise((r) => setImmediate(r)),
  }
}

test("nothing is read while the page is hidden, and one read is made when it is seen", async () => {
  const h = visibleHarness()
  h.poller.start()
  h.reads[0].resolve(1)
  await h.settle()
  h.setHidden(true)
  assert.equal(h.timer(), null, "the scheduled read is not left running for a hidden page")
  h.setHidden(false)
  assert.equal(h.reads.length, 2, "one read on coming back")
  assert.equal(h.reads[1].why, "visible")
  h.setHidden(false)
  assert.equal(h.reads.length, 2, "and not one per event")
  h.poller.stop()
  assert.equal(h.heard.size, 0, "stop stops listening")
})

test("a read that comes due while hidden waits for the page to be seen", async () => {
  const h = visibleHarness()
  h.poller.start()
  h.reads[0].resolve(1)
  await h.settle()
  h.setHiddenQuietly(true)
  h.timer()!()
  assert.equal(h.reads.length, 1, "a tick due while hidden is not read")
  h.setHidden(false)
  assert.equal(h.reads.length, 2, "it is read when the page is seen")
  assert.equal(h.reads[1].why, "visible")
  h.poller.stop()
})

test("a poller started on a hidden page reads nothing until it is seen", () => {
  const h = visibleHarness()
  h.setHidden(true)
  h.poller.start()
  assert.equal(h.reads.length, 0)
  h.setHidden(false)
  assert.equal(h.reads.length, 1)
  assert.equal(h.reads[0].why, "visible")
  h.poller.stop()
})

test("a poke during a read reads once more when it settles; two pokes are still one", async () => {
  const h = visibleHarness()
  h.poller.start()
  h.poller.poke()
  h.poller.poke()
  assert.equal(h.reads.length, 1)
  h.reads[0].resolve(1)
  await h.settle()
  assert.equal(h.reads.length, 2, "the change it was poked for is read")
  assert.equal(h.reads[1].why, "poke")
  h.reads[1].resolve(2)
  await h.settle()
  assert.equal(h.reads.length, 2)
  assert.ok(h.timer(), "and the interval resumes after it")
  h.poller.poke()
  assert.equal(h.reads.length, 3, "a poke between reads reads now")
  assert.equal(h.timer(), null, "replacing the scheduled read")
  h.poller.stop()
})

test("failed reads back off 2, 4, 8 seconds up to 30, and a success returns to the interval", async () => {
  const h = visibleHarness({ backoff: { firstMs: 2000, maxMs: 30000 } })
  h.poller.start()
  for (let i = 0; i < 6; i++) {
    h.reads[i].reject(new Error("timeout"))
    await h.settle()
    h.timer()!()
  }
  assert.deepEqual(h.waits, [2000, 4000, 8000, 16000, 30000, 30000])
  h.reads[6].resolve(1)
  await h.settle()
  assert.equal(h.waits.at(-1), 30000)
  h.poller.setIntervalMs(2000)
  assert.equal(h.reads.length, 7, "a new pace is not a read of its own")
  assert.equal(h.waits.at(-1), 2000, "it moves the scheduled read")
  h.poller.stop()
})

/** A poller with a 20 s read deadline, both of its clocks driven by hand. */
function deadlineHarness(answers: ((n: number) => Promise<number>)) {
  let state: PollState<number> | null = null
  let pace: (() => void) | null = null
  let deadline: (() => void) | null = null
  let asked = 0
  let now = 1000
  const poller = new Poller<number>({
    read: () => answers(++asked),
    intervalMs: 30_000,
    deadlineMs: 20_000,
    classify: () => "unknown",
    onChange: (s) => (state = s),
    now: () => now,
    setTimer: (fn) => (pace = fn),
    clearTimer: () => (pace = null),
    setDeadline: (fn) => (deadline = fn),
    clearDeadline: () => (deadline = null),
  })
  const settle = () => new Promise((r) => setImmediate(r))
  return { poller, settle, state: () => state!, asked: () => asked,
    later: (ms: number) => { now += ms },
    tick: () => { const fn = pace!; pace = null; fn() },
    expire: () => { const fn = deadline!; deadline = null; fn() } }
}

test("a first read nobody answers ends at its deadline as unavailable, and once it is taken for lost the next answer restores it", async () => {
  // Read 1 never settles: the machine asked is silent. Read 2 answers.
  const h = deadlineHarness((n) => n === 1 ? new Promise<number>(() => {}) : Promise.resolve(42))
  h.poller.start()
  await h.settle()
  assert.equal(h.state().pending, true, "nothing has settled yet")
  h.expire()
  await h.settle()
  assert.equal(h.state().pending, false, "the deadline settles the first read: no endless loading")
  assert.equal(h.state().reading, false)
  assert.match(h.state().error ?? "", /not answered within 20000 ms/)
  assert.equal(h.state().failures, 1)
  assert.equal(h.state().data, null, "a failed read is not an empty answer")
  // The read is still out, so the next tick waits on it and asks nothing new.
  h.tick()
  await h.settle()
  assert.equal(h.asked(), 1, "one read out: the tick joined it")
  h.expire()
  await h.settle()
  assert.equal(h.state().failures, 2)
  // Out for longer than a read can be: taken for lost, and asked again.
  h.later(180_000)
  h.tick()
  await h.settle()
  assert.equal(h.asked(), 2)
  assert.equal(h.state().data, 42, "the next answer is shown without a reload")
  assert.equal(h.state().error, null)
  assert.equal(h.state().failures, 0)
})

test("a first read refused once is restored by the next answer", async () => {
  const h = deadlineHarness((n) => n === 1 ? Promise.reject(new Error("refused")) : Promise.resolve(7))
  h.poller.start()
  await h.settle()
  assert.equal(h.state().pending, false)
  assert.equal(h.state().error, "refused")
  h.tick()
  await h.settle()
  assert.equal(h.state().data, 7)
  assert.equal(h.state().error, null)
  assert.equal(h.state().failures, 0)
})

test("control: a read answered inside its deadline is the answer, and a late deadline changes nothing", async () => {
  const h = deadlineHarness(() => Promise.resolve(5))
  h.poller.start()
  await h.settle()
  assert.equal(h.state().data, 5)
  assert.equal(h.state().pending, false)
  assert.equal(h.state().error, null)
})

test("an answer that arrives after its deadline is shown, and ends the failure the deadline declared", async () => {
  let late: (v: number) => void = () => {}
  const h = deadlineHarness((n) => n === 1 ? new Promise<number>((resolve) => (late = resolve)) : new Promise<number>(() => {}))
  h.poller.start()
  await h.settle()
  h.expire()
  await h.settle()
  assert.equal(h.state().failures, 1)
  late(99)
  await h.settle()
  assert.equal(h.state().data, 99)
  assert.equal(h.state().error, null)
  assert.equal(h.state().failures, 0)
  assert.equal(h.state().failingSince, null)
  assert.equal(h.asked(), 1)
})

test("an answer that arrives while the next tick is waiting on the same read is that tick's answer", async () => {
  let late: (v: number) => void = () => {}
  const h = deadlineHarness(() => new Promise<number>((resolve) => (late = resolve)))
  h.poller.start()
  await h.settle()
  h.expire()
  await h.settle()
  h.tick()
  await h.settle()
  assert.equal(h.asked(), 1, "the second tick did not start a second read")
  assert.equal(h.state().reading, true)
  late(7)
  await h.settle()
  assert.equal(h.state().data, 7)
  assert.equal(h.state().reading, false)
  assert.equal(h.state().failures, 0)
})

test("an answer that arrives after the poller stopped is not shown", async () => {
  let late: (v: number) => void = () => {}
  const h = deadlineHarness(() => new Promise<number>((resolve) => (late = resolve)))
  h.poller.start()
  await h.settle()
  h.expire()
  await h.settle()
  h.poller.stop()
  late(99)
  await h.settle()
  assert.equal(h.state().data, null)
  assert.equal(h.state().failures, 1)
})

/** One clock for pacing, deadlines and the reads themselves. */
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

/** The quota footer's poller: read every 30 s, wait 20 s on each. */
function footer(clock: ReturnType<typeof fakeClock>, read: () => Promise<number>) {
  let state: PollState<number> | null = null
  let out = 0
  let most = 0
  let started = 0
  const poller = new Poller<number>({
    read: () => {
      started++
      most = Math.max(most, ++out)
      const done = () => { out-- }
      const p = read()
      p.then(done, done)
      return p
    },
    intervalMs: 30_000,
    deadlineMs: 20_000,
    classify: () => "unanswered",
    onChange: (s) => (state = s),
    now: clock.now,
    setTimer: clock.set,
    clearTimer: clock.clear,
    setDeadline: clock.set,
    clearDeadline: clock.clear,
  })
  return { poller, state: () => state!, most: () => most, started: () => started }
}

test("a machine whose reading always takes 25 s is shown its reading, not 'unavailable' for good", async () => {
  const clock = fakeClock()
  let n = 0
  const h = footer(clock, () => new Promise<number>((resolve) => clock.set(() => resolve(++n), 25_000)))
  h.poller.start()
  await clock.advance(21_000)
  assert.equal(h.state().pending, false, "the deadline still ends 'reading'")
  assert.equal(h.state().failures, 1)
  await clock.advance(5_000)
  assert.equal(h.state().data, 1, "the 25 s answer is shown when it arrives")
  assert.equal(h.state().error, null)
  assert.equal(h.state().failures, 0)
  await clock.advance(274_000)
  assert.ok((h.state().data ?? 0) >= 4, "five minutes in, readings have kept arriving: " + h.state().data)
  h.poller.stop()
})

test("a machine that does not answer for five minutes has one reading out at a time, not one per retry", async () => {
  // A reading nobody answers ends after the relay's own minute, twice: the
  // footer's read asks once more when the first way of asking fails.
  const clock = fakeClock()
  const h = footer(clock, () => new Promise<number>((_, reject) => clock.set(() => reject(new Error("cloud_read_timeout")), 120_000)))
  h.poller.start()
  await clock.advance(300_000)
  assert.ok(h.most() <= 1, "most readings out at once: " + h.most() + ", started: " + h.started())
  assert.equal(h.state().data, null)
  assert.ok(h.state().failures > 0)
  h.poller.stop()
})

test("a reading that never settles at all is asked again only once it is taken for lost", async () => {
  const clock = fakeClock()
  const h = footer(clock, () => new Promise<number>(() => {}))
  h.poller.start()
  await clock.advance(300_000)
  // 0 s, then the first tick at or after 180 s. Before, one every 50 s: 6.
  assert.ok(h.started() <= 2, "readings started in five minutes: " + h.started())
  h.poller.stop()
})

// ---- HeldReads (not in the tree before this change) ----

test("READS_OUT_PER_TARGET is what HeldReads keeps: asking a target again while its read is out starts nothing", async () => {
  const clock = fakeClock()
  const held = new HeldReads<number>({ deadlineMs: 20_000, now: clock.now, setTimer: clock.set, clearTimer: clock.clear })
  let started = 0
  const start = () => { started++; return new Promise<number>((resolve) => clock.set(() => resolve(started), 50_000)) }
  const first = held.read("a", start, () => {})
  const second = held.read("a", start, () => {})
  const other = held.read("b", start, () => {})
  assert.equal(started, 2, "one for each target")
  assert.equal(held.outstanding("a"), READS_OUT_PER_TARGET)
  first.catch(() => {}); second.catch(() => {}); other.catch(() => {})
  await clock.advance(60_000)
  assert.equal(held.outstanding("a"), 0)
})

test("only the newest wait hears a late answer; a wait that a newer one replaced hears nothing", async () => {
  const clock = fakeClock()
  const held = new HeldReads<number>({ deadlineMs: 20_000, now: clock.now, setTimer: clock.set, clearTimer: clock.clear })
  const heard: string[] = []
  const start = () => new Promise<number>((resolve) => clock.set(() => resolve(5), 50_000))
  {
    const wait = held.read("a", start, () => heard.push("old"))
    wait.catch(() => {})
    await clock.advance(25_000)
    await assert.rejects(wait, /not answered within 20000 ms/)
  }
  {
    const wait = held.read("a", start, () => heard.push("new"))
    wait.catch(() => {})
    await clock.advance(22_000)
    await assert.rejects(wait, /not answered within 20000 ms/)
  }
  await clock.advance(10_000)
  assert.deepEqual(heard, ["new"])
})

test("a read given up on as lost does not speak once another has replaced it", async () => {
  const clock = fakeClock()
  const held = new HeldReads<number>({ deadlineMs: 20_000, now: clock.now, setTimer: clock.set, clearTimer: clock.clear })
  const heard: number[] = []
  let started = 0
  const start = () => {
    const mine = ++started
    return new Promise<number>((resolve) => clock.set(() => resolve(mine), mine === 1 ? HELD_READ_LOST_MS + 30_000 : 5_000))
  }
  {
    const wait = held.read("a", start, (v) => heard.push(v))
    wait.catch(() => {})
    await clock.advance(HELD_READ_LOST_MS)
    await assert.rejects(wait, /not answered within 20000 ms/)
  }
  const second = held.read("a", start, (v) => heard.push(v))
  assert.equal(started, 2, "the lost read no longer holds the target")
  await clock.advance(60_000)
  assert.equal(await second, 2)
  assert.deepEqual(heard, [], "the lost read's answer, 30 s later, replaced nothing")
})

test("a read that fails after its deadline says nothing more, and the target is free again", async () => {
  const clock = fakeClock()
  const held = new HeldReads<number>({ deadlineMs: 20_000, now: clock.now, setTimer: clock.set, clearTimer: clock.clear })
  let heard = 0
  const start = () => new Promise<number>((_, reject) => clock.set(() => reject(new Error("refused")), 60_000))
  {
    const wait = held.read("a", start, () => heard++)
    wait.catch(() => {})
    await clock.advance(61_000)
    await assert.rejects(wait, /not answered within/)
  }
  assert.equal(heard, 0)
  assert.equal(held.outstanding("a"), 0)
})
