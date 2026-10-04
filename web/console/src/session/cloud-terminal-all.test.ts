import assert from "node:assert/strict"
import test from "node:test"
import type { Terminal } from "@clawdline/contract"
// @ts-expect-error -- Node strips TypeScript for this focused test.
import { collectCloudTerminals, readCloudTerminalList } from "./cloud-terminal-all.ts"

test("a relay grant refusal retries one list read after renewal without retrying other errors", async () => {
  let calls = 0
  const pauses: number[] = []
  const answer = await readCloudTerminalList(async () => {
    if (++calls === 1) throw Object.assign(new Error("rate_limited"), { code: "rate_limited" })
    return "rows"
  }, async (ms) => { pauses.push(ms) })
  assert.equal(answer, "rows")
  assert.equal(calls, 2)
  assert.deepEqual(pauses, [2_100])
  await assert.rejects(readCloudTerminalList(async () => {
    calls++
    throw Object.assign(new Error("forbidden"), { code: "forbidden" })
  }, async () => { throw new Error("must not pause") }), /forbidden/)
  assert.equal(calls, 3)
})

const terminal = (id: string, project_id: string): Terminal => ({ id, project_id, created: 1, dir: "/work",
  cols: 80, rows: 24, status: "running", control: { held: false, epoch: 0 } }) as Terminal
const cloudID = (machine: string, local: string) => "cloud." + btoa(JSON.stringify([machine, local])).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")

test("the Cloud terminal list maps each machine's local project without dropping another machine on failure", async () => {
  const machines = [{ id: "mac", name: "Desk" }, { id: "linux", name: "Server" }]
  const places = [{ id: cloudID("mac", "one"), label: "One" }, { id: cloudID("linux", "two"), label: "Two" }]
  const first = await collectCloudTerminals(machines, places, [], async (machine: string) =>
    machine === "mac" ? [terminal("a", "one")] : [terminal("b", "two")])
  assert.deepEqual(first.rows.map((row: { machine: string; project: string; terminal: Terminal }) =>
    [row.machine, row.project, row.terminal.id]), [["mac", places[0]!.id, "a"], ["linux", places[1]!.id, "b"]])
  assert.deepEqual(first.errors, [])

  const second = await collectCloudTerminals(machines, places, first.rows, async (machine: string) => {
    if (machine === "mac") throw Object.assign(new Error("offline"), { code: "machine_offline" })
    return []
  })
  assert.deepEqual(second.rows.map((row: { machine: string; stale: boolean; terminal: Terminal }) =>
    [row.machine, row.stale, row.terminal.id]), [["mac", true, "a"]])
  assert.deepEqual(second.errors, [{ machine: "mac", code: "machine_offline" }])
})

test("unknown projects remain visible but cannot route to another project's terminal", async () => {
  const result = await collectCloudTerminals([{ id: "mac", name: "Desk" }], [], [], async () => [terminal("a", "secret")])
  assert.equal(result.rows[0]?.project, "")
  assert.equal(result.rows[0]?.terminal.id, "a")
})

test("slow and failed machines publish bounded progressive results", async () => {
  const machines = ["a", "b", "c"].map((id) => ({ id, name: id }))
  const waiting = new Map<string, { resolve: (value: Terminal[]) => void; reject: (error: Error) => void }>()
  const snapshots: string[][] = []
  const result = collectCloudTerminals(machines, [], [], (machine) => new Promise<Terminal[]>((resolve, reject) => {
    waiting.set(machine, { resolve, reject })
  }), (progress) => snapshots.push(progress.rows.map((row) => row.machine)))
  await Promise.resolve()
  assert.deepEqual([...waiting.keys()], ["a", "b"])
  waiting.get("b")!.resolve([terminal("two", "p")])
  await new Promise((resolve) => setImmediate(resolve))
  assert.deepEqual(snapshots, [["b"]])
  assert.deepEqual([...waiting.keys()], ["a", "b", "c"])
  waiting.get("c")!.reject(Object.assign(new Error("offline"), { code: "machine_offline" }))
  await new Promise((resolve) => setImmediate(resolve))
  assert.deepEqual(snapshots, [["b"], ["b"]])
  waiting.get("a")!.resolve([terminal("one", "p")])
  const final = await result
  assert.deepEqual(final.rows.map((row) => row.machine), ["a", "b"])
  assert.deepEqual(final.errors, [{ machine: "c", code: "machine_offline" }])
})

test("machine reads begin while Project names are still loading", async () => {
  let releasePlaces!: (places: { id: string; label: string }[]) => void
  const places = new Promise<{ id: string; label: string }[]>((resolve) => { releasePlaces = resolve })
  const started: string[] = []
  const result = collectCloudTerminals([{ id: "mac", name: "Desk" }], places, [], async (machine) => {
    started.push(machine)
    return [terminal("a", "one")]
  })
  await Promise.resolve()
  assert.deepEqual(started, ["mac"])
  releasePlaces([{ id: cloudID("mac", "one"), label: "One" }])
  assert.equal((await result).rows[0]?.projectName, "One")
})
