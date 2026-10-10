// A replay of an hour of the hosted Session page against the order this
// machine really publishes in: `node --test web/console/src/cloud/*.test.ts`.
//
// What a phone hit: "could not read the conversation" several times an hour on
// the original Session page, while this machine's log held no refused or slow
// read. A pinned read never leaves the page unless the page holds a coherent
// ss/ pass for the Session (`pinnedSession`), so these tests replay what the
// page holds, envelope by envelope, and count the reads it refuses itself.
//
// The order is the publisher's (`internal/transport/cloud/publish.go`
// publishSessions → session_status.go publishStatuses): every changed or
// heartbeat pass sends every Session's ss/ row with a fresh pass id and then
// the ss/ marker with the same id. A phone verifies and opens each envelope in
// turn, so the rows of one pass land a few milliseconds apart and the marker
// last. A read whose Session row has already turned over while the marker has
// not is the window this replays.
//
// Each suspect is counted on its own: the pass window, the machine on its
// quiet 240-second heartbeat, and the socket reconnecting mid-read. The
// counts are written to stdout as one JSON line per scenario so a run can be
// kept as evidence.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- node runs the source TypeScript directly.
import { RelayReader, type CloudEvent, type CloudReadClient, type CloudRow } from "./relay-reader.ts"

const MACHINE = "mac-a"
const MARKER = JSON.stringify([MACHINE, "__clawdline_inventory_v1__"])
const SESSIONS = Array.from({ length: 10 }, (_, i) => `%${i}`)
const EXECUTION = "e".repeat(32)

/** A small deterministic generator, so a count is the same on every run. */
function random(seed: number) {
  let state = seed >>> 0
  return () => {
    state = (state * 1664525 + 1013904223) >>> 0
    return state / 2 ** 32
  }
}

function pass(rng: () => number) {
  return Array.from({ length: 32 }, () => Math.floor(rng() * 16).toString(16)).join("")
}

/** The page's client as far as a pinned read touches it: held ss/ rows, the connection, one machine. */
class ReplayClient implements CloudReadClient {
  ready = true
  statusSnapshots = new Map<string, { payload: Record<string, unknown> }>()
  sessionInventoryByMachine = new Map<string, unknown>()
  pinned = 0
  answered = 0
  machineMs = 300
  schedule: (at: number, run: () => void) => void = () => {}
  clock = { t: 0 }
  recorded: { event: string; data: Record<string, unknown> }[] = []
  viewerEvents = { record: (event: string, data: Record<string, unknown>) => { this.recorded.push({ event, data }) } }
  private listeners = new Set<(event: CloudEvent) => void>()
  events(listener: (event: CloudEvent) => void) {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }
  async machines() { return { machines: [{ id: MACHINE, freshness: "current" as const }], syncing: false, retryAfterMs: 0 } }
  async sessions() {
    const rows: CloudRow[] = SESSIONS.map((id) => ({ machine: MACHINE, session: id, id, execution_generation: EXECUTION,
      line: "line " + Math.floor(this.clock.t / 1000), source: { freshness: "current", observed_at: this.clock.t / 1000 } } as CloudRow))
    return { sessions: rows, at: Math.floor(this.clock.t / 1000), scan: { emptyAuthoritative: true, recovering: [], failures: [] } }
  }
  transcript() { return Promise.reject(new Error("the original page reads pinned")) }
  transcriptForGeneration(target: { sessionID: string; executionGeneration: string }) {
    this.pinned += 1
    return new Promise((resolve, reject) => {
      this.schedule(this.clock.t + this.machineMs, () => {
        if (!this.ready) return reject(Object.assign(new Error("offline"), { code: "cloud_reconnecting", ref: { seq: 1 } }))
        this.answered += 1
        resolve({ id: target.sessionID, entries: [{ role: "user", text: "x" }], signature: String(this.clock.t), evidence: "transcript" })
      })
    })
  }
  emit(event: CloudEvent) { for (const listener of this.listeners) listener(event) }
}

interface Scenario {
  name: string
  /** ms between two passes the machine sends (a busy machine changes every scan). */
  passEveryMs: number
  /** ms between two envelopes of one pass as the phone opens them. */
  envelopeMs: number
  /** ms between two transcript polls of the open Session. */
  pollMs: number
  /** ms between two socket reconnects, 0 for none; each holds the page offline for `offlineMs`. */
  reconnectEveryMs: number
  offlineMs: number
  minutes: number
  seed: number
}

interface Counts {
  scenario: string
  reads: number
  sent: number
  ok: number
  failed: number
  byCondition: Record<string, number>
  byCode: Record<string, number>
  /** Pinned reads asked while the open Session's row had turned over and the marker had not. */
  mid?: number
}

async function replay(s: Scenario): Promise<Counts> {
  const rng = random(s.seed)
  const client = new ReplayClient()
  const clock = client.clock
  const queue: { at: number; order: number; run: () => void }[] = []
  let order = 0
  const at = (when: number, run: () => void) => { queue.push({ at: when, order: order++, run }) }
  client.schedule = at
  const r = new RelayReader(MACHINE, { classicStatus: true, now: () => clock.t })
  r.attach(client)

  const now = () => clock.t / 1000
  const publishPass = (start: number) => {
    const id = pass(rng)
    SESSIONS.forEach((session, i) => at(start + i * s.envelopeMs, () => {
      client.statusSnapshots.set(JSON.stringify([MACHINE, session]), { payload: {
        snapshot_generation: id, execution_generation: EXECUTION, projected_at: now(),
        source: { freshness: "current", observed_at: now() },
      } })
         client.emit({ type: "session_status", identity: { machine: MACHINE, session } } as CloudEvent)
    }))
    at(start + SESSIONS.length * s.envelopeMs, () => {
      client.statusSnapshots.set(MARKER, { payload: {
        complete: true, inventory: { version: 1, sessions: [...SESSIONS] }, at: now(), snapshot_generation: id,
      } })
      client.emit({ type: "session_status", identity: { machine: MACHINE, session: "__clawdline_inventory_v1__" } } as CloudEvent)
    })
  }
  const end = s.minutes * 60_000
  // The page opens on a coherent pass.
  publishPass(0)
  for (let t = s.passEveryMs; t < end; t += s.passEveryMs) publishPass(t + Math.floor(rng() * 2000))
  if (s.reconnectEveryMs > 0) {
    for (let t = s.reconnectEveryMs; t < end; t += s.reconnectEveryMs) {
      const drop = t + Math.floor(rng() * 1000)
      at(drop, () => { client.ready = false })
      at(drop + s.offlineMs, () => { client.ready = true })
    }
  }
  const counts: Counts = { scenario: s.name, reads: 0, sent: 0, ok: 0, failed: 0, byCondition: {}, byCode: {} }
  const reads: Promise<void>[] = []
  const open = SESSIONS[3]
  let mid = 0
  // The page polls again a pace after the last answer, so polls drift
  // against the machine's passes rather than sitting on a grid.
  for (let t = 1000 + Math.floor(rng() * s.pollMs); t < end; t += s.pollMs + Math.floor(rng() * 700)) {
    at(t, () => {
      counts.reads += 1
      const row = client.statusSnapshots.get(JSON.stringify([MACHINE, open]))?.payload
      if (row && row.snapshot_generation !== client.statusSnapshots.get(MARKER)?.payload.snapshot_generation) mid += 1
      // Each poll must reach the machine: the row moves every second here.
      reads.push(r.fetch(`/v1/transcript?session=${encodeURIComponent(open)}&limit=200`).then(async (answer: Response) => {
        if (answer.ok) { counts.ok += 1; return }
        counts.failed += 1
        const body = await answer.json().catch(() => ({})) as { error?: string }
        const code = body.error ?? "http_" + answer.status
        counts.byCode[code] = (counts.byCode[code] ?? 0) + 1
      }, (error: unknown) => {
        // An unanswered read rejects, as a fetch that never reached a server does.
        counts.failed += 1
        const code = String((error as Error)?.message ?? error).split(":")[0]
        counts.byCode[code] = (counts.byCode[code] ?? 0) + 1
      }))
    })
  }
  queue.sort((a, b) => a.at - b.at || a.order - b.order)
  while (queue.length) {
    const next = queue.shift()!
    clock.t = next.at
    next.run()
    // Let the reader's awaits run before the clock moves on.
    for (let i = 0; i < 6; i += 1) await Promise.resolve()
    queue.sort((a, b) => a.at - b.at || a.order - b.order)
  }
  await Promise.all(reads)
  counts.sent = client.pinned
  for (const row of client.recorded) {
    if (row.event !== "cloud.read.failed") continue
    const cond = String(row.data.cond ?? row.data.code)
    counts.byCondition[cond] = (counts.byCondition[cond] ?? 0) + 1
  }
  counts.mid = mid
  console.log("replay " + JSON.stringify(counts))
  return counts
}

const HOUR = 60

test("a busy machine's 15-second passes never make the open Session's poll refuse itself", async () => {
  const runs: Counts[] = []
  for (const envelopeMs of [5, 20, 50]) {
    runs.push(await replay({ name: `busy-pass envelope=${envelopeMs}ms`, passEveryMs: 15_000, envelopeMs,
      pollMs: 4_000, reconnectEveryMs: 0, offlineMs: 0, minutes: HOUR, seed: 7 + envelopeMs }))
  }
  // The window was reached: without that, a zero below proves nothing.
  assert.ok(runs.reduce((sum, counts) => sum + (counts.mid ?? 0), 0) > 10, "no poll landed inside a pass")
  for (const counts of runs) {
    assert.ok(counts.reads > 700, `the hour polled ${counts.reads} times`)
    assert.deepEqual(counts.byCondition, {}, `${counts.scenario}: the page refused its own reads`)
    assert.equal(counts.failed, 0)
  }
})

test("a quiet machine on its 240-second heartbeat never makes the open Session go stale", async () => {
  const counts = await replay({ name: "quiet-heartbeat", passEveryMs: 240_000, envelopeMs: 20,
    pollMs: 4_000, reconnectEveryMs: 0, offlineMs: 0, minutes: HOUR, seed: 11 })
  assert.deepEqual(counts.byCondition, {})
  assert.equal(counts.failed, 0)
})

test("a reconnect mid-read fails only the reads that were on the wire, and says so", async () => {
  const counts = await replay({ name: "reconnect-every-10min", passEveryMs: 15_000, envelopeMs: 20,
    pollMs: 4_000, reconnectEveryMs: 10 * 60_000, offlineMs: 2_000, minutes: HOUR, seed: 13 })
  // Nothing here belongs to the pass window; every failure is the connection.
  for (const cond of Object.keys(counts.byCondition)) {
    assert.ok(cond === "cloud_reconnecting" || cond === "offline", `${cond} is not a reconnect`)
  }
  assert.ok(counts.failed > 0, "no reconnect met a read; the scenario replays nothing")
  assert.ok(counts.failed <= 2 * 6, `${counts.failed} reads failed for 6 reconnects`)
})

test("a pass whose marker never arrives is still refused, after waiting for it", async () => {
  const client = new ReplayClient()
  client.clock.t = 100_000
  client.schedule = (_at, run) => run()
  const now = client.clock.t / 1000
  const row = (id: string) => ({ payload: { snapshot_generation: id, execution_generation: EXECUTION, projected_at: now,
    source: { freshness: "current", observed_at: now } } })
  client.statusSnapshots.set(MARKER, { payload: { complete: true, inventory: { version: 1, sessions: [...SESSIONS] },
    at: now, snapshot_generation: "a".repeat(32) } })
  client.statusSnapshots.set(JSON.stringify([MACHINE, SESSIONS[3]]), row("b".repeat(32)))
  const r = new RelayReader(MACHINE, { classicStatus: true, now: () => client.clock.t })
  r.attach(client)
  const started = Date.now()
  const answer = await r.fetch(`/v1/transcript?session=${encodeURIComponent(SESSIONS[3])}&limit=200`)
  assert.equal(answer.status, 409)
  assert.ok(Date.now() - started >= 1_900, "the page refused before the rest of the pass could arrive")
  assert.equal(client.pinned, 0, "nothing was sent for an incoherent pass")
  assert.deepEqual(client.recorded.map((row) => row.data.cond), ["pass_mismatch"])
})
