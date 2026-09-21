import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node's strip-types test runner.
import { machinesByCapability } from "./machine-access.ts"

function row(id: string, pairing: "paired" | "not_paired" | "unknown") {
  return {
    id,
    label: id,
    observedAt: Date.UTC(2026, 8, 21),
    freshness: "current" as const,
    pairing,
    sessions: 10,
    selectable: pairing !== "not_paired",
    autoSelectable: true,
  }
}

test("a decrypted authenticated envelope outranks an older not-paired answer", async () => {
  const stale = row("linux", "not_paired")
  const forgotten: string[] = []
  const client = {
    viewerVerified: new Map([["linux", { sender: "machine-key", at_ms: 1 }]]),
    forgetMachinePairingAnswer(machine: string) {
      forgotten.push(machine)
    },
    async machines() {
      return { machines: [stale], syncing: false, retryAfterMs: 0 }
    },
  }

  const answer = await machinesByCapability(client)
  assert.equal(answer.machines[0].pairing, "paired")
  assert.equal(answer.machines[0].selectable, true)
  assert.deepEqual(forgotten, ["linux"])
  assert.equal(stale.pairing, "not_paired", "the copied client's frozen row is not mutated")
})

test("an account row without decryption proof stays unpaired", async () => {
  const unpaired = row("linux", "not_paired")
  const client = {
    viewerVerified: new Map(),
    async machines() {
      return { machines: [unpaired], syncing: true, retryAfterMs: 800 }
    },
  }

  const answer = await machinesByCapability(client)
  assert.equal(answer.machines[0], unpaired)
  assert.equal(answer.machines[0].selectable, false)
  assert.equal(answer.syncing, true)
  assert.equal(answer.retryAfterMs, 800)
})

test("a newly claimed handover bootstraps a machine before its first envelope opens", async () => {
  const unpaired = row("linux", "not_paired")
  const forgotten: string[] = []
  const client = {
    viewerVerified: new Map(),
    forgetMachinePairingAnswer(machine: string) {
      forgotten.push(machine)
    },
    async machines() {
      return { machines: [unpaired], syncing: false, retryAfterMs: 0 }
    },
  }

  const answer = await machinesByCapability(client, new Set(["linux"]))
  assert.equal(answer.machines[0].pairing, "paired")
  assert.equal(answer.machines[0].selectable, true)
  assert.deepEqual(forgotten, ["linux"])
})
