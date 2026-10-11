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
import type { SessionDestination } from "./all-machine-sessions.js"
import { authenticatedRefusalKey } from "./refusal-client.js"

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
  readonly allowWrites?: boolean
  /**
   * `keepConnected`'s hook on every client it made (`cloud-boot.js`,
   * `attach`): asked `"demand"`, it answers whether a connection is live or on
   * its way — `true`; `false` when none will. A hidden page answers the same
   * way: it keeps its connection. A retired client keeps it, which is what lets a
   * read asked through one wake the loop instead of failing at once.
   */
  lifecycle?(reason: string): boolean
  /** machine → the last inventory marker it published; present once one arrived. */
  readonly sessionInventoryByMachine?: Map<string, unknown>
  /** Content-free ss/ envelopes; the hosted Session list reads only these. */
  readonly statusSnapshots?: ReadonlyMap<string, unknown>
  /**
   * machine → its last decrypted `orch/` snapshot. Read here only for
   * `snapshot.app`'s `version` and `api_level` (`publishDescriptor`,
   * internal/transport/cloud/publish.go), duck-typed: a machine from before
   * those fields, or a copied client that keeps no such map, is unknown and
   * says nothing, never zero.
   */
  readonly orchestratorSnapshots?: Map<string, unknown>
  /**
   * machine → the descriptor this browser remembered from an earlier
   * connection, as `{ machine: { …, commands } }`
   * (`machineDescriptors`/`_loadMachineDescriptors`, legacy/js/net/cloud-client.js).
   * It is what decides a word before any snapshot has arrived, and its
   * `commands` list was cut to the first 64 words on the way into storage, so
   * it is read here for one thing only: to tell a word the machine does not
   * answer from a word the cut never reached (`machineWordPending`).
   *
   * Duck-typed like `orchestratorSnapshots`: a copied client that keeps no
   * such map says nothing, and nothing waits.
   */
  readonly machineDescriptors?: Map<string, unknown>
  /**
   * machine → the words it answered `unknown_command` to (`machineLacks`).
   * One client's own memory, emptied when a descriptor arrives; a word in it
   * is a machine that has itself said no, which is never waited for.
   */
  readonly machineLacks?: Map<string, unknown>
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
  transcriptForGeneration?(destination: { machineID: string; sessionID: string; executionGeneration: string }, signal?: AbortSignal): Promise<unknown>
  transcriptPageForGeneration?(destination: { machineID: string; sessionID: string; executionGeneration: string }, before: number, signal?: AbortSignal): Promise<unknown>
  readForGeneration?(destination: { machineID: string; sessionID: string; executionGeneration: string },
    word: string, fields: Record<string, unknown>, signal?: AbortSignal): Promise<unknown>
  /**
   * One provider subagent's conversation, and one background command's
   * output: the session reads whose subject is a second id.
   *
   * These two are session reads, not machine reads. The machine decodes each
   * body against an exact key set — `{type, session, agent, limit}`,
   * `{type, session, shell, bytes}` (`internal/app/cloudops/ops.go`) — and
   * answers on the session's own channel under `agent:<id>` / `shell:<id>`.
   * `_machineRequest` adds a `request` and waits on the machine's reply
   * channel, so the same words sent through it are a malformed read the
   * machine can answer to nobody: the subagent pane sat on its skeleton for
   * the whole read timeout, every time.
   *
   * The copied client asks each for a fixed window (`AGENT_LIMIT`,
   * `SHELL_BYTES`); `CLOUD_AGENT_LIMIT` and `CLOUD_SHELL_BYTES` below are the
   * same numbers, and a page asking for another window is refused by name.
   *
   * Optional for the reason `pushKey` is.
   */
  agent?(identity: CloudIdentity, agent: string): Promise<unknown>
  shell?(identity: CloudIdentity, shell: string): Promise<unknown>
  /** The copied client's session reader, used for cursor pages without changing the pinned copy. */
  _read?(identity: CloudIdentity, word: string, body: Record<string, unknown>, answer: string): Promise<unknown>
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
   * and none for a word that console never had — the whole work system and
   * this daemon's Project catalog are such words
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
   * `board` asks the account for one — and this seam is reading one machine
   * that was chosen before the console was drawn. One path, one gate, one
   * machine.
   *
   * Optional for the reason `pushKey` is: a client without it is refused by
   * name rather than throwing where nobody is catching.
   */
  _machineRequest?(machine: string, word: string, body: Record<string, unknown>, kind: "read" | "action", timeoutMs?: number, readOptions?: { signal?: AbortSignal }): Promise<unknown>
  /** Resolve a Cloud-safe Project picker id to the machine-local Project id it names. */
  _place?(value: unknown): { machine: string; id: string; path: string }
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
 * The copied client's words for "this machine does not have that word"
 * (`UNSUPPORTED_CODES` and `_unsupportedRefusal`, legacy/js/net/cloud-client.js).
 * The same three `machineNeedsUpdate` (core/src/refusal.ts) reads as "needs an
 * update"; this file only uses them to attach the machine's version.
 */
const MACHINE_LACKS_WORD: ReadonlySet<string> = new Set(["unknown_command", "cloud_feature_unavailable", "cloud_machine_unsupported"])

/**
 * The chosen machine's Clawdline version and route level, out of the
 * `app` object of its live `orch/` snapshot. Each is present only when the
 * machine said it: absent is unknown, never zero.
 */
export function machineApp(client: CloudReadClient, machine: string): { version?: string; api_level?: number } {
  const snapshot = client.orchestratorSnapshots?.get?.(machine)
  const app = typeof snapshot === "object" && snapshot !== null ? (snapshot as { app?: unknown }).app : undefined
  if (typeof app !== "object" || app === null) return {}
  const { version, api_level } = app as { version?: unknown; api_level?: unknown }
  return {
    ...(typeof version === "string" && version ? { version: version.slice(0, 64) } : {}),
    ...(typeof api_level === "number" && Number.isInteger(api_level) && api_level >= 0 ? { api_level } : {}),
  }
}

/**
 * How long a read waits for the chosen machine's own list of words before it
 * is asked or refused (`machineWordPending`). Registered as
 * `console.relay_feature_wait_seconds`.
 *
 * A page that has just opened holds the descriptor it remembered from last
 * time, and the copied client cut that list to its first 64 words on the way
 * into storage while this daemon publishes 151 (`cloudops.Implemented()`). So
 * for the moment between the socket coming up and the first `orch/` envelope
 * being opened, a read of a word past the cut was refused by the page itself,
 * before anything was sent: measured from this machine's daemon log on
 * 2026-10-10, 11 of 21 read failures between 11:30 and 14:22 were
 * `cloud_feature_unavailable` with `stage=viewer_refused`, in bursts at 12:08,
 * 12:41 and 13:29, every one of them for `transcript` or
 * `work.v2.session-todos` — the two words the page reads first and the cut
 * does not reach.
 *
 * Five seconds gives the live descriptor a chance to arrive. If it does not,
 * a cut remembered list is still inconclusive: StatusCloudClient asks the
 * named machine and lets its signed answer decide.
 */
export const FEATURE_WAIT_MS = 5_000

/**
 * The words a remembered descriptor can hold: `descriptorCommands` in the
 * copied client keeps `value.slice(0, 64)`. A remembered list this long is one
 * that was cut, so it says nothing about the words past it — and a shorter one
 * is the whole truth, which is why a machine that really lacks a word is still
 * refused at once.
 */
export const REMEMBERED_COMMANDS_CUT = 64

/** An object that is not an array, which is what a descriptor is. */
function descriptorObject(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : null
}

/**
 * Whether this page is about to refuse itself a word on evidence that was cut
 * short, and the machine's own word list could still arrive.
 *
 * It mirrors the copied client's `_descriptorFor` and `_machineImplements`,
 * because what it has to answer is what that pair is about to answer and why:
 *
 * - a live `orch/` snapshot's descriptor is the machine's current word list,
 *   whole, so there is nothing to wait for — including when it carries no
 *   `commands` at all, which is a machine the client judges by platform;
 * - a word the machine itself answered `unknown_command` to is a fact, not a
 *   cut, so it is refused now as it is today;
 * - with nothing remembered the client answers "unknown", which it does not
 *   refuse, so again nothing waits;
 * - a remembered list that names the word answers "yes" — nothing waits;
 * - what is left is a remembered list that does not name the word. If the
 *   list is at the cut it is not evidence of anything and the live descriptor
 *   is worth waiting for; shorter than the cut, it is the whole list and the
 *   machine really does not answer the word.
 */
export function machineWordPending(client: CloudReadClient, machine: string, word: string): boolean {
  if (!machine || !word) return false
  const snapshot = descriptorObject(client.orchestratorSnapshots?.get?.(machine))
  if (snapshot && descriptorObject(snapshot.machine)) return false
  const lacks = client.machineLacks?.get?.(machine) as { has?(word: string): unknown } | undefined
  if (typeof lacks?.has === "function" && lacks.has(word)) return false
  const remembered = descriptorObject(descriptorObject(client.machineDescriptors?.get?.(machine))?.machine)
  const commands = remembered?.commands
  if (!Array.isArray(commands) || commands.includes(word)) return false
  return commands.length >= REMEMBERED_COMMANDS_CUT
}

/** The copied client's `AGENT_LIMIT`: the entries one subagent read carries. */
export const CLOUD_AGENT_LIMIT = 200
/** The copied client's `TRANSCRIPT_LIMIT`: the entries one session read carries. */
export const CLOUD_TRANSCRIPT_LIMIT = 200
/** The copied client's `SHELL_BYTES`: the tail one background-command read carries. */
export const CLOUD_SHELL_BYTES = 64 * 1024

/**
 * The longest an answer is reused with nothing on its row moving at all. A
 * Swift Mac puts `transcript_signature` on the row and the page re-reads when
 * it changes; the Go daemon does not compute one yet (docs/records/cloud-wire-implementation-2026-09.md
 * §16.6), so a row can stay still while its transcript grows. This is the
 * bound on how stale that can make the page.
 */
export const TRANSCRIPT_MAX_REUSE_MS = 30_000

/**
 * How long a pinned read waits for the rest of a pass when the page holds a
 * Session row newer than its marker (`coherentSession`). A pass is ten-odd
 * envelopes sent back to back, so its marker follows within milliseconds; a
 * pass the machine stopped sending is still refused after this.
 */
export const PASS_SETTLE_MS = 2_000

/**
 * How often, at most, a reader asks the machine to restate its rows because the
 * Session a person has open is not among them.
 *
 * A row is published when the machine has something to say about that Session
 * and restated on its Cloud status cadence; an idle Session's row is therefore
 * minutes old and arrives again only when that cadence comes round. A page that
 * does not hold it has nothing to draw the opened Session with, and used to
 * wait: measured on 2026-10-11, re-reading one from the relay's retained
 * channel took 19.6 seconds, with the pane saying "Reading this Session's
 * conversation" for all of it, while the conversation itself answered in 131 ms.
 * Asking the machine for its list instead put the row back in one second.
 */
export const ROW_RESTATE_MS = 5_000

/**
 * How long, after this page did something to a session, every poll asks the
 * machine again until the transcript it answers has changed. A message just
 * typed is the turn the page is waiting to see (`session/pending.ts`); reusing
 * the answer from before it would hold the card at "the machine has it" for up to
 * `TRANSCRIPT_MAX_REUSE_MS` with the turn already written.
 */
export const TRANSCRIPT_EXPECT_MS = 45_000

/**
 * Inside `TRANSCRIPT_EXPECT_MS`, the machine is asked again when the row moved,
 * and otherwise at most this often. The page asks every two seconds while a
 * card is on its way (`session/transcript-follow.ts`); every one of those used
 * to be an envelope to the machine for 45 seconds after any write, whether or
 * not anything had been written. This is the page's old pace, so the card is
 * replaced no later than it was.
 */
export const TRANSCRIPT_EXPECT_REREAD_MS = 4_000

/**
 * How long a machine read waits for this page's own Cloud connection when it
 * is asked while that connection is renewing or reconnecting and `keepConnected`
 * says one is on its way; a press in that second used to fail at once as
 * `offline`.
 */
export const RECONNECT_WAIT_MS = 10_000

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

/** Row paths that move on every reading without making the transcript newer (`publish.go`), plus the line. */
const FRESHNESS_ONLY: readonly (readonly string[])[] = [
  ["line"],
  ["source", "observed_at"],
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

/** Content-free stages of the two Session header reads, retained for one hour. */
export interface HeaderReadDiagnostic {
  at: number
  operation: "todos" | "attention"
  stage: "connection_unavailable" | "ask_started" | "answer_observed" | "caller_canceled" | "read_failed"
  code: string
  elapsed: "<1s" | "1-5s" | "5-15s" | ">15s"
}

const HEADER_DIAGNOSTIC_KEY = "clawdline.header-read-diagnostics.v1"
const HEADER_DIAGNOSTIC_LIMIT = 24
const HEADER_DIAGNOSTIC_AGE_MS = 60 * 60 * 1000
const HEADER_CODES = new Set(["cloud_reconnecting", "cloud_not_carried", "cloud_read_abandoned", "offline", "cloud_failed", "cloud_read_unavailable", "cloud_read_timeout", "cloud_read_settled", "socket_error", "cloud_starting"])

/** Revalidate stored data on every read, so malformed or older records cannot be copied. */
export function headerReadDiagnostics(now = Date.now()): HeaderReadDiagnostic[] {
  try {
    const raw = JSON.parse(sessionStorage.getItem(HEADER_DIAGNOSTIC_KEY) || "[]") as unknown
    if (!Array.isArray(raw)) return []
    return raw.filter((row): row is HeaderReadDiagnostic => {
      if (!row || typeof row !== "object") return false
      const r = row as Record<string, unknown>
      return Number.isFinite(r.at) && typeof r.at === "number" && r.at <= now && r.at > now - HEADER_DIAGNOSTIC_AGE_MS
        && (r.operation === "todos" || r.operation === "attention")
        && ["connection_unavailable", "ask_started", "answer_observed", "caller_canceled", "read_failed"].includes(String(r.stage))
        && typeof r.code === "string" && (r.code === "none" || r.code === "other" || HEADER_CODES.has(r.code))
        && ["<1s", "1-5s", "5-15s", ">15s"].includes(String(r.elapsed))
    }).slice(-HEADER_DIAGNOSTIC_LIMIT).map((r) => ({ at: r.at, operation: r.operation, stage: r.stage, code: r.code, elapsed: r.elapsed }))
  } catch { return [] }
}

function recordHeaderRead(row: HeaderReadDiagnostic): void {
  try { sessionStorage.setItem(HEADER_DIAGNOSTIC_KEY, JSON.stringify([...headerReadDiagnostics(row.at), row].slice(-HEADER_DIAGNOSTIC_LIMIT))) }
  catch { /* storage can be disabled without changing a read */ }
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
  /** `RECONNECT_WAIT_MS`, for a test. */
  reconnectWaitMs?: number
  /** `FEATURE_WAIT_MS`, for a test. */
  featureWaitMs?: number
  /** Hosted ss/ compatibility clock; no rich s/ snapshot is read when supplied. */
  statusList?: () => { at: number; complete: boolean }
  /** The original one-machine page reads exact rich rows named by ss/. */
  classicStatus?: boolean
  /** The fleet detail currently admitted for this reader; absent in single-machine mode. */
  fleetTarget?: () => SessionDestination | null
  /** Recheck one visible fleet mutation against the latest ss/ pass. */
  admitFleetMutation?: (target: SessionDestination) => Promise<void>
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

function inventoryRowKeys(value: unknown): ReadonlySet<string> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null
  const ids = (value as { ids?: unknown }).ids
  if (ids instanceof Set && [...ids].every((id) => typeof id === "string")) return ids as Set<string>
  if (Array.isArray(ids) && ids.every((id) => typeof id === "string")) return new Set(ids)
  return null
}

export class RelayReader {
  private client: CloudReadClient | null = null
  private readonly now: () => number
  /** Identifies this page's sequence of snapshots, as a daemon's epoch identifies its process. */
  private readonly epoch: number
  private generation = 0
  private readonly streams = new Set<OpenStream>()
  /** The first fleet read waits for the inventory receipt before it paints. */
  private initialSnapshotDelivered = false
  private initialSnapshotFlight: Promise<SessionsSnapshot> | null = null
  private readonly transcripts = new Map<string, HeldTranscript>()
  /** Successful terminal closes waiting for a newer terminal enumeration. */
  private readonly closedAt = new Map<string, number>()
  private readonly rows: SeamRow[] = []
  private readonly options: RelayReaderOptions
  private writer: WriteSeam | null = null
  /** Machine reads waiting for a connected client (`connectedFor`). */
  private readonly awaitingClient = new Set<() => void>()
  /** Whether this page has already said what it and the machine disagree about (`drift`). */
  private saidDrift = false
  /** The opened destination this reader last asked the machine to restate, and when. */
  private rowAsked: { key: string; at: number } | null = null
  /** The one machine this reads. */
  readonly machine: string

  constructor(machine: string, options: RelayReaderOptions = {}) {
    this.machine = machine
    this.options = options
    this.now = options.now ?? (() => Date.now())
    this.epoch = this.now()
  }

  /** The paired device's current write capability, for disabling controls. */
  mayWrite(): boolean {
    return this.client?.allowWrites === true
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
    if (client.ready !== false) for (const wake of [...this.awaitingClient]) wake()
  }

  /**
   * What `relay-writer.ts` is given of this seam: the machine, the client, the
   * log, and `wrote`, which is how a write reaches the reads after it.
   */
  get writeHost(): WriteHost {
    return {
      machine: this.machine,
      exactTarget: this.options.fleetTarget,
      admitExactTarget: this.options.admitFleetMutation,
      connected: () => this.connected() as CloudWriteClient,
      connectedFor: async (signal, word) => {
        const client = await this.connectedFor(signal)
        if (!client && signal?.aborted) throw new AbandonedRead(word)
        return client as CloudWriteClient | null
      },
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

  /** The line went away (`reconnecting`, a terminal refusal). */
  lost(): void {
    for (const stream of this.streams) stream.handlers.onError?.(new Error("the relay connection is down"))
  }

  /**
   * `fetch`, for the console's own-origin `/v1/…` requests.
   *
   * A Session read that fails — the conversation, an older page of it, its
   * to-dos, its info, the Session list — is also recorded in the copied
   * client's own failure log (`cloud.read.failed`), which that client delivers
   * to the paired machine's log by itself (`diagnostics.events`). On
   * 2026-10-10 the hosted page said "could not read the conversation" several
   * times an hour while the machine's log held no refused or slow read: the
   * row says where it failed, so the machine's log can say which side it was.
   */
  readonly fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const started = this.now()
    const seen: { error?: unknown } = {}
    const read = readFailureWord(input, init)
    try {
      const answer = await this.answerFetch(input, init, seen)
      if (read && answer.status >= 400) this.reportReadFailure(read, started, seen.error, answer)
      return answer
    } catch (error) {
      if (read) this.reportReadFailure(read, started, seen.error ?? error, null)
      throw error
    }
  }

  private readonly answerFetch = async (input: RequestInfo | URL, init: RequestInit | undefined, seen: { error?: unknown }): Promise<Response> => {
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
      const projectFile = projectFileRoute(path)
      if (projectFile) {
        this.only(url, path)
        const client = this.connected()
        if (typeof client._place !== "function") throw Object.assign(new Error("the Cloud client cannot resolve this Project"), { code: "cloud_not_carried", status: 501 })
        const place = client._place(projectFile.project)
        if (place.machine !== this.machine) throw Object.assign(new Error("this Project belongs to another machine"), { code: "cloud_project_machine_mismatch", status: 409 })
        return await this.machineRead(init?.signal, method, path,
          projectFile.file ? "project-file-read" : "project-file-list",
          { project: place.id, ...(projectFile.file ? { file: projectFile.file } : {}) })
      }
      const unifyProject = projectUnifyRoute(path)
      if (unifyProject) {
        this.only(url, path)
        const client = this.connected()
        if (typeof client._place !== "function") throw Object.assign(new Error("the Cloud client cannot resolve this Project"), { code: "cloud_not_carried", status: 501 })
        const place = client._place(unifyProject)
        if (place.machine !== this.machine) throw Object.assign(new Error("this Project belongs to another machine"), { code: "cloud_project_machine_mismatch", status: 409 })
        return await this.machineRead(init?.signal, method, path, "project-unify-plan", { project: place.id })
      }
      const projectTree = projectTreeRoute(path)
      if (projectTree) {
        const query = this.only(url, path, projectTree.file ? "path" : "directory")
        const client = this.connected()
        if (typeof client._place !== "function") throw Object.assign(new Error("the Cloud client cannot resolve this Project"), { code: "cloud_not_carried", status: 501 })
        const place = client._place(projectTree.project)
        if (place.machine !== this.machine) throw Object.assign(new Error("this Project belongs to another machine"), { code: "cloud_project_machine_mismatch", status: 409 })
        return await this.machineRead(init?.signal, method, path,
          projectTree.file ? "project-tree-read" : "project-tree-list",
          { project: place.id, ...(projectTree.file ? { path: query.path ?? "" } : { directory: query.directory ?? "" }) })
      }
      // The one carried read whose parameter is a path segment rather than a
      // query field, so it cannot be a case below.
      const project = worktreeLifecycleProject(path)
      if (project) {
        this.only(url, path)
        return await this.machineRead(init?.signal, method, path, "project-worktree-lifecycle", { project })
      }
      const schedule = scheduleDetailID(path)
      if (schedule) {
        // `?machine=` is the machine the list said the row is on: a schedule
        // lives on the machine that runs it, which need not be this page's.
        const q = this.only(url, path, "machine")
        return await this.machineRead(init?.signal, method, path, "schedule", { id: schedule }, q.machine || undefined)
      }
      const agent = sessionAgent(path)
      if (agent) {
        const q = this.only(url, path, "limit", "before")
        this.window(path, "limit", q.limit, CLOUD_AGENT_LIMIT)
        const before = historyCursor(q.before)
        if (q.before !== undefined && !before) return this.refuse(method, path, 400, "invalid_cursor", "Invalid transcript cursor.")
        return await this.sessionRead(init?.signal, method, path, "agent", (client) =>
          this.options.fleetTarget?.() ? this.coherentSession(agent.session, init?.signal, "agent").then((target) => client.readForGeneration?.(target, "agent",
            { agent: agent.agent, limit: CLOUD_AGENT_LIMIT, ...(before ? { before } : {}) }, init?.signal ?? undefined))
            : before ? client._read?.({ machine: this.machine, session: agent.session }, "agent",
            { agent: agent.agent, limit: CLOUD_AGENT_LIMIT, before }, `agent:${agent.agent}.before.${before}`)
            : client.agent?.({ machine: this.machine, session: agent.session }, agent.agent))
      }
      const shell = sessionShell(path)
      if (shell) {
        const q = this.only(url, path, "bytes")
        this.window(path, "bytes", q.bytes, CLOUD_SHELL_BYTES)
        return await this.sessionRead(init?.signal, method, path, "shell", (client) =>
          this.options.fleetTarget?.() ? this.coherentSession(shell.session, init?.signal, "shell").then((target) => client.readForGeneration?.(target, "shell",
            { shell: shell.shell, bytes: CLOUD_SHELL_BYTES }, init?.signal ?? undefined))
            : client.shell?.({ machine: this.machine, session: shell.session }, shell.shell))
      }
      const workTerminal = workV2SessionTodosTerminal(path)
      if (workTerminal) {
        const q = this.only(url, path, "summary")
        if (q.summary && q.summary !== "1") return this.refuse(method, path, 400, "invalid_summary", "Summary must be 1.")
        return await this.machineRead(init?.signal, method, path, "work.v2.session-todos",
          { terminal: workTerminal, ...(q.summary ? { summary: true } : {}) })
      }
      const humanConversation = workV2HumanInterventionConversation(path)
      if (humanConversation) {
        this.only(url, path)
        return await this.machineRead(init?.signal, method, path, "work.v2.human-interventions", { terminal: humanConversation })
      }
      const workItem = workV2ItemID(path)
      if (path.startsWith("/v1/work/decisions/")) {
        this.only(url, path)
        const id = path.slice("/v1/work/decisions/".length)
        if (!/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(id)) {
          return this.refuse(method, path, 404, "not_found", "No such decision route.")
        }
        return await this.machineRead(init?.signal, method, path, "work.decision", { id })
      }
      if (path.startsWith("/v1/squad/definitions/")) {
        const q = this.only(url, path, "version")
        const id = decodeURIComponent(path.slice("/v1/squad/definitions/".length))
        if (!id || id.includes("/")) return this.refuse(method, path, 404, "unknown_definition", "No definition has that ID.")
        return await this.machineRead(init?.signal, method, path, "squad.definition", { definition_id: id, ...(q.version ? { version: q.version } : {}) })
      }
      if (path.startsWith("/v1/squad/session-snapshots/")) {
        this.only(url, path)
        const conversation = decodeURIComponent(path.slice("/v1/squad/session-snapshots/".length))
        if (!conversation || conversation.includes("/")) return this.refuse(method, path, 404, "snapshot_not_found", "No role snapshot belongs to that conversation.")
        return await this.machineRead(init?.signal, method, path, "squad-session-snapshot", { conversation })
      }
      const gateExport = workV2ItemActionID(path, "gate-export")
      if (gateExport) {
        this.only(url, path)
        return await this.machineRead(init?.signal, method, path, "work.v2.gate-export", { id: gateExport })
      }
      if (workItem) {
        this.only(url, path)
        return await this.machineRead(init?.signal, method, path, "work.v2.item", { id: workItem })
      }
      switch (path) {
        case "/v1/squad/catalog":
          this.only(url, path)
          return await this.machineRead(init?.signal, method, path, "squad.catalog", {})
        case "/v1/squad/scopes":
          this.only(url, path)
          return await this.machineRead(init?.signal, method, path, "squad.scopes", {})
        case "/v1/squad/skill-sources": {
          const q = this.only(url, path, "provider", "place_id", "id", "folder")
          if (!q.provider || !["project", "claude-code", "codex"].includes(q.provider) ||
            (!!q.id !== !!q.folder) || (q.folder && q.folder !== "true" && q.folder !== "false")) {
            return this.refuse(method, path, 400, "invalid_source", "Choose a listed skill source.")
          }
          return await this.machineRead(init?.signal, method, path, "squad.skill-sources", q)
        }
        case "/v1/squad/settings": {
          const q = this.only(url, path, "place_id", "scope_id")
          if (q.place_id && q.scope_id) return this.refuse(method, path, 400, "scope_mismatch", "Name one Project scope.")
          return await this.machineRead(init?.signal, method, path, "squad.settings", q.place_id ? { place_id: q.place_id } : q.scope_id ? { scope_id: q.scope_id } : {})
        }
        case "/v1/squad/session-bindings":
          this.only(url, path)
          return await this.machineRead(init?.signal, method, path, "squad-session-bindings", {})
        case "/v1/squad/events/head":
          this.only(url, path)
          return await this.machineRead(init?.signal, method, path, "squad-event-head", {})
        case "/v1/squad/events": {
          const q = this.only(url, path, "after", "limit")
          const after = q.after ?? "0", limit = q.limit ?? "50"
          if (!/^(0|[1-9][0-9]*)$/.test(after) || !/^[1-9][0-9]*$/.test(limit) || !Number.isSafeInteger(Number(after)) || !Number.isSafeInteger(Number(limit))) {
            return this.refuse(method, path, 400, "invalid_cursor", "The squad event cursor or limit is invalid.")
          }
          return await this.machineRead(init?.signal, method, path, "squad-events", { after: Number(after), limit: Number(limit) })
        }
        case "/v1/sessions":
          this.note(method, path, "local")
          return json(200, await this.initialSnapshot(init?.signal,
            new Headers(init?.headers).get("X-Clawdline-Initial-Read") === "1"))
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
        case "/v1/orchestrator/leases":
          this.only(url, path)
          return await this.machineRead(init?.signal, method, path, "coordination.leases", {})
        case "/v1/orchestrator/waits":
          this.only(url, path)
          return await this.machineRead(init?.signal, method, path, "coordination.waits", {})
        case "/v1/orchestrator/pauses":
          this.only(url, path)
          return await this.machineRead(init?.signal, method, path, "coordination.pauses", {})
        case "/v1/orchestrator/schedules": {
          // The list under the session list, which on a phone drew nothing at
          // all: the word was in `DEFERRED` with a sentence saying schedules
          // were not read over Cloud yet, and the machine had been answering it
          // the whole time. The sentence was what had stopped being true.
          //
          // Unlike the task list this is asked of the machines (`schedules` is
          // a word, and this daemon publishes no schedules on its descriptor).
          //
          // **Every machine's rows, each with its machine.** This answer used
          // to be cut to this machine's rows, because a row from another
          // machine was a schedule this page could not open, edit or run. It
          // can now: each action names the row's machine (`?machine=`, below
          // and in `relay-writer.ts`), and a schedule is chosen onto a machine
          // and moved between them from this list (docs/schedules.md). A
          // person with a Mac and a Linux machine sees both machines'
          // schedules without switching the whole console.
          this.only(url, path)
          const client = this.connected()
          if (typeof client.schedules !== "function") {
            return this.refuse(method, path, 501, "cloud_not_carried",
              "This console cannot read this machine's schedules.")
          }
          const answer = await client.schedules({ fresh: true })
          // **A resolved answer is not every machine's answer.** The read fans
          // out and settles as long as one machine replied, so a machine that
          // refused, timed out or was never asked would otherwise come back as
          // a machine with no schedules. Each is named instead, with its code,
          // and the page draws it as a machine it could not read — never as an
          // empty list (`pages/schedules.tsx`). Only the typed code goes out:
          // the failure object is the copied client's and is not JSON.
          const unanswered = [
            ...(answer.unanswered ?? []).flatMap((row) => typeof row?.machine === "string" && row.machine
              ? [{ machine: row.machine, label: row.label ?? row.machine, code: failureCode(row.error) }]
              : []),
            ...(answer.unconfirmed ?? []).map((machine) => ({ machine, label: machine, code: "cloud_read_unavailable" })),
          ]
          const schedules = (answer.schedules ?? []).filter((row) => typeof row?.machine === "string" && row.machine)
          // Typed against the table, as `transcript` below is: dropping it
          // from `CARRIED` is a compile error here rather than a silent
          // disagreement.
          const word: CarriedWord = "schedules"
          this.note(method, path, "relay", undefined, { word })
          return json(200, { schedules, at: answer.at || Math.floor(this.now() / 1000), unanswered })
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
          // An appended read (`after=`) is the daemon's own: this seam answers
          // from its held page, which is not "only what was appended", so it
          // says no and the page reads whole. Pages from here never carry
          // `nextAfter`, so a page only asks this of a daemon it reaches itself.
          if (url.searchParams.has("after")) {
            return this.refuse(method, path, 501, "cloud_not_carried", "Appended transcript reads are not carried over Clawdline Cloud.")
          }
          const beforeRaw = url.searchParams.get("before")
          const before = historyCursor(beforeRaw)
          if (beforeRaw !== null && !before) return this.refuse(method, path, 400, "invalid_cursor", "Invalid transcript cursor.")
          if (before) {
            const client = this.connected()
            if (this.options.classicStatus ? !client.transcriptPageForGeneration : !client._read) {
              return this.refuse(method, path, 501, "cloud_not_carried", "This console cannot read earlier messages.")
            }
            const body = this.options.classicStatus
              ? await client.transcriptPageForGeneration!(await this.coherentSession(session, init?.signal), before, init?.signal ?? undefined)
              : await client._read?.({ machine: this.machine, session }, "transcript",
                { limit: CLOUD_TRANSCRIPT_LIMIT, before, priority: "foreground" }, `transcript.before.${before}`)
            const page = transcriptPage(body, session)
            this.note(method, path, "relay", undefined, { word: "transcript" })
            return json(200, page)
          }
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
            ...machineApp(client, this.machine),
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
            return await this.machineRead(init?.signal, method, path, "board", {
              project: q.project ?? "", item: q.item ?? "",
            })
          }
          return await this.machineRead(init?.signal, method, path, "board.items", {
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
        case "/v1/work/proposals": {
          const q = this.only(url, path, "project")
          return await this.machineRead(init?.signal, method, path, "work.proposals", { project: q.project ?? "" })
        }
        case "/v1/work/decisions": {
          this.only(url, path)
          return await this.machineRead(init?.signal, method, path, "work.decisions", {})
        }
        case "/v1/work/digests": {
          const q = this.only(url, path, "kind")
          return await this.machineRead(init?.signal, method, path, "work.digests", { kind: q.kind ?? "" })
        }
        case "/v1/work/v2/items": {
          const q = this.only(url, path, "project", "status", "q", "cursor")
          let project = q.project ?? ""
          if (project) {
            const client = this.connected()
            if (typeof client._place !== "function") {
              throw Object.assign(new Error("the Cloud client cannot resolve this Project"), {
                code: "cloud_not_carried", status: 501,
              })
            }
            const place = client._place(project)
            if (place.machine !== this.machine) {
              throw Object.assign(new Error("this Project belongs to another machine"), {
                code: "cloud_project_machine_mismatch", status: 409,
              })
            }
            project = place.id
          }
          // Keep the first page's older body exact: daemons from before Board
          // pagination reject an extra empty cursor instead of ignoring it.
          if (q.status !== undefined || q.q !== undefined) {
            return await this.machineRead(init?.signal, method, path, "work.v2.search", {
              project, status: q.status ?? "open", query: q.q ?? "",
              ...(q.cursor ? { cursor: q.cursor } : {}),
            })
          }
          return await this.machineRead(init?.signal, method, path, "work.v2.items", {
            project, ...(q.cursor ? { cursor: q.cursor } : {}),
          })
        }
        case "/v1/work/v2/proposals": {
          const q = this.only(url, path, "state")
          return await this.machineRead(init?.signal, method, path, "work.v2.proposals", { state: q.state ?? "" })
        }
        case "/v1/projects": {
          // This daemon's Project catalog, which the Projects page reads as
          // its `board` (`legacy/projects-bridge.ts`); the Swift app's
          // `board` is the different, older reading above.
          this.only(url, path)
          return await this.machineRead(init?.signal, method, path, "projects", {})
        }
        case "/v1/personas": {
          // The built-in personas a start may name (docs/personas.md). A
          // machine whose descriptor does not list the word is a daemon from
          // before them, and the copied client refuses the read before it
          // leaves (`_unsupportedRefusal`: `cloud_machine_unsupported`, or
          // `cloud_feature_unavailable` from a Mac), so the start sheet hears
          // "none" now rather than at the read timeout.
          //
          // `?machine=` is the start sheet's, for a Session being started on a
          // machine other than the one this page reads: the roles offered have
          // to be the roles the machine that will run it knows. It is the same
          // naming `/v1/places` takes for that sheet's Projects.
          const q = this.only(url, path, "machine")
          return await this.machineRead(init?.signal, method, path, "personas", {}, q.machine || undefined)
        }
        // Project settings sync (docs/project-sync.md): this machine's offer,
        // one offered project in full, and what this machine mirrors.
        case "/v1/project-sync/manifest":
          this.only(url, path)
          return await this.machineRead(init?.signal, method, path, "project-manifest", {})
        case "/v1/project-sync/entry": {
          const q = this.only(url, path, "repo")
          return await this.machineRead(init?.signal, method, path, "project-entry", { repo: q.repo ?? "" })
        }
        case "/v1/project-sync/mirror":
          this.only(url, path)
          return await this.machineRead(init?.signal, method, path, "project-mirror", {})
        case "/v1/timeline": {
          // `upcoming` is a filter with two meanings and the page sends it
          // every time; the rest are left as the page left them, because this
          // route reads an absent `environment` as production and an empty one
          // as a refusal (`internal/transport/http/timeline.go`).
          const q = this.only(url, path, "project", "entry", "cursor", "environment", "category", "upcoming")
          return await this.machineRead(init?.signal, method, path, "timeline", {
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
      seen.error = error
      if (error instanceof AbandonedRead) {
        // A fetch whose signal fired rejects; it does not answer.
        this.note(method, path, "unanswered", error.code, { word: error.word })
        throw error
      }
      const failure = error as { code?: unknown; status?: unknown; message?: unknown }
      const code = typeof failure?.code === "string" ? failure.code : "cloud_failed"
      const message = typeof failure?.message === "string" ? failure.message : String(error)
      if (UNANSWERED.has(code) || error instanceof NotConnected) {
        this.note(method, path, "unanswered", code)
        throw new TypeError(`${code}: ${message}`)
      }
      const status = typeof failure?.status === "number" && failure.status >= 400 && failure.status < 600 ? failure.status : 502
      const version = MACHINE_LACKS_WORD.has(code) && this.client ? machineApp(this.client, this.machine).version : undefined
      return this.refuse(method, path, status, code, message, authenticatedRefusalKey(error), version)
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
    return (await this.snapshotReading()).snapshot
  }

  /**
   * The first list is one level snapshot, not every retained row as it arrives.
   *
   * A Cloud machine publishes each Session on its own channel and publishes an
   * inventory marker after the pass. On a new browser connection the retained
   * channels arrive separately, so handing every intermediate answer to React
   * makes an existing fleet appear one row at a time. The marker already says
   * how many rows make up the pass; wait until all of them are held, then hand
   * the first answer over once. Later stream frames remain incremental.
   *
   * `ClawdlineClient` supplies the request's existing AbortSignal. If an older
   * machine never publishes a marker, that signal ends the wait at the normal
   * request deadline and the best partial reading is returned. No second clock
   * or retry policy is introduced here.
   */
  private async initialSnapshot(signal?: AbortSignal | null, initial = false): Promise<SessionsSnapshot> {
    if (this.initialSnapshotDelivered) {
      if (this.options.statusList) return this.snapshot()
      // Remounting the list is another view of the same relay reading. Only a
      // person's refresh asks the machine again, unless the held pass is
      // incomplete and a restatement could recover missing rows.
      if (initial) {
        const held = await this.snapshot()
        if (held.scan.complete) return held
      }
      // A person's refresh is a new question about the machine, not a read of
      // the relay's retained rows. Those rows may omit a quiet Session after
      // relay eviction. The connection's recovery asks once; ask again here.
      await this.machineRead(signal, "GET", "/v1/sessions", "sessions.snapshot", {})
      return this.snapshot()
    }
    if (!signal) return this.snapshot()
    if (!this.initialSnapshotFlight) {
      this.initialSnapshotFlight = this.waitForInitialSnapshot(signal).finally(() => {
        this.initialSnapshotDelivered = true
        this.initialSnapshotFlight = null
      })
    }
    return this.initialSnapshotFlight
  }

  private waitForInitialSnapshot(signal: AbortSignal): Promise<SessionsSnapshot> {
    if (signal.aborted) return this.snapshot()
    return new Promise<SessionsSnapshot>((resolve, reject) => {
      let latest: SessionsSnapshot | null = null
      let checking = false
      let checkAgain = false
      let finished = false
      let handle: StreamHandle | null = null

      const clean = () => {
        signal.removeEventListener("abort", aborted)
        handle?.close()
      }
      const finish = (snapshot: SessionsSnapshot) => {
        if (finished) return
        finished = true
        clean()
        resolve(snapshot)
      }
      const fail = (cause: unknown) => {
        if (finished) return
        finished = true
        clean()
        reject(cause)
      }
      const check = async () => {
        if (finished) return
        if (checking) {
          checkAgain = true
          return
        }
        checking = true
        try {
          do {
            checkAgain = false
            const reading = await this.snapshotReading()
            latest = reading.snapshot
            if (reading.settled) {
              finish(reading.snapshot)
              return
            }
          } while (checkAgain && !finished)
        } catch (cause) {
          fail(cause)
        } finally {
          checking = false
        }
      }
      const aborted = () => {
        if (latest) finish(latest)
        else void this.snapshot().then(finish, fail)
      }

      signal.addEventListener("abort", aborted, { once: true })
      handle = this.stream().open("/v1/events", {
        onFrame: (event) => {
          if (event === "sessions" || event === "message") void check()
        },
      })
      void check()
    })
  }

  private async snapshotReading(): Promise<{ snapshot: SessionsSnapshot; settled: boolean }> {
    if (this.options.statusList) {
      const status = this.options.statusList()
      this.generation += 1
      return {
        snapshot: {
          at: Math.floor(status.at / 1000),
          scan: {
            complete: status.complete,
            completed: { complete: status.complete, sequence: this.generation },
            emptyAuthoritative: false,
            epoch: this.epoch,
            generation: this.generation,
            provenance: "cloud",
          },
          sessions: [],
        },
        settled: true,
      }
    }
    const client = this.connected()
    const all = await client.sessions()
    this.sayDrift()
    const recovering = (all.scan.recovering ?? []).includes(this.machine)
    const failed = (all.scan.failures ?? []).some((f) => f.machine === this.machine)
    const statusMarker = client.statusSnapshots?.get(JSON.stringify([this.machine, "__clawdline_inventory_v1__"])) as
      | { payload?: { complete?: unknown; inventory?: { version?: unknown; sessions?: unknown } } } | undefined
    const statusIDs = statusMarker?.payload?.complete === true && statusMarker.payload.inventory?.version === 1 &&
      Array.isArray(statusMarker.payload.inventory.sessions)
      ? statusMarker.payload.inventory.sessions.filter((id): id is string => typeof id === "string" && !!id) : null
    const inventory = client.sessionInventoryByMachine?.get(this.machine)
    const expected = this.options.classicStatus
      ? statusIDs === null ? null : new Set(statusIDs.map((id) => this.machine + "\u0000" + id))
      : inventoryRowKeys(inventory)
    const machineRows = all.sessions.filter((row) => {
      if (row.machine !== this.machine) return false
      if (!this.options.classicStatus) return true
      const id = typeof row.session === "string" ? row.session : row.id
      if (typeof id !== "string" || !expected?.has(this.machine + "\u0000" + id)) return false
      const status = client.statusSnapshots?.get(JSON.stringify([this.machine, id])) as
        | { payload?: { execution_generation?: unknown } } | undefined
      return typeof status?.payload?.execution_generation === "string" &&
        row.execution_generation === status.payload.execution_generation
    })
    this.askForOpenedRow(client, machineRows)
    // The marker can arrive before another retained channel. Its id set is the
    // receipt: a marker alone is not yet the whole list it describes.
    const hasInventory = this.options.classicStatus ? expected !== null : inventory !== undefined
    const heldKeys = new Set(machineRows.map((row) => {
      const id = typeof row.session === "string" ? row.session : typeof row.id === "string" ? row.id : ""
      return this.machine + "\u0000" + id
    }))
    const hasEveryRow = expected === null ? !this.options.classicStatus : [...expected].every((key) => heldKeys.has(key))
    const whole = hasInventory && hasEveryRow && !recovering && !failed
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
    const snapshot: SessionsSnapshot = {
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
    // The original page can show verified rich rows as they arrive. The ss/
    // marker remains the deletion barrier; a gap never claims an empty list.
    return { snapshot, settled: whole || failed || (this.options.classicStatus === true && hasInventory && machineRows.length > 0) }
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
    if (expecting && held?.answer && !fresh && !held.stale && rowKey === held.rowKey &&
      now - held.at < TRANSCRIPT_EXPECT_REREAD_MS) {
      return { page: held.answer, reused: true }
    }
    if (held?.answer && !fresh && !held.stale && !expecting) {
      const age = now - held.at
      const moved = rowKey !== held.rowKey
      const lineMoved = line !== held.line
      if (!moved && age < TRANSCRIPT_MAX_REUSE_MS && !(lineMoved && age >= TRANSCRIPT_LINE_REREAD_MS)) {
        return { page: held.answer, reused: true }
      }
    }
    const entry: HeldTranscript = held ?? this.hold(session)
    // `transcript` is past the remembered descriptor's cut too, and this read
    // does not go through `machineRead` (`client.transcript`).
    await this.settledWords("transcript")
    if (this.options.classicStatus && !client.transcriptForGeneration) {
      throw Object.assign(new Error("Pinned Session reading is unavailable"), { code: "cloud_not_carried" })
    }
    const asked = (this.options.classicStatus
      ? this.coherentSession(session).then((target) => client.transcriptForGeneration!(target))
      : client.transcript({ machine: this.machine, session }, undefined, { foreground: true }))
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
  private async machineRead(
    signal: AbortSignal | null | undefined,
    method: string,
    path: string,
    word: CarriedWord,
    body: Record<string, unknown>,
    machine: string = this.machine,
  ): Promise<Response> {
    const operation = word === "work.v2.session-todos" ? "todos" : word === "work.v2.human-interventions" ? "attention" : null
    const started = this.now()
    const mark = (stage: HeaderReadDiagnostic["stage"], code = "none") => {
      if (!operation) return
      const ms = Math.max(0, this.now() - started)
      recordHeaderRead({ at: this.now(), operation, stage, code: HEADER_CODES.has(code) || code === "none" ? code : "other",
        elapsed: ms < 1000 ? "<1s" : ms < 5000 ? "1-5s" : ms < 15000 ? "5-15s" : ">15s" })
    }
    const client = await this.connectedFor(signal)
    if (!client) {
      mark(signal?.aborted ? "caller_canceled" : "connection_unavailable", signal?.aborted ? "cloud_read_abandoned" : "cloud_reconnecting")
      if (signal?.aborted) throw new NotConnected()
      // Not `offline`: that is `jsonFetch`'s word for a network that failed,
      // and it put "is it still running on the machine?" under a machine that
      // was online. What is down is this page's own line, which is renewing
      // or reconnecting, and the machine was not asked.
      return this.refuse(method, path, 503, "cloud_reconnecting",
        "This page's Cloud connection is renewing or reconnecting; the machine was not asked.")
    }
    if (typeof client._machineRequest !== "function") {
      mark("read_failed", "cloud_not_carried")
      // A copied client older than the generic. Named rather than thrown:
      // this is the page refusing itself, and it says which word it is about.
      return this.refuse(method, path, 501, "cloud_not_carried",
        `This console cannot ask this machine for ${word}.`)
    }
    // The caller's own deadline holds here as it does on the machine's own
    // network. This seam used to ignore `init.signal`, so the work pages'
    // fifteen-second bound (`pages/work/api.ts`) silently became the copied
    // client's sixty-second read timeout plus its ten-second status probe:
    // a Session's to-do fold said "loading" for over a minute per try.
    if (signal?.aborted) {
      mark("caller_canceled", "cloud_read_abandoned")
      throw new AbandonedRead(word)
    }
    // The reads a page fires the moment it opens are the ones that used to be
    // refused here for a word the remembered descriptor's cut list did not
    // reach. Nothing else waits, and a caller that gave up is not kept.
    await this.settledWords(word, signal, machine)
    if (signal?.aborted) {
      mark("caller_canceled", "cloud_read_abandoned")
      throw new AbandonedRead(word)
    }
    let asked: Promise<unknown>
    try {
      asked = client._machineRequest(machine, word, body, "read", undefined, signal ? { signal } : undefined)
      // This proves the browser invoked its Cloud client, not that the
      // relay or machine accepted the request.
      mark("ask_started")
      const answer = await abandonable(asked, signal, word)
      mark("answer_observed")
      this.note(method, path, "relay", undefined, { word })
      return json(200, answer)
    } catch (error) {
      if (error instanceof AbandonedRead || signal?.aborted) mark("caller_canceled", "cloud_read_abandoned")
      else mark("read_failed", failureCode(error))
      throw error
    }
  }

  /**
   * A session read through the copied client's own method for it (`agent`,
   * `shell`), with the caller's deadline and the log line `machineRead` gives.
   * `ask` answers undefined when the client has no such method.
   */
  private async sessionRead(
    signal: AbortSignal | null | undefined,
    method: string,
    path: string,
    word: CarriedWord,
    ask: (client: CloudReadClient) => Promise<unknown> | undefined,
  ): Promise<Response> {
    const pending = ask(this.connected())
    if (!pending) {
      return this.refuse(method, path, 501, "cloud_not_carried",
        `This console cannot ask this machine for ${word}.`)
    }
    const answer = await abandonable(pending, signal, word)
    this.note(method, path, "relay", undefined, { word })
    return json(200, answer)
  }

  /**
   * A window the copied client asks for on its own (`CLOUD_AGENT_LIMIT`,
   * `CLOUD_SHELL_BYTES`). Absent, or the same number, is that window; any other
   * number is a question this path would answer with a different one, so it
   * is refused by name as `only` refuses a field.
   */
  private window(path: string, field: string, asked: string | undefined, fixed: number): void {
    if (asked === undefined || Number(asked) === fixed) return
    throw Object.assign(new Error(`${path}?${field}=${asked} is not carried over Clawdline Cloud: it reads ${fixed}.`), {
      code: "cloud_not_carried", status: 501,
    })
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

  /**
   * The connected client, or — while the attached one is retired or not yet
   * ready — the next one `keepConnected` attaches, if it says one is coming
   * and it arrives within the wait. Null when none does.
   */
  private async connectedFor(signal: AbortSignal | null | undefined): Promise<CloudReadClient | null> {
    const current = this.client
    if (current && current.ready !== false) return current
    let coming = false
    try {
      coming = current?.lifecycle?.("demand") ?? false
    } catch {
      coming = false
    }
    if (coming !== true) return null
    const bound = this.options.reconnectWaitMs ?? RECONNECT_WAIT_MS
    await new Promise<void>((resolve) => {
      const done = () => {
        clearTimeout(timer)
        this.awaitingClient.delete(done)
        signal?.removeEventListener("abort", done)
        resolve()
      }
      const timer = setTimeout(done, bound)
      this.awaitingClient.add(done)
      signal?.addEventListener("abort", done)
    })
    const next = this.client
    return next && next.ready !== false && !signal?.aborted ? next : null
  }

  private connected(): CloudReadClient {
    if (!this.client || this.client.ready === false) throw new NotConnected()
    return this.client
  }

  /**
   * `pinnedSession`, after the pass it is reading has finished arriving.
   *
   * The machine sends a pass as every Session's ss/ row and then the marker,
   * all with one new pass id (`internal/transport/cloud/session_status.go`).
   * A page opens them one at a time, so for the few milliseconds the rest of
   * a pass is on its way it holds a row from the new pass beside the marker
   * of the old one, and a read asked then was refused here as
   * `pass_mismatch` — on the page, before anything was sent, so the machine
   * never saw it. A replay of an hour of 15-second passes refused 2 to 7
   * polls of the open Session this way (`read-replay.test.ts`).
   *
   * Only that condition waits, and only for the marker: the next envelope of
   * this machine's marker, or `PASS_SETTLE_MS`, whichever comes first. Then
   * the same checks decide, so a pass that never completes is still refused.
   */
  private async coherentSession(session: string, signal?: AbortSignal | null, word = "transcript"): Promise<{ machineID: string; sessionID: string; executionGeneration: string }> {
    try {
      return this.pinnedSession(session)
    } catch (error) {
      if ((error as { condition?: unknown })?.condition !== "pass_mismatch") throw error
    }
    const client = this.connected()
    await new Promise<void>((resolve) => {
      let off: (() => void) | null = null
      const done = () => {
        clearTimeout(timer)
        off?.()
        signal?.removeEventListener("abort", done)
        resolve()
      }
      const timer = setTimeout(done, PASS_SETTLE_MS)
      signal?.addEventListener("abort", done)
      off = client.events((event) => {
        if (event.type === "session_status" && event.identity?.machine === this.machine &&
          event.identity?.session === "__clawdline_inventory_v1__") done()
      }) as (() => void) | null
    })
    if (signal?.aborted) throw new AbandonedRead(word)
    return this.pinnedSession(session)
  }

  /**
   * Hold a read until the chosen machine's own word list has arrived, when the
   * page would otherwise refuse it on a word list that was cut short
   * (`machineWordPending`). A cut remembered list alone cannot refuse the
   * read when this wait ends; the named machine answers it instead.
   *
   * Only that condition waits, and only for the descriptor: the next `orch/`
   * envelope of this machine that carries one, or `FEATURE_WAIT_MS`, whichever
   * comes first. A live descriptor or a machine's signed refusal remains
   * authoritative. The stored 64-word prefix is not.
   */
  private async settledWords(word: string, signal?: AbortSignal | null, machine: string = this.machine): Promise<void> {
    const client = this.client
    if (!client || signal?.aborted) return
    if (!machineWordPending(client, machine, word)) return
    const bound = this.options.featureWaitMs ?? FEATURE_WAIT_MS
    await new Promise<void>((resolve) => {
      let off: (() => void) | null = null
      const done = () => {
        clearTimeout(timer)
        off?.()
        signal?.removeEventListener("abort", done)
        resolve()
      }
      const timer = setTimeout(done, bound)
      signal?.addEventListener("abort", done)
      // A status-only `orch/` notice carries no descriptor and sets no
      // snapshot (`_applySnapshot`), so the condition is asked again rather
      // than the first envelope being taken for the answer.
      off = client.events((event) => {
        if (event.type === "orchestrator" && event.machine === machine &&
          !machineWordPending(client, machine, word)) done()
      }) as (() => void) | null
    })
  }

  private pinnedSession(session: string): { machineID: string; sessionID: string; executionGeneration: string } {
    const held = this.connected().statusSnapshots
    const marker = held?.get(JSON.stringify([this.machine, "__clawdline_inventory_v1__"])) as
      | { payload?: { complete?: unknown; inventory?: { sessions?: unknown }; snapshot_generation?: unknown; at?: unknown } } | undefined
    const row = held?.get(JSON.stringify([this.machine, session])) as
      | { payload?: { snapshot_generation?: unknown; execution_generation?: unknown; projected_at?: unknown;
        source?: { freshness?: unknown; observed_at?: unknown } } } | undefined
    const meta = marker?.payload
    const data = row?.payload
    const now = this.now() / 1000
    const fresh = (value: unknown) => typeof value === "number" && Math.abs(now - value) <= 300
    // The first condition that fails names the refusal, so a page's report can
    // say which one it was (`reportReadFailure`). The refusal is unchanged.
    const condition = !marker ? "no_marker"
      : meta?.complete !== true || !Array.isArray(meta.inventory?.sessions) ? "marker_incomplete"
      : !meta.inventory.sessions.includes(session) ? "not_listed"
      : !data ? "no_row"
      : data.snapshot_generation !== meta.snapshot_generation ? "pass_mismatch"
      : typeof data.execution_generation !== "string" || !/^[0-9a-f]{32}$/u.test(data.execution_generation) ? "no_execution"
      : data.source?.freshness !== "current" ? "not_current"
      : !fresh(meta.at) ? "marker_stale"
      : !fresh(data.projected_at) ? "row_stale"
      : !fresh(data.source?.observed_at) ? "observed_stale"
      : null
    if (condition !== null || !data) throw Object.assign(new Error("The Session execution is no longer current"), {
      code: "execution_generation_changed", status: 409, condition: condition ?? "no_row",
    })
    const target = { machineID: this.machine, sessionID: session, executionGeneration: data.execution_generation as string }
    const intended = this.options.fleetTarget?.()
    if (intended && (intended.machineID !== target.machineID || intended.sessionID !== target.sessionID ||
      intended.executionGeneration !== target.executionGeneration)) {
      throw Object.assign(new Error("The opened Session execution has changed"), {
        code: "execution_generation_changed", status: 409, condition: "target_changed",
      })
    }
    return target
  }

  /**
   * The row under the Session a person has open, asked for rather than waited for.
   *
   * The machine's ss/ pass says this execution is the one running, and the page
   * has no rich row for it: that is the one case where asking the machine for
   * its list answers a question nobody else will. The reply arrives on the
   * direct carrier when there is one, so the row is back within a second
   * instead of on the next status pass (`ROW_RESTATE_MS`).
   *
   * Only when the two agree about the execution. A ss/ row naming a different
   * one is a replaced execution, which `readDetail` already refuses as
   * `changed` and offers the running Session for; asking again would not
   * produce the row a person is looking at.
   */
  private askForOpenedRow(client: CloudReadClient, rows: readonly CloudRow[]): void {
    const target = this.options.fleetTarget?.()
    if (!this.options.classicStatus || !target || target.machineID !== this.machine) return
    const held = rows.some((row) => {
      const id = typeof row.session === "string" ? row.session : row.id
      return id === target.sessionID && row.execution_generation === target.executionGeneration
    })
    if (held) {
      this.rowAsked = null
      return
    }
    const status = client.statusSnapshots?.get(JSON.stringify([this.machine, target.sessionID])) as
      | { payload?: { execution_generation?: unknown } } | undefined
    if (status?.payload?.execution_generation !== target.executionGeneration) return
    const key = target.sessionID + "\u0000" + target.executionGeneration
    const now = this.now()
    if (this.rowAsked?.key === key && now - this.rowAsked.at < ROW_RESTATE_MS) return
    this.rowAsked = { key, at: now }
    void this.machineRead(null, "GET", "/v1/sessions", "sessions.snapshot", {}).catch(() => undefined)
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
          if (((event.type === "session_status" || event.type === "sessions") ||
            event.type === "orchestrator") && machine === this.machine) {
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

  private refuse(method: string, path: string, status: number, code: string, detail: string, detailKey: string | null = null, version?: string): Response {
    this.note(method, path, "refused", code)
    return json(status, { error: code, detail, route: path, ...(detailKey ? { detail_key: detailKey } : {}), ...(version ? { version } : {}) })
  }

  /**
   * One `cloud.read.failed` row in the copied client's failure log: which read,
   * where it stopped, and what this page held when it did. Content-free: no
   * message text, no title, no line — the terminal id is the only id.
   *
   * `stage` is the side of the wire it stopped on, read from the failure the
   * copied client settled it with: a failure carrying no envelope sequence was
   * never sent (`viewer_refused`), `cloud_read_timeout` is `timeout`, the
   * relay's own layer is `relay_refused`, and a machine's is `answered`. A read
   * the page itself stopped waiting for is not a failure and is not recorded.
   */
  private reportReadFailure(read: { word: string; session: string | null }, started: number, error: unknown, answer: Response | null): void {
    try {
      const log = (this.client as { viewerEvents?: { record?(event: string, data: Record<string, unknown>, key?: string): unknown } } | null)?.viewerEvents
      if (typeof log?.record !== "function") return
      const failure = (error ?? null) as { code?: unknown; layer?: unknown; status?: unknown; condition?: unknown;
        ref?: { seq?: unknown } | null; name?: unknown } | null
      if (error instanceof AbandonedRead || failure?.code === "cloud_read_abandoned" || failure?.name === "AbortError") return
      const code = typeof failure?.code === "string" ? failure.code : error instanceof NotConnected ? "offline"
        : answer ? "http_" + answer.status : "cloud_failed"
      const layer = typeof failure?.layer === "string" ? failure.layer : null
      const sent = Number.isSafeInteger(failure?.ref?.seq)
      const stage = code === "cloud_read_timeout" ? "timeout"
        : !sent ? "viewer_refused"
        : layer === "relay" ? "relay_refused"
        : "answered"
      const client = this.client
      const held = client?.statusSnapshots
      const marker = held?.get(JSON.stringify([this.machine, "__clawdline_inventory_v1__"])) as { payload?: { at?: unknown } } | undefined
      const row = read.session ? held?.get(JSON.stringify([this.machine, read.session])) as { payload?: { projected_at?: unknown } } | undefined : undefined
      const nowS = this.now() / 1000
      const age = (at: unknown) => typeof at === "number" && Number.isFinite(at) ? Math.round(nowS - at) : null
      const offline = (client as { machineOffline?: ReadonlyMap<string, { until?: unknown }> } | null)?.machineOffline?.get(this.machine)
      const data: Record<string, unknown> = {
        word: read.word, stage, code,
        cond: typeof failure?.condition === "string" ? failure.condition : null,
        status: answer?.status ?? (typeof failure?.status === "number" ? failure.status : null),
        layer, ms: Math.max(0, Math.round(this.now() - started)),
        connection: !client ? "none" : client.ready === false ? "not_ready" : "ready",
        marker_age_s: age(marker?.payload?.at), row_age_s: age(row?.payload?.projected_at),
        offline_hold_ms: offline && typeof offline.until === "number" ? Math.max(0, Math.round(offline.until - this.now())) : null,
        machine: this.machine, session: read.session,
      }
      log.record("cloud.read.failed", data, ["cloud.read.failed", read.word, stage, code].join("|"))
    } catch {
      // A recorder cannot fail the read it is recording.
    }
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

/**
 * A carried read whose caller stopped waiting (its `AbortSignal` fired). Named
 * `AbortError`, as `fetch` names the same rejection; the machine may still
 * answer, and that late answer settles nothing on this page.
 */
export class AbandonedRead extends Error {
  readonly code = "cloud_read_abandoned"
  readonly word: string
  constructor(word: string) {
    super(`cloud_read_abandoned: this page stopped waiting for the machine's ${word} answer`)
    this.name = "AbortError"
    this.word = word
  }
}

function abandonable<T>(asked: Promise<T>, signal: AbortSignal | null | undefined, word: string): Promise<T> {
  if (!signal) return asked
  if (signal.aborted) {
    asked.catch(() => {})
    return Promise.reject(new AbandonedRead(word))
  }
  return new Promise<T>((resolve, reject) => {
    const aborted = () => {
      asked.catch(() => {})
      reject(new AbandonedRead(word))
    }
    signal.addEventListener("abort", aborted, { once: true })
    asked.then(
      (value) => { signal.removeEventListener("abort", aborted); resolve(value) },
      (error: unknown) => { signal.removeEventListener("abort", aborted); reject(error) },
    )
  })
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

/** The two ids in `/v1/sessions/{session}/shells/{shell}`, decoded after splitting. */
function sessionShell(path: string): { session: string; shell: string } | null {
  const parts = path.split("/")
  if (parts.length !== 6 || parts[1] !== "v1" || parts[2] !== "sessions" || parts[4] !== "shells") return null
  try {
    const session = decodeURIComponent(parts[3])
    const shell = decodeURIComponent(parts[5])
    return session && shell ? { session, shell } : null
  } catch {
    return null
  }
}

/** The two ids in `/v1/sessions/{session}/agents/{agent}`, decoded after splitting. */
function sessionAgent(path: string): { session: string; agent: string } | null {
  const parts = path.split("/")
  if (parts.length !== 6 || parts[1] !== "v1" || parts[2] !== "sessions" || parts[4] !== "agents") return null
  try {
    const session = decodeURIComponent(parts[3])
    const agent = decodeURIComponent(parts[5])
    return session && agent ? { session, agent } : null
  } catch {
    return null
  }
}

/** A typed failure's code, as a machine that did not answer the list is named by. */
/**
 * The Session reads a failure of which is reported to the machine
 * (`reportReadFailure`), by the route the page asked for, or null.
 */
function readFailureWord(input: RequestInfo | URL, init?: RequestInit): { word: string; session: string | null } | null {
  try {
    const method = (init?.method ?? (typeof input === "object" && "method" in input ? input.method : "GET")).toUpperCase()
    if (method !== "GET") return null
    const href = typeof input === "string" ? input : input instanceof URL ? input.href : input.url
    const url = new URL(href, "http://relay.invalid/")
    const path = url.pathname
    if (path === "/v1/transcript") {
      return { word: url.searchParams.has("before") ? "transcript_page" : "transcript", session: url.searchParams.get("session") }
    }
    if (path === "/v1/sessions") return { word: "session_list", session: null }
    const todos = workV2SessionTodosTerminal(path)
    if (todos) return { word: "session_todos", session: todos }
    const info = /^\/v1\/sessions\/([^/]+)\/info$/u.exec(path)
    if (info) return { word: "info", session: decodeURIComponent(info[1]!) }
    return null
  } catch {
    return null
  }
}

function failureCode(error: unknown): string {
  const code = error && typeof error === "object" ? (error as { code?: unknown }).code : undefined
  return typeof code === "string" && code ? code : "unanswered"
}

/** The id in `GET /v1/orchestrator/schedules/{id}`, decoded after splitting. */
function scheduleDetailID(path: string): string {
  const parts = path.split("/")
  if (parts.length !== 5 || parts[1] !== "v1" || parts[2] !== "orchestrator" || parts[3] !== "schedules") return ""
  try {
    return decodeURIComponent(parts[4])
  } catch {
    return ""
  }
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

/** One inventoried file under a Project, or the Project's inventory itself. */
function projectFileRoute(path: string): { project: string; file?: string } | null {
  const parts = path.split("/")
  if ((parts.length !== 5 && parts.length !== 6) || parts[1] !== "v1" || parts[2] !== "projects" || parts[4] !== "files") return null
  try {
    const project = decodeURIComponent(parts[3])
    const file = parts[5] ? decodeURIComponent(parts[5]) : undefined
    if (!project || parts.length === 6 && !/^[0-9a-f]{32}$/.test(file ?? "")) return null
    return { project, ...(file ? { file } : {}) }
  } catch { return null }
}

/** The Project a unify plan read names, or "" for any other path. */
function projectUnifyRoute(path: string): string {
  const parts = path.split("/")
  if (parts.length !== 5 || parts[1] !== "v1" || parts[2] !== "projects" || parts[4] !== "unify") return ""
  try { return decodeURIComponent(parts[3]) } catch { return "" }
}

function projectTreeRoute(path: string): { project: string; file: boolean } | null {
  const parts = path.split("/")
  if ((parts.length !== 5 && parts.length !== 6) || parts[1] !== "v1" || parts[2] !== "projects" || parts[4] !== "tree" ||
    parts.length === 6 && parts[5] !== "file") return null
  try {
    const project = decodeURIComponent(parts[3])
    return project ? { project, file: parts.length === 6 } : null
  } catch { return null }
}

/** The terminal in the exact Work v2 Session to-do list route. */
function workV2SessionTodosTerminal(path: string): string {
  const parts = path.split("/")
  if (parts.length !== 6 || parts[1] !== "v1" || parts[2] !== "work" || parts[3] !== "v2" || parts[4] !== "session-todos") return ""
  try {
    return decodeURIComponent(parts[5])
  } catch {
    return ""
  }
}

function workV2HumanInterventionConversation(path: string): string {
  const parts = path.split("/")
  if (parts.length !== 6 || parts[1] !== "v1" || parts[2] !== "work" || parts[3] !== "v2" || parts[4] !== "human-interventions") return ""
  try {
    const conversation = decodeURIComponent(parts[5])
    return /^conversation:[0-9a-f-]{36}$/.test(conversation) ? conversation : ""
  } catch {
    return ""
  }
}

/** The item id in the exact Work v2 single-item read route. */
function workV2ItemID(path: string): string {
  const parts = path.split("/")
  if (parts.length !== 6 || parts[1] !== "v1" || parts[2] !== "work" || parts[3] !== "v2" || parts[4] !== "items") return ""
  try {
    return decodeURIComponent(parts[5])
  } catch {
    return ""
  }
}

function workV2ItemActionID(path: string, action: string): string {
  const parts = path.split("/")
  if (parts.length !== 7 || parts[1] !== "v1" || parts[2] !== "work" || parts[3] !== "v2" || parts[4] !== "items" || parts[6] !== action) return ""
  try { return decodeURIComponent(parts[5]) } catch { return "" }
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })
}

/**
 * A relay row as the console's list takes it. The machine's own row is kept whole;
 * the relay's routing key stays as an explicit identity for actions such as
 * opening documents. `machine` also stays so the list can name the machine.
 */
function consoleRow(row: CloudRow): SessionRow {
  const { optimisticIdentity: _optimistic, ...rest } = row as CloudRow & { optimisticIdentity?: unknown }
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
  // `nextAfter` is a byte cursor into the machine's own record, for a page that
  // reads the daemon directly; over the relay the transcript is this seam's
  // held answer, so the cursor is not passed on (see the `after=` refusal).
  const { optimisticIdentity: _identity, nextAfter: _after, ...page } = body as Record<string, unknown>
  return {
    ...page,
    id: typeof page.id === "string" && page.id ? page.id : session,
    entries: Array.isArray(page.entries) ? page.entries : [],
    signature: typeof page.signature === "string" ? page.signature : "",
    evidence: typeof page.evidence === "string" ? page.evidence : "transcript",
  } as TranscriptPage
}

function historyCursor(value: string | null | undefined): number {
  if (!value || !/^[1-9][0-9]*$/.test(value)) return 0
  const cursor = Number(value)
  return Number.isSafeInteger(cursor) ? cursor : 0
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
