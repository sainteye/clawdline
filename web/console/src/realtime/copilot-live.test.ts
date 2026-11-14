import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner loads this source in its test suite.
import { LiveTranscript, liveBackendUSD, liveBackendUpdate, liveFunctionCall, liveInstructions, liveNote, liveToolReply } from "./copilot-live.ts"
// @ts-expect-error -- Node's strip-types runner loads this source in its test suite.
import { COPILOT_TOOLS } from "./copilot-tools.ts"

test("the live model gets a short persona that hands every Session question to the backend", () => {
  const text = liveInstructions({ extra: "叫我老闆", brief: true })
  assert.match(text, /<\|SpawnThinking\|>/)
  assert.match(text, /Backchannel policy:/)
  assert.match(text, /Interruption policy:/)
  assert.match(text, /叫我老闆/)
  assert.match(text, /全程台灣繁體中文/)
  // The persona stays far under GPT-Live's 16,384-token instructions and the machine's byte bound.
  assert.ok(new TextEncoder().encode(text).length < 4096, `${new TextEncoder().encode(text).length} bytes`)
  assert.match(liveInstructions({ language: "en" }), /全程用英文（en）說話/)
})

test("the backend receives the call's tool rules, snapshot and every tool, one call at a time", () => {
  const event = liveBackendUpdate("earlier turns", "【通話開始時的 Session 快照】 two Sessions", { origin: { id: "%2", label: "Fix the build" } }) as {
    type: string; session: { delegation: { responses: { instructions: string; tools: unknown[]; tool_choice: string; parallel_tool_calls: boolean } } }
  }
  const backend = event.session.delegation.responses
  assert.equal(event.type, "session.update")
  assert.equal(backend.tools.length, COPILOT_TOOLS.length)
  assert.equal(backend.parallel_tool_calls, false)
  assert.match(backend.instructions, /另一個語音模型會把你寫的結果用口語轉述給他/)
  assert.match(backend.instructions, /smart_rename_sessions/)
  assert.match(backend.instructions, /two Sessions/)
  assert.match(backend.instructions, /earlier turns/)
  assert.match(backend.instructions, /Fix the build/)
})

test("a finished backend function call is read from its envelope, and nothing else is", () => {
  const call = liveFunctionCall({ type: "response.event", delegation_id: "item_1", event: { type: "response.output_item.done", response_id: "resp_1", item: { type: "function_call", call_id: "call_1", name: "list_sessions", arguments: "{}" } } })
  assert.deepEqual(call, { delegation: "item_1", response: "resp_1", callID: "call_1", name: "list_sessions", arguments: "{}" })
  assert.equal(liveFunctionCall({ type: "response.event", event: { type: "response.output_item.done", item: { type: "message" } } }), null)
  assert.equal(liveFunctionCall({ type: "response.event", event: { type: "response.function_call_arguments.done", item: { type: "function_call", call_id: "c", name: "n" } } }), null)
  assert.equal(liveFunctionCall({ type: "session.output_transcript.delta", delta: "hi" }), null)
})

test("the backend continues only after its last pending result", () => {
  const more = liveToolReply("call_1", '{"ok":true}', false)
  assert.equal(more.length, 1)
  assert.deepEqual(more[0], { type: "response.item.create", event_id: "tool_call_1", item: { type: "function_call_output", call_id: "call_1", output: '{"ok":true}' } })
  const last = liveToolReply("call_2", "{}", true)
  assert.deepEqual(last.map((e) => e.type), ["response.item.create", "response.create"])
})

test("the backend's tokens are priced from its completed response", () => {
  const usd = liveBackendUSD({ type: "response.event", event: { type: "response.completed", response: { usage: { input_tokens: 20000, output_tokens: 200, input_tokens_details: { cached_tokens: 10000 } } } } })
  // 10,000 fresh at $0.10, 10,000 cached at $0.01, 200 out at $0.50 per 1M.
  assert.ok(Math.abs(usd - (10000 * 0.1 + 10000 * 0.01 + 200 * 0.5) / 1e6) < 1e-12, String(usd))
  assert.equal(liveBackendUSD({ type: "response.event", event: { type: "response.created" } }), 0)
})

test("notes to the live model are session-wide and fragments are joined exactly", () => {
  const note = liveNote("thinking", "focus moved") as { type: string; delegation_id: null; content: string }
  assert.equal(note.type, "session.thinking.append")
  assert.equal(note.delegation_id, null)
  const t = new LiveTranscript()
  t.add("What is"); t.add(" going on"); t.add(7)
  assert.equal(t.peek(), "What is going on")
  assert.equal(t.take(), "What is going on")
  assert.equal(t.take(), "")
})
