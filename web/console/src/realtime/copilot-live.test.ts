import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner loads this source in its test suite.
import { LIVE_NOTE_CHARS, LiveTranscript, liveBackendUSD, liveCost, liveReadNotes, clipRecap, liveUpdatesText, liveBackendUpdate, liveFunctionCall, liveInstructions, liveNote, liveToolReply } from "./copilot-live.ts"
// @ts-expect-error -- Node's strip-types runner loads this source in its test suite.
import { formatDuration } from "./copilot-cost.ts"
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
    type: string; session: { delegation: { type: string; responses: { model: string; instructions: string; tools: unknown[]; tool_choice: string; parallel_tool_calls: boolean } } }
  }
  const backend = event.session.delegation.responses
  assert.equal(event.type, "session.update")
  // OpenAI refused the update without these on the first real call (2026-11-15).
  assert.equal(event.session.delegation.type, "responses")
  assert.equal(backend.model, "gpt-6-luna")
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

test("a GPT-Live call's cost counts its own clock between OpenAI's usage updates, never less than billed", () => {
  // 42 s on the call's clock, OpenAI has billed 30: the 42 is shown.
  const running = liveCost(42, 30, 0.002)
  assert.equal(running.seconds, 42)
  assert.ok(Math.abs(running.usd - (42 * 0.05 / 60 + 0.002)) < 1e-12)
  // OpenAI's count ahead of the clock (a slow tab) wins.
  assert.equal(liveCost(10, 15, 0).seconds, 15)
})

test("a call's length reads as minutes and seconds", () => {
  assert.equal(formatDuration(0), "0:00")
  assert.equal(formatDuration(42.9), "0:42")
  assert.equal(formatDuration(185), "3:05")
  assert.equal(formatDuration(3730), "1:02:10")
})

test("each Session a read returns becomes one note the live model says at once", () => {
  const group = JSON.stringify({ sessions: [
    { label: "Bots tower", recap: "Mockups are done; wiring floors.", recap_written: "6 分鐘前" },
    { label: "Keyboard fix", delivered_summary: "Fixed and deployed." },
    { label: "Unknown", error: "unreadable" },
    { label: "Empty" },
  ] })
  const notes = liveReadNotes("get_sessions_summary", group)
  assert.equal(notes.length, 2, "only Sessions with something to say")
  assert.match(notes[0], /Session "Bots tower" \(written 6 分鐘前\): Mockups are done/)
  assert.match(notes[1], /Session "Keyboard fix": Fixed and deployed\./)
  // A recap runs from how the work began to where it stands now; a long one keeps its first sentence and its end.
  const long = liveReadNotes("get_session_summary", JSON.stringify({ label: "Long", recap: "It prepares screenshots. " + "x".repeat(LIVE_NOTE_CHARS + 50) + " Now all six wait for approval." }))
  assert.match(long[0], /It prepares screenshots\.…x+ Now all six wait for approval\.$/)
  assert.ok(long[0].length < LIVE_NOTE_CHARS + 300)
  assert.equal(clipRecap("short"), "short")
  assert.equal(clipRecap("y".repeat(LIVE_NOTE_CHARS + 10)).length, LIVE_NOTE_CHARS, "without an early sentence end, only the end is kept")
  assert.equal(liveUpdatesText(JSON.stringify({ updates: [{ role: "tool", text: "ls" }, { role: "assistant", text: "First." }, { role: "user", text: "go" }, { role: "assistant", text: "Second." }, { role: "assistant", text: "Done and deployed." }] })), "Second. Done and deployed.", "only the Session's own last two reports")
  assert.equal(liveUpdatesText("not json"), "")
  assert.deepEqual(liveReadNotes("list_sessions", group), [], "a listing is not spoken this way")
  assert.deepEqual(liveReadNotes("get_session_summary", "not json"), [])
})

test("the backend answers an overview from the snapshot's recaps and does not repeat what was already spoken", () => {
  const event = liveBackendUpdate("", "【通話開始時的 Session 快照】 {}", {}) as { session: { delegation: { responses: { instructions: string } } } }
  const text = event.session.delegation.responses.instructions
  assert.match(text, /直接用快照的 recap 回答/)
  assert.match(text, /不要把查過的 Session 再逐一重講/)
  // The Live flow comes after the Realtime rules it overrides.
  assert.ok(text.indexOf("GPT-Live 的做法") > text.indexOf("【工具】"))
})
