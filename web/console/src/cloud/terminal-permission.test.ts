import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- the focused test runner bundles TypeScript before Node executes it.
import { readTerminalPermissionDevice } from "./terminal-permission.ts"

const api = "https://api.example.test"
const id = "web_current"

function reply(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

test("reads only the signed-in browser's active identity", async () => {
  let requests = 0
  const fetcher = async (_url: string, init?: RequestInit) => {
    requests += 1
    assert.equal(init?.credentials, "include")
    assert.equal(init?.cache, "no-store")
    return reply(200, { devices: [
      { id: "web_other", caps: ["send_prompt"], capability_epoch: 2, revoked_at: null },
      { id, caps: ["read_sessions"], capability_epoch: 3, revoked_at: null },
    ] })
  }
  assert.deepEqual((await readTerminalPermissionDevice(api, id, fetcher)).caps, ["read_sessions"])
  assert.equal(requests, 1)
})

test("a missing or revoked browser cannot be shown as terminal capable", async () => {
  await assert.rejects(readTerminalPermissionDevice(api, id, async () => reply(401, {})), /device_unavailable/)
  await assert.rejects(readTerminalPermissionDevice(api, id, async () => reply(200, { devices: [] })), /device_unavailable/)
  await assert.rejects(readTerminalPermissionDevice(api, id, async () => reply(200, {
    devices: [{ id, caps: ["send_prompt"], capability_epoch: 2, revoked_at: "2026-10-03T00:00:00Z" }],
  })), /device_unavailable/)
})
