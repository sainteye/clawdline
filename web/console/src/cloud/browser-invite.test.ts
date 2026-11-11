import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
// @ts-expect-error Node executes TypeScript directly.
import { CPaceParty } from "./cpace.ts"
// @ts-expect-error Node executes TypeScript directly.
import { BROWSER_AD, MACHINE_AD, confirmBody, cpaceInputs, deriveInviteKeys, joinBrowserInvite, openInvite, parseInviteCode, readHandover, sealInvite, InviteError } from "./browser-invite.ts"

// Recorded from Go (internal/domain/cloud/browser_invite_test.go). Agreeing with it byte for
// byte is what lets a browser and a machine that never met finish the same exchange.
const v = JSON.parse(readFileSync(new URL("../../../../internal/domain/cloud/testdata/browser-invite-v1.json", import.meta.url), "utf8"))
const unhex = (s: string) => Uint8Array.from(Buffer.from(s, "hex"))
const hex = (b: Uint8Array) => Buffer.from(b).toString("hex")

test("both CPace messages and the ISK are the ones Go recorded", () => {
  const inputs = cpaceInputs(v.invite_id, v.secret)
  const browser = new CPaceParty(unhex(v.browser_scalar), inputs.prs, inputs.ci, inputs.sid, BROWSER_AD, true)
  const machine = new CPaceParty(unhex(v.machine_scalar), inputs.prs, inputs.ci, inputs.sid, MACHINE_AD, false)
  assert.equal(hex(browser.message), v.browser_message)
  assert.equal(hex(machine.message), v.machine_message)
  assert.equal(hex(browser.finish(machine.message, MACHINE_AD)), v.isk)
})

test("keys and comparison digits derive as in Go", async () => {
  const keys = await deriveInviteKeys(unhex(v.isk), v.invite_id, true)
  assert.equal(keys.code, v.code)
  assert.equal(hex(keys.raw!.confirm), v.confirm_key)
  assert.equal(hex(keys.raw!.handover), v.handover_key)
})

test("the confirmation seals to Go's bytes and Go's handover opens here", async () => {
  const keys = await deriveInviteKeys(unhex(v.isk), v.invite_id)
  const c = v.confirm
  const sealed = await sealInvite(keys.confirm, v.invite_id, confirmBody({ inviteID: c.invite_id, machineID: c.machine_id,
    deviceID: c.device_id, publicKey: c.public_key, name: c.name }), unhex(v.confirm_nonce))
  assert.equal(sealed, v.sealed_confirm)
  const opened = readHandover(await openInvite(keys.handover, v.invite_id, v.sealed_handover), v.invite_id,
    v.handover.device_id, v.handover.machine_id)
  assert.equal(opened.masterSecret, v.handover.master_secret)
  assert.equal(opened.expiresAt, v.handover.expires_at)
  await assert.rejects(openInvite(keys.handover, "K7M4R", v.sealed_handover), (e: unknown) => e instanceof InviteError && e.code === "code_mismatch")
  assert.throws(() => readHandover({ ...v.handover }, v.invite_id, "dev_other", v.handover.machine_id), InviteError)
})

test("codes parse as people type them", () => {
  assert.deepEqual(parseInviteCode(" k7m4q-9x2ftb "), { id: "K7M4Q", secret: "9X2FTB" })
  assert.deepEqual(parseInviteCode("o1l2i000000"), { id: "01121", secret: "000000" })
  assert.equal(parseInviteCode("K7M4Q-9X2FT"), null)
  assert.equal(parseInviteCode("K7M4U-9X2FTB"), null)
})

/** A Cloud and a machine in one: answers the browser's calls the way the two would. */
function fakeCloud(secret: string, opts: { deny?: boolean } = {}) {
  const state: { machine?: CPaceParty; keys?: Awaited<ReturnType<typeof deriveInviteKeys>>; confirm?: string; handover?: string
    deviceID: string; status: string; machineMessage?: string } = { deviceID: "web_invited", status: "waiting" }
  const reply = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })
  const fetcher = async (url: string, init?: RequestInit) => {
    const path = new URL(url).pathname
    const body = init?.body ? JSON.parse(String(init.body)) : {}
    if (path.endsWith("/join")) {
      const inputs = cpaceInputs("K7M4Q", secret)
      state.machine = CPaceParty.create(inputs.prs, inputs.ci, inputs.sid, MACHINE_AD, false)
      const isk = state.machine.finish(Uint8Array.from(Buffer.from(body.browser_message, "base64")), BROWSER_AD)
      state.keys = await deriveInviteKeys(isk, "K7M4Q")
      state.machineMessage = Buffer.from(state.machine.message).toString("base64")
      state.status = "joined"
      return reply(200, { join_token: "t".repeat(43), device_id: state.deviceID, machine_id: "mac_x", machine_name: "Studio", expiry: "8h" })
    }
    if (path.endsWith("/browser")) {
      return reply(200, { status: state.status, machine_message: state.machineMessage, ...(state.handover ? { handover: state.handover } : {}) })
    }
    if (path.endsWith("/confirm")) {
      try {
        await openInvite(state.keys!.confirm, "K7M4Q", body.browser_confirm)
      } catch {
        state.status = "denied" // the machine cannot open it: wrong secret
        return reply(200, {})
      }
      if (opts.deny) { state.status = "denied"; return reply(200, {}) }
      state.handover = await sealInvite(state.keys!.handover, "K7M4Q", { v: 1, type: "clawdline_browser_invite_handover",
        invite_id: "K7M4Q", account_id: "usr_1", machine_id: "mac_x", machine_signing_key: "AAAA", machine_fingerprint: "F",
        key_id: "ms-1", master_secret: "BBBB", device_id: state.deviceID, scope: "operate", expires_at: 0 })
      state.status = "admitted"
      return reply(200, {})
    }
    if (path.endsWith("/claim")) return reply(200, { account_id: "usr_1", device_id: state.deviceID, machine_id: "mac_x", expires_at: null })
    return reply(404, { error: { code: "not_found" } })
  }
  return { fetcher, state }
}

test("a browser walks a whole invitation against a machine that knows the secret", async () => {
  const cloud = fakeCloud("9X2FTB")
  const stages: string[] = []
  const result = await joinBrowserInvite("https://api.example", "K7M4Q-9X2FTB", "Linux Chrome",
    (s: { stage: string }) => stages.push(s.stage), { fetch: cloud.fetcher })
  assert.deepEqual(stages, ["joining", "exchanging", "compare", "claiming"])
  assert.equal(result.handover.masterSecret, "BBBB")
  assert.equal(result.machineID, "mac_x")
})

test("a mistyped secret ends denied and is handed nothing", async () => {
  const cloud = fakeCloud("9X2FTC")
  await assert.rejects(joinBrowserInvite("https://api.example", "K7M4Q-9X2FTB", "x", () => {}, { fetch: cloud.fetcher }),
    (e: unknown) => e instanceof InviteError && e.code === "denied")
  assert.equal(cloud.state.handover, undefined)
})
