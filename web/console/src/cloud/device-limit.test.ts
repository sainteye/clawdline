// The device-limit card, against a fake control plane:
// `node --test web/console/src/cloud/device-limit.test.ts`.
//
// The session is the copied one (`cloud-boot.js`), so the requests checked
// here are the ones the hosted console sends; only `fetch`, storage and
// IndexedDB are fakes. The fake answers as `api/src/routes/auth.ts` does: a
// full account refuses `POST /v1/auth/session` with `409 device_limit_reached`,
// the fresh login ticket may then list and revoke, and after one revoke the
// same ticket is given a session.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see cloud/forget.test.ts.
import { COPIED_TIER_FALLBACK, DeviceLimitRun, knownTier, readFailure, type DeviceLimitState } from "./device-limit.ts"
import { CloudViewerSession } from "../legacy/js/net/cloud-boot.js"

const API = "https://api.example.test"
const CONFIG = { appOrigin: "https://app.example.test", apiOrigin: API, relayURL: "wss://relay.example.test", build: "test", strings: {} }

const DEVICES = [
  { id: "dev_old", kind: "browser", name: "Browser", created_at: "2026-08-01T10:00:00.000Z", last_seen_at: "2026-08-02T10:00:00.000Z" },
  { id: "dev_phone", kind: "ios", name: "iPhone", created_at: "2026-09-01T10:00:00.000Z", last_seen_at: null },
]

interface Asked {
  method: string
  path: string
}

/** The control plane as a full account sees it, recording each request. */
function plane(options: { listStatus?: number; listCode?: string; revokeStatus?: number; tier?: string } = {}) {
  const asked: Asked[] = []
  let free = false
  const reply = (status: number, body: unknown) =>
    ({ status, json: () => Promise.resolve(body) }) as Response
  const fetch = (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    const path = url.slice(API.length)
    asked.push({ method, path })
    if (path === "/v1/auth/session" && method === "GET") {
      return Promise.resolve(reply(401, { error: { code: "no_session", message: "Sign in first" } }))
    }
    if (path === "/v1/auth/session" && method === "POST") {
      return Promise.resolve(
        free
          ? reply(201, { account_id: "acct_test", device_id: "dev_new", caps: ["read_sessions"] })
          : reply(409, {
              error: {
                code: "device_limit_reached",
                message: "limit",
                details: options.tier === undefined ? { tier: "pro", limit: 2 } : { limit: 2 },
              },
            }),
      )
    }
    if (path === "/v1/auth/recovery/devices" && method === "GET") {
      if (options.listStatus) {
        return Promise.resolve(
          reply(options.listStatus, { error: { code: options.listCode ?? "boom", message: "no" } }),
        )
      }
      return Promise.resolve(reply(200, { tier: options.tier ?? "pro", limit: 2, active: 2, devices: DEVICES }))
    }
    if (path.startsWith("/v1/auth/recovery/devices/") && method === "DELETE") {
      if (options.revokeStatus) {
        return Promise.resolve(reply(options.revokeStatus, { error: { code: "unknown_device", message: "No such device" } }))
      }
      free = true
      return Promise.resolve(reply(200, { status: "revoked", active: 1, limit: 2 }))
    }
    return Promise.resolve(reply(404, { error: { code: "not_found", message: path } }))
  }
  return { asked, fetch }
}

/** Just enough IndexedDB for `storeCryptoKey` to put a key away. */
function keyStore() {
  const kept = new Map<string, unknown>()
  const db = {
    objectStoreNames: { contains: () => true },
    createObjectStore() {},
    close() {},
    transaction() {
      const tx: { oncomplete?: () => void; onerror?: () => void; onabort?: () => void; objectStore: () => unknown } = {
        objectStore: () => ({
          put(value: unknown, name: string) {
            kept.set(name, value)
            queueMicrotask(() => tx.oncomplete?.())
          },
        }),
      }
      return tx
    },
  }
  return {
    kept,
    open() {
      const request: { result: unknown; onsuccess?: () => void; onupgradeneeded?: () => void } = { result: db }
      queueMicrotask(() => request.onsuccess?.())
      return request
    },
  }
}

function memoryStorage() {
  const held = new Map<string, string>()
  return {
    getItem: (key: string) => held.get(key) ?? null,
    setItem: (key: string, value: string) => void held.set(key, value),
    removeItem: (key: string) => void held.delete(key),
  }
}

function session(control: ReturnType<typeof plane>) {
  return new CloudViewerSession({
    config: CONFIG,
    fetch: control.fetch,
    storage: memoryStorage(),
    indexedDB: keyStore(),
    deviceKind: "browser",
    deviceName: "Browser",
  })
}

test("a full account lists its devices, and removing one asks for the session again and gets it", async () => {
  const control = plane()
  const viewer = session(control)
  const refused = await viewer.ensureSession()
  assert.equal(refused.state, "device_limit_reached")

  const states: DeviceLimitState[] = []
  const continued: Promise<unknown>[] = []
  const run = new DeviceLimitRun(viewer, {
    onState: (state: DeviceLimitState) => states.push(state),
    continueSignIn: () => continued.push(viewer.ensureSession()),
    tier: refused.tier,
    limit: refused.limit,
  })
  await run.load()
  const listed = run.current
  assert.equal(listed.phase, "listed")
  assert.ok(listed.phase === "listed")
  assert.deepEqual(listed.devices.map((d: { id: string }) => d.id), ["dev_old", "dev_phone"], "the card lists every active device")
  assert.equal(listed.tier, "pro")
  assert.equal(listed.limit, 2)
  assert.equal(listed.devices[1]!.last_seen_at, null, "a device never seen since it was added says so, not a made-up time")

  await run.revoke("dev_old")
  assert.equal(control.asked.filter((a) => a.method === "DELETE").length, 0, "nothing is removed before the in-card question is answered")

  run.ask("dev_old")
  assert.equal(run.current.phase === "listed" && run.current.asking, "dev_old")
  await run.revoke("dev_old")
  assert.equal(run.current.phase, "continuing")
  assert.equal(continued.length, 1, "a confirmed revoke asks for the session once")
  const signedIn = (await continued[0]) as { state: string; deviceID: string }
  assert.equal(signedIn.state, "ready", "the freed slot is taken without a reload")
  assert.equal(signedIn.deviceID, "dev_new")

  assert.deepEqual(control.asked, [
    { method: "GET", path: "/v1/auth/session" },
    { method: "POST", path: "/v1/auth/session" },
    { method: "GET", path: "/v1/auth/recovery/devices" },
    { method: "DELETE", path: "/v1/auth/recovery/devices/dev_old" },
    { method: "GET", path: "/v1/auth/session" },
    { method: "POST", path: "/v1/auth/session" },
  ])
  assert.deepEqual(states.map((s) => s.phase), ["loading", "listed", "listed", "listed", "continuing"])
})

test("an expired login ticket is told apart from a list that failed for another reason", async () => {
  const expired = new DeviceLimitRun(session(plane({ listStatus: 401, listCode: "no_login_ticket" })), {
    onState: () => {},
    continueSignIn: () => assert.fail("nothing was revoked"),
  })
  await expired.load()
  assert.deepEqual(expired.current, {
    phase: "list_failed",
    failure: { step: "list", code: "no_login_ticket", expired: true },
  })

  const broken = new DeviceLimitRun(session(plane({ listStatus: 503, listCode: "store_unavailable" })), {
    onState: () => {},
    continueSignIn: () => assert.fail("nothing was revoked"),
  })
  await broken.load()
  assert.deepEqual(broken.current, {
    phase: "list_failed",
    failure: { step: "list", code: "store_unavailable", expired: false },
  })
})

test("a revoke that fails keeps the list on screen with the reason, and does not sign in", async () => {
  const control = plane({ revokeStatus: 404 })
  let asked = 0
  const run = new DeviceLimitRun(session(control), { onState: () => {}, continueSignIn: () => asked++ })
  await run.load()
  run.ask("dev_phone")
  await run.revoke("dev_phone")
  const after = run.current
  assert.equal(after.phase, "listed")
  assert.ok(after.phase === "listed")
  assert.deepEqual(after.failure, { step: "revoke", code: "unknown_device", expired: false })
  assert.equal(after.revoking, null)
  assert.equal(after.devices.length, 2)
  assert.equal(asked, 0)
})

test("the copied session's English stand-in is never shown as a plan's name", async () => {
  assert.equal(knownTier(COPIED_TIER_FALLBACK), null)
  assert.equal(knownTier(""), null)
  assert.equal(knownTier(undefined), null)
  assert.equal(knownTier("pro"), "pro")

  const run = new DeviceLimitRun(session(plane({ tier: COPIED_TIER_FALLBACK })), {
    onState: () => {},
    continueSignIn: () => {},
    tier: COPIED_TIER_FALLBACK,
  })
  await run.load()
  assert.equal(run.current.phase === "listed" && run.current.tier, null)
})

test("a fetch that never reached the control plane says so", () => {
  assert.deepEqual(readFailure("list", new TypeError("Failed to fetch")), {
    step: "list",
    code: "network_unreachable",
    expired: false,
  })
})
