import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { sourceHealth } from "./source-status.ts"
// @ts-expect-error Node executes source extensions.
import { words } from "./words.ts"
const w = words("zh-Hant") as unknown as Record<string, string>
const source = (over = {}) => ({ machine_id: "m1", state: "online", access: "interactive", readable: true, writable: true, fresh: true, observed_at: "", capabilities: [], ...over })
const names = new Map([["m1", "MacBook Pro"]])
test("a working machine is one green line, and an agent never used says so in plain words",()=>{
  const h = sourceHealth([source()] as never, names, [], w)
  assert.equal(h.ok, true)
  assert.equal(h.summary, "MacBook Pro：已連線")
  assert.ok(h.machines[0].rows.some((r: {state: string; detail: string}) => r.state === "unknown" && r.detail === w.statusNotUsed))
  assert.ok(!JSON.stringify(h).includes("尚未驗證原生能力"))
})
test("a problem names the machine and the fix; there is no view-only phone",()=>{
  const off = sourceHealth([source({ state: "offline" })] as never, names, [], w)
  assert.equal(off.ok, false); assert.equal(off.summary, "MacBook Pro：離線"); assert.equal(off.fix, w.fixOffline)
  const cloud = sourceHealth([source({ writable: false })] as never, names, [], w)
  assert.equal(cloud.summary, "MacBook Pro：關閉了從 Clawdline Cloud 操作")
  assert.ok(!JSON.stringify(cloud).includes("只能查看"))
  const login = sourceHealth([source({ capabilities: [{ agent: "codex", cli_version: "", unavailable: { code: "auth_required" } }] })] as never, names, [], w)
  assert.equal(login.summary, "MacBook Pro：Codex 需要先登入")
})
test("a machine without Bots is listed apart, not as an error, and an unnamed one is this machine",()=>{
  const h = sourceHealth([source()] as never, new Map(), ["m2"], w)
  assert.equal(h.summary, "這台機器：已連線")
  assert.deepEqual(h.withoutBots, [{ id: "m2", name: "這台機器" }])
})
