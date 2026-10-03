import assert from "node:assert/strict"
import test from "node:test"
import type { Terminal } from "@clawdline/contract"
// @ts-expect-error -- Node strips TypeScript for this focused test.
import { collectCloudTerminals } from "./cloud-terminal-all.ts"

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
