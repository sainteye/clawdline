import assert from "node:assert/strict"
import { test } from "node:test"
// @ts-expect-error -- Node's strip-types runner loads this source directly.
import { nativePairing, nativePairingAvailable } from "./native-pairing.ts"

test("a plain browser has no access to a machine's native pairing credential", async () => {
  assert.equal(nativePairingAvailable({}), false)
  await assert.rejects(nativePairing({ kind: "invitation" }, {}), { code: "unavailable" })
})

test("a native invitation names the local machine and keeps the shell's refusal", async () => {
  const calls: unknown[] = []
  const host = { webkit: { messageHandlers: { clawdlinePairLocal: {
    postMessage: async (body: unknown) => { calls.push(body); return { ok: false, error: "cancelled" } },
  } } } }
  assert.equal(nativePairingAvailable(host), true)
  await assert.rejects(nativePairing({ kind: "invitation", machine_id: "mac_pair_fixture" }, host), { code: "cancelled" })
  assert.deepEqual(calls, [{ kind: "invitation", machine_id: "mac_pair_fixture" }])
})
