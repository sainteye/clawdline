// A replay of the page opening before the machine's word list has arrived:
// `node --test web/console/src/cloud/feature-wait-replay.test.ts`.
//
// What a phone hit: "this machine answered that it does not know transcript" for a
// few seconds after opening the console, with nothing refused on the machine.
// The refusal was the page's own. A hosted page decides whether a machine
// answers a word from the descriptor it remembered last time
// (`machineDescriptors`, legacy/js/net/cloud-client.js), and that remembered
// list was cut to its first 64 words on the way into storage
// (`descriptorCommands`) while this daemon publishes 151
// (`cloudops.Implemented()`). `transcript` is the 101st of them and
// `work.v2.session-todos` the 148th, so between the socket coming up and the
// first `orch/` envelope being opened both were "words this machine does not
// have" — and those are the first two words a Session page reads.
//
// Measured in this machine's daemon log on 2026-10-10: 11 of 21 read failures
// between 11:30 and 14:22 were `cloud_feature_unavailable` with
// `stage=viewer_refused`, in bursts at 12:08:43, 12:41:34 and 13:29:00, every
// one of them for `session_todos` or `transcript`.
//
// So the replay is the page opening: every read fires at once, the descriptor
// arrives 0.5 to 3 seconds later, and what is counted is the refusals. The
// counts go to stdout as one JSON line per scenario so a run can be kept as
// evidence. Real timers, because the wait is one.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- node runs the source TypeScript directly.
import { RelayReader, FEATURE_WAIT_MS, REMEMBERED_COMMANDS_CUT, machineWordPending, type CloudEvent, type CloudReadClient } from "./relay-reader.ts"

const MACHINE = "mac-a"
const SESSIONS = ["%1", "%2", "%3"]

/**
 * What this daemon publishes, in the order it publishes it: 151 words with
 * `transcript` at 100 and `work.v2.session-todos` at 147, which is where this
 * machine's live `/v1/cloud/status` had them on 2026-10-10. Only the two
 * indices matter; the rest are placeholders of the right count.
 */
const PUBLISHED = Array.from({ length: 151 }, (_, i) => `word.${i}`)
PUBLISHED[19] = "git"
PUBLISHED[100] = "transcript"
PUBLISHED[147] = "work.v2.session-todos"

/** What the page remembered: the same list, cut where the copied client cuts it. */
const REMEMBERED = PUBLISHED.slice(0, REMEMBERED_COMMANDS_CUT)

/**
 * The page's client as far as these two reads touch it, refusing exactly where
 * the copied one does: `_machineImplements` answers from the live descriptor
 * if there is one and the remembered one otherwise, and a "no" about an
 * evident Mac is `cloud_feature_unavailable` raised with no envelope sequence
 * — the `viewer_refused` stage the log counted.
 */
class OpeningClient implements CloudReadClient {
  ready = true
  orchestratorSnapshots = new Map<string, unknown>()
  machineDescriptors = new Map<string, unknown>()
  machineLacks = new Map<string, unknown>()
  sent: string[] = []
  refused: string[] = []
  recorded: { event: string; data: Record<string, unknown> }[] = []
  viewerEvents = { record: (event: string, data: Record<string, unknown>) => { this.recorded.push({ event, data }) } }
  private listeners = new Set<(event: CloudEvent) => void>()

  constructor(remembered: string[] | null) {
    if (remembered) {
      this.machineDescriptors.set(MACHINE, { machine: { platform: "darwin", name: "a machine", commands: remembered },
        build: "0.0.1-test", at_ms: 0, features: null })
    }
  }

  events(listener: (event: CloudEvent) => void) {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  /** The machine's whole word list arrives, as one `orch/` envelope being opened. */
  arrive(commands: string[] = PUBLISHED) {
    this.orchestratorSnapshots.set(MACHINE, { at: 1, app: { version: "0.0.1-test" },
      machine: { platform: "darwin", name: "a machine", commands } })
    for (const listener of this.listeners) listener({ type: "orchestrator", machine: MACHINE } as CloudEvent)
  }

  /** A status-only `orch/` notice: no descriptor, so nothing is answered by it. */
  statusOnly() {
    for (const listener of this.listeners) listener({ type: "orchestrator", machine: MACHINE } as CloudEvent)
  }

  private implemented(word: string): "yes" | "no" | "unknown" {
    const live = this.orchestratorSnapshots.get(MACHINE) as { machine?: { commands?: unknown } } | undefined
    const remembered = this.machineDescriptors.get(MACHINE) as { machine?: { commands?: unknown } } | undefined
    const commands = live?.machine?.commands ?? remembered?.machine?.commands
    if (Array.isArray(commands)) return commands.includes(word) ? "yes" : "no"
    // No descriptor at all: the copied client answers "unknown" and refuses
    // nothing, because the request named that machine and no other can serve it.
    return "unknown"
  }

  private answer(word: string, body: unknown): Promise<unknown> {
    if (this.implemented(word) === "no") {
      this.refused.push(word)
      return Promise.reject(Object.assign(new Error("this machine answered that it does not know " + word),
        { code: "cloud_feature_unavailable", status: 502, layer: "browser" }))
    }
    this.sent.push(word)
    return Promise.resolve(body)
  }

  _machineRequest(_machine: string, word: string): Promise<unknown> {
    return this.answer(word, { read: { todos: [] } })
  }

  transcript(identity: { session?: string }): Promise<unknown> {
    return this.answer("transcript", { id: identity.session, entries: [{ role: "user", text: "x" }],
      signature: "s1", evidence: "transcript" })
  }

  async sessions() {
    return { sessions: SESSIONS.map((id) => ({ machine: MACHINE, session: id, id, line: "a line" })),
      at: 1, scan: { emptyAuthoritative: true, recovering: [], failures: [] } } as never
  }
}

interface Counts {
  scenario: string
  /** The reads the page fired the moment it opened. */
  reads: number
  /** Of those, the ones that reached the machine. */
  sent: number
  ok: number
  failed: number
  byCode: Record<string, number>
  /** `cloud.read.failed` rows the page reported, by the word it named. */
  reportedWords: Record<string, number>
  /** How long the slowest of them took, in milliseconds. */
  ms: number
}

/**
 * One page opening. `waitMs` is the seam's bound — 0 is the page as it was
 * before this wait existed — and `arriveMs` when the machine's descriptor is
 * opened. `remembered` is the word list the page starts with: the cut one, a
 * short honest one, or none at all.
 */
async function replay(s: { name: string; waitMs: number; arriveMs: number | null;
  remembered?: string[] | null; live?: string[] }): Promise<Counts> {
  const client = new OpeningClient(s.remembered === undefined ? REMEMBERED : s.remembered)
  if (s.live) client.arrive(s.live)
  const reader = new RelayReader(MACHINE, { featureWaitMs: s.waitMs })
  reader.attach(client)
  const counts: Counts = { scenario: s.name, reads: 0, sent: 0, ok: 0, failed: 0, byCode: {}, reportedWords: {}, ms: 0 }
  const started = Date.now()
  if (s.arriveMs !== null) setTimeout(() => client.arrive(), s.arriveMs)
  const reads: Promise<void>[] = []
  // What a Session page reads the moment it is drawn: each Session's to-do
  // fold and its transcript.
  for (const session of SESSIONS) {
    for (const path of [`/v1/work/v2/session-todos/${encodeURIComponent(session)}`,
      `/v1/transcript?session=${encodeURIComponent(session)}&limit=200`]) {
      counts.reads += 1
      reads.push(reader.fetch(path).then(async (answer: Response) => {
        if (answer.ok) { counts.ok += 1; return }
        counts.failed += 1
        const body = await answer.json().catch(() => ({})) as { error?: string }
        const code = body.error ?? "http_" + answer.status
        counts.byCode[code] = (counts.byCode[code] ?? 0) + 1
      }, (error: unknown) => {
        counts.failed += 1
        const code = (error as { code?: string })?.code ?? String((error as Error)?.message ?? error)
        counts.byCode[code] = (counts.byCode[code] ?? 0) + 1
      }))
    }
  }
  await Promise.all(reads)
  counts.ms = Date.now() - started
  counts.sent = client.sent.length
  for (const row of client.recorded) {
    if (row.event !== "cloud.read.failed") continue
    const word = String(row.data.word ?? "none")
    counts.reportedWords[word] = (counts.reportedWords[word] ?? 0) + 1
  }
  console.log("replay " + JSON.stringify(counts))
  return counts
}

test("the page that has just opened refuses its own first reads, and the wait is what stops it", async () => {
  // Before: the page as it was. Every read is refused, by the page, before
  // anything is sent — the burst the daemon log counted.
  const before = await replay({ name: "page just opened, no wait", waitMs: 0, arriveMs: 1_000 })
  assert.equal(before.sent, 0, "a read reached the machine")
  assert.equal(before.failed, before.reads)
  assert.deepEqual(before.byCode, { cloud_feature_unavailable: before.reads })
  // And it reported them as the measurement found them: by word.
  assert.deepEqual(before.reportedWords, { session_todos: 3, transcript: 3 })

  // After: the same opening, with the descriptor arriving 0.5 to 3 seconds
  // later, which is what it does.
  for (const arriveMs of [500, 1_500, 3_000]) {
    const after = await replay({ name: `page just opened, descriptor at ${arriveMs}ms`, waitMs: FEATURE_WAIT_MS, arriveMs })
    assert.equal(after.failed, 0, `${after.scenario}: ${JSON.stringify(after.byCode)}`)
    assert.equal(after.sent, after.reads, "a read did not reach the machine")
    assert.deepEqual(after.reportedWords, {}, "a failure was still reported")
    assert.ok(after.ms >= arriveMs - 50, `the reads answered in ${after.ms}ms, before the descriptor arrived`)
    assert.ok(after.ms < FEATURE_WAIT_MS, `the reads took ${after.ms}ms; they waited out the bound`)
  }
})

test("a machine that really does not answer the word is still refused, and at once", async () => {
  // A short remembered list is the whole list: 20 words, and neither of these
  // two among them. Nothing waits, and the refusal is the one it always was.
  const short = PUBLISHED.slice(0, 20)
  const honest = await replay({ name: "the machine lacks the word", waitMs: FEATURE_WAIT_MS, arriveMs: null, remembered: short })
  assert.equal(honest.failed, honest.reads)
  assert.deepEqual(honest.byCode, { cloud_feature_unavailable: honest.reads })
  assert.ok(honest.ms < 1_000, `the refusal took ${honest.ms}ms; it must not wait for a descriptor it has`)

  // A live descriptor that lacks the word is the machine's own current answer.
  const live = await replay({ name: "the live descriptor lacks the word", waitMs: FEATURE_WAIT_MS, arriveMs: null,
    remembered: null, live: short })
  assert.equal(live.failed, live.reads)
  assert.ok(live.ms < 1_000, `the refusal took ${live.ms}ms`)

  // A machine that has published nothing is "unknown", which the page does not
  // refuse: the read goes out as it always did.
  const unknown = await replay({ name: "no descriptor at all", waitMs: FEATURE_WAIT_MS, arriveMs: null, remembered: null })
  assert.equal(unknown.failed, 0, JSON.stringify(unknown.byCode))
  assert.equal(unknown.sent, unknown.reads)
  assert.ok(unknown.ms < 1_000, `an unknown machine's read waited ${unknown.ms}ms`)
})

test("a wait the descriptor never ends is the refusal it always was, once", async () => {
  const bound = 300
  const client = new OpeningClient(REMEMBERED)
  const reader = new RelayReader(MACHINE, { featureWaitMs: bound })
  reader.attach(client)
  // A status-only `orch/` notice carries no descriptor, so it must not be
  // taken for one: it is the envelope a machine sends before its first
  // snapshot.
  setTimeout(() => client.statusOnly(), 50)
  const started = Date.now()
  const answer = await reader.fetch(`/v1/work/v2/session-todos/${encodeURIComponent(SESSIONS[0]!)}`)
  const took = Date.now() - started
  assert.equal(answer.status, 502)
  assert.equal((await answer.json() as { error?: string }).error, "cloud_feature_unavailable")
  assert.ok(took >= bound - 20, `the read was refused after ${took}ms, before the bound`)
  assert.ok(took < bound * 4, `the read waited ${took}ms for a ${bound}ms bound`)
  assert.equal(client.sent.length, 0)
})

test("only a read the cut list cannot answer for waits", () => {
  const cut = new OpeningClient(REMEMBERED)
  assert.equal(machineWordPending(cut, MACHINE, "transcript"), true)
  assert.equal(machineWordPending(cut, MACHINE, "work.v2.session-todos"), true)
  // A word the remembered list does name is answered, not waited for.
  assert.equal(machineWordPending(cut, MACHINE, "git"), false)
  // Nothing remembered: "unknown", which is not refused.
  assert.equal(machineWordPending(new OpeningClient(null), MACHINE, "transcript"), false)
  // A list shorter than the cut is the whole list.
  assert.equal(machineWordPending(new OpeningClient(PUBLISHED.slice(0, 20)), MACHINE, "transcript"), false)
  // The live descriptor is the machine's own answer, whichever way it goes.
  const arrived = new OpeningClient(REMEMBERED)
  arrived.arrive()
  assert.equal(machineWordPending(arrived, MACHINE, "transcript"), false)
  const arrivedShort = new OpeningClient(REMEMBERED)
  arrivedShort.arrive(PUBLISHED.slice(0, 20))
  assert.equal(machineWordPending(arrivedShort, MACHINE, "transcript"), false)
  // A word the machine itself said it does not know is a fact, not a cut.
  const said = new OpeningClient(REMEMBERED)
  said.machineLacks.set(MACHINE, new Set(["transcript"]))
  assert.equal(machineWordPending(said, MACHINE, "transcript"), false)
  assert.equal(machineWordPending(cut, "", "transcript"), false)
})
