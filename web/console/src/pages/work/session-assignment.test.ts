import assert from "node:assert/strict"
import test from "node:test"
import type { SessionRow } from "@clawdline/contract"
import type { SessionWorkV2 } from "./api.js"
// @ts-expect-error -- a `.ts` path is required by Node's native type stripping.
import { assistantName, rememberAssistant, rememberedAssistant, sessionActivityName, sessionWorkCounts, sessionWorkStateName } from "./session-assignment.ts"

test("assignment choices distinguish live activity from unreadable state", () => {
  assert.equal(sessionActivityName("working"), "Working")
  assert.equal(sessionActivityName("idle"), "Idle")
  assert.equal(sessionActivityName("unknown"), "狀況不明")
})

test("assignment choices explain what an otherwise idle Session needs", () => {
  assert.equal(sessionWorkStateName("ready" as SessionRow["work_state"]), "可接工作")
  assert.equal(sessionWorkStateName("waiting_you" as SessionRow["work_state"]), "等你回應")
  assert.equal(sessionWorkStateName("milestone_complete" as SessionRow["work_state"]), "待驗收")
})

test("unfinished counts include open Board items, their pending steps, and direct to-dos", () => {
  const page = {
    assigned_items: [{ id: "board-a", steps: [{ done: false }, { done: true }] }, { id: "board-b" }],
    direct_todos: [{ id: "todo-a" }],
    recent_items: [{ id: "done-a" }, { id: "done-b" }],
  } as SessionWorkV2
  assert.deepEqual(sessionWorkCounts(page), { board: 2, todos: 2, unfinished: 4 })
})

test("assignment choices name which company's assistant each Session runs", () => {
  assert.equal(assistantName("claude"), "Claude Code")
  assert.equal(assistantName("codex"), "Codex")
  assert.equal(assistantName(undefined), "助理不明")
})

test("a new Session keeps the assistant last chosen, and Codex before any choice", () => {
  const kept = new Map<string, string>()
  const storage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v) }
  assert.equal(rememberedAssistant(storage), "codex")
  rememberAssistant("claude", storage)
  assert.equal(rememberedAssistant(storage), "claude")
  kept.set("clawdline.work.new-session-assistant", "gemini")
  assert.equal(rememberedAssistant(storage), "codex")
  const blocked = { getItem: () => { throw new Error("blocked") }, setItem: () => { throw new Error("blocked") } }
  assert.equal(rememberedAssistant(blocked), "codex")
  assert.doesNotThrow(() => rememberAssistant("claude", blocked))
})
