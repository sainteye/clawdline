import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionRow, SessionShell, ShellOutputReply } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { backgroundCounts, backgroundLine, backgroundLive, openShell, showsBackground, stepShell, sticksToBottom } from "./background.ts"

const words = {
  shellOne: "1 background shell",
  shellMany: "{n} background shells",
  agentRunningOne: "1 subagent running",
  agentRunningMany: "{n} subagents running",
  agentListedOne: "1 subagent",
  agentListedMany: "{n} subagents",
  agentUnknown: "subagents ?",
}

const shell: SessionShell = { id: "b0aau3e6s", at: 1_800_000_000, command: "npm run build", what: "Build the console", doing: "vite v7" }

function row(extra: Partial<SessionRow>): SessionRow {
  return { id: "%fixture", sessionId: "conversation-fixture", ...extra } as SessionRow
}

test("the strip is absent when the session has nothing in the background", () => {
  const quiet = backgroundCounts(row({ agents_reading: { state: "complete" } } as Partial<SessionRow>))
  assert.equal(showsBackground(quiet), false)
  // An unknown reading with nothing listed is every session's first row: not
  // a reason for a strip under every conversation.
  const unknown = backgroundCounts(row({}))
  assert.equal(showsBackground(unknown), false)
})

test("a running shell alone shows the strip and lights it", () => {
  const counts = backgroundCounts(row({ shells: [shell, { ...shell, id: "b1oas8ao7" }], agents_reading: { state: "complete" } } as Partial<SessionRow>))
  assert.equal(showsBackground(counts), true)
  assert.equal(backgroundLive(counts), true)
  assert.equal(backgroundLine(counts, words), "2 background shells")
})

test("listed subagents show the strip, running ones counted, finished ones listed", () => {
  const agents = [
    { id: "a1", at: 0, depth: 1, type: "Explore", what: "inspect", state: "running" },
    { id: "a2", at: 0, depth: 1, type: "Explore", what: "verify", state: "done" },
  ]
  const running = backgroundCounts(row({ shells: [shell], agents, agents_reading: { state: "complete" } } as Partial<SessionRow>))
  assert.equal(backgroundLine(running, words), "1 background shell · 1 subagent running")
  const finished = backgroundCounts(row({ agents: [agents[1]], agents_reading: { state: "complete" } } as Partial<SessionRow>))
  assert.equal(showsBackground(finished), true)
  assert.equal(backgroundLive(finished), false)
  assert.equal(backgroundLine(finished, words), "1 subagent")
})

test("an incomplete agent reading is said as ?, never as zero", () => {
  const agents = [{ id: "a1", at: 0, depth: 1, type: "Explore", what: "inspect", state: "running" }]
  const counts = backgroundCounts(row({ agents, agents_reading: { state: "unknown", reason: "unreadable" } } as Partial<SessionRow>))
  assert.equal(counts.running, null)
  assert.equal(backgroundLine(counts, words), "subagents ?")
  // Truncated rows are something to open too.
  const cut = backgroundCounts(row({ agents_reading: { state: "complete", truncated: 3 } } as Partial<SessionRow>))
  assert.equal(showsBackground(cut), true)
  assert.equal(backgroundLine(cut, words), "3 subagents")
})

function reply(text: string, signature: string, ended = false, meta: Partial<SessionShell> = {}): ShellOutputReply {
  return { shell: { id: shell.id, at: shell.at, ...meta }, text, signature, ended }
}

test("the panel repaints only when the signature moves, and stops when the command ends", () => {
  let view = openShell(shell)
  let step = stepShell(view, { kind: "answer", reply: reply("one\n", "s1", false, { command: "npm run build" }) })
  assert.equal(step.repaint, true)
  assert.equal(step.poll, true)
  view = step.view
  step = stepShell(view, { kind: "answer", reply: reply("one\n", "s1") })
  assert.equal(step.repaint, false, "the same bytes are not drawn again")
  assert.equal(step.poll, true)
  step = stepShell(step.view, { kind: "answer", reply: reply("one\ntwo\n[exited with code 0]\n", "s2", true) })
  assert.equal(step.repaint, true)
  assert.equal(step.poll, false, "an ended command is not asked again")
  assert.equal(step.view.text, "one\ntwo\n[exited with code 0]\n")
  // The command line captured at open survives an answer that no longer
  // carries it.
  assert.equal(step.view.meta.command, "npm run build")
  assert.equal(step.view.meta.what, "Build the console")
})

test("a 404 after the panel was open is the command finishing, and the last text stays", () => {
  const view = stepShell(openShell(shell), { kind: "answer", reply: reply("building…\n", "s1") }).view
  const step = stepShell(view, { kind: "missing" })
  assert.equal(step.view.ended, true)
  assert.equal(step.view.error, "")
  assert.equal(step.view.text, "building…\n")
  assert.equal(step.poll, false)
})

test("a failed read is said, keeps the text, and stops asking", () => {
  const view = stepShell(openShell(shell), { kind: "answer", reply: reply("building…\n", "s1") }).view
  const step = stepShell(view, { kind: "failed", sentence: "offline" })
  assert.equal(step.view.error, "offline")
  assert.equal(step.view.text, "building…\n")
  assert.equal(step.view.ended, false)
  assert.equal(step.poll, false)
})

test("the reader stays at the bottom unless they scrolled up", () => {
  assert.equal(sticksToBottom(true, 0, 400, 4000), true, "the first draw goes to the end")
  assert.equal(sticksToBottom(false, 3600, 400, 4000), true)
  assert.equal(sticksToBottom(false, 3590, 400, 4000), true, "within a line of the end is at the end")
  assert.equal(sticksToBottom(false, 1200, 400, 4000), false)
})
