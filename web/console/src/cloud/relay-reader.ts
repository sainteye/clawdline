// The console's same-origin daemon, answered from the relay instead.
//
// Everything in this console reads one daemon on its own origin: `client.ts`
// is a `ClawdlineClient` with no base URL, and the fleet list follows
// `/v1/events`. `@clawdline/core` already says what a host must supply for that
// to work — `fetch`, and a stream transport — so that is the whole seam: this
// file is a `fetch` that answers the console's reads out of the copied
// `CloudClient` (`legacy/js/net/cloud-client.js`), and a `StreamTransport` that
// turns its events into the `sessions` frames `FleetStore` already takes.
// Nothing above it knows which one it is talking to.
//
// It reads one machine. The console's list is one machine's by construction
// (`legacy/bridge.ts` `publish`), and the relay carries every machine on the
// account, so the machine is chosen before the console is drawn and this
// answers for that one only.
//
// The writes — sending, answering, starting, ending, dictating — are
// `relay-writer.ts`, handed in with `carryWrites`; this file routes the
// console's requests to it and keeps what a write means for the reads after it
// (`wrote`).
//
// It does not import the copied modules: the client is handed in, typed by the
// little of it this file reads, so the rules here run under `node --test`
// without a page (`relay-reader.test.ts`).
import type { CarriedWord, CarryTable } from "./carry.js"
import type { Health, SessionRow, SessionsSnapshot, TranscriptPage } from "@clawdline/contract"
import type { StreamHandle, StreamHandlers, StreamTransport } from "@clawdline/core"
import type { CloudWriteClient, WriteHost, WriteRoute } from "./relay-writer.js"

/** One machine and one of its sessions, as the relay's channels name them. */
export interface CloudIdentity {
  machine: string
  session: string
}

/** A row as `CloudClient` holds it: the Mac's own row, plus where it came from. */
export type CloudRow = Record<string, unknown> & {
  id?: string
  machine?: string
  session?: string
  identity?: CloudIdentity
}

/** `CloudClient.sessions()`'s answer (`_sessionResponse`). */
export interface CloudSessions {
  sessions: CloudRow[]
  at: number
  scan: {
    emptyAuthoritative?: boolean
    recovering?: string[]
    failures?: { machine: string; code: string }[]
  }
}

/** What `CloudClient.events()` hands a listener; only these fields are read. */
export interface CloudEvent {
  type: string
  state?: string
  machine?: string
  identity?: Partial<CloudIdentity>
}

/**
 * The part of the copied `CloudClient` this seam reads. Reads only: the writes
 * are `CloudWriteClient`, reached only through `relay-writer.ts`.
 */
export interface CloudReadClient {
  readonly ready?: boolean
  /** machine → the last inventory marker it published; present once one arrived. */
  readonly sessionInventoryByMachine?: Map<string, unknown>
  events(listener: (event: CloudEvent) => void): () => void
  sessions(): Promise<CloudSessions>
  transcript(identity: CloudIdentity, phases?: unknown, demand?: { foreground?: boolean }): Promise<unknown>
}

/**
 * How long a transcript answer is reused while its row shows only its status
 * line moving. The line is the assistant's own timer, and it moves on every
 * scan whether or not an entry was written: the Swift console measured 472
 * transcript reads in fourteen hours on one phone, 223 of them identical to the
 * read before (`session/transcript-requests.js`, `TRANSCRIPT_LINE_REREAD_MS`,
 * the same fifteen seconds).
 */
export const TRANSCRIPT_LINE_REREAD_MS = 15_000

/**
 * The longest an answer is reused with nothing on its row moving at all. A
 * Swift Mac puts `transcript_signature` on the row and the page re-reads when
 * it changes; the Go daemon does not compute one yet (docs/cloud-wire.md
 * §16.6), so a row can stay still while its transcript grows. This is the
 * bound on how stale that can make the page.
 */
export const TRANSCRIPT_MAX_REUSE_MS = 30_000

/**
 * How long, after this page did something to a session, every poll asks the
 * machine again until the transcript it answers has changed. A message just
 * typed is the turn the page is waiting to see (`session/pending.ts`); reusing
 * the answer from before it would hold the card at "the Mac has it" for up to
 * `TRANSCRIPT_MAX_REUSE_MS` with the turn already written.
 */
export const TRANSCRIPT_EXPECT_MS = 45_000

/**
 * Failures that mean nobody answered, rather than that somebody said no. They
 * reject the `fetch` the way a network failure does, so `ClawdlineClient`
 * reports a `TransportError`, which the console already draws as "away" and
 * not as a refusal (`core/src/refusal.ts`).
 */
const UNANSWERED = new Set([
  "offline",
  "socket_error",
  "cloud_starting",
  "cloud_reconnecting",
  "cloud_read_timeout",
  "cloud_read_settled",
])

/** Row paths that move on every reading and that nobody reads (`publish.go`), plus the line. */
const FRESHNESS_ONLY: readonly (readonly string[])[] = [
  ["line"],
  ["closeability", "observed_at"],
  ["closeability", "session_generation"],
  ["closeability", "source", "observed_at"],
]

/** One thing this seam was asked, and how it answered. */
export interface SeamRow {
  at: number
  method: string
  path: string
  /** `relay`: asked of the machine; `cache`: the last answer reused; `local`: answered here. */
  answer: "relay" | "cache" | "local" | "refused" | "unanswered"
  code?: string
  /** The Cloud command a write was carried as. */
  word?: string
  /** From the request to its answer, for a write. */
  ms?: number
  /** The envelope a refusal names, `sender·seq`. */
  ref?: string
}

export interface RelayReaderOptions {
  now?: () => number
  /** The interface's words for `GET /v1/strings`; the build's static catalog. */
  strings?: () => Promise<Record<string, string>>
  /** Called with a row each time something is answered, for the diagnostics log. */
  onAnswer?: (row: SeamRow) => void
  /**
   * What this bundle carries, and what it says about what it does not
   * (`carry.ts`, handed in by `CloudGate`).
   *
   * It is handed in rather than imported because nothing in this file is
   * imported at run time — that is what lets `node --test` load it as it is —
   * and because there is exactly one table and it is the one the drift guard
   * reads (internal/app/cloudops/carry_test.go). Without it a refusal still
   * says the route and `drift` says it does not know.
   */
  carry?: CarryTable
}

interface HeldTranscript {
  answer: TranscriptPage | null
  at: number
  rowKey: string
  line: string
  inflight: Promise<TranscriptPage> | null
  /** Ask the machine on the next read, whatever the row says: something was done to this session. */
  stale: boolean
  /** Until when a write's effect is awaited (`TRANSCRIPT_EXPECT_MS`), and the signature it is awaited against. */
  expectUntil: number
  expectFrom: string | null
}

/** The writer `carryWrites` hands in: `RelayWriter`, typed by what this file calls. */
export interface WriteSeam {
  route(method: string, path: string): WriteRoute | null
  answer(route: WriteRoute, method: string, url: URL, init?: RequestInit): Promise<Response>
}

interface OpenStream {
  handlers: StreamHandlers
  off: (() => void) | null
  queued: boolean
}

export class RelayReader {
  private client: CloudReadClient | null = null
  private readonly now: () => number
  /** Identifies this page's sequence of snapshots, as a daemon's epoch identifies its process. */
  private readonly epoch: number
  private generation = 0
  private readonly streams = new Set<OpenStream>()
  private readonly transcripts = new Map<string, HeldTranscript>()
  private readonly rows: SeamRow[] = []
  private readonly options: RelayReaderOptions
  private writer: WriteSeam | null = null
  /** Whether this page has already said what it and the Mac disagree about (`drift`). */
  private saidDrift = false
  /** The one machine this reads. */
  readonly machine: string

  constructor(machine: string, options: RelayReaderOptions = {}) {
    this.machine = machine
    this.options = options
    this.now = options.now ?? (() => Date.now())
    this.epoch = this.now()
  }

  /** What was asked of this seam, newest last, at most 400 rows. */
  get log(): readonly SeamRow[] {
    return this.rows
  }

  /**
   * The client to read through, first or after a renewal. `keepConnected`
   * replaces the client every few minutes when the device token is renewed
   * and after every reconnect; an open list follows the new one and is told
   * the line is up.
   */
  attach(client: CloudReadClient): void {
    this.client = client
    for (const stream of this.streams) {
      this.bind(stream)
      if (client.ready !== false) stream.handlers.onOpen?.()
      this.queueFrame(stream)
    }
  }

  /**
   * What `relay-writer.ts` is given of this seam: the machine, the client, the
   * log, and `wrote`, which is how a write reaches the reads after it.
   */
  get writeHost(): WriteHost {
    return {
      machine: this.machine,
      connected: () => this.connected() as CloudWriteClient,
      wrote: (session, outcome) => this.wrote(session, outcome),
      note: (row) => this.note(row.method, row.path, row.answer, row.code, row),
    }
  }

  /** Carry the console's writes through `writer` (`RelayWriter`); without one, every write is refused by name. */
  carryWrites(writer: WriteSeam): void {
    this.writer = writer
  }

  /**
   * Something was done to `session`. The next read asks the machine whatever
   * the row says, and — unless the Mac refused, which changes nothing — every
   * read for the next `TRANSCRIPT_EXPECT_MS` does too, until the transcript it
   * answers is not the one from before.
   */
  wrote(session: string, outcome: "done" | "unknown" | "refused"): void {
    const held = this.transcripts.get(session) ?? this.hold(session)
    held.stale = true
    if (outcome === "refused") return
    held.expectUntil = this.now() + TRANSCRIPT_EXPECT_MS
    held.expectFrom = held.answer?.signature ?? null
  }

  /**
   * What this bundle and this Mac disagree about, right now, on the page.
   *
   * The build-time half of this is a Go test that reads `carry.ts`
   * (internal/app/cloudops/carry_test.go), and it can only compare this bundle
   * with the checkout it was built from. A hosted console is an *older* bundle
   * reading a Mac that has been updated since, which no test in either repo can
   * see. The Mac says what it can do in its own descriptor —
   * `cloudops.Implemented()`, carried as `machine.commands` — so the same
   * question is asked here of the machine actually being read.
   *
   * `notCarried` is what this Mac answers and this bundle never asks for;
   * `notOnThisMac` is what this bundle would ask for and this Mac does not
   * list. Empty when the descriptor has not arrived: unknown is not agreement,
   * and `null` says which of the two this is.
   */
  drift(): { notCarried: string[]; notOnThisMac: string[] } | null {
    const carried = this.options.carry?.carried
    if (!carried) return null
    const client = this.client as { machineDescriptor?: (machine: string) => { machine?: { commands?: unknown } } | null } | null
    const commands = client?.machineDescriptor?.(this.machine)?.machine?.commands
    if (!Array.isArray(commands)) return null
    const listed = new Set(commands.filter((word): word is string => typeof word === "string"))
    return {
      notCarried: [...listed].filter((word) => !carried.includes(word)).sort(),
      notOnThisMac: carried.filter((word) => !listed.has(word)).sort(),
    }
  }

  /**
   * Put `drift` in this page's own log, once, as soon as the machine's
   * descriptor has arrived. It is a row and not a refusal because nothing is
   * broken: the page carries what it carries. It is recorded because the
   * alternative is what happened with `info` — a Mac answering a word for
   * months, a page never asking for it, and nothing anywhere saying so.
   */
  private sayDrift(): void {
    if (this.saidDrift) return
    const found = this.drift()
    if (!found) return
    this.saidDrift = true
    if (!found.notCarried.length && !found.notOnThisMac.length) return
    this.note("GET", "/v1/sessions", "local", "cloud_vocabulary_drift", {
      word: [...found.notCarried, ...found.notOnThisMac.map((w) => "-" + w)].join(" "),
    })
  }

  /** The line went away (`reconnecting`, `paused`, a terminal refusal). */
  lost(): void {
    for (const stream of this.streams) stream.handlers.onError?.(new Error("the relay connection is down"))
  }

  /** `fetch`, for the console's own-origin `/v1/…` requests. */
  readonly fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const method = (init?.method ?? (typeof input === "object" && "method" in input ? input.method : "GET")).toUpperCase()
    const href = typeof input === "string" ? input : input instanceof URL ? input.href : input.url
    const url = new URL(href, "http://relay.invalid/")
    const path = url.pathname
    try {
      const write = this.writer?.route(method, path) ?? null
      if (write) return await this.writer!.answer(write, method, url, init)
      if (method !== "GET") {
        return this.refuse(method, path, 501, "cloud_not_carried", this.notCarried(method, path))
      }
      switch (path) {
        case "/v1/sessions":
          this.note(method, path, "local")
          return json(200, await this.snapshot())
        case "/v1/transcript": {
          const session = url.searchParams.get("session")
          if (!session) return this.refuse(method, path, 400, "bad_request", "No session was named.")
          // `cache: "no-store"` is a caller that must see the machine's answer
          // now: a failed send's "try again" checks the words did not arrive
          // after all before it types them a second time (`session/send.ts`).
          const fresh = init?.cache === "no-store" || init?.cache === "reload" || init?.cache === "no-cache"
          const { page, reused } = await this.transcript(session, fresh)
          // The one word this file carries itself; the rest are the writer's.
          // Typed against the table so that dropping it from `CARRIED` is a
          // compile error here rather than a silent disagreement.
          const word: CarriedWord = "transcript"
          this.note(method, path, reused ? "cache" : "relay", undefined, { word })
          return json(200, page)
        }
        case "/v1/health": {
          // The console's light asks this every fifteen seconds. Across the
          // relay the honest answer is whether the line is up: a line that is
          // down rejects, which the light draws as offline.
          this.connected()
          const health: Health = { at: Math.floor(this.now() / 1000), authed: true, ok: true, password: false, served_by: "cloud-relay" }
          this.note(method, path, "local")
          return json(200, health)
        }
        case "/v1/strings": {
          // The words are the bundle's own file, not the Mac's (`cloud/strings.ts`):
          // they belong to the screen and the screen is here. An empty answer
          // is not a failure — the console has built-in English and uses it —
          // but it is a degradation, and the seam log says so by name rather
          // than looking like a catalog that happened to be empty.
          const words = this.options.strings ? await this.options.strings() : {}
          this.note(method, path, "local", Object.keys(words).length ? undefined : "no_catalog")
          return json(200, words)
        }
        default:
          return this.refuse(method, path, 501, "cloud_not_carried", this.notCarried(method, path))
      }
    } catch (error) {
      const failure = error as { code?: unknown; status?: unknown; message?: unknown }
      const code = typeof failure?.code === "string" ? failure.code : "cloud_failed"
      const message = typeof failure?.message === "string" ? failure.message : String(error)
      if (UNANSWERED.has(code) || error instanceof NotConnected) {
        this.note(method, path, "unanswered", code)
        throw new TypeError(`${code}: ${message}`)
      }
      const status = typeof failure?.status === "number" && failure.status >= 400 && failure.status < 600 ? failure.status : 502
      return this.refuse(method, path, status, code, message)
    }
  }

  /** The stream `FleetStore` follows: a `sessions` frame whenever this machine's rows change. */
  stream(): StreamTransport {
    return {
      open: (_url: string, handlers: StreamHandlers): StreamHandle => {
        const stream: OpenStream = { handlers, off: null, queued: false }
        this.streams.add(stream)
        this.bind(stream)
        if (this.client && this.client.ready !== false) handlers.onOpen?.()
        return {
          close: () => {
            stream.off?.()
            stream.off = null
            this.streams.delete(stream)
          },
        }
      },
    }
  }

  /**
   * This machine's rows as `/v1/sessions` would answer them.
   *
   * Empty is believed only once the machine has published its inventory
   * marker and is not still sending its rows: before that, an empty list is
   * "not arrived yet", and the scan says so, as a daemon's incomplete scan
   * does. `CloudClient` keeps that distinction in `recovering` and in the
   * marker; this is where it becomes the console's `emptyAuthoritative`.
   */
  async snapshot(): Promise<SessionsSnapshot> {
    const client = this.connected()
    const all = await client.sessions()
    this.sayDrift()
    const recovering = (all.scan.recovering ?? []).includes(this.machine)
    const failed = (all.scan.failures ?? []).some((f) => f.machine === this.machine)
    const inventory = client.sessionInventoryByMachine?.has(this.machine) === true
    const whole = inventory && !recovering && !failed
    const sessions = all.sessions.filter((row) => row.machine === this.machine).map(consoleRow)
    this.generation += 1
    return {
      at: all.at || Math.floor(this.now() / 1000),
      scan: {
        complete: whole,
        completed: { complete: whole, sequence: this.generation },
        emptyAuthoritative: whole,
        epoch: this.epoch,
        generation: this.generation,
        provenance: "cloud",
      },
      sessions,
    }
  }

  /**
   * One session's transcript, asked of the machine only when it could have
   * changed.
   *
   * The console asks every four seconds (`session/Transcript.tsx`), which is
   * right against a daemon on the same machine and wrong across the relay:
   * every ask is an envelope out and a transcript back, billed both ways. So an
   * answer is reused until the row says something moved — at once for any
   * change but the status line, after fifteen seconds for the line alone, and
   * after thirty seconds regardless. Two asks while one is in flight share it.
   *
   * A write changes that (`wrote`): the page is waiting to see what it did, so
   * the machine is asked on every poll until the answer is a different one, and
   * a `fresh` caller is never handed an answer asked before it.
   */
  async transcript(session: string, fresh = false): Promise<{ page: TranscriptPage; reused: boolean }> {
    const client = this.connected()
    const all = await client.sessions()
    const row = all.sessions.find((r) => r.machine === this.machine && (r.session ?? r.id) === session)
    const rowKey = row ? keyOf(row) : ""
    const line = row && typeof row.line === "string" ? row.line : ""
    const now = this.now()
    let held = this.transcripts.get(session)
    // A read already on its way is shared — unless the caller must see an
    // answer asked after it asked (`fresh`), which that one may predate. That
    // caller waits it out and then asks anew: asking beside it is not a new
    // read, because the copied client hands a second ask for the same session
    // the answer of the first one on its way (`readKey`), and "try again"
    // would be checking the words against a transcript from before they were
    // sent (F2).
    if (held?.inflight && !fresh) return { page: await held.inflight, reused: true }
    if (fresh && held?.inflight) {
      const before = held.inflight
      await before.catch(() => undefined)
      held = this.transcripts.get(session)
      // One asked after this caller asked may be shared: it cannot predate it.
      if (held?.inflight && held.inflight !== before) return { page: await held.inflight, reused: true }
    }
    const expecting = !!held && held.expectUntil > now && (held.answer?.signature ?? null) === held.expectFrom
    if (held?.answer && !fresh && !held.stale && !expecting) {
      const age = now - held.at
      const moved = rowKey !== held.rowKey
      const lineMoved = line !== held.line
      if (!moved && age < TRANSCRIPT_MAX_REUSE_MS && !(lineMoved && age >= TRANSCRIPT_LINE_REREAD_MS)) {
        return { page: held.answer, reused: true }
      }
    }
    const entry: HeldTranscript = held ?? this.hold(session)
    const asked = client
      .transcript({ machine: this.machine, session }, undefined, { foreground: true })
      .then((body) => transcriptPage(body, session))
    entry.inflight = asked
    this.transcripts.set(session, entry)
    try {
      const page = await asked
      entry.answer = page
      entry.at = this.now()
      entry.rowKey = rowKey
      entry.line = line
      entry.stale = false
      // The awaited change has arrived; from here the ordinary rules apply.
      if (entry.expectUntil && page.signature !== entry.expectFrom) entry.expectUntil = 0
      return { page, reused: false }
    } finally {
      // A failed ask keeps the last good answer and its age, so the next poll
      // asks again rather than reusing it as though nothing had happened.
      entry.inflight = null
    }
  }

  private hold(session: string): HeldTranscript {
    const entry: HeldTranscript = { answer: null, at: 0, rowKey: "", line: "", inflight: null, stale: false, expectUntil: 0, expectFrom: null }
    this.transcripts.set(session, entry)
    return entry
  }

  private connected(): CloudReadClient {
    if (!this.client || this.client.ready === false) throw new NotConnected()
    return this.client
  }

  private bind(stream: OpenStream): void {
    stream.off?.()
    stream.off = this.client
      ? this.client.events((event) => {
          if (event.type === "connection") {
            if (event.state === "live") stream.handlers.onOpen?.()
            else if (event.state === "offline") stream.handlers.onError?.(new Error("the relay connection dropped"))
            return
          }
          const machine = event.type === "orchestrator" ? event.machine : event.identity?.machine
          if ((event.type === "sessions" || event.type === "orchestrator") && machine === this.machine) {
            this.queueFrame(stream)
          }
        })
      : null
  }

  /**
   * One frame per turn of the event loop, however many envelopes arrived in
   * it. A reconnect replays every retained row at once, and a list redrawn
   * once per row would redraw forty times for one change.
   */
  private queueFrame(stream: OpenStream): void {
    if (stream.queued) return
    stream.queued = true
    setTimeout(() => {
      stream.queued = false
      if (!this.streams.has(stream)) return
      this.snapshot().then(
        (snapshot) => stream.handlers.onFrame("sessions", JSON.stringify(snapshot)),
        () => {
          /* not connected: the connection event says so */
        },
      )
    }, 0)
  }

  /**
   * What a refusal says about a route this bundle does not carry.
   *
   * Nothing reads it for its wording — the code is `cloud_not_carried` and each
   * screen chooses its own sentence by that (`legacy/js/core/failure-text.js`)
   * — but it is what a person looking at this page's own log or at devtools
   * gets, and "snippets are not carried" is a different fact from "something is
   * not carried". The table says which; without one this says the route.
   */
  private notCarried(method: string, path: string): string {
    return (
      this.options.carry?.detail(method, path) ??
      `${method} ${path} is not carried over Clawdline Cloud: do it on the Mac itself.`
    )
  }

  private refuse(method: string, path: string, status: number, code: string, detail: string): Response {
    this.note(method, path, "refused", code)
    return json(status, { error: code, detail, route: path })
  }

  private note(method: string, path: string, answer: SeamRow["answer"], code?: string, extra?: Partial<SeamRow>): void {
    const row: SeamRow = {
      at: this.now(),
      method,
      path,
      answer,
      ...(code ? { code } : {}),
      ...(extra?.word ? { word: extra.word } : {}),
      ...(typeof extra?.ms === "number" ? { ms: extra.ms } : {}),
      ...(extra?.ref ? { ref: extra.ref } : {}),
    }
    this.rows.push(row)
    if (this.rows.length > 400) this.rows.shift()
    this.options.onAnswer?.(row)
  }
}

class NotConnected extends Error {
  readonly code = "offline"
  constructor() {
    super("the relay connection is not up")
  }
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })
}

/**
 * A relay row as the console's list takes it. The Mac's own row is kept whole;
 * the relay's routing key is dropped, and `machine` stays, so the list can say
 * which Mac a row is on rather than "this Mac" (`legacy/bridge.ts`).
 */
function consoleRow(row: CloudRow): SessionRow {
  const { identity: _identity, optimisticIdentity: _optimistic, ...rest } = row as CloudRow & { optimisticIdentity?: unknown }
  return { ...rest, id: typeof row.id === "string" && row.id ? row.id : String(row.session ?? "") } as unknown as SessionRow
}

/** A transcript answer as `/v1/transcript` would give it. */
function transcriptPage(body: unknown, session: string): TranscriptPage {
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    throw Object.assign(new Error("the machine's transcript answer is not an object"), { code: "bad_payload" })
  }
  const { optimisticIdentity: _identity, ...page } = body as Record<string, unknown>
  return {
    ...page,
    id: typeof page.id === "string" && page.id ? page.id : session,
    entries: Array.isArray(page.entries) ? page.entries : [],
    signature: typeof page.signature === "string" ? page.signature : "",
    evidence: typeof page.evidence === "string" ? page.evidence : "transcript",
  } as TranscriptPage
}

/** A row with the fields that move on every reading taken out, as one comparable string. */
function keyOf(row: CloudRow): string {
  const copy = structuredClone(row) as Record<string, unknown>
  for (const path of FRESHNESS_ONLY) remove(copy, path)
  return JSON.stringify(copy)
}

function remove(object: Record<string, unknown>, path: readonly string[]): void {
  if (path.length === 1) {
    delete object[path[0]]
    return
  }
  const child = object[path[0]]
  if (child && typeof child === "object" && !Array.isArray(child)) remove(child as Record<string, unknown>, path.slice(1))
}
