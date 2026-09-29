import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { MAX_BATCH_BYTES, MAX_QUEUED_BYTES, RENEW_MS, TerminalInputClient } from "./input-client.ts"

/** A clock that moves only when the test says so. */
class FakeClock {
  t = 0
  private timers: { at: number; fn: () => void; id: number }[] = []
  private ids = 0
  now = () => this.t
  setTimeout = (fn: () => void, ms: number) => {
    const id = ++this.ids
    this.timers.push({ at: this.t + ms, fn, id })
    return id
  }
  clearTimeout = (id: unknown) => {
    this.timers = this.timers.filter((x) => x.id !== id)
  }
  async advance(ms: number) {
    const end = this.t + ms
    for (;;) {
      this.timers.sort((a, b) => a.at - b.at)
      const next = this.timers[0]
      if (!next || next.at > end) break
      this.timers.shift()
      this.t = next.at
      next.fn()
      await settle()
    }
    this.t = end
    await settle()
  }
}

async function settle() {
  for (let i = 0; i < 5; i++) await new Promise((r) => setImmediate(r))
}

type Call = { path: string; body: Record<string, unknown>; keepalive?: boolean }
type Reply = { status: number; body: unknown } | "network"

/** A daemon that answers each request with whatever the test queued, or 200. */
class FakeDaemon {
  calls: Call[] = []
  replies: Reply[] = []
  epoch = 1
  post = async (path: string, body: unknown, opts?: { keepalive?: boolean }) => {
    const b = body as Record<string, unknown>
    this.calls.push({ path, body: b, keepalive: opts?.keepalive })
    const r = this.replies.shift()
    if (r === "network") throw new TypeError("Failed to fetch")
    if (r) return r
    if (path.endsWith("/control")) {
      return { status: 200, body: { held: true, epoch: this.epoch, applied_through: 0, holder: { name: "t", local: true, same_device: true, same_client: true } } }
    }
    return { status: 200, body: { applied_through: b.seq, duplicate: false } }
  }
  inputs() {
    return this.calls.filter((c) => c.path.endsWith("/input") || c.path.endsWith("/paste"))
  }
}

const bytes = (s: string) => new TextEncoder().encode(s)
const decoded = (c: Call) => (c.body.text as string) ?? atob(c.body.data as string)

async function holding() {
  const clock = new FakeClock()
  const daemon = new FakeDaemon()
  const input = new TerminalInputClient("t1", "tab-a", daemon, clock)
  assert.equal(await input.control("acquire"), "")
  // Keep the stream fresh unless a test says otherwise.
  return { clock, daemon, input }
}

test("keystrokes gathered within 8 ms go as one batch, numbered from 1", async () => {
  const { clock, daemon, input } = await holding()
  input.type(bytes("l"))
  input.type(bytes("s"))
  input.type(bytes("\r"))
  await clock.advance(8)
  const sent = daemon.inputs()
  assert.equal(sent.length, 1)
  assert.equal(sent[0].body.seq, 1)
  assert.equal(sent[0].body.epoch, 1)
  assert.equal(decoded(sent[0]), "ls\r")
  assert.equal(input.state.appliedThrough, 1)
})

test("no batch is larger than 4 KiB", async () => {
  const { clock, daemon, input } = await holding()
  input.type(new Uint8Array(MAX_BATCH_BYTES * 2 + 10).fill(0x61))
  await clock.advance(50)
  const sizes = daemon.inputs().map((c) => atob(c.body.data as string).length)
  assert.deepEqual(sizes, [MAX_BATCH_BYTES, MAX_BATCH_BYTES, 10])
  assert.deepEqual(daemon.inputs().map((c) => c.body.seq), [1, 2, 3])
})

test("a paste goes to /paste in its turn, never inside a keystroke batch", async () => {
  const { clock, daemon, input } = await holding()
  input.type(bytes("a"))
  input.paste("one\ntwo\n")
  input.type(bytes("b"))
  await clock.advance(50)
  const sent = daemon.inputs()
  assert.deepEqual(sent.map((c) => c.path.split("/").pop()), ["input", "paste", "input"])
  assert.deepEqual(sent.map(decoded), ["a", "one\ntwo\n", "b"])
  assert.deepEqual(sent.map((c) => c.body.seq), [1, 2, 3])
})

test("(e) within one lease a batch is only resent under its own seq", async () => {
  const { clock, daemon, input } = await holding()
  daemon.replies.push("network", { status: 502, body: null })
  input.type(bytes("x"))
  await clock.advance(8)
  assert.equal(input.state.retrying, true)
  input.type(bytes("y"))
  await clock.advance(5_000)
  const sent = daemon.inputs()
  assert.deepEqual(sent.map((c) => [c.body.seq, c.body.epoch, decoded(c)]), [
    [1, 1, "x"], [1, 1, "x"], [1, 1, "x"], [2, 1, "y"],
  ])
  assert.equal(input.state.appliedThrough, 2)
  assert.equal(input.state.retrying, false)
})

test("terminal_busy resends the same seq and holds later bytes here", async () => {
  const { clock, daemon, input } = await holding()
  daemon.replies.push({ status: 429, body: { error: "terminal_busy" } }, { status: 429, body: { error: "terminal_busy" } })
  input.type(bytes("1"))
  await clock.advance(8)
  input.type(bytes("2"))
  input.type(bytes("3"))
  assert.equal(input.state.queued, 3)
  await clock.advance(5_000)
  assert.deepEqual(daemon.inputs().map((c) => [c.body.seq, decoded(c)]), [[1, "1"], [1, "1"], [1, "1"], [2, "23"]])
})

test("past 64 KiB waiting, keys are refused and the page is told the input is full", async () => {
  const { clock, daemon, input } = await holding()
  for (let i = 0; i < 40; i++) daemon.replies.push({ status: 429, body: { error: "terminal_busy" } })
  assert.equal(input.type(new Uint8Array(MAX_QUEUED_BYTES).fill(0x61)), true)
  await clock.advance(8)
  assert.equal(input.type(bytes("z")), false)
  assert.equal(input.state.full, true)
  assert.equal(input.canType, false)
})

test("(b) no beat for 6 s turns typing off until the stream is heard again", async () => {
  const { clock, daemon, input } = await holding()
  input.heardStream()
  await clock.advance(5_000)
  assert.equal(input.state.stale, false)
  await clock.advance(2_000)
  assert.equal(input.state.stale, true)
  assert.equal(input.canType, false)
  assert.equal(input.type(bytes("q")), false)
  await clock.advance(50)
  assert.equal(daemon.inputs().length, 0)
  input.heardStream()
  assert.equal(input.state.stale, false)
  assert.equal(input.type(bytes("q")), true)
  await clock.advance(8)
  assert.equal(daemon.inputs().length, 1)
})

test("(c) input_state_unknown empties the queue and nothing is resent", async () => {
  const { clock, daemon, input } = await holding()
  daemon.replies.push({ status: 409, body: { error: "input_state_unknown", applied_through: 0 } })
  input.type(bytes("rm -rf build"))
  await clock.advance(8)
  input.type(bytes("\r"))
  await clock.advance(1)
  assert.equal(input.state.holding, false)
  assert.equal(input.state.queued, 0)
  assert.equal(input.state.notice, "input_state_unknown")
  assert.ok(input.state.dropped > 0)
  await clock.advance(5_000)
  assert.equal(daemon.inputs().length, 1)
})

test("(d) network error, then lease_expired, then a new acquire: the queue is empty and nothing is resent", async () => {
  const { clock, daemon, input } = await holding()
  daemon.replies.push("network", { status: 409, body: { error: "lease_expired" } })
  input.type(bytes("make deploy\r"))
  await clock.advance(8)
  assert.equal(input.state.retrying, true)
  input.type(bytes("more"))
  await clock.advance(200)
  assert.equal(input.state.holding, false)
  assert.equal(input.state.notice, "lease_expired")
  const before = daemon.inputs().length
  // After a daemon restart the new lease can even carry the same epoch.
  daemon.epoch = 1
  assert.equal(await input.control("acquire"), "")
  assert.equal(input.state.holding, true)
  assert.equal(input.state.queued, 0)
  assert.equal(input.state.notice, "lease_expired", "the person is still told to look first")
  await clock.advance(5_000)
  assert.equal(daemon.inputs().length, before, "nothing typed under the old lease is sent into the new one")
  input.type(bytes("k"))
  await clock.advance(8)
  const last = daemon.inputs().at(-1)!
  assert.equal(decoded(last), "k")
  assert.equal(last.body.seq, 1)
})

test("an acquire while bytes are unconfirmed drops them and says so", async () => {
  const { clock, daemon, input } = await holding()
  daemon.replies.push("network")
  input.type(bytes("abc"))
  await clock.advance(8)
  daemon.epoch = 2
  assert.equal(await input.control("takeover"), "")
  assert.equal(input.state.notice, "reacquired")
  assert.equal(input.state.dropped, 3)
  assert.equal(input.state.epoch, 2)
  await clock.advance(5_000)
  assert.ok(daemon.inputs().every((c) => c.body.epoch === 1))
})

test("a control event naming another holder ends this tab's lease and drops its bytes", async () => {
  const { clock, daemon, input } = await holding()
  daemon.replies.push("network")
  input.type(bytes("x"))
  await clock.advance(8)
  input.controlEvent({ held: true, epoch: 2, holder: { name: "phone", local: false, same_device: false, same_client: false } })
  assert.equal(input.state.holding, false)
  assert.equal(input.state.notice, "lease_superseded")
  assert.equal(input.state.queued, 0)
  await clock.advance(5_000)
  assert.equal(daemon.inputs().length, 1)
})

test("input_gap with applied_through past the batch counts it as typed", async () => {
  const { clock, daemon, input } = await holding()
  daemon.replies.push({ status: 409, body: { error: "input_gap", applied_through: 1 } })
  input.type(bytes("a"))
  await clock.advance(8)
  assert.equal(input.state.holding, true)
  assert.equal(input.state.appliedThrough, 1)
  input.type(bytes("b"))
  await clock.advance(8)
  assert.equal(daemon.inputs().at(-1)!.body.seq, 2)
})

test("the lease is renewed every 10 s, and released with keepalive when the tab leaves", async () => {
  const { clock, daemon, input } = await holding()
  input.heardStream()
  await clock.advance(RENEW_MS)
  const renews = daemon.calls.filter((c) => c.body.action === "renew")
  assert.equal(renews.length, 1)
  input.releaseOnLeave()
  const release = daemon.calls.at(-1)!
  assert.equal(release.body.action, "release")
  assert.equal(release.keepalive, true)
})

test("a renew that finds another epoch ends the lease", async () => {
  const { clock, daemon, input } = await holding()
  daemon.replies.push({ status: 200, body: { held: true, epoch: 7, applied_through: 0, holder: { name: "x", local: true, same_device: true, same_client: false } } })
  await clock.advance(RENEW_MS)
  assert.equal(input.state.holding, false)
  assert.equal(input.state.notice, "lease_superseded")
})

test("keys are not taken without a lease", async () => {
  const clock = new FakeClock()
  const daemon = new FakeDaemon()
  const input = new TerminalInputClient("t1", "tab-a", daemon, clock)
  assert.equal(input.type(bytes("a")), false)
  daemon.replies.push({ status: 409, body: { error: "terminal_controlled", holder: { name: "phone", local: false, same_device: false, same_client: false } } })
  assert.equal(await input.control("acquire"), "terminal_controlled")
  assert.equal(input.state.holder?.name, "phone")
  assert.equal(input.state.holding, false)
})
