import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node's strip-types test runner.
import { machinesByCapability, needsMachinePairing, reportsMachinePresence, watchStoredMachinePairings } from "./machine-access.ts"

test("unknown pairing never tells a returning person to pair again", () => {
  assert.equal(needsMachinePairing("unknown"), false)
  assert.equal(needsMachinePairing("paired"), false)
  assert.equal(needsMachinePairing("not_paired"), true)
})

const drain = () => new Promise<void>(resolve => setImmediate(resolve))

test("stored pairings restore independently and a foreground burst shares each pending read", async () => {
  const document = Object.assign(new EventTarget(), { hidden: false })
  const window = new EventTarget()
  const asked: string[] = []
  let release!: () => void
  let changed = 0
  const stop = watchStoredMachinePairings({ restoreMachinePairing(machine) {
    asked.push(machine)
    return machine === "slow" ? new Promise<void>(resolve => { release = resolve }) : Promise.resolve()
  } }, ["slow", "fast"], () => changed++, { document, window })
  await drain()
  assert.deepEqual(asked, ["slow", "fast"])
  assert.equal(changed, 1, "one machine does not wait for another machine's storage")
  for (let n = 0; n < 60; n++) window.dispatchEvent(new Event("pageshow"))
  await drain()
  assert.equal(asked.filter(id => id === "slow").length, 1)
  assert.equal(asked.filter(id => id === "fast").length, 2)
  stop()
  release()
  await drain()
  assert.equal(changed, 2, "a retired client cannot update the new account or connection")
  window.dispatchEvent(new Event("online"))
  await drain()
  assert.equal(asked.length, 3)
})

test("a foreground return during a failed key read is replayed once without another app launch", async () => {
  const document = Object.assign(new EventTarget(), { hidden: false })
  const window = new EventTarget()
  let fail!: (error: Error) => void
  let asks = 0, changed = 0
  const stop = watchStoredMachinePairings({ restoreMachinePairing() {
    asks++
    return asks === 1 ? new Promise<void>((_, reject) => { fail = reject }) : Promise.resolve()
  } }, ["mac"], () => changed++, { document, window })
  await drain()
  for (let n = 0; n < 60; n++) window.dispatchEvent(new Event("pageshow"))
  fail(new Error("IndexedDB was not ready"))
  await drain()
  assert.equal(asks, 2)
  assert.equal(changed, 1)
  await drain()
  assert.equal(asks, 2, "no polling or repeating recovery loop")
  document.hidden = true
  window.dispatchEvent(new Event("online"))
  await drain()
  assert.equal(asks, 2)
  document.hidden = false
  document.dispatchEvent(new Event("visibilitychange"))
  await drain()
  assert.equal(asks, 3)
  stop()
})

test("reports from either machine refresh presence while a single-machine reader is open", () => {
  for (const type of ["sessions", "orchestrator", "session_status", "machine_reachability"]) {
    assert.equal(reportsMachinePresence({ type }), true, type)
  }
  assert.equal(reportsMachinePresence({ type: "connection" }), false)
})

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
