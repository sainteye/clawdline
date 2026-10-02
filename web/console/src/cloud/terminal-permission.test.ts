import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path for Node's test runner.
import { changeTerminalPermission, readTerminalPermissionDevice } from "./terminal-permission.ts"

const api = "https://api.example.test"
const id = "web_current"
const reply = (status: number, body: unknown) => ({ ok: status >= 200 && status < 300, status, json: async () => body }) as Response

test("the signed-in browser changes only its own terminal cap and preserves its other caps", async () => {
  let caps = ["read_sessions", "send_prompt"]
  let epoch = 7
  const asked: { path: string; method: string; credentials: RequestCredentials | undefined; body?: unknown }[] = []
  const fetcher = async (url: string, init?: RequestInit): Promise<Response> => {
    const path = new URL(url).pathname
    const method = init?.method ?? "GET"
    asked.push({ path, method, credentials: init?.credentials, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (method === "GET") return reply(200, { devices: [
      { id: "web_other", caps: ["terminal_control"], capability_epoch: 2, revoked_at: null },
      { id, caps, capability_epoch: epoch, revoked_at: null },
    ] })
    assert.equal(path, `/v1/devices/${id}/capabilities`)
    const input = JSON.parse(String(init?.body)) as { caps: string[]; expected_capability_epoch: number }
    assert.equal(input.expected_capability_epoch, epoch)
    caps = input.caps
    epoch++
    return reply(200, { status: "rotated", capability_epoch: epoch, caps })
  }
  assert.deepEqual((await changeTerminalPermission(api, id, true, fetcher)).caps,
    ["read_sessions", "send_prompt", "terminal_control"])
  assert.deepEqual((await changeTerminalPermission(api, id, false, fetcher)).caps,
    ["read_sessions", "send_prompt"])
  assert.equal(asked.filter((row) => row.method === "PATCH").length, 2)
  assert.ok(asked.every((row) => row.credentials === "include"))
})

test("a refused or ambiguous account read never writes a permission", async () => {
  const asked: string[] = []
  const fetcher = async (url: string): Promise<Response> => {
    asked.push(url)
    return reply(200, { devices: [{ id, caps: ["read_sessions"], capability_epoch: 1, revoked_at: "2026-10-02" }] })
  }
  await assert.rejects(changeTerminalPermission(api, id, true, fetcher), /device_unavailable/)
  assert.equal(asked.length, 1)
  await assert.rejects(readTerminalPermissionDevice(api, id, async () => reply(503, { error: { code: "offline" } })), /devices_unavailable/)
})

test("a stale epoch refusal leaves the UI without a success receipt", async () => {
  const fetcher = async (_url: string, init?: RequestInit): Promise<Response> => init?.method === "PATCH"
    ? reply(409, { error: { code: "stale_capability_epoch" } })
    : reply(200, { devices: [{ id, caps: ["read_sessions"], capability_epoch: 4, revoked_at: null }] })
  await assert.rejects(changeTerminalPermission(api, id, true, fetcher), /stale_capability_epoch/)
})

test("a successful patch without a changed account read is not reported as enabled", async () => {
  const fetcher = async (_url: string, init?: RequestInit): Promise<Response> => init?.method === "PATCH"
    ? reply(200, { status: "rotated" })
    : reply(200, { devices: [{ id, caps: ["read_sessions"], capability_epoch: 4, revoked_at: null }] })
  await assert.rejects(changeTerminalPermission(api, id, true, fetcher), /permission_unconfirmed/)
})

test("a lost PATCH response is resolved from the account's actual changed state", async () => {
  let calls = 0
  const fetcher = async (_url: string, init?: RequestInit): Promise<Response> => {
    if (init?.method === "PATCH") throw new TypeError("network response lost")
    calls++
    return reply(200, { devices: [{ id, caps: calls === 1 ? ["read_sessions"] : ["read_sessions", "terminal_control"],
      capability_epoch: calls === 1 ? 4 : 5, revoked_at: null }] })
  }
  assert.deepEqual((await changeTerminalPermission(api, id, true, fetcher)).caps, ["read_sessions", "terminal_control"])
  assert.equal(calls, 2)
})

test("an unreadable account after PATCH is explicitly unknown", async () => {
  let calls = 0
  const fetcher = async (_url: string, init?: RequestInit): Promise<Response> => {
    if (init?.method === "PATCH") throw new TypeError("network response lost")
    calls++
    return calls === 1
      ? reply(200, { devices: [{ id, caps: ["read_sessions"], capability_epoch: 4, revoked_at: null }] })
      : reply(503, { error: { code: "offline" } })
  }
  await assert.rejects(changeTerminalPermission(api, id, true, fetcher), /permission_unknown/)
})
