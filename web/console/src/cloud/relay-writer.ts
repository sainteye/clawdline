// The console's writes, carried to a machine across the relay.
//
// The console already knows how to act on a session: the composer POSTs
// `/v1/sessions/<id>/send`, a waiting card POSTs `/key`, the start sheet POSTs
// `/v1/places/<id>/start`, and so on — each against a daemon on its own
// origin, each with the words and the states it shows while that is under way
// (`session/pending.ts`, `Waiting.tsx`, `Start.tsx`). This file is the other
// half of `relay-reader.ts`: it answers those same requests out of the copied
// `CloudClient`, so every one of those screens keeps its own look and its own
// words and does not learn that the machine is far away.
//
// Two rules decide everything here.
//
// **Only words the machine already has.** Each route is one command the machine
// lists in its descriptor (`cloudops.Implemented()` on the Go daemon, carried
// as `machine.commands`), spelled the way `net/cloud-client.js` spells it. A
// route with no command behind it is refused by name, here, before anything is
// sealed; a command this machine does not list is refused by the copied
// client before it leaves (`cloud_feature_unavailable`). Nothing is invented.
//
// **A failure is an answer, not a silence.** A read that nobody answered
// rejects its `fetch`, because the console draws that as "away" and polls
// again. A write cannot be treated that way: the command may have reached the
// machine and run, and the page must be able to say which of those it knows. So
// every write settles with a typed refusal in the spelling the route's own
// reader takes, carrying the code, the layer and the envelope's `ref`, and the
// `outcome` this seam can vouch for (`outcomeOf` below): `not_done` only when
// it can prove the envelope never reached the machine, `unknown` otherwise, and
// none at all for a refusal from the machine's own route, whose code says it — as
// the same refusal from a daemon on this machine does (`session/outcome.ts`).
//
// Nothing is imported at run time, so `node --test` loads it as it is.
import type { CarriedWord } from "./carry.js"
import type { CloudIdentity, CloudReadClient, CloudRow, SeamRow } from "./relay-reader.js"

// One admitted request may wait behind one turn, and each turn may try two
// 30-second CLIs. Ten seconds leaves the relay enough room to deliver either
// the draft or its typed refusal after that worst-case 120-second path.
const INTENT_TIMEOUT_MS = 130_000

/**
 * The copied Cloud client gives every machine-local Project id an account-safe
 * id before `/v1/places` reaches the page. An intent is planned on one machine,
 * so its answer still carries that machine's local id; cross the same boundary
 * here before the draft is compared with the Project picker.
 *
 * This is the `cloudPlaceID` spelling in the copied client. It lives here too
 * because this seam deliberately has no runtime imports from the legacy copy.
 */
function cloudProjectID(machine: string, project: string): string {
  const bytes = new TextEncoder().encode(JSON.stringify([machine, project]))
  let raw = ""
  for (const byte of bytes) raw += String.fromCharCode(byte)
  return "cloud." + btoa(raw).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
}

/** A typed failure as the copied modules raise it (`cloud-failure.js`). */
interface CloudFailureLike {
  code?: unknown
  message?: unknown
  status?: unknown
  layer?: unknown
  ref?: { sender?: unknown; seq?: unknown; request?: unknown } | null
  retryable?: unknown
  detail?: Record<string, unknown>
  reasons?: unknown
  app?: unknown
  reason?: unknown
  lost?: unknown
}

/** The machine's descriptor as `CloudClient.machineDescriptor` returns it. */
interface RememberedDescriptor {
  machine?: { commands?: unknown }
}

/**
 * The part of the copied `CloudClient` the writes call. Every method is its
 * own, with its own checks: `allowWrites` (the device's `send_prompt`
 * capability), the machine's declared vocabulary, the relay's word on the
 * envelope, and the machine's answer on `t/<machine>/<session>`.
 */
export interface CloudWriteClient extends CloudReadClient {
  readonly allowWrites?: boolean
  /** Machines that have shown `cloud_status.v >= 1` (§11.4). */
  readonly macCapabilities?: Set<string>
  machineDescriptor?(machine: string): RememberedDescriptor | null
  send(identity: CloudIdentity, text: string, images: string[]): Promise<unknown>
  answer(identity: CloudIdentity, answer: string): Promise<unknown>
  end(identity: CloudIdentity, acceptLoss: boolean, closeabilityVersion: string): Promise<unknown>
  focus(identity: CloudIdentity): Promise<unknown>
  places(machine: string): Promise<unknown>
  pastSessions(place: string, assistant: string): Promise<unknown>
  startPlace(place: string, assistant: string, model: string): Promise<unknown>
  resumePlace(place: string, past: string, assistant: string, requestId?: string): Promise<unknown>
  voice(audio: string, rate: number): Promise<unknown>
  /** The planner follows dictation to the same explicitly chosen voice host. */
  voiceHost?(): Promise<{ machine: string }>
  /**
   * The status line's read, and the Session info card's. The copied client has
   * had both since the Swift console; nothing here asked for them, so every
   * cloud session's status line drew "Loading…" and said why to nobody
   * (`cloud-client.js`, `info`).
   */
  info(identity: CloudIdentity): Promise<unknown>
  infoSummary(identity: CloudIdentity): Promise<unknown>
  /**
   * The Git panel's read. The copied client has had it since the Swift
   * console and nothing here asked for it, so 「Git 變更」 said "無法讀取 Git
   * 變更" on every phone while this machine answered the word to anyone who did
   * ask (`cloud-client.js`, `git`). It is not optional for the reason `info`
   * is not: both have been in the copied client since it was copied.
   */
  git(identity: CloudIdentity): Promise<unknown>
  /**
   * The live screen's read (`cloud-client.js`, `screen`). The copied client
   * says a tmux screen is `on-demand` at the one-second floor, because no
   * revision event crosses the relay and the panel only asks again on its own
   * for a backend that says it has to be asked.
   */
  screen(identity: CloudIdentity): Promise<unknown>
  setVoiceHost?(machine: string): Promise<unknown>
  /**
   * The three notification writes. Each picks the account's push machine strictly
   * (`_pushMachine`): the key, the subscription and its removal must all reach
   * the same one, so it is never the freshest of two. Optional for the reason
   * `pushKey` is: a copied client older than these words is refused by name
   * rather than throwing where nobody is catching.
   */
  pushSubscribe?(subscription: unknown): Promise<unknown>
  pushUnsubscribe?(id: string): Promise<unknown>
  pushTest?(session: string): Promise<unknown>
  /**
   * Schedule creation routes itself by the Project in the body. The other
   * three writes go to this page's already authenticated machine through
   * `_machineRequest`: a detail read also goes straight there, so saving an
   * open schedule must not look its id up again in a retained inventory that
   * a later descriptor may have replaced.
   *
   * They mint their own request id per call, which is what becomes the machine's
   * `Idempotency-Key` (`cloudops.route`). That is not a divergence from the
   * local path: `schedules-bridge.ts` mints a fresh `uuid()` per press too, so
   * one press is one key on either transport and a second press is a second
   * one on both.
   *
   * Optional for the reason the push words are: a copied client older than
   * them is refused by name rather than throwing where nobody is catching.
   */
  createSchedule?(schedule: unknown): Promise<unknown>
  _scheduleBody?(schedule: unknown): { machine: string; schedule: Record<string, unknown> }
  /** The generic machine request under the UI press's durable idempotency key. */
  _machineRequestAs?(
    request: string,
    machine: string,
    word: string,
    body: Record<string, unknown>,
    kind: "read" | "action",
    timeoutMs?: number,
  ): Promise<unknown>
  /**
   * The four snippet writes. Each names the machine whose settings change
   * through a session identity — only its `machine` is read
   * (`_snippetRequest`) — because a snippet id, or the first machine in a
   * snapshot, is not authority to choose which machine gets edited.
   *
   * They mint their own request id per call, which is what becomes the machine's
   * `Idempotency-Key` (`cloudops.route`). That matches the local path rather
   * than diverging from it: `session/snippets-api.ts` mints a fresh key per
   * press too, so one press is one key on either transport. What the key buys
   * is the same on both — the machine files the outcome under it and answers a
   * resend with the first answer instead of writing twice.
   *
   * `orderSnippets` takes the group and its complete order separately, and the
   * copied client assembles the `ordering` object the machine's word carries; the
   * local route reads the same three fields under no name at all.
   *
   * Optional for the reason the push words are: a copied client older than
   * them is refused by name rather than throwing where nobody is catching.
   */
  createSnippet?(snippet: unknown, identity: CloudIdentity): Promise<unknown>
  updateSnippet?(id: string, snippet: unknown, identity: CloudIdentity): Promise<unknown>
  deleteSnippet?(id: string, identity: CloudIdentity): Promise<unknown>
  orderSnippets?(scope: string, project: string, order: string[], identity: CloudIdentity): Promise<unknown>
  image?(identity: CloudIdentity, id: string): Promise<{ id: string; media_type: string; bytes: Uint8Array }>
  /**
   * The copied client's one read, which `answer` itself calls for a machine that
   * has shown `cloud_status`. See `RelayWriter.answer` for why it is called
   * directly for a machine that names `answer` in its descriptor instead.
   */
  _read?(
    identity: CloudIdentity,
    type: string,
    extra: Record<string, unknown>,
    answer: string,
    timeoutMs?: number,
    options?: { retireUncertain?: boolean },
  ): Promise<unknown>
}

/** What the seam around this writer gives it. */
export interface WriteHost {
  readonly machine: string
  /** The client, or a throw coded `offline` when the line is not up. */
  connected(): CloudWriteClient
  /** Something was just done to this session: its transcript is about to change. */
  wrote(session: string, outcome: "done" | "unknown" | "refused"): void
  /** The machine's terminal backend confirmed that this session was closed. */
  closed(session: string): void
  note(row: Omit<SeamRow, "at">): void
}

/**
 * One request this file carries, parsed from the console's own route.
 *
 * `word` is the Cloud command the machine is asked for — the word its
 * descriptor lists and `cloud-client.js` sends — and is what the log and the
 * report name.
 */
export type WriteRoute =
 | { op: "project-icon-copy"; word: Carried<"project-icon-copy">; id: string }
  | { op: "project-mirror-apply"; word: Carried<"project-mirror-apply"> }
  | { op: "project-mirror-detach"; word: Carried<"project-mirror-detach"> }
  | { op: "send"; word: Carried<"send">; session: string }
  | { op: "info"; word: Carried<"info">; session: string }
  | { op: "git"; word: Carried<"git">; session: string }
  | { op: "git-diff"; word: Carried<"git-diff">; session: string }
  | { op: "screen"; word: Carried<"screen">; session: string }
  | { op: "answer"; word: Carried<"answer">; session: string }
  | { op: "end"; word: Carried<"end">; session: string }
  | { op: "focus"; word: Carried<"focus">; session: string }
  | { op: "smart-title"; word: Carried<"smart-title">; session: string }
  | { op: "interrupt"; word: Carried<"interrupt">; session: string }
  | { op: "start"; word: Carried<"start">; place: string; assistant: string; model: string }
  | { op: "resume"; word: Carried<"resume">; place: string; assistant: string; past: string }
  | { op: "voice"; word: Carried<"voice"> }
  | { op: "intents"; word: Carried<"intents"> }
  | { op: "places"; word: Carried<"places"> }
  | { op: "past"; word: Carried<"past-sessions">; place: string; assistant: string }
  | { op: "image"; word: Carried<"image">; artifact: string }
  | { op: "work-v2-image"; word: Carried<"work.v2.image">; artifact: string }
  | { op: "usage"; word: Carried<"usage.session" | "usage.task" | "usage.item">; id: string }
  | { op: "usage-compare"; word: Carried<"usage.compare-compaction"> }
  | { op: "capacity"; word: Carried<"capacity"> }
  | { op: "machine-usage"; word: Carried<"machine-usage"> }
  // The sessions a reboot took away: one machine read and two machine
  // commands, none of them a session's — the rows are conversations the
  // machine no longer has a terminal for.
  | { op: "restorable"; word: Carried<"restorable-sessions"> }
  | { op: "restore"; word: Carried<"restore-sessions" | "dismiss-restorable"> }
  // Things waiting to be verified. Machine words, not session ones: a record
  // belongs to the machine, and the page that reads it holds no session.
  | { op: "verification-read"; word: Carried<"verification.list" | "verification.get">; id: string }
  | { op: "verification-write"; word: Carried<"verification.create" | "verification.note" | "verification.close">; id: string }
  | { op: "verification-criterion"; word: Carried<"verification.criterion">; id: string; index: number }
  | { op: "verification-delete"; word: Carried<"verification.delete">; id: string }
  | { op: "push-subscribe"; word: Carried<"push-subscribe"> }
  | { op: "push-unsubscribe"; word: Carried<"push-unsubscribe"> }
  | { op: "push-test"; word: Carried<"push-test"> }
  | { op: "schedule-create"; word: Carried<"schedule-create"> }
  | { op: "schedule-update"; word: Carried<"schedule-update">; schedule: string }
  | { op: "schedule-delete"; word: Carried<"schedule-delete">; schedule: string }
  | { op: "schedule-run"; word: Carried<"schedule-run">; schedule: string }
  // The four snippet writes. None carries a session: the snippet belongs to
  // the machine and not to the session, and the session the sheet was opened
  // on rides in the query, where `carry` reads it — so `sessionOf` leaves them
  // alone and a snippet saved does not mark a transcript stale.
  | { op: "snippet-create"; word: Carried<"snippet-create"> }
  | { op: "snippet-update"; word: Carried<"snippet-update">; snippet: string }
  | { op: "snippet-delete"; word: Carried<"snippet-delete">; snippet: string }
  | { op: "snippet-order"; word: Carried<"snippet-order"> }
  | { op: "worktree-refresh"; word: Carried<"project-worktree-lifecycle-refresh">; project: string }
  | { op: "work-v2-create"; word: Carried<"work.v2.create"> }
  | { op: "work-v2-edit"; word: Carried<"work.v2.edit">; id: string }
  | { op: "work-v2-assign"; word: Carried<"work.v2.assign">; id: string }
  | { op: "work-v2-remind"; word: Carried<"work.v2.remind">; id: string }
  | { op: "work-v2-cancel"; word: Carried<"work.v2.cancel">; id: string }
  | { op: "work-v2-image-create"; word: Carried<"work.v2.image-create">; id: string }
  | { op: "work-v2-image-delete"; word: Carried<"work.v2.image-delete">; id: string; image: string }
  | { op: "work-v2-proposal-resolve"; word: Carried<"work.v2.proposal-resolve">; id: string; decision: "accept" | "reject" }
  | { op: "work-v2-todo-create"; word: Carried<"work.v2.todo-create">; terminal: string }
  | { op: "work-v2-todo-image-create"; word: Carried<"work.v2.todo-image-create">; terminal: string; id: string }
  | { op: "work-v2-todo-action"; word: Carried<"work.v2.todo-action">; terminal: string; id: string; action: "send" | "complete" | "delete" }
  // Manual `title` is not a Cloud word at all — not here and not in the Swift
  // app's vocabulary — so this one is a plain string.
  | { op: "uncarried"; word: string; session?: string }

/**
 * The tie between a route here and `carry.ts`: a word that table does not list
 * as carried is a compile error on the route that names it, which is why the
 * vocabulary is not written down a second time in this file.
 */
type Carried<K extends CarriedWord> = K

/** The token ledger's three routes, by the path segment that names each (`GET /v1/usage/<kind>/<id>`). */
const USAGE_WORD: Readonly<Record<string, Carried<"usage.session" | "usage.task" | "usage.item">>> = {
  sessions: "usage.session",
  tasks: "usage.task",
  items: "usage.item",
}

/**
 * The routes this daemon answers locally that have no command on the Cloud
 * wire at all — not on the Go daemon and not in the Swift app's vocabulary
 * (docs/cloud-wire.md §10.3). Each is refused by its own name, with where it
 * can be done instead, rather than as a generic "not carried".
 */
const NO_CLOUD_WORD: Readonly<Record<string, string>> = {
  rename: "title",
  title: "title",
}

/**
 * Codes that mean the envelope went and no answer came back: the relay handed
 * it to the machine and then nothing, or the machine ran it and its reply could not be
 * delivered (`cloud-failure.js`, `_readTimedOut`). Whatever else the failure
 * carries, the command may have been carried out.
 */
const SENT_UNANSWERED = new Set([
  "cloud_read_timeout",
  "reply_not_received",
  "command_answer_undeliverable",
  "read_answer_undeliverable",
  "delivery_unconfirmed",
  "receipt_expired",
  "ready_expired",
  "peer_rejected",
])

/**
 * Layers whose refusal is decided before anything is carried out: the machine's
 * admission (the write switch, the roster, the clock) and its transport (the
 * queue was full).
 */
const REFUSED_BEFORE_RUNNING = new Set(["mac_preflight", "mac_transport"])

/**
 * What this seam can vouch for about a failed write (F3). The burden is on
 * `not_done`, because it is what sends a person to "try again": it is said
 * only when the envelope provably never reached the machine — the copied client
 * refused before sealing it (no sequence was spent, so no `ref`), the relay
 * said the machine was not connected, or the machine refused it at the door. A refusal
 * from the machine's own route is left to its code (`undefined`). Everything else —
 * a socket that dropped after the frame was written, a token replaced
 * mid-flight, an error nobody named — is `unknown`.
 */
export function outcomeOf(error: CloudFailureLike): "not_done" | "unknown" | undefined {
  const code = typeof error?.code === "string" ? error.code : ""
  if (SENT_UNANSWERED.has(code)) return "unknown"
  const seq = error?.ref?.seq
  if (!Number.isSafeInteger(seq)) return "not_done"
  if (error.layer === "relay" && code === "machine_offline") return "not_done"
  if (typeof error.layer === "string" && REFUSED_BEFORE_RUNNING.has(error.layer)) return "not_done"
  if (error.layer === "mac_route") return undefined
  return "unknown"
}

/** The status a refusal is answered with when the failure carries none of its own. */
const HTTP_STATUS: Readonly<Record<string, number>> = {
  offline: 503,
  socket_error: 503,
  cloud_starting: 503,
  cloud_reconnecting: 503,
  machine_offline: 503,
  cloud_read_timeout: 504,
  // The machine's reply channel was full. Its refusal carries 429 itself; this
  // is for one that arrives without it, so a page that backs off on 429
  // (`pages/work/reference-images.ts`) still sees one.
  cloud_read_busy: 429,
  cloud_read_only: 403,
  cloud_read_needs_send_prompt: 403,
  cloud_commands_disabled: 403,
  unknown_sender: 403,
  cloud_feature_unavailable: 501,
  cloud_machine_unsupported: 501,
  cloud_not_carried: 501,
}

/** Parse a console route into the command it stands for, or null for one this file does not carry. */
export function writeRoute(method: string, path: string): WriteRoute | null {
  const parts = path.split("/").slice(1)
  if (parts[0] !== "v1") return null
  let segments: string[]
  try {
    segments = parts.slice(1).map((p) => decodeURIComponent(p))
  } catch {
    return null
  }
  const [head, a, b, c, d] = segments
  if (method === "GET") {
    if (head === "work" && a === "v2" && b === "images" && c && segments.length === 4) {
      return { op: "work-v2-image", word: "work.v2.image", artifact: c }
    }
    // The token bill's three reads. The machine answers them on its one reply
    // channel as `work.v2.item` is answered, so a Session's bill is not asked
    // under the session's identity: the conversation is the ledger's key, and
    // this page may hold no row for a child's conversation at all.
    if (head === "usage" && a === "compare-compaction" && segments.length === 2) {
      return { op: "usage-compare", word: "usage.compare-compaction" }
    }
    if (head === "usage" && b && segments.length === 3) {
      const word = USAGE_WORD[a ?? ""]
      if (word) return { op: "usage", word, id: b }
    }
    // The capacity block on Settings: the register's rows and the dead
    // letters, machine-wide, on the machine's one reply channel.
    if (head === "capacity" && segments.length === 1) return { op: "capacity", word: "capacity" }
    if (head === "sessions" && a === "restorable" && segments.length === 2) {
      return { op: "restorable", word: "restorable-sessions" }
    }
    // The dashboard behind the session counts: the machine's CPU and memory
    // and each session's share, on the machine's one reply channel.
    if (head === "machine" && a === "usage" && segments.length === 2) {
      return { op: "machine-usage", word: "machine-usage" }
    }
    if (head === "verifications" && segments.length === 1) {
      return { op: "verification-read", word: "verification.list", id: "" }
    }
    if (head === "verifications" && a && segments.length === 2) {
      return { op: "verification-read", word: "verification.get", id: a }
    }
    if (head === "places" && segments.length === 1) return { op: "places", word: "places" }
    if (head === "places" && a && b === "sessions" && segments.length <= 4) {
      return { op: "past", word: "past-sessions", place: a, assistant: c ?? "" }
    }
    if (head === "artifacts" && a === "images" && b && segments.length === 3) {
      return { op: "image", word: "image", artifact: b }
    }
    // `?parts=` is not read here: the route is the same request either way and
    // the half is read off the URL where the read is made, so one route cannot
    // be parsed into two words.
    if (head === "sessions" && a && b === "info" && segments.length === 3) {
      return { op: "info", word: "info", session: a }
    }
    // The second read parsed here rather than in `relay-reader.ts`, and for
    // the same reason `info` is: it is a session's read, so it has to be asked
    // under the session's own identity as this page holds it (`identity`,
    // F5) rather than on the machine channel the reader's generic uses.
    if (head === "sessions" && a && b === "git" && segments.length === 3) {
      return { op: "git", word: "git", session: a }
    }
    if (head === "sessions" && a && b === "git" && c === "diff" && segments.length === 4) {
      return { op: "git-diff", word: "git-diff", session: a }
    }
    // A session's read for the same reason: asked under the row's identity.
    if (head === "sessions" && a && b === "screen" && segments.length === 3) {
      return { op: "screen", word: "screen", session: a }
    }
    return null
  }
  // The three schedule writes that are not a POST. They are parsed before the
  // gate below rather than after it, because that gate is what said "this
  // console writes with POST and nothing else" — and a schedule is saved with
  // PATCH and removed with DELETE, exactly as `schedules-bridge.ts` spells
  // them against a daemon on this machine's own network.
  if (head === "orchestrator" && a === "schedules" && b && segments.length === 3) {
    if (method === "PATCH" || method === "PUT") return { op: "schedule-update", word: "schedule-update", schedule: b }
    if (method === "DELETE") return { op: "schedule-delete", word: "schedule-delete", schedule: b }
  }
  // And the two snippet writes that are not a POST, for the same reason: a
  // snippet is saved with PATCH and removed with DELETE, exactly as
  // `session/snippets-api.ts` spells them against a daemon on this machine's
  // own network.
  if (head === "snippets" && a && a !== "order" && segments.length === 2) {
    if (method === "PATCH" || method === "PUT") return { op: "snippet-update", word: "snippet-update", snippet: a }
    if (method === "DELETE") return { op: "snippet-delete", word: "snippet-delete", snippet: a }
  }
  if (head === "work" && a === "v2" && b === "items" && c && d === "images" && segments[5] && segments.length === 6 && method === "DELETE") {
    return { op: "work-v2-image-delete", word: "work.v2.image-delete", id: c, image: segments[5] }
  }
  if (head === "work" && a === "v2" && b === "items" && c && segments.length === 4 && method === "PATCH") {
    return { op: "work-v2-edit", word: "work.v2.edit", id: c }
  }
  if (head === "projects" && a && b === "icon" && segments.length === 3 && method === "PUT") return { op: "project-icon-copy", word: "project-icon-copy", id: a }
  if (head === "project-sync" && a === "mirror" && segments.length === 2 && method === "DELETE") {
    return { op: "project-mirror-detach", word: "project-mirror-detach" }
  }
  // One record, by its id: there is no route that deletes more than one.
  if (head === "verifications" && a && segments.length === 2 && method === "DELETE") {
    return { op: "verification-delete", word: "verification.delete", id: a }
  }
  if (method !== "POST") return null
  if (head === "verifications") {
    if (segments.length === 1) return { op: "verification-write", word: "verification.create", id: "" }
    if (a && b === "notes" && segments.length === 3) return { op: "verification-write", word: "verification.note", id: a }
    if (a && b === "close" && segments.length === 3) return { op: "verification-write", word: "verification.close", id: a }
    if (a && b === "criteria" && c && /^(0|[1-9][0-9]?)$/.test(c) && segments.length === 4) {
      return { op: "verification-criterion", word: "verification.criterion", id: a, index: Number(c) }
    }
    return null
  }
  if (head === "project-sync" && a === "mirror" && segments.length === 2) return { op: "project-mirror-apply", word: "project-mirror-apply" }
  if (head === "work" && a === "v2") {
    if (b === "items" && segments.length === 3) return { op: "work-v2-create", word: "work.v2.create" }
    if (b === "items" && c && d === "assign" && segments.length === 5) {
      return { op: "work-v2-assign", word: "work.v2.assign", id: c }
    }
    if (b === "items" && c && d === "remind" && segments.length === 5) {
      return { op: "work-v2-remind", word: "work.v2.remind", id: c }
    }
    if (b === "items" && c && d === "cancel" && segments.length === 5) {
      return { op: "work-v2-cancel", word: "work.v2.cancel", id: c }
    }
    if (b === "items" && c && d === "images" && segments.length === 5) {
      return { op: "work-v2-image-create", word: "work.v2.image-create", id: c }
    }
    if (b === "proposals" && c && (d === "accept" || d === "reject") && segments.length === 5) {
      return { op: "work-v2-proposal-resolve", word: "work.v2.proposal-resolve", id: c, decision: d }
    }
    if (b === "session-todos" && c && segments.length === 4) {
      return { op: "work-v2-todo-create", word: "work.v2.todo-create", terminal: c }
    }
    const todoID = segments[4]
    const action = segments[5]
    if (b === "session-todos" && c && todoID && action === "images" && segments.length === 6) {
      return { op: "work-v2-todo-image-create", word: "work.v2.todo-image-create", terminal: c, id: todoID }
    }
    if (b === "session-todos" && c && todoID && (action === "send" || action === "complete" || action === "delete") && segments.length === 6) {
      return { op: "work-v2-todo-action", word: "work.v2.todo-action", terminal: c, id: todoID, action }
    }
    return null
  }
  if (head === "projects" && a && b === "worktrees" && c === "refresh" && segments.length === 4) {
    return { op: "worktree-refresh", word: "project-worktree-lifecycle-refresh", project: a }
  }
  if (head === "snippets") {
    // `GET /v1/snippets` is the reader's, and falls out above.
    if (segments.length === 1) return { op: "snippet-create", word: "snippet-create" }
    if (segments.length === 2 && a === "order") return { op: "snippet-order", word: "snippet-order" }
    return null
  }
  if (head === "orchestrator" && a === "schedules") {
    // `GET /v1/orchestrator/schedules` is the reader's, and falls out above.
    if (segments.length === 2) return { op: "schedule-create", word: "schedule-create" }
    if (segments.length === 4 && b && c === "run") return { op: "schedule-run", word: "schedule-run", schedule: b }
    return null
  }
  // Before the session writes below, which would read `restorable` as a
  // session id and `restore` as an action it does not have.
  if (head === "sessions" && a === "restorable" && segments.length === 3) {
    if (b === "restore") return { op: "restore", word: "restore-sessions" }
    if (b === "dismiss") return { op: "restore", word: "dismiss-restorable" }
    return null
  }
  if (head === "sessions" && a && segments.length === 3) {
    switch (b) {
      case "send":
        return { op: "send", word: "send", session: a }
      case "key":
        return { op: "answer", word: "answer", session: a }
      case "close":
        return { op: "end", word: "end", session: a }
      case "focus":
        return { op: "focus", word: "focus", session: a }
      case "smart-title":
        return { op: "smart-title", word: "smart-title", session: a }
      case "interrupt":
        return { op: "interrupt", word: "interrupt", session: a }
    }
    if (b && NO_CLOUD_WORD[b]) return { op: "uncarried", word: NO_CLOUD_WORD[b], session: a }
    return null
  }
  if (head === "places" && a && b === "start" && segments.length <= 5) {
    return { op: "start", word: "start", place: a, assistant: c ?? "", model: d ?? "" }
  }
  if (head === "places" && a && b === "resume") {
    // `/resume/<past>` or `/resume/<assistant>/<past>`, as `start-bridge.ts` spells it.
    if (segments.length === 4 && c) return { op: "resume", word: "resume", place: a, assistant: "", past: c }
    if (segments.length === 5 && c && d) return { op: "resume", word: "resume", place: a, assistant: c, past: d }
    return null
  }
  if (head === "voice" && segments.length === 1) return { op: "voice", word: "voice" }
  if (head === "intents" && segments.length === 1) return { op: "intents", word: "intents" }
  // The three requests that change something about notifications. `key` is
  // not among them: it is a read, and `relay-reader.ts` answers it.
  if (head === "push" && segments.length === 2) {
    switch (a) {
      case "subscribe":
        return { op: "push-subscribe", word: "push-subscribe" }
      case "unsubscribe":
        return { op: "push-unsubscribe", word: "push-unsubscribe" }
      case "test":
        return { op: "push-test", word: "push-test" }
    }
    return null
  }
  return null
}

/** A refusal body as the route's own reader takes it. */
type Spelling = "flat" | "nested"

/**
 * The spelling each route's reader parses. `ClawdlineClient` (the composer's
 * send, the close sheet) and `askFocus` recognise only the flat
 * `{error, detail}` and take anything else for a transport failure; the start
 * sheet and the voice row read `app` and `reason`, which only the nested
 * `{error: {code, message, …}}` carries to them. Both are this daemon's own
 * spellings (`internal/transport/http/write.go`, `start.go`).
 *
 * The three push routes are named rather than left to the default, because
 * theirs is the spelling their own reader documents: `push/api.ts` says at the
 * top that `/v1/push/*` answers the gate's nested envelope and not the flat
 * one the rest of this daemon uses. It reads both — what must not happen is a
 * third.
 */
function spellingOf(route: WriteRoute): Spelling {
  switch (route.op) {
    case "send":
    case "answer":
    case "end":
    case "focus":
    case "smart-title":
    case "interrupt":
    case "info":
    case "git":
    case "git-diff":
    case "screen":
    case "uncarried":
      return "flat"
    case "push-subscribe":
    case "push-unsubscribe":
    case "push-test":
      return "nested"
    // The four schedule writes are named rather than left to the default for
    // the reason the push ones are: theirs is the spelling their own route
    // answers. `/v1/orchestrator/schedules*` refuses through `writeAuthRefusal`
    // and `writeBrokerRefusal` (internal/transport/http/schedules.go), both of
    // which send `{"error":{"code","message",…}}` — and a broker refusal's
    // extra fields ride inside that object, where the form reads them.
    case "schedule-create":
    case "schedule-update":
    case "schedule-delete":
    case "schedule-run":
      return "nested"
    // The snippet routes are the other way round: `writeSnippetRefusal` and
    // `writeRefusal` (internal/transport/http/snippets.go) both answer the
    // flat `{"error":"code","detail":"…"}`, and `session/snippets-api.ts`
    // reads both spellings — it has to, because the gate in front of them
    // answers the nested one. Named so that the seam sends the refusal in the
    // spelling the route itself would have sent.
    case "snippet-create":
    case "snippet-update":
    case "snippet-delete":
    case "snippet-order":
      return "flat"
    case "worktree-refresh":
    case "project-icon-copy":
    case "project-mirror-apply":
    case "project-mirror-detach":
      return "flat"
    case "work-v2-create":
    case "work-v2-edit":
    case "work-v2-assign":
    case "work-v2-remind":
    case "work-v2-cancel":
    case "work-v2-image-create":
    case "work-v2-image-delete":
    case "work-v2-proposal-resolve":
    case "work-v2-todo-create":
    case "work-v2-todo-image-create":
    case "work-v2-todo-action":
    // `/v1/usage/*` refuses with `writeRefusal`, the flat spelling
    // (internal/transport/http/usage.go), and the bill's reader takes it.
    case "usage":
    case "usage-compare":
    // `/v1/verifications*` refuses with `writeRefusal`, flat
    // (internal/transport/http/verify.go), and `pages/verify/api.ts` reads it.
    case "verification-read":
    case "verification-write":
    case "verification-criterion":
    case "verification-delete":
    // `/v1/sessions/restorable*` refuses flat (docs/session-restore.md), and
    // `session/restore-offer.ts` reads that spelling.
    case "restorable":
    case "restore":
      return "flat"
    default:
      return "nested"
  }
}

export interface RelayWriterOptions {
  now?: () => number
  /** Makes the request id a waiting-card answer is sent under; `crypto.randomUUID` by default. */
  requestID?: () => string
}

export class RelayWriter {
  private readonly host: WriteHost
  private readonly now: () => number
  private readonly requestID: () => string

  constructor(host: WriteHost, options: RelayWriterOptions = {}) {
    this.host = host
    this.now = options.now ?? (() => Date.now())
    this.requestID = options.requestID ?? (() => globalThis.crypto.randomUUID())
  }

  /** Answer one carried request. `route` is `writeRoute`'s parse of it. */
  async answer(route: WriteRoute, method: string, url: URL, init?: RequestInit): Promise<Response> {
    const started = this.now()
    const path = url.pathname
    const spelling = spellingOf(route)
    if (route.op === "uncarried") {
      return this.refuse(route, method, path, started, spelling, {
        code: "cloud_not_carried",
        message: `${route.word} has no Clawdline Cloud command: do it on the machine itself.`,
        status: 501,
        layer: "browser",
      })
    }
    let client: CloudWriteClient
    try {
      client = this.host.connected()
    } catch (error) {
      return this.refuse(route, method, path, started, spelling, error as CloudFailureLike)
    }
    try {
      const body = await this.carry(client, route, url, init)
      const ms = this.now() - started
      if (route.op === "image" || route.op === "work-v2-image") {
        this.host.note({ method, path, answer: "relay", word: route.word, ms })
        const picture = body as { media_type: string; bytes: Uint8Array }
        return new Response(picture.bytes as BodyInit, { status: 200, headers: { "content-type": picture.media_type } })
      }
      // Showing a session on the machine changes nothing its transcript holds, so
      // it does not set every poll re-reading it (F12).
      const session = route.op === "focus" ? null : sessionOf(route)
      if (session) this.host.wrote(session, "done")
      // This is not an optimistic delete. A successful `end` answer is the
      // daemon reporting that its terminal backend took the tab or pane away.
      // Apply that existence fact before an older retained Cloud row can win
      // the redraw; a later current terminal enumeration may supersede it.
      if (route.op === "end") this.host.closed(route.session)
      this.host.note({ method, path, answer: "relay", word: route.word, ms })
      return json(200, cleanAnswer(body))
    } catch (error) {
      return this.refuse(route, method, path, started, spelling, error as CloudFailureLike)
    }
  }

  private async carry(client: CloudWriteClient, route: WriteRoute, url: URL, init?: RequestInit): Promise<unknown> {
    switch (route.op) {
      case "send": {
        const body = await bodyOf(init)
        const text = typeof body.text === "string" ? body.text : ""
        const images = Array.isArray(body.images) ? body.images.filter((x): x is string => typeof x === "string") : []
        const identity = await this.identity(client, route.session)
        // F2: the card's one request for every attempt, so the machine answers a
        // second attempt with the first one's answer (its receipt) instead of
        // typing the words again. The copied `send` mints a new id per call,
        // so the same read it makes is made here with the card's.
        const request = headerOf(init, "idempotency-key")
        if (request && typeof client._read === "function") {
          return client._read(identity, "send", { request, text, images }, "action:" + request)
        }
        return client.send(identity, text, images)
      }
      case "answer": {
        const body = await bodyOf(init)
        const expect = typeof body.expect === "string" ? body.expect : ""
        const identity = await this.identity(client, route.session)
        return this.press(client, identity, String(body.key ?? ""), expect, headerOf(init, "idempotency-key"))
      }
      case "end": {
        const body = await bodyOf(init)
        const identity = await this.identity(client, route.session)
        // What the page last read of this session's close gates. The Go daemon
        // carries it and does not compare it yet (cloudops `Divergences`); a
        // Swift Mac refuses a close against a reading that has moved.
        const row = await this.row(client, route.session)
        const closeability = row?.closeability as { version?: unknown } | undefined
        const version = typeof closeability?.version === "string" ? closeability.version : ""
        const request = headerOf(init, "idempotency-key")
        if (request && typeof client._read === "function") {
          return client._read(identity, "end", {
            request, accept_loss: body.force === true, expected_closeability_version: version,
          }, "action:" + request)
        }
        return client.end(identity, body.force === true, version)
      }
      case "focus":
        return client.focus(await this.identity(client, route.session))
      case "smart-title":
      case "interrupt": {
        const request = headerOf(init, "idempotency-key")
        if (!request) {
          throw failure("bad_request", `${route.word} needs an Idempotency-Key`, 400)
        }
        if (typeof client._read !== "function") {
          throw failure("cloud_not_carried", route.word, 501)
        }
        return client._read(
          await this.identity(client, route.session),
          route.word,
          { request },
          "action:" + request,
        )
      }
      case "info": {
        // The two halves are two reads on the wire, answered on two channels
        // (`info.full`, `info.summary`), because a full answer settled by a
        // summary would be held as complete while missing what the summary
        // leaves out (`cloudops` `info`). This daemon answers the same body for
        // both — its own divergence — and the page still asks for the half it
        // wants, so a machine that does tell them apart is asked correctly.
        const identity = await this.identity(client, route.session)
        return url.searchParams.get("parts") === "summary" ? client.infoSummary(identity) : client.info(identity)
      }
      case "git":
        // A read, so nothing is marked written and `sessionOf` leaves it out:
        // asking what a repository has changed changes nothing. The machine's own
        // refusal crosses as its code — `not_a_repo` is the one the panel
        // branches on — because `git-bridge.ts` reads the code and not the
        // sentence (`session/GitPanel.tsx`).
        return client.git(await this.identity(client, route.session))
      case "screen":
        // A read, and the lease on the machine's capture: asking is how the
        // page says it is still watching. The answer crosses as the machine
        // wrote it — a first read is `pending` with no `text` — except that
        // the copied client names a tmux screen `on-demand`, which is what
        // keeps the panel asking at the machine's floor (`screen`).
        return client.screen(await this.identity(client, route.session))
      case "git-diff": {
        const path = url.searchParams.get("path") ?? ""
        if (!path || typeof client._read !== "function") {
          throw failure("malformed_read", "a Git diff is read with its changed path", 400)
        }
        const request = this.requestID()
        return client._read(
          await this.identity(client, route.session),
          "git-diff",
          { request, path },
          "read:" + request,
        )
      }
      case "places":
        // `?machine=` is the schedule form's: a schedule is made on the
        // machine chosen in its 「機器」 field, and the Projects offered are
        // that machine's (docs/schedules.md). Every other reader asks this
        // page's machine, as before.
        return client.places(this.namedMachine(url))
      case "past":
        return client.pastSessions(route.place, route.assistant)
      case "start":
        return client.startPlace(route.place, route.assistant, route.model)
      case "resume": {
        // The sheet mints one key per press; carried as the command's request
        // id, a retry of that press is the same request (`resumePlace`).
        const key = headerOf(init, "idempotency-key")
        return client.resumePlace(route.place, route.past, route.assistant, key || undefined)
      }
      case "voice": {
        const body = await bodyOf(init)
        return this.dictate(client, String(body.audio ?? ""), Number(body.rate))
      }
      case "intents": {
        const body = await bodyOf(init)
        if (typeof client.voiceHost !== "function" || typeof client._machineRequest !== "function") {
          throw failure("cloud_not_carried", "intents", 501)
        }
        const host = await client.voiceHost()
        const answer = await client._machineRequest(host.machine, "intents", { text: String(body.text ?? "") }, "action", INTENT_TIMEOUT_MS)
        if (!answer || typeof answer !== "object") return answer
        const draft = (answer as { draft?: unknown }).draft
        if (!draft || typeof draft !== "object") return answer
        const place = (draft as { place_id?: unknown }).place_id
        if (typeof place !== "string" || !place) return answer
        return { ...answer, draft: { ...draft, place_id: cloudProjectID(host.machine, place) } }
      }
      case "image": {
        const session = url.searchParams.get("session") ?? ""
        if (!session || typeof client.image !== "function") {
          throw failure("malformed_read", "a picture is read with the session it belongs to", 400)
        }
        return client.image(await this.identity(client, session), route.artifact)
      }
      case "usage": {
        if (typeof client._machineRequest !== "function") {
          throw failure("cloud_not_carried", route.word, 501)
        }
        // The local route reads no query, and neither does the word: a field
        // added here would be dropped on the machine and answered as if it
        // had not been asked, so it is refused by its own name instead.
        for (const [key] of url.searchParams) {
          throw failure("cloud_not_carried", `${url.pathname}?${key}= is not carried over Clawdline Cloud: read it on the machine.`, 501)
        }
        return client._machineRequest(this.host.machine, route.word, { id: route.id }, "read")
      }
      case "capacity":
      case "machine-usage": {
        if (typeof client._machineRequest !== "function") {
          throw failure("cloud_not_carried", route.word, 501)
        }
        // The route reads no query and neither does the word: a field added
        // here would be dropped on the machine, so it is refused by name.
        for (const [key] of url.searchParams) {
          throw failure("cloud_not_carried", `${url.pathname}?${key}= is not carried over Clawdline Cloud: read it on the machine.`, 501)
        }
        return client._machineRequest(this.host.machine, route.word, {}, "read")
      }
      case "restorable": {
        if (typeof client._machineRequest !== "function") {
          throw failure("cloud_not_carried", route.word, 501)
        }
        for (const [key] of url.searchParams) {
          throw failure("cloud_not_carried", `${url.pathname}?${key}= is not carried over Clawdline Cloud: read it on the machine.`, 501)
        }
        return client._machineRequest(this.host.machine, route.word, {}, "read")
      }
      case "restore": {
        // A restore is one resume per conversation, so the sheet's one key per
        // press is the command's request, as `resume` carries it: a retried
        // envelope is answered with the first answer and opens nothing twice.
        // The route refuses a press without one, and so does this seam,
        // before anything is sealed.
        const request = headerOf(init, "idempotency-key")
        if (!request) throw failure("bad_request", `${route.word} needs an Idempotency-Key`, 400)
        if (typeof client._machineRequestAs !== "function") throw failure("cloud_not_carried", route.word, 501)
        const sent = await bodyOf(init)
        // A dismissal that names no list dismisses every row on offer; an
        // empty list names none. The word keeps that difference by leaving
        // `conversations` out rather than sending it empty.
        const body: Record<string, unknown> = {}
        if (Array.isArray(sent.conversations)) body.conversations = sent.conversations
        return client._machineRequestAs(request, this.host.machine, route.word, body, "action")
      }
      case "verification-read": {
        if (typeof client._machineRequest !== "function") {
          throw failure("cloud_not_carried", route.word, 501)
        }
        // The routes read no query, and neither do the words: a field added
        // here would be dropped on the machine, so it is refused by name.
        for (const [key] of url.searchParams) {
          throw failure("cloud_not_carried", `${url.pathname}?${key}= is not carried over Clawdline Cloud: read it on the machine.`, 501)
        }
        return client._machineRequest(this.host.machine, route.word, route.id ? { id: route.id } : {}, "read")
      }
      case "verification-write": {
        const verification = await bodyOf(init)
        const body: Record<string, unknown> = route.id ? { id: route.id, verification } : { verification }
        return this.machineWorkV2(client, route.word, body, headerOf(init, "idempotency-key"))
      }
      case "verification-criterion": {
        return this.machineWorkV2(client, route.word, { id: route.id, index: route.index, verification: await bodyOf(init) },
          headerOf(init, "idempotency-key"))
      }
      case "verification-delete": {
        // `force` is the one query field, and the word always names it, so a
        // phone that did not say is read as not forcing — never as either.
        let force = false
        for (const [key, value] of url.searchParams) {
          if (key !== "force" || (value !== "1" && value !== "true")) {
            throw failure("cloud_not_carried", `${url.pathname}?${key}= is not carried over Clawdline Cloud: read it on the machine.`, 501)
          }
          force = true
        }
        return this.machineWorkV2(client, route.word, { id: route.id, force }, headerOf(init, "idempotency-key"))
      }
      case "usage-compare": {
        if (typeof client._machineRequest !== "function") {
          throw failure("cloud_not_carried", route.word, 501)
        }
        // `since` is the route's one query field and the word's one field;
        // anything else would be dropped on the machine, so it is refused by
        // its own name here. The machine checks since's spelling.
        const body: { since?: string } = {}
        for (const [key, value] of url.searchParams) {
          if (key !== "since" || "since" in body) {
            throw failure("cloud_not_carried", `${url.pathname}?${key}= is not carried over Clawdline Cloud: read it on the machine.`, 501)
          }
          body.since = value
        }
        return client._machineRequest(this.host.machine, route.word, body, "read")
      }
      case "work-v2-image": {
        if (typeof client._machineRequest !== "function") {
          throw failure("cloud_not_carried", route.word, 501)
        }
        // A card asks for the downscaled copy (`?size=thumb`) and only the
        // full-size viewer asks for the original. Over Cloud every answer
        // shares one reply channel on the machine, and a Board of full PNGs
        // filled it: the size is carried so the machine sends the small one.
        // The word's body is a fixed key set, so any other field, or a size
        // this wire has no name for, is refused by its own name rather than
        // dropped — dropped, the machine would answer the full picture to a
        // page that asked for a thumbnail (`relay-reader.ts` `only`).
        const body: { id: string; size?: "thumb" } = { id: route.artifact }
        for (const [key, value] of url.searchParams) {
          if (key !== "size" || value !== "thumb") {
            throw failure("cloud_not_carried", `${url.pathname}?${key}=${key === "size" ? value : ""} is not carried over Clawdline Cloud: read it on the machine.`, 501)
          }
          body.size = "thumb"
        }
        const answer = await client._machineRequest(this.host.machine, route.word, body, "read") as {
          media_type?: unknown; data?: unknown
        }
        if (typeof answer.media_type !== "string" || typeof answer.data !== "string") {
          throw failure("malformed_answer", "the reference image answer had no bytes", 502)
        }
        const raw = atob(answer.data)
        return { media_type: answer.media_type, bytes: Uint8Array.from(raw, (c) => c.charCodeAt(0)) }
      }
      case "push-subscribe": {
        // The browser's own subscription object, whole. `push/api.ts` posts
        // exactly what `PushSubscription.toJSON()` gave it — its endpoint and
        // its keys — and anything reshaped here would be a chance to get a
        // credential wrong on the way past.
        const subscription = await bodyOf(init)
        if (typeof client.pushSubscribe !== "function") {
          throw failure("cloud_not_carried", "push-subscribe", 501)
        }
        return client.pushSubscribe(subscription)
      }
      case "push-unsubscribe": {
        const body = await bodyOf(init)
        if (typeof client.pushUnsubscribe !== "function") {
          throw failure("cloud_not_carried", "push-unsubscribe", 501)
        }
        return client.pushUnsubscribe(String(body.id ?? ""))
      }
      case "push-test": {
        // The session the notification should tap back to, when the page has
        // one open. Nothing to tap back to is an empty target, not a missing
        // one: the machine's word carries the key either way.
        const body = await bodyOf(init)
        if (typeof client.pushTest !== "function") {
          throw failure("cloud_not_carried", "push-test", 501)
        }
        return client.pushTest(typeof body.session_id === "string" ? body.session_id : "")
      }
      case "schedule-create": {
        // The form's own body, whole: `input/schedule.js` gives both
        // transports the flat request the local route reads (`at`, `days`,
        // `place_id`, …) and the machine is what turns it into a stored record.
        // The copied client reads `place_id` out of it to find which machine the
        // Project is on, so nothing may be reshaped on the way past.
        const schedule = await bodyOf(init)
        if (typeof client.createSchedule !== "function") {
          throw failure("cloud_not_carried", "schedule-create", 501)
        }
        return client.createSchedule(schedule)
      }
      case "schedule-update": {
        const schedule = await bodyOf(init)
        if (typeof client._scheduleBody !== "function" || typeof client._machineRequest !== "function") {
          throw failure("cloud_not_carried", "schedule-update", 501)
        }
        // The row's machine, which the list named (`?machine=`); this page's
        // own machine when nothing was named, as before.
        const machine = this.namedMachine(url)
        const routed = client._scheduleBody(schedule)
        // A save stays a save. Moving a schedule to another machine is not a
        // save with another machine's Project in it — that would be one
        // machine storing a Project it does not have — but three writes the
        // page makes in order: disable here, create there, delete here
        // (`cloud/schedule-move.ts`). So a save naming another machine's
        // Project is still refused.
        if (routed.machine !== machine) {
          throw failure("cloud_schedule_machine_mismatch", "a schedule cannot be moved to a Project on another machine", 409)
        }
        return client._machineRequest(machine, "schedule-update",
          { id: route.schedule, schedule: routed.schedule }, "action")
      }
      case "schedule-delete": {
        if (typeof client._machineRequest !== "function") {
          throw failure("cloud_not_carried", "schedule-delete", 501)
        }
        return client._machineRequest(this.namedMachine(url), "schedule-delete", { id: route.schedule }, "action")
      }
      case "schedule-run": {
        if (typeof client._machineRequest !== "function") {
          throw failure("cloud_not_carried", "schedule-run", 501)
        }
        return client._machineRequest(this.namedMachine(url), "schedule-run", { id: route.schedule }, "action")
      }
      case "snippet-create": {
        // The sheet's own body, whole: `view/snippets-data.js`'s
        // `snippetCreateBody` gives both transports the same flat request the
        // local route reads (`title`, `body`, `scope`, `project`), and the machine
        // is what decides whether it is a snippet and where it lands.
        const snippet = await bodyOf(init)
        if (typeof client.createSnippet !== "function") {
          throw failure("cloud_not_carried", "snippet-create", 501)
        }
        return client.createSnippet(snippet, this.snippetIdentity(url))
      }
      case "snippet-update": {
        const snippet = await bodyOf(init)
        if (typeof client.updateSnippet !== "function") {
          throw failure("cloud_not_carried", "snippet-update", 501)
        }
        return client.updateSnippet(route.snippet, snippet, this.snippetIdentity(url))
      }
      case "snippet-delete": {
        if (typeof client.deleteSnippet !== "function") {
          throw failure("cloud_not_carried", "snippet-delete", 501)
        }
        return client.deleteSnippet(route.snippet, this.snippetIdentity(url))
      }
      case "snippet-order": {
        // Taken apart here and put together again by the copied client, which
        // is the producer for this word: it spells the sub-document
        // `ordering`, and the local route reads the same three fields under no
        // name at all. Neither spelling is derived from the other, so the one
        // that crosses the relay is the one the machine's word carries.
        const body = await bodyOf(init)
        if (typeof client.orderSnippets !== "function") {
          throw failure("cloud_not_carried", "snippet-order", 501)
        }
        const order = Array.isArray(body.order) ? body.order.filter((id): id is string => typeof id === "string") : []
        return client.orderSnippets(
          typeof body.scope === "string" ? body.scope : "",
          typeof body.project === "string" ? body.project : "",
          order,
          this.snippetIdentity(url),
        )
      }
      case "worktree-refresh": {
        if (typeof client._machineRequest !== "function") {
          throw failure("cloud_not_carried", "project-worktree-lifecycle-refresh", 501)
        }
        return client._machineRequest(
          this.host.machine,
          "project-worktree-lifecycle-refresh",
          { project: route.project },
          "action",
        )
      }
      case "project-icon-copy": {
        if (typeof client._place !== "function") throw failure("cloud_not_carried", route.word, 501)
        const place = client._place(route.id)
        if (place.machine !== this.host.machine) throw failure("cloud_project_machine_mismatch", "this Project belongs to another machine", 409)
        return this.machineWorkV2(client, route.word, { id: place.id, item: await bodyOf(init) }, headerOf(init, "idempotency-key"))
      }
      case "project-mirror-apply":
        return this.machineWorkV2(client, route.word, { item: await bodyOf(init) }, headerOf(init, "idempotency-key"))
      case "project-mirror-detach":
        return this.machineWorkV2(client, route.word, { repo: url.searchParams.get("repo") ?? "" }, headerOf(init, "idempotency-key"))
      case "work-v2-create": {
        const item = await bodyOf(init)
        if (typeof client._place !== "function") {
          throw failure("cloud_not_carried", "the Cloud client cannot resolve this Project", 501)
        }
        const place = client._place(item.project_id)
        if (place.machine !== this.host.machine) {
          throw failure("cloud_project_machine_mismatch", "this Project belongs to another machine", 409)
        }
        return this.machineWorkV2(client, route.word, { item: { ...item, project_id: place.id } }, headerOf(init, "idempotency-key"))
      }
      case "work-v2-edit": {
        return this.machineWorkV2(client, route.word, { id: route.id, item: await bodyOf(init) }, headerOf(init, "idempotency-key"))
      }
      case "work-v2-assign": {
        return this.machineWorkV2(client, route.word, { id: route.id, item: await bodyOf(init) }, headerOf(init, "idempotency-key"))
      }
      case "work-v2-remind": {
        return this.machineWorkV2(client, route.word, { id: route.id, item: await bodyOf(init) }, headerOf(init, "idempotency-key"))
      }
      case "work-v2-cancel": {
        return this.machineWorkV2(client, route.word, { id: route.id, item: await bodyOf(init) }, headerOf(init, "idempotency-key"))
      }
      case "work-v2-image-create": {
        return this.machineWorkV2(client, route.word, { id: route.id, item: await bodyOf(init) }, headerOf(init, "idempotency-key"))
      }
      case "work-v2-image-delete": {
        return this.machineWorkV2(client, route.word, { id: route.id, image: route.image, item: await bodyOf(init) }, headerOf(init, "idempotency-key"))
      }
      case "work-v2-proposal-resolve": {
        return this.machineWorkV2(client, route.word, { id: route.id, decision: route.decision, item: await bodyOf(init) }, headerOf(init, "idempotency-key"))
      }
      case "work-v2-todo-create": {
        return this.machineWorkV2(client, route.word, { terminal: route.terminal, item: await bodyOf(init) }, headerOf(init, "idempotency-key"))
      }
      case "work-v2-todo-image-create": {
        return this.machineWorkV2(client, route.word, { terminal: route.terminal, id: route.id, item: await bodyOf(init) }, headerOf(init, "idempotency-key"))
      }
      case "work-v2-todo-action": {
        return this.machineWorkV2(client, route.word, { terminal: route.terminal, id: route.id, action: route.action, item: await bodyOf(init) }, headerOf(init, "idempotency-key"))
      }
      case "uncarried":
        throw failure("cloud_not_carried", route.word, 501)
    }
  }

  private machineWorkV2(client: CloudWriteClient, word: CarriedWord, body: Record<string, unknown>, request: string): Promise<unknown> {
    if (request && typeof client._machineRequestAs === "function") {
      return client._machineRequestAs(request, this.host.machine, word, body, "action")
    }
    if (typeof client._machineRequest !== "function") {
      return Promise.reject(failure("cloud_not_carried", word, 501))
    }
    return client._machineRequest(this.host.machine, word, body, "action")
  }

  /**
   * A waiting card's press, answered by the machine rather than by the relay, and
   * only at the question it was chosen for (F1).
   *
   * The press names that question — `expect`, the fingerprint of the menu the
   * card drew (`session/fingerprint.ts`) — and the machine types nothing unless the
   * question on its screen still has it. So a press is sent only where that
   * check happens: to a machine that lists `answer` in its descriptor and has not
   * shown `cloud_status`, which is the Go daemon (`decodeAnswer` in
   * internal/app/cloudops/ops.go takes `expect`, and refuses an answer without
   * one). Everywhere else it is refused here, before anything is sealed:
   *
   * - no `expect` — a page that could not name the question;
   * - no descriptor yet — the machine's words are not known, and the copied
   *   `answer` would settle on the relay's `delivered`, which proves only
   *   that the envelope reached the machine's socket (F7);
   * - a machine that has shown `cloud_status` — the Swift app, whose `answer`
   *   checks its key set exactly, would refuse `expect`, and without it would
   *   press the digit at whatever question is up.
   *
   * The card's own key is the request when it sent one, so a press retried
   * after its answer was lost is the same request to the machine's receipt.
   */
  private press(client: CloudWriteClient, identity: CloudIdentity, key: string, expect: string, request: string): Promise<unknown> {
    const listed = declaredCommands(client, identity.machine)
    const checks = !client.macCapabilities?.has(identity.machine) && !!listed?.includes("answer") && typeof client._read === "function"
    if (!expect || !checks) {
      return Promise.reject(failure("menu_unverified", "this press cannot be checked against the machine's screen", 428))
    }
    const id = request || this.requestID()
    return client._read!(identity, "answer", { request: id, answer: key, expect }, "action:" + id, undefined, { retireUncertain: true })
  }

  /**
   * Dictation goes to the account's voice machine (`voiceHost`). With two machines and
   * no choice made yet that is ambiguous, and the one this page is reading is
   * the answer a person would give: it is chosen, once — `setVoiceHost` takes
   * it only if it can transcribe, and keeps it for this browser and account,
   * as choosing it on the Swift console's device sheet does.
   */
  private async dictate(client: CloudWriteClient, audio: string, rate: number): Promise<unknown> {
    try {
      return await client.voice(audio, rate)
    } catch (error) {
      const code = (error as CloudFailureLike)?.code
      if (code !== "cloud_voice_host_ambiguous" || !client.setVoiceHost) throw error
      try {
        await client.setVoiceHost(this.host.machine)
      } catch {
        throw error
      }
      return client.voice(audio, rate)
    }
  }

  /**
   * The relay's name for a console row: its own identity when the client
   * holds one. A session this page holds no row for is refused rather than
   * addressed by the raw id (F5): a terminal id outlives the conversation in
   * it, and words for a row that has gone could land in whatever that
   * terminal holds now.
   */
  private async identity(client: CloudWriteClient, session: string): Promise<CloudIdentity> {
    const row = await this.row(client, session)
    if (!row) throw failure("session_not_found", "this page holds no row for that session", 404)
    const identity = row.identity
    if (identity && typeof identity.machine === "string" && typeof identity.session === "string") return identity
    return { machine: this.host.machine, session: typeof row.session === "string" ? row.session : session }
  }

  /**
   * Which machine a snippet write changes, from the session the sheet was opened
   * on.
   *
   * Only the `machine` half is read on the other side (`_snippetRequest`), and
   * this seam already answers for one machine, so nothing here has to be
   * looked up in a session list — which also means a write does not wait on
   * one. The session travels anyway because it is what makes the identity
   * true: a snippet saved from this sheet is saved on the machine this session is
   * on, and an identity that named a machine and no session would be a
   * different claim.
   *
   * A write that names no session is refused rather than aimed at a guess.
   * Both halves of the sheet name one (`session/snippets-api.ts`), so this is
   * a caller that is not this sheet.
   */
  /**
   * The machine a schedule request names with `?machine=`, else this page's.
   *
   * A schedule lives on the machine that runs it, and the list shows every
   * machine's rows, so an action on a row names the row's machine. The name is
   * an authenticated channel's, never a descriptor's: the copied client refuses
   * one it has no route to (`_machineRequest`, `places`), and a machine this
   * browser is not paired with cannot be written to at all.
   */
  private namedMachine(url: URL): string {
    const named = url.searchParams.get("machine")
    return named ? named : this.host.machine
  }

  private snippetIdentity(url: URL): CloudIdentity {
    const session = url.searchParams.get("session")
    if (!session) throw failure("bad_request", "a snippet write names the session it was made from", 400)
    return { machine: this.host.machine, session }
  }

  private async row(client: CloudWriteClient, session: string): Promise<CloudRow | undefined> {
    const all = await client.sessions()
    return all.sessions.find((r) => r.machine === this.host.machine && ((r.session ?? r.id) === session || r.id === session))
  }

  private refuse(
    route: WriteRoute,
    method: string,
    path: string,
    started: number,
    spelling: Spelling,
    error: CloudFailureLike,
  ): Response {
    const code = typeof error?.code === "string" && /^[a-z][a-z0-9_]{0,63}$/.test(error.code) ? error.code : "cloud_failed"
    const message = typeof error?.message === "string" ? error.message : code
    const status =
      typeof error?.status === "number" && error.status >= 400 && error.status < 600 ? error.status : (HTTP_STATUS[code] ?? 502)
    const outcome = outcomeOf(error)
    const session = route.op === "focus" ? null : sessionOf(route)
    if (session) this.host.wrote(session, outcome === "not_done" ? "refused" : "unknown")
    const ref = refOf(error)
    this.host.note({
      method,
      path,
      answer: "refused",
      code,
      word: route.word,
      ms: this.now() - started,
      ...(ref ? { ref } : {}),
    })
    const fields: Record<string, unknown> = {
      layer: typeof error?.layer === "string" ? error.layer : "browser",
      ...(ref ? { ref } : {}),
      retryable: error?.retryable === true,
      // What the page may say about the effect (`outcomeOf`): absent for the
      // machine's own route, whose code the page reads as it does locally.
      ...(outcome ? { outcome } : {}),
      word: route.word,
    }
    // The fields each reader acts on, where the machine sent them: a blocked
    // close's `reasons`, a closed terminal's `app`, a missing Whisper's `reason`.
    for (const key of ["reasons", "app", "reason", "lost"] as const) {
      const value = error?.[key] ?? error?.detail?.[key]
      if (value !== undefined) fields[key] = value
    }
    const body =
      spelling === "flat"
        ? { error: code, detail: message, route: path, ...fields }
        : { error: { code, message, ...fields } }
    return json(status, body)
  }
}

/** The words the machine's descriptor lists, or null when it lists none. */
function declaredCommands(client: CloudWriteClient, machine: string): string[] | null {
  const commands = client.machineDescriptor?.(machine)?.machine?.commands
  return Array.isArray(commands) ? commands.filter((x): x is string => typeof x === "string") : null
}

function sessionOf(route: WriteRoute): string | null {
  switch (route.op) {
    case "send":
    case "answer":
    case "end":
    case "focus":
    case "interrupt":
      return route.session
    case "uncarried":
      return route.session ?? null
    default:
      return null
  }
}

/** The envelope a failure was sent under, as `refText` spells it: `sender·seq`. */
function refOf(error: CloudFailureLike): string | null {
  const ref = error?.ref
  if (!ref || !Number.isSafeInteger(ref.seq)) return null
  const sender = String(ref.sender ?? "").replace(/^web_/, "").slice(0, 8)
  return (sender ? sender + "·" : "") + String(ref.seq)
}

/** A successful answer as the local route gives it: the client's own bookkeeping taken off. */
function cleanAnswer(body: unknown): unknown {
  if (!body || typeof body !== "object" || Array.isArray(body)) return body ?? { ok: true }
  const { optimisticIdentity: _identity, optimisticRequest: _request, ...rest } = body as Record<string, unknown>
  return rest
}

async function bodyOf(init?: RequestInit): Promise<Record<string, unknown>> {
  const raw = init?.body
  if (raw == null) return {}
  const text = typeof raw === "string" ? raw : await new Response(raw).text()
  if (!text) return {}
  try {
    const parsed = JSON.parse(text) as unknown
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : {}
  } catch {
    throw failure("bad_request", "that body is not JSON", 400)
  }
}

function headerOf(init: RequestInit | undefined, name: string): string {
  const headers = init?.headers
  if (!headers) return ""
  if (headers instanceof Headers) return headers.get(name) ?? ""
  if (Array.isArray(headers)) return headers.find(([k]) => k.toLowerCase() === name)?.[1] ?? ""
  for (const [k, v] of Object.entries(headers)) if (k.toLowerCase() === name) return String(v)
  return ""
}

function failure(code: string, message: string, status: number): Error & { code: string; status: number; layer: string } {
  return Object.assign(new Error(message), { code, status, layer: "browser" })
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })
}
