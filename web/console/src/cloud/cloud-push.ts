// Web Push when the console is Clawdline Cloud's: the account sends, every
// machine seals.
//
// A browser holds one push subscription per service worker, bound to one VAPID
// key. While that key was one machine's, a second machine on the account could
// never notify the phone, and the copied client's strict `_pushMachine` refused
// the moment two machines could (`cloud_machine_ambiguous`). So on this console
// the key is the account's: the browser subscribes once with Cloud's key, gives
// Cloud the **endpoint only**, and hands `p256dh`/`auth` to each machine over
// the end-to-end `push-subscribe` word. A machine seals every message itself
// and asks Cloud to forward the ciphertext; Cloud never holds what would let it
// write a notification the phone can read (docs/push.md).
//
// The console a daemon serves on its own network installs nothing here, and
// `push.ts` then does exactly what it did before this file existed.
//
// Nothing here is imported at run time, so `node --test` loads it as it is:
// `node --test web/console/src/cloud/cloud-push.test.ts`.

/** One row of the copied client's `_machinesFor`, as far as this file reads it. */
export interface PushMachineRow {
  id: string
  name?: string
  label?: string
}

/** The part of the copied `CloudClient` the fan-out calls. */
export interface CloudPushClient {
  _machinesFor(type: string): { capable: PushMachineRow[]; unconfirmed: PushMachineRow[] }
  _machineRequest(machine: string, type: string, extra: Record<string, unknown>, kind: "read" | "action"): Promise<unknown>
}

/** What one machine made of being told. */
export interface MachineOutcome {
  machine: string
  name: string
  ok: boolean
  /** The machine's own id for the row it stored, which is what `push-unsubscribe` names. */
  localId?: string
  /** The typed failure, when it did not take it. */
  code?: string
}

/** What this browser remembers about its Cloud subscription. */
export interface CloudPushRecord {
  cloudId: string
  publicKey: string
  /** Machine id → that machine's row id, for every machine that took it. */
  machines: Record<string, string>
}

/** The browser's half: its current subscription and a way to make one. */
export interface BrowserPush {
  current(): Promise<PushSubscription | null>
  subscribe(key: string): Promise<PushSubscription>
}

/** Where the record is kept; localStorage in the page, a map in a test. */
export interface RecordStore {
  read(): CloudPushRecord | null
  write(record: CloudPushRecord | null): void
}

export const PUSH_SUBSCRIBE = "push-subscribe"
export const PUSH_UNSUBSCRIBE = "push-unsubscribe"
const RECORD_KEY = "clawdline.push.cloud"
const API_TIMEOUT_MS = 20_000

/**
 * A failure a machine answers when it simply has no push: it is not a machine
 * to tell, so it is neither named as unreached nor remembered.
 */
const NOT_A_PUSH_MACHINE = new Set(["unknown_command", "cloud_machine_unsupported", "cloud_feature_unavailable"])

function failure(code: string, status = 0): Error & { code: string; status: number } {
  return Object.assign(new Error(code), { code, status })
}

/** The account's push routes (the Cloud API's `/v1/push/*`), cross-origin with the session cookie. */
export class CloudPushAPI {
  readonly origin: string
  private readonly get: typeof fetch

  constructor(origin: string, get: typeof fetch = (...args) => fetch(...args)) {
    this.origin = origin
    this.get = get
  }

  private async call(path: string, init: RequestInit): Promise<{ status: number; body: Record<string, unknown> }> {
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), API_TIMEOUT_MS)
    let res: Response
    try {
      res = await this.get(this.origin + path, { ...init, credentials: "include", signal: controller.signal })
    } catch {
      throw failure("cloud_unreachable")
    } finally {
      clearTimeout(timer)
    }
    let body: Record<string, unknown> = {}
    if (res.status !== 204) {
      try {
        const parsed = (await res.json()) as unknown
        if (parsed && typeof parsed === "object") body = parsed as Record<string, unknown>
      } catch {
        /* an empty or non-JSON body; the status says what happened */
      }
    }
    if (res.status >= 400) {
      const code = typeof body.error === "string" ? body.error : "cloud_push_failed"
      throw failure(code, res.status)
    }
    return { status: res.status, body }
  }

  /** `GET /v1/push/key`: the account's application server key. */
  async key(): Promise<string> {
    const { body } = await this.call("/v1/push/key", { method: "GET" })
    if (typeof body.public_key !== "string" || !body.public_key) throw failure("malformed_answer")
    return body.public_key
  }

  /** `POST /v1/push/subscriptions`: the endpoint, and nothing else, for the account's id. */
  async register(endpoint: string): Promise<string> {
    const { body } = await this.call("/v1/push/subscriptions", {
      method: "POST",
      headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID().toLowerCase() },
      body: JSON.stringify({ endpoint }),
    })
    if (typeof body.id !== "string" || !body.id) throw failure("malformed_answer")
    return body.id
  }

  /** `DELETE /v1/push/subscriptions/:id`. One the account no longer has is already gone. */
  async remove(id: string): Promise<void> {
    try {
      await this.call("/v1/push/subscriptions/" + encodeURIComponent(id), { method: "DELETE" })
    } catch (e) {
      if ((e as { status?: number }).status !== 404) throw e
    }
  }
}

function nameOf(row: PushMachineRow): string {
  return row.name || row.label || row.id
}

/** Every machine this browser is paired with that takes `push-subscribe`, or may. */
export function pushMachines(client: CloudPushClient): PushMachineRow[] {
  const found = client._machinesFor(PUSH_SUBSCRIBE)
  const seen = new Set<string>()
  const out: PushMachineRow[] = []
  for (const row of [...found.capable, ...found.unconfirmed]) {
    if (!row?.id || seen.has(row.id)) continue
    seen.add(row.id)
    out.push(row)
  }
  return out
}

/**
 * Tell each of `machines` about the subscription, in parallel, and say what each
 * made of it. Never rejects: one machine offline is its own row and never costs
 * another machine its notification.
 */
export async function deliver(
  client: CloudPushClient,
  machines: PushMachineRow[],
  subscription: unknown,
  cloudId: string,
): Promise<MachineOutcome[]> {
  const settled = await Promise.all(
    machines.map(async (row): Promise<MachineOutcome | null> => {
      try {
        const answer = (await client._machineRequest(
          row.id,
          PUSH_SUBSCRIBE,
          { subscription, cloud_subscription_id: cloudId },
          "action",
        )) as { id?: unknown } | null
        const localId = typeof answer?.id === "string" && answer.id ? answer.id : undefined
        if (!localId) return { machine: row.id, name: nameOf(row), ok: false, code: "malformed_answer" }
        return { machine: row.id, name: nameOf(row), ok: true, localId }
      } catch (e) {
        const code = String((e as { code?: unknown } | null)?.code || "push_failed")
        if (NOT_A_PUSH_MACHINE.has(code)) return null
        return { machine: row.id, name: nameOf(row), ok: false, code }
      }
    }),
  )
  return settled.filter((row): row is MachineOutcome => row !== null)
}

/** The base64url of a key, however the browser holds it. */
export function keyText(key: ArrayBuffer | ArrayBufferView | string | null | undefined): string {
  if (key == null) return ""
  if (typeof key === "string") return key.replace(/=+$/, "").replace(/\+/g, "-").replace(/\//g, "_")
  const bytes = key instanceof ArrayBuffer ? new Uint8Array(key) : new Uint8Array(key.buffer, key.byteOffset, key.byteLength)
  let raw = ""
  for (const b of bytes) raw += String.fromCharCode(b)
  return btoa(raw).replace(/=+$/, "").replace(/\+/g, "-").replace(/\//g, "_")
}

/** Whether a browser subscription was made with `key`. One that cannot say is treated as not. */
export function madeWith(subscription: PushSubscription, key: string): boolean {
  const held = keyText(subscription.options?.applicationServerKey ?? null)
  return !!held && held === keyText(key)
}

function merged(record: CloudPushRecord, outcomes: MachineOutcome[]): CloudPushRecord {
  const machines = { ...record.machines }
  for (const row of outcomes) if (row.ok && row.localId) machines[row.machine] = row.localId
  return { ...record, machines }
}

/** Everything the three flows need, so a test can hand in fakes for each. */
export interface CloudPushSeam {
  api: CloudPushAPI
  client: () => CloudPushClient
  record: RecordStore
}

/**
 * Turn notifications on: Cloud's key, a browser subscription made with it (one
 * made with another key is dropped first — a push service would refuse every
 * message signed by Cloud for it), the endpoint registered with the account, and
 * the whole subscription handed to every machine.
 */
export async function cloudEnable(seam: CloudPushSeam, browser: BrowserPush): Promise<MachineOutcome[]> {
  const key = await seam.api.key()
  const existing = await browser.current()
  if (existing && !madeWith(existing, key)) await existing.unsubscribe()
  const subscription = existing && madeWith(existing, key) ? existing : await browser.subscribe(key)
  const cloudId = await seam.api.register(subscription.endpoint)
  const outcomes = await deliver(seam.client(), pushMachines(seam.client()), subscription.toJSON(), cloudId)
  seam.record.write(merged({ cloudId, publicKey: key, machines: {} }, outcomes))
  return outcomes
}

/**
 * A later visit: whether notifications are on here, and the subscription
 * delivered to every machine that does not have it yet — one that joined the
 * account since, or was offline last time.
 */
export async function cloudResume(
  seam: CloudPushSeam,
  browser: BrowserPush,
): Promise<{ subscribed: boolean; outcomes: MachineOutcome[] }> {
  const record = seam.record.read()
  const subscription = await browser.current()
  if (!record || !subscription) return { subscribed: false, outcomes: [] }
  if (!madeWith(subscription, record.publicKey)) return { subscribed: false, outcomes: [] }
  const missing = pushMachines(seam.client()).filter((row) => !(row.id in record.machines))
  if (!missing.length) return { subscribed: true, outcomes: [] }
  const outcomes = await deliver(seam.client(), missing, subscription.toJSON(), record.cloudId)
  seam.record.write(merged(record, outcomes))
  return { subscribed: true, outcomes }
}

/**
 * Turn them off: the browser's subscription, the account's row, and each
 * machine's row. Answers the machines that could not be told; the browser has
 * already stopped listening, so each of those only costs a machine a request
 * that its push service answers 410.
 */
export async function cloudDisable(seam: CloudPushSeam, browser: BrowserPush): Promise<MachineOutcome[]> {
  const record = seam.record.read()
  const subscription = await browser.current()
  if (subscription) await subscription.unsubscribe()
  seam.record.write(null)
  if (!record) return []
  let untold: MachineOutcome[] = []
  const names = new Map(pushMachines(seam.client()).map((row) => [row.id, nameOf(row)]))
  const told = await Promise.all(
    Object.entries(record.machines).map(async ([machine, localId]): Promise<MachineOutcome> => {
      const name = names.get(machine) ?? machine
      try {
        await seam.client()._machineRequest(machine, PUSH_UNSUBSCRIBE, { id: localId }, "action")
        return { machine, name, ok: true }
      } catch (e) {
        return { machine, name, ok: false, code: String((e as { code?: unknown } | null)?.code || "push_failed") }
      }
    }),
  )
  untold = told.filter((row) => !row.ok)
  try {
    await seam.api.remove(record.cloudId)
  } catch (e) {
    untold.push({ machine: "", name: CLOUD_ROW, ok: false, code: String((e as { code?: unknown }).code || "push_failed") })
  }
  return untold
}

/** The record in this browser's localStorage, which a private window may not have. */
export const localRecord: RecordStore = {
  read() {
    try {
      const text = localStorage.getItem(RECORD_KEY)
      if (!text) return null
      const parsed = JSON.parse(text) as Partial<CloudPushRecord>
      if (typeof parsed.cloudId !== "string" || typeof parsed.publicKey !== "string") return null
      const machines: Record<string, string> = {}
      if (parsed.machines && typeof parsed.machines === "object") {
        for (const [id, local] of Object.entries(parsed.machines)) if (typeof local === "string") machines[id] = local
      }
      return { cloudId: parsed.cloudId, publicKey: parsed.publicKey, machines }
    } catch {
      return null
    }
  },
  write(record) {
    try {
      if (record) localStorage.setItem(RECORD_KEY, JSON.stringify(record))
      else localStorage.removeItem(RECORD_KEY)
    } catch {
      /* a private window has no storage; the next visit delivers again */
    }
  },
}

/**
 * The test button on this console. The copied client sends a test with no
 * Session to the one push machine (`_pushMachine`, strict), which is exactly
 * what stops making sense once every machine holds the subscription: two of
 * them is `cloud_machine_ambiguous`. Here the test goes to a machine this
 * browser's subscription reached — the first of the account's push machines,
 * in the client's order, that took it, then any other that did — so a test
 * proves the road the next real notification will take. With no machine that
 * took it, the answer is `not_subscribed`, the sentence the button already has.
 */
export async function cloudTest(seam: CloudPushSeam): Promise<unknown> {
  const record = seam.record.read()
  const took = new Set(Object.keys(record?.machines ?? {}))
  if (took.size === 0) throw Object.assign(new Error("not_subscribed"), { code: "not_subscribed" })
  const client = seam.client()
  const ordered = pushMachines(client).map((row) => row.id).filter((id) => took.has(id))
  for (const id of took) if (!ordered.includes(id)) ordered.push(id)
  let last: unknown = null
  for (const machine of ordered) {
    try {
      return await client._machineRequest(machine, "push-test", { target: "" }, "action")
    } catch (e) {
      last = e
    }
  }
  throw last
}

/** The row naming Cloud itself when its own half of turning notifications off failed. */
const CLOUD_ROW = "Clawdline Cloud"

let installed: CloudPushSeam | null = null
const listeners = new Set<() => void>()

/** Installed by the hosted gate once a machine is chosen; the answer uninstalls it. */
export function installCloudPush(options: {
  apiOrigin: string
  connected: () => CloudPushClient
  record?: RecordStore
}): () => void {
  const seam: CloudPushSeam = {
    api: new CloudPushAPI(options.apiOrigin),
    client: options.connected,
    record: options.record ?? localRecord,
  }
  installed = seam
  for (const listener of listeners) listener()
  return () => {
    if (installed === seam) installed = null
  }
}

/** The Cloud seam, or null on a console with no Cloud account behind it. */
export function cloudPush(): CloudPushSeam | null {
  return installed
}

export function onCloudPush(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}
