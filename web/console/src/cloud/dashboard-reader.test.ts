import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner needs source extensions.
import { dashboardFetch } from "./dashboard-reader.ts"
// @ts-expect-error -- Node's strip-types runner needs source extensions.
import { RelayReader, type CloudReadClient } from "./relay-reader.ts"

test("inspecting another machine reads its usage without repointing the active reader", async () => {
  const asks: { machine: string; word: string; kind: string }[] = []
  const untouched = {
    events: () => () => undefined,
    sessions: async () => { throw new Error("usage inspection must not read the active list") },
    transcript: async () => { throw new Error("usage inspection must not read a transcript") },
  }
  const client: CloudReadClient = { ...untouched, ready: true, _machineRequest: async (machine, word, _body, kind) => {
    asks.push({ machine, word, kind })
    return { cpu_percent: machine === "machine-b" ? 20 : 10 }
  } }
  const active = new RelayReader("machine-a")
  active.attach(client)
  const inspect = dashboardFetch("machine-b", () => client)
  assert.equal((await (await inspect("/v1/machine/usage")).json()).cpu_percent, 20)
  assert.equal(active.machine, "machine-a")
  assert.equal(active.log.length, 0)
  assert.deepEqual(asks, [{ machine: "machine-b", word: "machine-usage", kind: "read" }])
})

test("an open inspection follows replacement Cloud clients while keeping the selected machine", async () => {
  const asks: string[] = []
  const make = (label: string): CloudReadClient => ({ ready: true,
    events: () => () => undefined,
    sessions: async () => { throw new Error("no list read expected") },
    transcript: async () => { throw new Error("no transcript read expected") }, _machineRequest: async (machine) => {
    asks.push(label + ":" + machine); return { cpu_percent: 5 }
  } })
  let connected = make("first")
  const inspect = dashboardFetch("machine-b", () => connected)
  await inspect("/v1/machine/usage")
  connected = make("renewed")
  await inspect("/v1/machine/usage")
  assert.deepEqual(asks, ["first:machine-b", "renewed:machine-b"])
})
