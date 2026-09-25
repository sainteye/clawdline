// The seam the Projects page reads a source machine through:
// `node --test --experimental-strip-types src/cloud/project-sync.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { syncSeamFor } from "./project-sync.ts"

test("the seam reads the named machine, not the one on screen, through the client of the moment", async () => {
  const asked: unknown[] = []
  const client = {
    machines: async () => ({ machines: [
      { id: "mac-a", label: "Studio · A", selectable: true },
      { id: "linux-b", name: "B", label: "Linux · B", selectable: true },
    ] }),
    machineDescriptor: (id: string) => (id === "mac-a" ? { machine: { commands: ["project-manifest"] } } : null),
    _machineRequest: async (machine: string, word: string, body: Record<string, unknown>, kind: string) => {
      asked.push([machine, word, body, kind])
      return { projects: [] }
    },
  }
  let current: typeof client | null = null
  const seam = syncSeamFor(() => current, () => "linux-b")
  await assert.rejects(seam.machines(), /not ready/)
  current = client
  assert.deepEqual(await seam.machines(), [
    { id: "mac-a", name: "Studio · A", offers: true, selectable: true },
    { id: "linux-b", name: "B", offers: null, selectable: true },
  ])
  assert.deepEqual(await seam.read("mac-a", "project-entry", { repo: "github.com/o/n" }), { projects: [] })
  assert.deepEqual(asked, [["mac-a", "project-entry", { repo: "github.com/o/n" }, "read"]])
  assert.equal(seam.here(), "linux-b")
})
