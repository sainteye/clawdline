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
// reader takes, carrying the code, the layer and the envelope's `ref`, and
// `outcome: "unknown"` when the Mac may have acted on it.
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
 * Codes after which the command may have been carried out even though this
 * page was not told so: the relay handed the envelope to the Mac and then the
 * answer did not come, or came back undeliverable (`cloud-failure.js`,
 * `_readTimedOut`). Trying again blindly could type the same words twice.
 */
const MAY_HAVE_RUN = new Set([
  "cloud_read_timeout",
  "reply_not_received",
  "command_answer_undeliverable",
  "read_answer_undeliverable",
  "delivery_unconfirmed",
  "receipt_expired",
  "ready_expired",
  "peer_rejected",
  // A socket that dropped after the envelope was written.
  "socket_error",
  "cloud_reconnecting",
  "going_away",
])

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
 */
function spellingOf(route: WriteRoute): Spelling {
  switch (route.op) {
    case "send":
    case "answer":
    case "end":
    case "focus":
    case "uncarried":
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
      const session = sessionOf(route)
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
        return client.send(await this.identity(client, route.session), text, images)
      }
      case "answer": {
        const body = await bodyOf(init)
        return this.press(client, await this.identity(client, route.session), String(body.key ?? ""))
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
      case "uncarried":
        throw failure("cloud_not_carried", route.word, 501)
    }
  }

  /**
   * A waiting card's press, answered by the Mac rather than by the relay.
   *
   * The copied `answer` sends a request id — and so waits for the Mac's own
   * `action:<request>` answer — only to a Mac that has shown `cloud_status`,
   * because an older Swift app checks the key set exactly and would refuse the
   * extra key. To any other Mac it sends no id and resolves on the relay's
   * `delivered`, which proves only that the envelope reached the Mac's socket.
   *
   * The Go daemon publishes no `cloud_status`, and it names `answer` in its
   * descriptor and takes `request` beside it (`decodeAnswer` in
   * internal/app/cloudops/ops.go). Sent without one, its refusal — writes
   * switched off, a sender it does not know — has no channel to come back on,
   * and the page would say "sent" for a key that was never pressed. So a Mac
   * that lists the word is asked exactly as the copied client asks a capable
   * one, through the same `_read`, and the press settles on what it did.
   */
  private press(client: CloudWriteClient, identity: CloudIdentity, key: string): Promise<unknown> {
    const listed = declaredCommands(client, identity.machine)
    if (client.macCapabilities?.has(identity.machine) || !listed?.includes("answer") || typeof client._read !== "function") {
      return client.answer(identity, key)
    }
    const request = this.requestID()
    return client._read(identity, "answer", { request, answer: key }, "action:" + request, undefined, { retireUncertain: true })
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

  /** The relay's name for a console row: its own identity when the client holds one. */
  private async identity(client: CloudWriteClient, session: string): Promise<CloudIdentity> {
    const row = await this.row(client, session)
    const identity = row?.identity
    if (identity && typeof identity.machine === "string" && typeof identity.session === "string") return identity
    return { machine: this.host.machine, session: typeof row?.session === "string" ? row.session : session }
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
    const mayHaveRun = MAY_HAVE_RUN.has(code)
    const session = sessionOf(route)
    if (session) this.host.wrote(session, mayHaveRun ? "unknown" : "refused")
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
      // What the page may say about the effect: "unknown" is the honest word
      // for a command that reached the Mac and was not answered.
      outcome: mayHaveRun ? "unknown" : "not_done",
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
