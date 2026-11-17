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
test("pins are one distinct first section, including a real Project named pinned", () => {
  const items=[bot("a","m1","pinned"),bot("b","m1","beta"),bot("a","m2","beta")]
  const pinned=JSON.stringify(["m1","b"])
  const groups=groupByProject(items as never,[pinned])
  assert.deepEqual(groups.map(g=>[g.kind,g.machineID,g.projectID,g.items.map(i=>i.bot.key.bot_id)]),[
    ["pinned","","",["b"]],["project","m1","pinned",["a"]],["project","m2","beta",["a"]]
  ])
  assert.notEqual(groups[0].key,groups[1].key)
})
test("custom Project/Bot order keeps later arrivals and ignores missing or foreign identities",()=>{
  const items=[bot("a1","m","alpha"),bot("b1","m","beta"),bot("a2","m","alpha"),bot("c","m","gamma")]
  const groups=groupByProject(items as never,[],[JSON.stringify(["m","beta"]),"missing"],[JSON.stringify(["m","a2"]),JSON.stringify(["m","a1"])])
  assert.deepEqual(groups.map(g=>[g.projectID,g.items.map(i=>i.bot.key.bot_id)]),[["beta",["b1"]],["alpha",["a2","a1"]],["gamma",["c"]]])
  assert.deepEqual(groupByProject(items as never).map(g=>g.projectID),["alpha","beta","gamma"])
})
