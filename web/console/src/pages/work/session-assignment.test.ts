import assert from "node:assert/strict"
import test from "node:test"
import type { SessionRow } from "@clawdline/contract"
import type { SessionWorkV2 } from "./api.js"
// @ts-expect-error -- a `.ts` path is required by Node's native type stripping.
import { assignmentCandidates, assistantName, awaitsAcceptance, rememberAssistant, rememberedAssistant, sessionActivityName, sessionWorkCounts, sessionWorkLabel, sessionWorkStateName } from "./session-assignment.ts"

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

test("an owned item can be handed to any other Session in its Project, never back to its owner", () => {
  const row = (id: string, sessionId: string, cwd = "/p") => ({ id, sessionId, cwd }) as SessionRow
  const sessions = [row("%1", "owner"), row("%2", "other"), row("%3", ""), row("%4", "elsewhere", "/q")]
  const item = { project: { path: "/p" }, owner_session: "owner" }
  assert.deepEqual(assignmentCandidates(sessions, item).map((s) => s.id), ["%2"])
  assert.deepEqual(assignmentCandidates(sessions, { ...item, owner_session: null }).map((s) => s.id), ["%1", "%2"])
})

test("a reported turn reads 本輪已回報 until an owned item awaits acceptance, and says when the Board was unread", () => {
  const reported = (acceptance?: SessionRow["acceptance"], work_state = "milestone_complete") => ({
    work_state: work_state as SessionRow["work_state"],
    disposition: { scope: "session", evidence: "authenticated_session_delivery", title: "summary" },
    acceptance,
  })
  assert.equal(sessionWorkLabel(reported({ state: "none" })), "本輪已回報")
  // A daemon that sends no answer has not said there is none.
  assert.equal(sessionWorkLabel(reported(undefined)), "本輪已回報 · 讀不到是否有待驗收項目")
  assert.equal(sessionWorkLabel(reported({ state: "pending", work_id: "w", title: "Ship it", phase: "deploying", count: 1 })), "待驗收 · Ship it")
  assert.equal(sessionWorkLabel(reported({ state: "pending", work_id: "w", title: "Ship it", phase: "done", count: 3 })), "待驗收 · Ship it · 共 3 項")
  assert.equal(sessionWorkLabel(reported({ state: "unknown" })), "本輪已回報 · 讀不到是否有待驗收項目")
  // A pending answer that names nothing is not trusted as one.
  assert.equal(sessionWorkLabel(reported({ state: "pending" })), "本輪已回報 · 讀不到是否有待驗收項目")
  assert.equal(sessionWorkLabel(reported({ state: "none" }, "work_complete")), "工作已完成")
  assert.equal(sessionWorkLabel(reported({ state: "pending", title: "Ship it" }, "work_complete")), "待驗收 · Ship it")
  // A task's delivery and an unfinished Session keep their own words.
  assert.equal(sessionWorkLabel({ work_state: "milestone_complete", disposition: { scope: "task", evidence: "authenticated_task_delivery", title: "t" },
    acceptance: { state: "pending", title: "Ship it" } }), "待驗收")
  assert.equal(sessionWorkLabel({ work_state: "working", acceptance: { state: "pending", title: "Ship it" } }), "執行中")
})

test("only deploying and done items wait for the person to look", () => {
  assert.equal(awaitsAcceptance("deploying"), true)
  assert.equal(awaitsAcceptance("done"), true)
  assert.equal(awaitsAcceptance("verifying"), false)
  assert.equal(awaitsAcceptance("cancelled"), false)
})
