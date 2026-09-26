// Cloud-sent notifications, against a fake account and fake machines:
// `node --test web/console/src/cloud/cloud-push.test.ts`.
//
// The fake account answers what the contract pins for `/v1/push/*`; the fake
// machines answer `push-subscribe` the way a daemon does — its own row id — or
// fail the way an offline machine's relay read does.
import { test } from "node:test"
import assert from "node:assert/strict"
import {
  CloudPushAPI,
  cloudDisable,
  cloudEnable,
  cloudPush,
  cloudResume,
  cloudTest,
  installCloudPush,
  keyText,
  type BrowserPush,
  type CloudPushClient,
  type CloudPushRecord,
  type CloudPushSeam,
  type PushMachineRow,
  type RecordStore,
  // @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
} from "./cloud-push.ts"

const API = "https://api.example.test"
const CLOUD_KEY = "BCloudKeyCloudKeyCloudKe"
const OLD_KEY = "BMachineKeyMachineKeyMac"
const ENDPOINT = "https://web.push.example.com/sub/abc"

interface Asked {
  url: string
  method?: string
  body?: unknown
  credentials?: string
  idempotency?: string | null
}

function account() {
  const asked: Asked[] = []
  const get = ((url: string, init?: RequestInit) => {
    const headers = new Headers(init?.headers)
    asked.push({
      url,
      method: init?.method,
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
      credentials: init?.credentials,
      idempotency: headers.get("Idempotency-Key"),
    })
    const path = url.slice(API.length)
    const answer = (status: number, body?: unknown) =>
      Promise.resolve({ status, json: () => Promise.resolve(body) } as Response)
    if (path === "/v1/push/key") return answer(200, { public_key: CLOUD_KEY })
    if (path === "/v1/push/subscriptions") return answer(201, { id: "cloud-sub-1" })
    if (path.startsWith("/v1/push/subscriptions/")) return answer(204)
    return answer(404, { error: "not_found" })
  }) as unknown as typeof fetch
  return { asked, api: new CloudPushAPI(API, get) }
}

/** Machines on the account; `offline` ones never answer. */
function fleet(rows: PushMachineRow[], offline: Set<string> = new Set()) {
  const sent: { machine: string; type: string; body: Record<string, unknown> }[] = []
  const client: CloudPushClient = {
    _machinesFor: () => ({ capable: rows, unconfirmed: [] }),
    _machineRequest: async (machine, type, body) => {
      sent.push({ machine, type, body })
      if (offline.has(machine)) throw Object.assign(new Error("timeout"), { code: "cloud_read_timeout" })
      return { ok: true, id: "row-" + machine }
    },
  }
  return { client, sent }
}

function memory(start: CloudPushRecord | null = null): RecordStore & { held: CloudPushRecord | null } {
  const store = {
    held: start,
    read: () => store.held,
    write: (record: CloudPushRecord | null) => {
      store.held = record
    },
  }
  return store
}

function bytesOf(text: string): ArrayBuffer {
  let padded = text.replace(/-/g, "+").replace(/_/g, "/")
  while (padded.length % 4) padded += "="
  return Uint8Array.from(atob(padded), (c) => c.charCodeAt(0)).buffer
}

/** A browser whose current subscription, if any, was made with `key`. */
function browser(key: string | null) {
  const log: string[] = []
  const make = (k: string, endpoint: string): PushSubscription =>
    ({
      endpoint,
      options: { applicationServerKey: bytesOf(k), userVisibleOnly: true },
      toJSON: () => ({ endpoint, keys: { p256dh: "p", auth: "a" } }),
      unsubscribe: async () => {
        log.push("unsubscribe:" + k)
        current = null
        return true
      },
    }) as unknown as PushSubscription
  let current: PushSubscription | null = key ? make(key, "https://old.example.com/sub") : null
  const push: BrowserPush = {
    current: async () => current,
    subscribe: async (k) => {
      log.push("subscribe:" + k)
      current = make(k, ENDPOINT)
      return current
    },
  }
  return { push, log }
}

const STUDIO = { id: "m-studio", name: "Studio" }
const BUILDER = { id: "m-builder", name: "Builder" }

test("on: one subscription, the endpoint alone to Cloud, the keys to both machines, the offline one named", async () => {
  const { asked, api } = account()
  const { client, sent } = fleet([STUDIO, BUILDER], new Set([BUILDER.id]))
  const record = memory()
  const seam: CloudPushSeam = { api, client: () => client, record }
  const outcomes = await cloudEnable(seam, browser(null).push)

  assert.deepEqual(asked[1]?.body, { endpoint: ENDPOINT }, "Cloud is given the endpoint and nothing else")
  assert.ok(asked.every((a) => a.credentials === "include"), "every call carries the account cookie")
  assert.ok(asked[1]?.idempotency, "the registration carries an Idempotency-Key")
  assert.deepEqual(sent.map((s) => [s.machine, s.type]), [
    [STUDIO.id, "push-subscribe"],
    [BUILDER.id, "push-subscribe"],
  ])
  assert.equal(sent[0]?.body.cloud_subscription_id, "cloud-sub-1")
  assert.deepEqual(sent[0]?.body.subscription, { endpoint: ENDPOINT, keys: { p256dh: "p", auth: "a" } })
  assert.deepEqual(outcomes, [
    { machine: STUDIO.id, name: "Studio", ok: true, localId: "row-m-studio" },
    { machine: BUILDER.id, name: "Builder", ok: false, code: "cloud_read_timeout" },
  ])
  assert.deepEqual(record.held, { cloudId: "cloud-sub-1", publicKey: CLOUD_KEY, machines: { [STUDIO.id]: "row-m-studio" } })
})

test("a browser subscribed with a machine's key is unsubscribed before Cloud's key is used", async () => {
  const { api } = account()
  const { client } = fleet([STUDIO])
  const { push, log } = browser(OLD_KEY)
  await cloudEnable({ api, client: () => client, record: memory() }, push)
  assert.deepEqual(log, ["unsubscribe:" + OLD_KEY, "subscribe:" + CLOUD_KEY])
})

test("a browser already subscribed with Cloud's key keeps its subscription", async () => {
  const { api } = account()
  const { client } = fleet([STUDIO])
  const { push, log } = browser(CLOUD_KEY)
  await cloudEnable({ api, client: () => client, record: memory() }, push)
  assert.deepEqual(log, [])
})

test("a later visit delivers to the machine that was offline or joined since, and to nobody else", async () => {
  const { api } = account()
  const { client, sent } = fleet([STUDIO, BUILDER])
  const record = memory({ cloudId: "cloud-sub-1", publicKey: CLOUD_KEY, machines: { [STUDIO.id]: "row-m-studio" } })
  const answer = await cloudResume({ api, client: () => client, record }, browser(CLOUD_KEY).push)
  assert.equal(answer.subscribed, true)
  assert.deepEqual(sent.map((s) => s.machine), [BUILDER.id])
  assert.deepEqual(record.held?.machines, { [STUDIO.id]: "row-m-studio", [BUILDER.id]: "row-m-builder" })

  sent.length = 0
  await cloudResume({ api, client: () => client, record }, browser(CLOUD_KEY).push)
  assert.deepEqual(sent, [], "a machine that has it is not asked again")
})

test("a visit whose browser subscription is not Cloud's is off, and tells nobody", async () => {
  const { api } = account()
  const { client, sent } = fleet([STUDIO])
  const record = memory({ cloudId: "cloud-sub-1", publicKey: CLOUD_KEY, machines: {} })
  const answer = await cloudResume({ api, client: () => client, record }, browser(OLD_KEY).push)
  assert.equal(answer.subscribed, false)
  assert.deepEqual(sent, [])
})

test("off: the browser, Cloud's row and every machine's row, with the untold machine named", async () => {
  const { asked, api } = account()
  const { client, sent } = fleet([STUDIO, BUILDER], new Set([BUILDER.id]))
  const record = memory({
    cloudId: "cloud-sub-1",
    publicKey: CLOUD_KEY,
    machines: { [STUDIO.id]: "row-m-studio", [BUILDER.id]: "row-m-builder" },
  })
  const { push, log } = browser(CLOUD_KEY)
  const untold = await cloudDisable({ api, client: () => client, record }, push)
  assert.deepEqual(log, ["unsubscribe:" + CLOUD_KEY])
  assert.deepEqual(sent.map((s) => [s.machine, s.type, s.body.id]), [
    [STUDIO.id, "push-unsubscribe", "row-m-studio"],
    [BUILDER.id, "push-unsubscribe", "row-m-builder"],
  ])
  assert.deepEqual(asked.map((a) => [a.method, a.url]), [["DELETE", API + "/v1/push/subscriptions/cloud-sub-1"]])
  assert.deepEqual(untold, [{ machine: BUILDER.id, name: "Builder", ok: false, code: "cloud_read_timeout" }])
  assert.equal(record.held, null)
})

test("the local console has no Cloud seam, so push.ts keeps its own routes", () => {
  assert.equal(cloudPush(), null)
  const uninstall = installCloudPush({ apiOrigin: API, connected: () => fleet([]).client, record: memory() })
  assert.ok(cloudPush())
  uninstall()
  assert.equal(cloudPush(), null)
})

test("keyText reads a key the way the account writes it", () => {
  assert.equal(keyText(bytesOf(CLOUD_KEY)), keyText(CLOUD_KEY))
})

test("the test goes to a machine the subscription reached, not to the one push machine the old client wanted", async () => {
  const { client, sent } = fleet([STUDIO, BUILDER], new Set([STUDIO.id]))
  const seam: CloudPushSeam = {
    api: account().api,
    client: () => client,
    record: memory({ cloudId: "cloud-sub-1", publicKey: CLOUD_KEY, machines: { [STUDIO.id]: "row-a", [BUILDER.id]: "row-b" } }),
  }
  await cloudTest(seam)
  assert.deepEqual(
    sent.map((s) => [s.machine, s.type]),
    [
      [STUDIO.id, "push-test"],
      [BUILDER.id, "push-test"],
    ],
  )
})

test("a test with no machine holding the subscription says not_subscribed and asks nobody", async () => {
  const { client, sent } = fleet([STUDIO, BUILDER])
  const seam: CloudPushSeam = { api: account().api, client: () => client, record: memory(null) }
  await assert.rejects(cloudTest(seam), { code: "not_subscribed" })
  assert.equal(sent.length, 0)
})
