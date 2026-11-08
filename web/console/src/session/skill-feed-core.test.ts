import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner uses the source .ts extension.
import { LIVE_PAGE_ROWS, POLL_MS, SkillFeed, type FeedClock } from "./skill-feed-core.ts"
import type { SessionSkillSnapshot, SkillMoment } from "./skill-use.js"

/**
 * A machine's `/v1/squad/events` as the daemon answers it
 * (internal/transport/http/squad_events.go): `conversation=` filters, `after`
 * is exclusive, `limit` rows a page. `filters: false` is a machine from before
 * the filter, which answers the whole log.
 */
class Machine {
  log: Record<string, unknown>[] = []
  requests: string[] = []
  readonly filters: boolean
  constructor(filters = true) { this.filters = filters }

  add(conversation: string, status = "read"): number {
    const seq = this.log.length + 1
    this.log.push({ seq, receipt_id: "r" + seq, snapshot_id: "snap." + conversation, conversation_id: conversation,
      definition_id: "persona.a", scope_id: "repo.a", skill_id: "skill.review", skill_version: "v1", status, at: seq * 1000 })
    return seq
  }

  read = async (path: string): Promise<{ status: number; value: unknown }> => {
    this.requests.push(path)
    const url = new URL(path, "http://machine")
    if (url.pathname === "/v1/squad/events/head") return { status: 200, value: { seq: this.log.length } }
    const after = Number(url.searchParams.get("after") ?? 0), limit = Number(url.searchParams.get("limit") ?? 50)
    const named = this.filters ? url.searchParams.getAll("conversation").flatMap((value) => value.split(",")) : []
    const rows = this.log.filter((row) => (row.seq as number) > after && (!named.length || named.includes(row.conversation_id as string)))
    const events = rows.slice(0, limit)
    return { status: 200, value: { events, next_after: events.length ? events.at(-1)!.seq : after, has_more: rows.length > limit } }
  }
}

/** Timers run only when the test says so: `tick()` runs the immediate ones, `tick(POLL_MS)` the next poll too. */
class Clock implements FeedClock {
  private due: { run: () => void; handle: number; ms: number }[] = []
  private next = 0
  set(run: () => void, ms: number): unknown { const handle = ++this.next; this.due.push({ run, handle, ms }); return handle }
  clear(handle: unknown): void { this.due = this.due.filter((entry) => entry.handle !== handle) }
  async tick(upTo = 0): Promise<void> {
    for (let round = 0; round < 50; round++) {
      await settle()
      const due = this.due.filter((entry) => entry.ms <= upTo)
      if (!due.length) return
      this.due = this.due.filter((entry) => entry.ms > upTo)
      for (const entry of due) entry.run()
      upTo = 0
    }
  }
}
const settle = async () => { for (let i = 0; i < 20; i++) await Promise.resolve() }

const snapshotOf = (conversation: string): SessionSkillSnapshot => ({ snapshotId: "snap." + conversation, conversationId: conversation,
  definitionId: "persona.a", scopeId: "repo.a", skills: [{ id: "skill.review", version: "v1", name: "Review" }] })

/** About a thousand receipts over forty Sessions, as on a machine that has run roles for a while. */
function busyMachine(filters = true): Machine {
  const machine = new Machine(filters)
  for (let i = 0; i < 1000; i++) machine.add("conversation." + (i % 40), i % 7 === 0 ? "applied" : "read")
  return machine
}

test("the still state is read only for the Sessions on screen, in one filtered read", async () => {
  const machine = busyMachine()
  const clock = new Clock()
  const moments: SkillMoment[] = []
  const feed = new SkillFeed(machine.read, () => {}, (moment: SkillMoment) => moments.push(moment), clock)
  const shown = ["conversation.3", "conversation.7", "conversation.11"]
  for (const conversation of shown) feed.show(conversation)
  feed.start()
  await clock.tick()
  await clock.tick()

  const reads = machine.requests.filter((path) => path.startsWith("/v1/squad/events?"))
  const still = reads.filter((path) => path.includes("conversation="))
  const live = reads.filter((path) => !path.includes("conversation="))
  assert.equal(still.length, 1, "one filtered read covers every shown Session")
  assert.deepEqual(new URL(still[0], "http://m").searchParams.getAll("conversation"), shown)
  assert.ok(live.every((path) => path.startsWith("/v1/squad/events?after=1000&")), "the live poll starts at the head: " + live.join(" "))
  // Head, the filtered read and one live poll — against 1 + 1000/50 = 21
  // reads from the start of the log before.
  assert.equal(machine.requests.length, 3, machine.requests.join("\n"))
  const before = 1 + Math.ceil(1000 / LIVE_PAGE_ROWS)
  assert.ok(before === 21 && machine.requests.length < before)

  for (const conversation of shown) assert.equal(feed.ledger.usesFor(snapshotOf(conversation)).get("skill.review")?.status, "applied")
  assert.equal(feed.ledger.usesFor(snapshotOf("conversation.4")).size, 0, "a Session not on screen was not read")
  assert.equal(moments.length, 0, "nothing on reload plays")

  // A new receipt plays once; a Session shown later is read alone, still quiet.
  machine.add("conversation.3", "read")
  machine.add("conversation.5", "applied")
  await clock.tick(POLL_MS)
  assert.deepEqual(moments.map((moment) => moment.receipt.seq), [1001, 1002])
  machine.requests = []
  feed.show("conversation.5")
  await clock.tick()
  // Its filtered read, then the live poll it took the place of.
  assert.deepEqual(machine.requests.map((path) => path.replace(/\?.*/, "")), ["/v1/squad/events", "/v1/squad/events"], machine.requests.join("\n"))
  assert.deepEqual(new URL(machine.requests[0], "http://m").searchParams.getAll("conversation"), ["conversation.5"])
  assert.equal(new URL(machine.requests[1], "http://m").searchParams.get("conversation"), null)
  const use = feed.ledger.usesFor(snapshotOf("conversation.5")).get("skill.review")
  assert.equal(use?.status, "applied")
  assert.equal(use?.firstSeq, 6, "an older receipt read later still places the first use")
  assert.equal(moments.length, 2, "the late still read plays nothing")
  feed.stop()
})

test("a reload reads the same still state and plays nothing", async () => {
  const machine = busyMachine()
  for (let round = 0; round < 2; round++) {
    const clock = new Clock()
    const moments: SkillMoment[] = []
    const feed = new SkillFeed(machine.read, () => {}, (moment: SkillMoment) => moments.push(moment), clock)
    feed.show("conversation.1")
    feed.start()
    await clock.tick()
    await clock.tick()
    assert.equal(moments.length, 0)
    assert.equal(feed.ledger.usesFor(snapshotOf("conversation.1")).get("skill.review")?.status, "applied")
    feed.stop()
  }
})

test("more Sessions on screen than one read may name are read in a few filtered reads, not one per row", async () => {
  const machine = busyMachine()
  const clock = new Clock()
  const feed = new SkillFeed(machine.read, () => {}, () => {}, clock)
  for (let i = 0; i < 60; i++) feed.show("conversation." + i)
  feed.start()
  await clock.tick()
  const still = machine.requests.filter((path) => path.includes("conversation="))
  // Paged by `after` within each set; the sets are the first fifty, then the other ten.
  const sets = new Set(still.map((path) => new URL(path, "http://m").searchParams.getAll("conversation").join(",")))
  assert.deepEqual([...sets].map((set) => set.split(",").length), [50, 10])
  assert.ok(still.every((path) => new URL(path, "http://m").searchParams.getAll("conversation").length <= 50))
  feed.stop()
})

test("a machine that ignores the filter is read as before and shows the same still state", async () => {
  const machine = busyMachine(false)
  const clock = new Clock()
  const moments: SkillMoment[] = []
  const feed = new SkillFeed(machine.read, () => {}, (moment: SkillMoment) => moments.push(moment), clock)
  feed.show("conversation.3")
  feed.start()
  await clock.tick()
  assert.equal(feed.ledger.usesFor(snapshotOf("conversation.3")).get("skill.review")?.status, "applied")
  assert.equal(moments.length, 0)
  feed.stop()
})
