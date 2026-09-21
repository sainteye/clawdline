import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node's strip-types test runner.
import { durablePairViewer, resumePendingPairing, type PairingScope, type PendingPairingStore } from "./pair-pending.ts"
import type { PendingPairing } from "./copied.js"

const scope: PairingScope = { account: "acct", device: "browser" }
const pending = {
  pairingID: "pair-1",
  claimNonce: "nonce",
  expiresAt: 10_000,
  fingerprint: "AAAA-BBBB-CCCC-DDDD",
  fragment: "offer",
  offer: { v: 1 },
  ephemeralPrivateKey: { type: "private", extractable: false } as CryptoKey,
} satisfies PendingPairing
const opened = { machineID: "linux", machineFingerprint: "EEEE-FFFF-GGGG-HHHH" }

function memoryStore(initial: PendingPairing | null = null) {
  let kept = initial
  const calls: string[] = []
  const store: PendingPairingStore = {
    async put(got, value) {
      assert.deepEqual(got, scope)
      calls.push("put")
      kept = value
    },
    async get(got) {
      assert.deepEqual(got, scope)
      calls.push("get")
      return kept
    },
    async remove(got, pairingID) {
      assert.deepEqual(got, scope)
      calls.push("remove:" + pairingID)
      if (kept?.pairingID === pairingID) kept = null
    },
  }
  return { store, calls, kept: () => kept }
}

function session(answers: Array<"pending" | "network" | typeof opened | { code: string }>) {
  let now = 1_000
  return {
    account: scope.account,
    deviceID: scope.device,
    signInURL: () => "",
    now: () => now,
    advance: (ms: number) => {
      now += ms
    },
    async startPairing() {
      return pending
    },
    async acceptPairingInvitation() {
      return pending
    },
    async claimPairing() {
      const answer = answers.shift()
      if (answer === "pending") throw Object.assign(new Error("pending"), { code: "pairing_unfinished" })
      if (!answer || answer === "network") throw new Error("network")
      if ("code" in answer) throw Object.assign(new Error(answer.code), answer)
      return answer
    },
  }
}

test("the private claim key is durable before the offer is shown", async () => {
  const memory = memoryStore()
  const order: string[] = []
  const originalPut = memory.store.put
  memory.store.put = async (...args) => {
    order.push("stored")
    await originalPut(...args)
  }
  const run = durablePairViewer(session([opened]), memory.store)
  const answer = await run({
    onOffer() {
      order.push("shown")
    },
    async sleep() {},
  })
  assert.deepEqual(answer, opened)
  assert.deepEqual(order, ["stored", "shown"])
  assert.deepEqual(memory.calls, ["put", "remove:pair-1"])
})

test("a stopped card leaves enough state for the next page to claim", async () => {
  const memory = memoryStore(pending)
  const next = session(["pending", opened])
  const answer = await resumePendingPairing(next, memory.store, {
    async sleep(ms: number) {
      assert.equal(ms, 2_000)
      next.advance(ms)
    },
  })
  assert.deepEqual(answer, opened)
  assert.equal(memory.kept(), null)
  assert.deepEqual(memory.calls, ["get", "remove:pair-1"])
})

test("an untyped interruption keeps a live claim for a later page", async () => {
  const memory = memoryStore(pending)
  await assert.rejects(resumePendingPairing(session(["network"]), memory.store), /network/)
  assert.equal(memory.kept(), pending)
  assert.deepEqual(memory.calls, ["get"])
})

test("a typed terminal answer removes a claim that cannot succeed", async () => {
  const memory = memoryStore(pending)
  await assert.rejects(resumePendingPairing(session([{ code: "pairing_gone" }]), memory.store), (error: unknown) => {
    return !!error && typeof error === "object" && (error as { code?: string }).code === "pairing_gone"
  })
  assert.equal(memory.kept(), null)
  assert.deepEqual(memory.calls, ["get", "remove:pair-1"])
})
