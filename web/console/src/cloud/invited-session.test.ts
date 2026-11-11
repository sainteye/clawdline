import { test } from "node:test"
import assert from "node:assert/strict"
import { CloudViewerSession } from "../legacy/js/net/cloud-boot.js"

// A browser a machine admitted by invitation (docs/browser-invite.md) boots as invited: it
// never registers an account device and never asks for GitHub sign-in.
const CONFIG = { appOrigin: "https://app.example.test", apiOrigin: "https://api.example.test", relayURL: "wss://relay.example.test", build: "test", strings: {} }

function storage(initial: Record<string, string> = {}) {
  const values = new Map(Object.entries(initial))
  return { getItem: (k: string) => values.get(k) ?? null, setItem: (k: string, v: string) => { values.set(k, v) }, removeItem: (k: string) => { values.delete(k) } }
}

function viewer(store: ReturnType<typeof storage>, asked: string[]) {
  const v = new CloudViewerSession({ config: CONFIG, storage: store, indexedDB: {},
    fetch: async (url: string, init?: RequestInit) => {
      const path = new URL(url).pathname
      asked.push((init?.method ?? "GET") + " " + path)
      if (path === "/v1/auth/session" && !init?.method) return new Response(JSON.stringify({ error: { code: "no_session" } }), { status: 401 })
      if (path === "/v1/pairing/browser-invites/session") {
        return new Response(JSON.stringify({ account_id: "usr_owner", device_id: "web_invited", machine_id: "mac_one", machine_name: "Studio", expires_at: null }))
      }
      return new Response(JSON.stringify({ error: { code: "no_login_ticket" } }), { status: 401 })
    } })
  v.restoreDeviceKey = async () => true
  v.readCapabilities = async () => ["read_sessions", "send_prompt"]
  return v
}

test("a browser that adopted an invitation boots invited, bound to its machine", async () => {
  const asked: string[] = []
  const v = viewer(storage({ "clawdline.invited-device": "web_invited" }), asked)
  const ready = await v.ensureSession()
  assert.deepEqual(ready, { state: "ready", accountID: "usr_owner", deviceID: "web_invited",
    invited: { machineID: "mac_one", machineName: "Studio", expiresAt: null } })
  assert.deepEqual(asked, ["GET /v1/auth/session", "GET /v1/pairing/browser-invites/session"])
})

test("an account browser that never adopted one makes no invitation request", async () => {
  const asked: string[] = []
  const v = viewer(storage(), asked)
  v.crypto = { subtle: { generateKey: async () => ({ publicKey: {} }), exportKey: async () => new Uint8Array(32).buffer } }
  const answer = await v.ensureSession()
  assert.equal(answer.state, "sign_in")
  assert.ok(!asked.some((a) => a.includes("browser-invites")), asked.join(", "))
})
