import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { groupByProject } from "./roster-groups.ts"
const bot = (id: string, machine: string, project: string) => ({ bot: { key: { machine_id: machine, bot_id: id }, project: { project_id: project } } })
test("Bots gather under their Project, newest Project first, and keep their order inside it", () => {
  // The roster arrives newest first: a1 (alpha), b1 (beta), a2 (alpha).
  const groups = groupByProject([bot("a1", "m", "alpha"), bot("b1", "m", "beta"), bot("a2", "m", "alpha")] as never)
  assert.deepEqual(groups.map(g => [g.projectID, g.items.map((i: { bot: { key: { bot_id: string } } }) => i.bot.key.bot_id)]), [["alpha", ["a1", "a2"]], ["beta", ["b1"]]])
})
test("the same Project ID on two machines is two Projects", () => {
  const groups = groupByProject([bot("x", "m1", "alpha"), bot("y", "m2", "alpha")] as never)
  assert.equal(groups.length, 2)
})
