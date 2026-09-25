import assert from "node:assert/strict"
import test, { mock } from "node:test"
import { RefusalError, TransportError } from "@clawdline/core/refusal"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { OneRead, readFailureReason, TODO_REFRESH_MS, todoHeaderState, watchTodoRefresh, type RefreshEnvironment } from "./todo-refresh.ts"

test("the header says loading only while there is no answer at all", () => {
  assert.equal(todoHeaderState(false, false), "loading")
  // The 2026-09-25 bug: a failed first read left the header on "loading".
  assert.equal(todoHeaderState(false, true), "failed")
  assert.equal(todoHeaderState(true, true), "stale")
  assert.equal(todoHeaderState(true, false), "loaded")
})

/** A page whose timers are the test's and whose visibility and focus the test sets. */
function fakePage() {
  const visible = { now: true }
  const onVisible: (() => void)[] = []
  const onFocus: (() => void)[] = []
  const env: RefreshEnvironment = {
    setInterval: (run, ms) => setInterval(run, ms),
    clearInterval: (handle) => clearInterval(handle as ReturnType<typeof setInterval>),
    visible: () => visible.now,
    onVisible: (run) => { onVisible.push(run); return () => onVisible.splice(onVisible.indexOf(run), 1) },
    onFocus: (run) => { onFocus.push(run); return () => onFocus.splice(onFocus.indexOf(run), 1) },
  }
  return { env, visible, onVisible, onFocus }
}

test("an open Session page asks again every fifteen seconds while visible, and never while hidden", (t) => {
  t.mock.timers.enable({ apis: ["setInterval"] })
  const page = fakePage()
  let asks = 0
  const stop = watchTodoRefresh(() => { asks++ }, page.env)
  assert.equal(TODO_REFRESH_MS, 15_000)
  t.mock.timers.tick(TODO_REFRESH_MS - 1)
  assert.equal(asks, 0, "asked before fifteen seconds")
  t.mock.timers.tick(1)
  assert.equal(asks, 1)
  t.mock.timers.tick(TODO_REFRESH_MS * 3)
  assert.equal(asks, 4)
  page.visible.now = false
  t.mock.timers.tick(TODO_REFRESH_MS * 4)
  assert.equal(asks, 4, "a hidden page asked the relay")
  // Coming back asks at once, and the ticks resume.
  page.visible.now = true
  page.onVisible.forEach((run) => run())
  assert.equal(asks, 5)
  page.onFocus.forEach((run) => run())
  assert.equal(asks, 6)
  t.mock.timers.tick(TODO_REFRESH_MS)
  assert.equal(asks, 7)
  stop()
  t.mock.timers.tick(TODO_REFRESH_MS * 10)
  assert.equal(asks, 7, "a closed Session page kept asking")
  assert.equal(page.onVisible.length + page.onFocus.length, 0, "listeners outlived the page")
})

/** A read the test finishes by hand. */
function handRead() {
  const pending: { resolve: () => void; reject: (e: unknown) => void }[] = []
  let started = 0
  const read = () => new Promise<void>((resolve, reject) => { started++; pending.push({ resolve, reject }) })
  return { read, pending, get started() { return started } }
}

const settle = () => new Promise((resolve) => setImmediate(resolve))

test("never two reads in flight: refreshes join the one in flight", async () => {
  const hand = handRead()
  const one = new OneRead(hand.read)
  const first = one.ask()
  const joined = one.ask()
  one.ask()
  assert.equal(hand.started, 1)
  assert.equal(one.reading, true)
  assert.equal(first, joined)
  hand.pending[0].resolve()
  await first
  assert.equal(one.reading, false)
  assert.equal(hand.started, 1, "a joined refresh started a read of its own")
  // After it ends, the next ask is a new read.
  const next = one.ask()
  assert.equal(hand.started, 2)
  hand.pending[1].resolve()
  await next
})

test("an ask after a change gets exactly one more read, after the one in flight", async () => {
  const hand = handRead()
  const one = new OneRead(hand.read)
  const flight = one.ask()
  one.ask(true)
  one.ask(true)
  assert.equal(hand.started, 1, "a fresh ask started a second read in flight")
  hand.pending[0].resolve()
  await settle()
  assert.equal(hand.started, 2, "the answer from before the change was the last word")
  hand.pending[1].resolve()
  await flight
  assert.equal(hand.started, 2)
})

test("a read that throws does not strand the next one", async () => {
  const hand = handRead()
  const one = new OneRead(hand.read)
  const flight = one.ask()
  one.ask(true)
  hand.pending[0].reject(new Error("the relay connection dropped"))
  await settle()
  assert.equal(hand.started, 2)
  hand.pending[1].resolve()
  await flight
  assert.equal(one.reading, false)
})

test("a refresh on the fifteen-second tick joins a slow read rather than piling up", async (t) => {
  t.mock.timers.enable({ apis: ["setInterval"] })
  const page = fakePage()
  const hand = handRead()
  const one = new OneRead(hand.read)
  const stop = watchTodoRefresh(() => { void one.ask() }, page.env)
  t.mock.timers.tick(TODO_REFRESH_MS)
  t.mock.timers.tick(TODO_REFRESH_MS)
  t.mock.timers.tick(TODO_REFRESH_MS)
  assert.equal(hand.started, 1, "three ticks over a slow read made more than one read")
  hand.pending[0].resolve()
  await settle()
  t.mock.timers.tick(TODO_REFRESH_MS)
  assert.equal(hand.started, 2)
  hand.pending[1].resolve()
  stop()
  mock.timers.reset()
})

test("the failure's reason names the refusal's code, or what the transport said underneath", () => {
  const refused = new RefusalError(404, { error: "session_not_found", detail: "no such terminal" })
  assert.equal(readFailureReason(refused), "session_not_found")
  const abandoned = Object.assign(new Error("cloud_read_abandoned: this page stopped waiting for the machine's work.v2.session-todos answer"),
    { name: "AbortError" })
  const lost = new TransportError("GET /v1/work/v2/session-todos/%251 did not complete", abandoned)
  assert.match(readFailureReason(lost), /did not complete \(cloud_read_abandoned: /)
  assert.equal(readFailureReason(new TransportError("x answered 502 with no refusal in it")), "x answered 502 with no refusal in it")
})
