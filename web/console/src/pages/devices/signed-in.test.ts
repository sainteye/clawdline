// The direct-pairing grant against a fake daemon:
// `node --experimental-strip-types --test web/console/src/pages/devices/signed-in.test.ts`.
//
// What is checked is what crosses the wire and what the block is allowed to
// say afterwards: the request a grant sends, how a refusal comes back, how a
// row's caps read, and that a grant is only called saved once the list read
// again agrees.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node's strip-types runner.
import { devicePermission, grantOutcome, readSignedIn, revokeDevice, setDevicePermission } from "./signed-in.ts"

type Call = { url: string; init?: RequestInit }

function daemon(status: number, body: unknown, calls: Call[] = []): typeof fetch {
  return (async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    const text = typeof body === "string" ? body : JSON.stringify(body)
    return new Response(text, { status, headers: { "Content-Type": "application/json" } })
  }) as unknown as typeof fetch
}

const row = (id: string, caps: string[]) => ({ id, name: "Phone " + id, caps, created: 1 })
const list = (...devices: ReturnType<typeof row>[]) => ({ kind: "list" as const, list: { configured: true, devices, password: false } })

test("a grant posts the chosen permission for that device only", async () => {
  const calls: Call[] = []
  await setDevicePermission("dev 1/x", "read-only", daemon(200, { ok: true }, calls))
  assert.equal(calls.length, 1)
  assert.equal(calls[0].url, "/v1/auth/devices/dev%201%2Fx/permission")
  assert.equal(calls[0].init?.method, "POST")
  assert.deepEqual(JSON.parse(String(calls[0].init?.body)), { permission: "read-only" })

  await setDevicePermission("d2", "read-and-operate", daemon(200, { ok: true }, calls))
  assert.deepEqual(JSON.parse(String(calls[1].init?.body)), { permission: "read-and-operate" })
})

test("a refused grant keeps its code so the block can say which refusal it was", async () => {
  const refusal = { error: { code: "store_unavailable", message: "device store unavailable" } }
  await assert.rejects(setDevicePermission("d1", "read-only", daemon(503, refusal)), (error: any) => {
    assert.equal(error.name, "RefusalError")
    assert.equal(error.status, 503)
    assert.equal(error.code, "store_unavailable")
    return true
  })
  await assert.rejects(
    setDevicePermission("d1", "read-only", daemon(502, "<html>bad gateway</html>")),
    (error: any) => error.name === "TransportError",
  )
})

test("revoke still sends an empty body", async () => {
  const calls: Call[] = []
  await revokeDevice("d1", daemon(200, { ok: true }, calls))
  assert.equal(calls[0].url, "/v1/auth/devices/d1/revoke")
  assert.equal(calls[0].init?.body, "{}")
})

test("caps read as a grant only when they are exactly one", () => {
  assert.equal(devicePermission(["read"]), "read-only")
  assert.equal(devicePermission(["read", "send"]), "read-and-operate")
  assert.equal(devicePermission(["send", "read"]), "read-and-operate")
  assert.equal(devicePermission([]), "unknown")
  assert.equal(devicePermission(["send"]), "unknown")
  assert.equal(devicePermission(["read", "send", "admin"]), "unknown")
  assert.equal(devicePermission(undefined), "unknown")
})

test("a grant is saved only when the list read again shows it", () => {
  assert.deepEqual(grantOutcome(list(row("a", ["read"])), "a", "read-only"), { kind: "saved" })
  assert.deepEqual(grantOutcome(list(row("a", ["read", "send"])), "a", "read-only"), {
    kind: "mismatch",
    now: "read-and-operate",
    caps: ["read", "send"],
  })
  assert.deepEqual(grantOutcome(list(row("b", ["read"])), "a", "read-only"), { kind: "mismatch", now: "unknown", caps: [] })
  assert.deepEqual(grantOutcome({ kind: "failed", why: "503 Service Unavailable" }, "a", "read-only"), {
    kind: "unread",
    why: "503 Service Unavailable",
  })
  assert.equal(grantOutcome({ kind: "device" }, "a", "read-only").kind, "unread")
})

test("the list read tells a device browser, a failure and a list apart", async () => {
  assert.deepEqual(await readSignedIn(daemon(403, { error: { code: "forbidden", message: "no" } })), { kind: "device" })
  assert.deepEqual(await readSignedIn(daemon(500, { error: { code: "x", message: "store broke" } })), {
    kind: "failed",
    why: "store broke",
  })
  const answer = await readSignedIn(daemon(200, { devices: [row("a", ["read"])], password: false }))
  assert.equal(answer.kind, "list")
  assert.deepEqual(answer.list.devices[0].caps, ["read"])
})
