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
import type { Health, SessionRow, SessionsSnapshot, TaskList, TaskRow, TranscriptPage } from "@clawdline/contract"
import type { StreamHandle, StreamHandlers, StreamTransport } from "@clawdline/core"
import type { CloudWriteClient, WriteHost, WriteRoute } from "./relay-writer.js"

/** One machine and one of its sessions, as the relay's channels name them. */
export interface CloudIdentity {
  machine: string
  session: string
}

/** A row as `CloudClient` holds it: the machine's own row, plus where it came from. */
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

/**
 * One task as the machine published it, plus the machine it came from.
 *
 * It is not a `TaskRow`: the machine cuts the record to the nine paths a viewer
 * reads before it goes out (`internal/transport/cloud/tasklist.go`
 * `cloudTaskFields`), so most of the contract's fields are simply not there.
 * Typed as what it is — an object with a machine on it — so nothing here reads
 * a field that was never sent.
 */
export type CloudTask = Record<string, unknown> & { machine?: string }

/** `CloudClient.tasks()`'s answer: every machine's rows, merged. */
export interface CloudTasks {
  tasks?: CloudTask[]
}

/** One schedule row as the machine's list answers it, plus the machine it came from. */
export type CloudSchedule = Record<string, unknown> & { machine?: string }

/**
 * `CloudClient.schedules()`'s answer: every machine's rows, merged, and — for
 * the `fresh` reading this seam makes — what the fan-out could not settle.
 *
 * `unanswered` is a machine that could have answered and did not, with its own
 * typed failure; `unconfirmed` is one that was never asked because its
 * descriptor has not arrived. Both matter here for one reason: the copied
 * client resolves as long as *some* machine answered, and a resolved answer
 * that is missing this machine's rows would be read as "this machine has none".
 */
export interface CloudSchedules {
  schedules?: CloudSchedule[]
  at?: number
  unanswered?: { machine?: string; label?: string; error?: unknown }[]
  unconfirmed?: string[]
}

/** One snippet as the machine's list answers it, plus the machine it came from. */
export type CloudSnippet = Record<string, unknown> & { machine?: string }

/**
 * `CloudClient.snippets()`'s answer for one machine: its rows, and when they
 * were read.
 *
 * There is no `unanswered` here and there does not need to be. The account-wide
 * form of that read fans out and settles on the first reply, which is why the
 * schedule list has to check who did not answer; the form this seam makes names
 * one machine, asks that one, and throws its own failure — `cloud_read_
 * unavailable` for a machine nothing has told us about, `cloud_snippets_
 * unpublished` for one whose inventory carries no such field. Neither ever
 * resolves as an empty list, because "this machine has none" and "nobody answered"
 * are opposite facts and the sheet draws a different thing for each.
 */
export interface CloudSnippets {
  snippets?: CloudSnippet[]
  at?: number
}

/** What `CloudClient.events()` hands a listener; only these fields are read. */
export interface CloudEvent {
  type: string
  state?: string
  machine?: string
  identity?: Partial<CloudIdentity>
  error?: unknown
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
  /** The current account rows, used here only to answer health for the chosen machine. */
  machines?(): Promise<{
    machines: { id: string; freshness: "current" | "stale" | "unknown" }[]
    syncing: boolean
    retryAfterMs: number
  }>
  sessions(): Promise<CloudSessions>
  /**
   * The dispatched work every machine on this account published, out of the
   * `orch/` snapshots this client has already decrypted
   * (`_allOrchestratorRows("tasks")`). Nothing is asked of any machine for it: the
   * list rides on the machine descriptor, which arrives whether or not a page
   * reads it.
   *
   * Optional for the same reason `pushKey` is: a copied client older than the
   * method must be refused by name rather than throw where nobody is catching.
   */
  tasks?(): Promise<CloudTasks>
  /**
   * This account's schedules. `{ fresh: true }` is the only reading this seam
   * makes, and not for freshness' sake: the retained `orch/` snapshot is where
   * `schedules()` answers from otherwise, and this daemon does not put its
   * schedules on it (`internal/transport/cloud/publish.go` carries `tasks` and
   * not these), so the retained reading refuses `cloud_schedules_unpublished`
   * forever. `fresh` asks each machine the word instead, which is the read the
   * machine has always answered.
   *
   * It is also what makes the four writes reachable: the copied client finds
   * which machine a schedule id belongs to by looking it up in the snapshot
   * (`_scheduleMachine`), and `fresh` is what puts the rows there.
   *
   * Optional for the same reason `pushKey` and `tasks` are: a copied client
   * older than the word must be refused by name rather than throw where
   * nobody is catching.
   */
  schedules?(options?: { fresh?: boolean }): Promise<CloudSchedules>
  /**
   * One machine's snippets. The identity names which — only its `machine` is
   * read (`_snippetRequest`), and the session travels so that what is asked
   * for is the machine the open session is on and not the first one in a
   * snapshot.
   *
   * `{ fresh: true }` for the reason the schedule list asks it: the retained
   * `orch/` snapshot is a first paint and not evidence, and a reconnect can
   * hand back one older than the feature. Optional for the reason `pushKey`
   * and `tasks` are: a copied client older than the word must be refused by
   * name rather than throw where nobody is catching.
   */
  snippets?(identity: CloudIdentity, options?: { fresh?: boolean }): Promise<CloudSnippets>
  transcript(identity: CloudIdentity, phases?: unknown, demand?: { foreground?: boolean }): Promise<unknown>
  /**
   * The application server key, as the machine's `push-key` read answers it.
   *
   * Optional because a copied client older than the word does not have it, and
   * a page that reached that build must be refused by name rather than throw
   * where nobody is catching. It is a read and not a write — asking is what
   * mints the key, and nothing about a session changes — so it is answered
   * here beside the transcript rather than in `relay-writer.ts`, where the
   * three requests that follow it live.
   */
  pushKey?(): Promise<unknown>
  /**
   * One read of one machine, by the word the machine lists.
   *
   * The copied client has a method per word of the Swift console's vocabulary
   * and none for a word that console never had — the whole work system, and
   * this daemon's Project catalog and verification ledger are four such words
   * — and the copy is held to its source byte for byte
   * (`tools/check-legacy-css.sh`), so a method cannot be added to it here.
   * This is the generic underneath all of them (`_machineRequest`), and the
   * reads carried through it get exactly what the named methods get: the
   * machine's own capability gate before anything is sealed, the
   * `unknown_command` a machine that lacks the word answers, and the
   * `machineLacks` memory that stops the page asking it twice.
   *
   * It is called for every word this file carries, including the four the
   * copied client does name, because those four pick their own machine —
   * `board` asks the account for one, `projectWorktrees` looks one up in the
   * places it has read — and this seam is reading one machine that was chosen
   * before the console was drawn. One path, one gate, one machine.
   *
   * Optional for the reason `pushKey` is: a client without it is refused by
   * name rather than throwing where nobody is catching.
   */
  _machineRequest?(machine: string, word: string, body: Record<string, unknown>, kind: "read"): Promise<unknown>
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
 * the answer from before it would hold the card at "the machine has it" for up to
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
  /** Successful terminal closes waiting for a newer terminal enumeration. */
  private readonly closedAt = new Map<string, number>()
  private readonly rows: SeamRow[] = []
  private readonly options: RelayReaderOptions
  private writer: WriteSeam | null = null
  /** Whether this page has already said what it and the machine disagree about (`drift`). */
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
      closed: (session) => this.closed(session),
      note: (row) => this.note(row.method, row.path, row.answer, row.code, row),
    }
  }

  /** Carry the console's writes through `writer` (`RelayWriter`); without one, every write is refused by name. */
  carryWrites(writer: WriteSeam): void {
    this.writer = writer
  }

  /**
   * Something was done to `session`. The next read asks the machine whatever
   * the row says, and — unless the machine refused, which changes nothing — every
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
   * A successful `end` is the daemon's terminal backend answering that the tab
   * or pane is gone. Hide a row observed before that answer immediately, and
   * tell every open list. This is deliberately not permanent client state: a
   * newer current terminal observation can show the same id again, while an
   * authoritative inventory without it confirms the deletion and clears it.
   */
  closed(session: string): void {
    this.closedAt.set(session, Math.floor(this.now() / 1000))
    for (const stream of this.streams) this.queueFrame(stream)
  }

  /**
   * What this bundle and this machine disagree about, right now, on the page.
   *
   * The build-time half of this is a Go test that reads `carry.ts`
   * (internal/app/cloudops/carry_test.go), and it can only compare this bundle
   * with the checkout it was built from. A hosted console is an *older* bundle
   * reading a machine that has been updated since, which no test in either repo can
   * see. The machine says what it can do in its own descriptor —
   * `cloudops.Implemented()`, carried as `machine.commands` — so the same
   * question is asked here of the machine actually being read.
   *
   * `notCarried` is what this machine answers and this bundle never asks for;
   * `notOnThisMachine` is what this bundle would ask for and this machine does not
   * list. Empty when the descriptor has not arrived: unknown is not agreement,
   * and `null` says which of the two this is.
   */
  drift(): { notCarried: string[]; notOnThisMachine: string[] } | null {
    const carried = this.options.carry?.carried
    if (!carried) return null
    const client = this.client as { machineDescriptor?: (machine: string) => { machine?: { commands?: unknown } } | null } | null
    const commands = client?.machineDescriptor?.(this.machine)?.machine?.commands
    if (!Array.isArray(commands)) return null
    const listed = new Set(commands.filter((word): word is string => typeof word === "string"))
    return {
      notCarried: [...listed].filter((word) => !carried.includes(word)).sort(),
      notOnThisMachine: carried.filter((word) => !listed.has(word)).sort(),
    }
  }

  /**
   * Put `drift` in this page's own log, once, as soon as the machine's
   * descriptor has arrived. It is a row and not a refusal because nothing is
   * broken: the page carries what it carries. It is recorded because the
   * alternative is what happened with `info` — a machine answering a word for
   * months, a page never asking for it, and nothing anywhere saying so.
   */
  private sayDrift(): void {
    if (this.saidDrift) return
    const found = this.drift()
    if (!found) return
    this.saidDrift = true
    if (!found.notCarried.length && !found.notOnThisMachine.length) return
    this.note("GET", "/v1/sessions", "local", "cloud_vocabulary_drift", {
      word: [...found.notCarried, ...found.notOnThisMachine.map((w) => "-" + w)].join(" "),
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
      // The one carried read whose parameter is a path segment rather than a
      // query field, so it cannot be a case below.
      const project = worktreeLifecycleProject(path)
      if (project) {
        this.only(url, path)
        return await this.machineRead(method, path, "project-worktree-lifecycle", { project })
      }
      switch (path) {
        case "/v1/sessions":
          this.note(method, path, "local")
          return json(200, await this.snapshot())
        case "/v1/orchestrator/tasks": {
          // The list the session list's indent is computed from. Without it
          // `groupUnderRoots` has nothing to group by and returns the rows as
          // they stand, which is what a phone showed: a flat list of sessions
          // where the console on the machine itself puts each child under the
          // session that dispatched it.
          //
          // It is answered here and not asked of the machine. The machine publishes
          // the list on its machine descriptor (`tasklist.go`), the copied
          // client holds every descriptor it has opened, and `tasks()` reads
          // it back out of that. So this costs the relay nothing at all — it
          // is the same bytes the page was already holding and throwing away.
          const client = this.connected()
          if (typeof client.tasks !== "function") {
            return this.refuse(method, path, 501, "cloud_not_carried",
              "This console cannot read this machine's dispatched work.")
          }
          const list = taskList(await client.tasks(), this.machine, Math.floor(this.now() / 1000))
          this.note(method, path, "local")
          return json(200, list)
        }
        case "/v1/orchestrator/schedules": {
          // The list under the session list, which on a phone drew nothing at
          // all: the word was in `DEFERRED` with a sentence saying schedules
          // were not read over Cloud yet, and the machine had been answering it
          // the whole time. The sentence was what had stopped being true.
          //
          // Unlike the task list this is asked of the machine (`schedules` is a
          // word, and this daemon publishes no schedules on its descriptor),
          // and the answer is cut to this machine's rows for the reason the
          // session list is: the copied client merges every machine's into
          // one, and a row from another machine under this one's list would be a
          // schedule this page cannot open, edit or run.
          const client = this.connected()
          if (typeof client.schedules !== "function") {
            return this.refuse(method, path, 501, "cloud_not_carried",
              "This console cannot read this machine's schedules.")
          }
          const answer = await client.schedules({ fresh: true })
          // **A resolved answer is not this machine's answer.** The read fans
          // out and settles as long as one machine replied, so a machine that
          // refused, timed out or was never asked comes back as an account
          // with fewer rows in it — which is this page saying "there are
          // none" on its behalf. Its own failure is raised instead, and the
          // page draws nothing rather than an empty list (`pages/schedules.tsx`).
          const missed = (answer.unanswered ?? []).find((row) => row?.machine === this.machine)
          if (missed) throw missed.error ?? new NotConnected()
          if ((answer.unconfirmed ?? []).includes(this.machine)) {
            return this.refuse(method, path, 503, "cloud_read_unavailable",
              "This machine has not published an inventory to this account yet.")
          }
          const schedules = (answer.schedules ?? []).filter((row) => row?.machine === this.machine)
          // Typed against the table, as `transcript` below is: dropping it
          // from `CARRIED` is a compile error here rather than a silent
          // disagreement.
          const word: CarriedWord = "schedules"
          this.note(method, path, "relay", undefined, { word })
          return json(200, { schedules, at: answer.at || Math.floor(this.now() / 1000) })
        }
        case "/v1/snippets": {
          // 常用句 — the sheet a phone could not open. Both halves were true
          // at once: this machine's catalog knew the word and had no route behind
          // it, so the seam listed it in `NO_MACHINE_ROUTE` and refused the read
          // to itself rather than asking a machine that would have said
          // `unknown_command`. The sentence was honest; what it described is
          // gone.
          //
          // **The whole machine's list, and the grouping stays on the page.**
          // The wire carries no session (`cloudops`' `snippets` op), so the
          // machine cannot resolve this session's project the way the local route
          // does and the answer names none: `view/snippets-data.js`'s
          // `snippetGroups` matches each row's `project` against the session's
          // own `cwd` instead. The rows are cut to this machine's for the
          // reason the schedule list's are — the copied client merges every
          // machine's rows into one, and another machine's snippet is text this
          // session cannot even be about.
          const session = url.searchParams.get("session")
          if (!session) return this.refuse(method, path, 400, "bad_request", "No session was named.")
          const client = this.connected()
          if (typeof client.snippets !== "function") {
            return this.refuse(method, path, 501, "cloud_not_carried",
              "This console cannot read this machine's snippets.")
          }
          const answer = await client.snippets({ machine: this.machine, session }, { fresh: true })
          const snippets = (answer.snippets ?? []).filter((row) => row?.machine === this.machine)
          const word: CarriedWord = "snippets"
          this.note(method, path, "relay", undefined, { word })
          return json(200, { snippets })
        }
        case "/v1/transcript": {
          const session = url.searchParams.get("session")
          if (!session) return this.refuse(method, path, 400, "bad_request", "No session was named.")
          // `cache: "no-store"` is a caller that must see the machine's answer
          // now: a failed send's "try again" checks the words did not arrive
          // after all before it types them a second time (`session/send.ts`).
          const fresh = init?.cache === "no-store" || init?.cache === "reload" || init?.cache === "no-cache"
          const { page, reused } = await this.transcript(session, fresh)
          // One of the three words this file carries itself — the others are
          // the schedule list and the snippet list above; the rest are the
          // writer's. Typed against the table so that dropping it from
          // `CARRIED` is a compile error here rather than a silent
          // disagreement.
          const word: CarriedWord = "transcript"
          this.note(method, path, reused ? "cache" : "relay", undefined, { word })
          return json(200, page)
        }
        case "/v1/health": {
          // The console's light asks this every fifteen seconds. The relay
          // socket and the chosen machine are two subjects: a live socket can
          // hold only a stale retained row for a machine that is gone. Health
          // is therefore successful only when the account list currently
          // measures this machine as current. A non-current row is a typed
          // refusal, so the header cannot call the relay's own line "live" on
          // behalf of a machine whose Session list is still waiting.
          const client = this.connected()
          if (typeof client.machines !== "function") {
            return this.refuse(method, path, 503, "machine_freshness_unavailable",
              "This console cannot measure whether the chosen machine is current.")
          }
          const answer = await client.machines()
          const machine = answer.machines.find((row) => row.id === this.machine)
          if (!machine) {
            return this.refuse(method, path, 503, "machine_not_reported",
              "The chosen machine has not reported to this account.")
          }
          if (machine.freshness !== "current") {
            const code = machine.freshness === "stale" ? "machine_stale" : "machine_freshness_unknown"
            return this.refuse(method, path, 503, code,
              "The chosen machine is not currently reporting through Clawdline Cloud.")
          }
          const health: Health = {
            at: Math.floor(this.now() / 1000),
            authed: true,
            ok: true,
            password: false,
            served_by: "cloud-machine-via-relay",
          }
          this.note(method, path, "local")
          return json(200, health)
        }
        case "/v1/push/key": {
          // The first request of a registration, and the one the whole
          // feature stopped on: a phone that pressed "notify me" got as far
          // as the iOS permission dialog and then asked for this, which was
          // refused here as a route this console does not carry. The three
          // requests that follow it are writes and are `relay-writer.ts`'s.
          const client = this.connected()
          if (typeof client.pushKey !== "function") {
            return this.refuse(method, path, 501, "cloud_not_carried",
              "This console cannot ask this machine for a notification key.")
          }
          const key = await client.pushKey()
          this.note(method, path, "relay", undefined, { word: "push-key" })
          return json(200, key)
        }
        case "/v1/board": {
          // One path, two words, told apart exactly as the page tells them
          // apart: a read that names an audience is the paged card list and
          // everything else is the envelope (`legacy/board-bridge.ts`).
          const q = this.only(url, path, "project", "item", "audience", "cursor", "limit")
          if (q.audience === undefined) {
            return await this.machineRead(method, path, "board", {
              project: q.project ?? "", item: q.item ?? "",
            })
          }
          return await this.machineRead(method, path, "board.items", {
            project: q.project ?? "", audience: q.audience,
            // Absent is not zero and not the maximum: this route reads a
            // missing `cursor` as 0 and a missing `limit` as its own default
            // (`clampInt`, internal/transport/http/board.go), and the page
            // leaves both off for the first page of a Project it has just
            // opened. The word carries two integers and no absence, so the
            // route's own numbers are what is sent — anything else would page
            // a phone differently from the browser on the machine.
            cursor: whole(q.cursor, 0),
            limit: whole(q.limit, BOARD_PAGE_DEFAULT),
          })
        }
        case "/v1/work/board":
        case "/v1/work/backlog": {
          // The board and the Backlog, paged the same way
          // (`pages/work/api.ts`). Two words and not one with an `area`,
          // because a word is the finest thing a machine's descriptor can
          // tell this page it has.
          const q = this.only(url, path, "project", "cursor")
          const word: CarriedWord = path === "/v1/work/board" ? "work.board" : "work.backlog"
          return await this.machineRead(method, path, word, {
            project: q.project ?? "", cursor: q.cursor ?? "",
          })
        }
        case "/v1/work/proposals": {
          const q = this.only(url, path, "project")
          return await this.machineRead(method, path, "work.proposals", { project: q.project ?? "" })
        }
        case "/v1/work/decisions": {
          this.only(url, path)
          return await this.machineRead(method, path, "work.decisions", {})
        }
        case "/v1/work/digests": {
          const q = this.only(url, path, "kind")
          return await this.machineRead(method, path, "work.digests", { kind: q.kind ?? "" })
        }
        case "/v1/projects": {
          // This daemon's Project catalog, which the Projects page reads as
          // its `board` (`legacy/projects-bridge.ts`); the Swift app's
          // `board` is the different, older reading above.
          this.only(url, path)
          return await this.machineRead(method, path, "projects", {})
        }
        case "/v1/orchestrator/usage/project-worktrees": {
          const q = this.only(url, path, "project")
          return await this.machineRead(method, path, "project-worktrees", { project: q.project ?? "" })
        }
        case "/v1/orchestrator/landings": {
          // No parameters: the landing ledger is machine-wide, and the one
          // page that reads it asks what this machine owes, not what one
          // repository does.
          this.only(url, path)
          return await this.machineRead(method, path, "landings", {})
        }
        case "/v1/orchestrator/usage/verification-ledger": {
          const q = this.only(url, path, "graph")
          return await this.machineRead(method, path, "verification-ledger", { graph: q.graph ?? "" })
        }
        case "/v1/timeline": {
          // `upcoming` is a filter with two meanings and the page sends it
          // every time; the rest are left as the page left them, because this
          // route reads an absent `environment` as production and an empty one
          // as a refusal (`internal/transport/http/timeline.go`).
          const q = this.only(url, path, "project", "entry", "cursor", "environment", "category", "upcoming")
          return await this.machineRead(method, path, "timeline", {
            project: q.project ?? "", entry: q.entry ?? "", cursor: q.cursor ?? "",
            environment: q.environment ?? "", category: q.category ?? "",
            upcoming: q.upcoming !== "false",
          })
        }
        case "/v1/strings": {
          // The words are the bundle's own file, not the machine's (`cloud/strings.ts`):
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
    const machineRows = all.sessions.filter((row) => row.machine === this.machine)
    const sessions = machineRows.filter((row) => {
      const id = typeof row.session === "string" ? row.session : typeof row.id === "string" ? row.id : ""
      const closedAt = this.closedAt.get(id)
      if (closedAt === undefined) return true
      const source = row.source && typeof row.source === "object" && !Array.isArray(row.source)
        ? row.source as { freshness?: unknown; observed_at?: unknown }
        : null
      // Only the terminal source may reverse the earlier terminal answer. A
      // carried/unverified row is display history, never existence evidence.
      if (source?.freshness === "current" && typeof source.observed_at === "number" && source.observed_at > closedAt) {
        this.closedAt.delete(id)
        return true
      }
      return false
    }).map(consoleRow)
    if (whole) {
      const present = new Set(machineRows.map((row) => typeof row.session === "string" ? row.session : row.id))
      for (const id of this.closedAt.keys()) {
        if (!present.has(id)) this.closedAt.delete(id)
      }
    }
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

  /**
   * One carried read, asked of the machine this page is reading.
   *
   * Every word goes out the same way, and the answer is the route's own body:
   * a refusal from the machine's route arrives as a typed failure and is turned
   * into the same refusal a daemon on this machine's own network would have
   * sent, by the `catch` in `fetch` above. Nothing here reads the body, so a
   * page's own shape is never a thing this seam has to keep right.
   */
  private async machineRead(method: string, path: string, word: CarriedWord, body: Record<string, unknown>): Promise<Response> {
    const client = this.connected()
    if (typeof client._machineRequest !== "function") {
      // A copied client older than the generic. Named rather than thrown:
      // this is the page refusing itself, and it says which word it is about.
      return this.refuse(method, path, 501, "cloud_not_carried",
        `This console cannot ask this machine for ${word}.`)
    }
    const answer = await client._machineRequest(this.machine, word, body, "read")
    this.note(method, path, "relay", undefined, { word })
    return json(200, answer)
  }

  /**
   * The query fields a carried read may carry, or a refusal naming one it may
   * not.
   *
   * A field this seam drops silently is the failure this whole table exists to
   * stop: the page asks a narrower question, the machine answers a wider one, and
   * the screen draws the answer to a question nobody asked. A word's body is a
   * fixed key set on this wire, so a field with nowhere to go is not carried —
   * and says so by its own name.
   */
  private only(url: URL, path: string, ...carried: string[]): Record<string, string | undefined> {
    const out: Record<string, string | undefined> = {}
    for (const [key, value] of url.searchParams) {
      if (!carried.includes(key)) {
        throw Object.assign(new Error(`${path}?${key}= is not carried over Clawdline Cloud: read it on the machine.`), {
          code: "cloud_not_carried", status: 501,
        })
      }
      out[key] = value
    }
    return out
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
      `${method} ${path} is not carried over Clawdline Cloud: do it on the machine itself.`
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

/**
 * The page size `/v1/board` uses when a read names no `limit`
 * (`boardstore.DefaultPageLimit`, internal/adapters/board/board.go). It is
 * here because the Cloud word carries two integers and no absence, so the
 * seam has to say what the route would have said. The two are checked against
 * each other by `TestTheBoardsCloudPageSizeIsTheRoutesOwn`.
 */
const BOARD_PAGE_DEFAULT = 30

/** A whole number from a query field, or `fallback` when it names none. */
function whole(value: string | undefined, fallback: number): number {
  const n = Number(value)
  return value !== undefined && Number.isSafeInteger(n) && n >= 0 ? n : fallback
}

/**
 * The Project id in `/v1/projects/{project}/worktrees`, or "" for any other
 * path. The refresh beside it (`/worktrees/refresh`) is a POST that runs
 * processes on the machine, so it is not this read and not this round.
 */
function worktreeLifecycleProject(path: string): string {
  const parts = path.split("/")
  if (parts.length !== 5 || parts[1] !== "v1" || parts[2] !== "projects" || parts[4] !== "worktrees") return ""
  try {
    return decodeURIComponent(parts[3])
  } catch {
    return ""
  }
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })
}

/**
 * A relay row as the console's list takes it. The machine's own row is kept whole;
 * the relay's routing key is dropped, and `machine` stays, so the list can say
 * which machine a row is on rather than "this machine" (`legacy/bridge.ts`).
 */
function consoleRow(row: CloudRow): SessionRow {
  const { identity: _identity, optimisticIdentity: _optimistic, ...rest } = row as CloudRow & { optimisticIdentity?: unknown }
  return { ...rest, id: typeof row.id === "string" && row.id ? row.id : String(row.session ?? "") } as unknown as SessionRow
}

/**
 * The machine's published task list, as `/v1/orchestrator/tasks` would answer it.
 *
 * **This machine's rows only.** The copied client merges every machine's
 * `orch/` snapshot into one list, and a terminal id is a tmux pane name — two
 * machines both have a `%1`. A row from another machine would sit under whichever
 * session here happened to share its name, so the machine is the filter, as it
 * is for the session list (`snapshot`). The rows keep the `machine` the client
 * tagged them with, for the reason a session row does: a row read across the
 * relay is not on this machine and says so.
 *
 * `page`, `store` and `source` are answered rather than left out. The type says
 * they are there, and a field that is declared and absent is the kind of thing
 * that is discovered by something breaking. So: `store` is `unknown`, because
 * this page has not read the machine's task store and cannot say how fresh it
 * is; `source` is `stale`, which is the honest word for what this is — the
 * machine's published projection, nine paths of each row rather than the row,
 * with no reading of the other store behind it. It is not `current`, because
 * this console did not read the machine, and it is not `missing`, because the
 * rows are real. A page drawing this number now knows not to call it the whole
 * answer; and this is not a page — the machine publishes
 * the whole list a viewer can reach on its descriptor, bounded there, and this
 * hands that over whole, so there is no cursor to follow and no limit was
 * asked for. `fields` says `cloud` rather than `list` because the rows are the
 * machine's Cloud projection — nine paths (`internal/transport/cloud/tasklist.go`)
 * — and a reader that finds a field missing can see from the answer why.
 */
function taskList(answer: CloudTasks, machine: string, at: number): TaskList {
  const rows = (answer.tasks ?? []).filter((row) => row.machine === machine)
  // Over, or still going, by the field the wire already decides it with rather
  // than by a third copy of the state rule: a finished task carries
  // `finishedAt`, and that is what holds its child's row in place while the
  // tab is open (`legacy/js/view/derive.js` `taskShaping`).
  const finished = rows.filter((row) => typeof row.finishedAt === "number" && row.finishedAt > 0).length
  return {
    at,
    page: { cursor: 0, fields: "cloud", limit: 0, finished, unfinished: rows.length - finished },
    store: "unknown",
    source: { observed_at: at, provenance: "relay", freshness: "stale" },
    tasks: rows as unknown as TaskRow[],
  }
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
