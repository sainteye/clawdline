// The console's writes, carried to a machine across the relay.
//
// The console already knows how to act on a session: the composer POSTs
// `/v1/sessions/<id>/send`, a waiting card POSTs `/key`, the start sheet POSTs
// `/v1/places/<id>/start`, and so on — each against a daemon on its own
// origin, each with the words and the states it shows while that is under way
// (`session/pending.ts`, `Waiting.tsx`, `Start.tsx`). This file is the other
// half of `relay-reader.ts`: it answers those same requests out of the copied
// `CloudClient`, so every one of those screens keeps its own look and its own
// words and does not learn that the Mac is far away.
//
// Two rules decide everything here.
//
// **Only words the Mac already has.** Each route is one command the machine
// lists in its descriptor (`cloudops.Implemented()` on the Go daemon, carried
// as `machine.commands`), spelled the way `net/cloud-client.js` spells it. A
// route with no command behind it is refused by name, here, before anything is
// sealed; a command this machine does not list is refused by the copied
// client before it leaves (`cloud_feature_unavailable`). Nothing is invented.
//
// **A failure is an answer, not a silence.** A read that nobody answered
// rejects its `fetch`, because the console draws that as "away" and polls
// again. A write cannot be treated that way: the command may have reached the
// Mac and run, and the page must be able to say which of those it knows. So
// every write settles with a typed refusal in the spelling the route's own
// reader takes, carrying the code, the layer and the envelope's `ref`, and the
// `outcome` this seam can vouch for (`outcomeOf` below): `not_done` only when
// it can prove the envelope never reached the Mac, `unknown` otherwise, and
// none at all for a refusal from the Mac's own route, whose code says it — as
// the same refusal from a daemon on this machine does (`session/outcome.ts`).
//
// Nothing is imported at run time, so `node --test` loads it as it is.
import type { CloudIdentity, CloudReadClient, CloudRow, SeamRow } from "./relay-reader.js"

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
 * envelope, and the Mac's answer on `t/<machine>/<session>`.
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
  setVoiceHost?(machine: string): Promise<unknown>
  /**
   * The three notification writes. Each picks the account's push Mac strictly
   * (`_pushMachine`): the key, the subscription and its removal must all reach
   * the same one, so it is never the freshest of two. Optional for the reason
   * `pushKey` is: a copied client older than these words is refused by name
   * rather than throwing where nobody is catching.
   */
  pushSubscribe?(subscription: unknown): Promise<unknown>
  pushUnsubscribe?(id: string): Promise<unknown>
  pushTest?(session: string): Promise<unknown>
  image?(identity: CloudIdentity, id: string): Promise<{ id: string; media_type: string; bytes: Uint8Array }>
  /**
   * The copied client's one read, which `answer` itself calls for a Mac that
   * has shown `cloud_status`. See `RelayWriter.answer` for why it is called
   * directly for a Mac that names `answer` in its descriptor instead.
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
  | { op: "send"; word: "send"; session: string }
  | { op: "answer"; word: "answer"; session: string }
  | { op: "end"; word: "end"; session: string }
  | { op: "focus"; word: "focus"; session: string }
  | { op: "start"; word: "start"; place: string; assistant: string; model: string }
  | { op: "resume"; word: "resume"; place: string; assistant: string; past: string }
  | { op: "voice"; word: "voice" }
  | { op: "places"; word: "places" }
  | { op: "past"; word: "past-sessions"; place: string; assistant: string }
  | { op: "image"; word: "image"; artifact: string }
  | { op: "push-subscribe"; word: "push-subscribe" }
  | { op: "push-unsubscribe"; word: "push-unsubscribe" }
  | { op: "push-test"; word: "push-test" }
  | { op: "uncarried"; word: string; session?: string }

/**
 * The routes this daemon answers locally that have no command on the Cloud
 * wire at all — not on the Go daemon and not in the Swift app's vocabulary
 * (docs/cloud-wire.md §10.3). Each is refused by its own name, with where it
 * can be done instead, rather than as a generic "not carried".
 */
const NO_CLOUD_WORD: Readonly<Record<string, string>> = {
  interrupt: "interrupt",
  rename: "title",
  title: "title",
}

/**
 * Codes that mean the envelope went and no answer came back: the relay handed
 * it to the Mac and then nothing, or the Mac ran it and its reply could not be
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
 * Layers whose refusal is decided before anything is carried out: the Mac's
 * admission (the write switch, the roster, the clock) and its transport (the
 * queue was full).
 */
const REFUSED_BEFORE_RUNNING = new Set(["mac_preflight", "mac_transport"])

/**
 * What this seam can vouch for about a failed write (F3). The burden is on
 * `not_done`, because it is what sends a person to "try again": it is said
 * only when the envelope provably never reached the Mac — the copied client
 * refused before sealing it (no sequence was spent, so no `ref`), the relay
 * said the Mac was not connected, or the Mac refused it at the door. A refusal
 * from the Mac's own route is left to its code (`undefined`). Everything else —
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
    if (head === "places" && segments.length === 1) return { op: "places", word: "places" }
    if (head === "places" && a && b === "sessions" && segments.length <= 4) {
      return { op: "past", word: "past-sessions", place: a, assistant: c ?? "" }
    }
    if (head === "artifacts" && a === "images" && b && segments.length === 3) {
      return { op: "image", word: "image", artifact: b }
    }
    return null
  }
  if (method !== "POST") return null
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
    case "uncarried":
      return "flat"
    case "push-subscribe":
    case "push-unsubscribe":
    case "push-test":
      return "nested"
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
        message: `${route.word} has no Clawdline Cloud command: do it on the Mac itself.`,
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
      if (route.op === "image") {
        this.host.note({ method, path, answer: "relay", word: route.word, ms })
        const picture = body as { media_type: string; bytes: Uint8Array }
        return new Response(picture.bytes as BodyInit, { status: 200, headers: { "content-type": picture.media_type } })
      }
      // Showing a session on the Mac changes nothing its transcript holds, so
      // it does not set every poll re-reading it (F12).
      const session = route.op === "focus" ? null : sessionOf(route)
      if (session) this.host.wrote(session, "done")
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
        // F2: the card's one request for every attempt, so the Mac answers a
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
      case "places":
        return client.places(this.host.machine)
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
      case "image": {
        const session = url.searchParams.get("session") ?? ""
        if (!session || typeof client.image !== "function") {
          throw failure("malformed_read", "a picture is read with the session it belongs to", 400)
        }
        return client.image(await this.identity(client, session), route.artifact)
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
        // one: the Mac's word carries the key either way.
        const body = await bodyOf(init)
        if (typeof client.pushTest !== "function") {
          throw failure("cloud_not_carried", "push-test", 501)
        }
        return client.pushTest(typeof body.session_id === "string" ? body.session_id : "")
      }
      case "uncarried":
        throw failure("cloud_not_carried", route.word, 501)
    }
  }

  /**
   * A waiting card's press, answered by the Mac rather than by the relay, and
   * only at the question it was chosen for (F1).
   *
   * The press names that question — `expect`, the fingerprint of the menu the
   * card drew (`session/fingerprint.ts`) — and the Mac types nothing unless the
   * question on its screen still has it. So a press is sent only where that
   * check happens: to a Mac that lists `answer` in its descriptor and has not
   * shown `cloud_status`, which is the Go daemon (`decodeAnswer` in
   * internal/app/cloudops/ops.go takes `expect`, and refuses an answer without
   * one). Everywhere else it is refused here, before anything is sealed:
   *
   * - no `expect` — a page that could not name the question;
   * - no descriptor yet — the Mac's words are not known, and the copied
   *   `answer` would settle on the relay's `delivered`, which proves only
   *   that the envelope reached the Mac's socket (F7);
   * - a Mac that has shown `cloud_status` — the Swift app, whose `answer`
   *   checks its key set exactly, would refuse `expect`, and without it would
   *   press the digit at whatever question is up.
   *
   * The card's own key is the request when it sent one, so a press retried
   * after its answer was lost is the same request to the Mac's receipt.
   */
  private press(client: CloudWriteClient, identity: CloudIdentity, key: string, expect: string, request: string): Promise<unknown> {
    const listed = declaredCommands(client, identity.machine)
    const checks = !client.macCapabilities?.has(identity.machine) && !!listed?.includes("answer") && typeof client._read === "function"
    if (!expect || !checks) {
      return Promise.reject(failure("menu_unverified", "this press cannot be checked against the Mac's screen", 428))
    }
    const id = request || this.requestID()
    return client._read!(identity, "answer", { request: id, answer: key, expect }, "action:" + id, undefined, { retireUncertain: true })
  }

  /**
   * Dictation goes to the account's voice Mac (`voiceHost`). With two Macs and
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
      // Mac's own route, whose code the page reads as it does locally.
      ...(outcome ? { outcome } : {}),
      word: route.word,
    }
    // The fields each reader acts on, where the Mac sent them: a blocked
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
