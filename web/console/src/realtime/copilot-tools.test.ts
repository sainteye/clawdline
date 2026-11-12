import { test } from "node:test"
import assert from "node:assert/strict"

// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { ACTIVITY_TOTAL_CHARS, countSummary, durationPhrase, whenPhrase, BRIEF_RULE, LONG_RULE, adjustEvent, nextSpeed, choiceOf, progressOf, COPILOT_INSTRUCTIONS, REPLY_CHARS, COPILOT_TOOLS, readRecap, sessionsSnapshot, SCREEN_READ_ATTEMPTS, activityOf, plainScreenText, runCopilotTool, sessionUpdateEvent, toolReplyEvents, type ToolDeps, greetingEvent, instructionsFor, copilotRefusal, resolveSession, CONTENT_NOT_READ } from "./copilot-tools.ts"

// Rows in the shape `GET /v1/sessions` answers (the daemon's row and the Cloud
// reader's snapshot carry the same keys).
const ROWS = [
  { id: "owned:acp:AAAA", label: "Refactor the billing page", assistant: "antigravity", state: "working", work_state: "working", cwd: "/x", working_since: 1_000_000, line: "Working… (10m)" },
  { id: "%2", label: "Prepare screenshots", assistant: "codex", state: "idle", work_state: "unknown", cwd: "/y", activity: { at: 1_000_300, known: true },
    closeability: { state: "safe", version: "cl1_abc", reasons: [] },
    disposition: { evidence: "authenticated_session_delivery", receiptAt: 1_000_000, scope: "session", title: "Captured six feature screenshots; \x1b[1mabc123\x1b[0m is on main." } },
  { id: "%7", label: "Fix the flaky test", assistant: "claude", state: "waiting", work_state: "waiting", cwd: "/z",
    sessionId: "conversation-of-the-flaky-test", attention_count: 2,
    menu: { question: "\x1b[1mPush to main?\x1b[0m", options: [{ n: 1, label: "Yes", can: true, selected: true }, { n: 2, label: "No", can: true, selected: false }] } },
  { id: "%9", label: "Fix the login test", assistant: "claude", state: "working", work_state: "working", cwd: "/z",
    closeability: { state: "blocked", version: "cl1_def", reasons: [{ code: "terminal_working" }] } },
]

// A capture as the daemon sends it: SGR colour runs and trailing blank rows.
const PAINTED =
  "\x1b[1m$\x1b[0m go test ./...\n" +
  "ok  \tgithub.com/example/pkg\t0.42s\n" +
  "\x1b[38;2;143;179;239m--- FAIL: TestLogin\x1b[39m (0.01s)\n" +
  "    login_test.go:12: want 200, got 500   \n" +
  "\x1b[1m?\x1b[0m for shortcuts\n\n\n"

interface Call { path: string; init?: RequestInit }

function fakeMachine(screens: Record<string, unknown[]>, opts: { sessionsStatus?: number; transcripts?: Record<string, unknown>; notes?: unknown; snippets?: unknown[] } = {}) {
  const calls: Call[] = []
  const sleeps: number[] = []
  const reads: Record<string, number> = {}
  const deps: ToolDeps = {
    async fetch(path, init) {
      calls.push({ path, init })
      if (path === "/v1/sessions") {
        if (opts.sessionsStatus) return Response.json({ error: { code: "unauthorized", message: "sign in again" } }, { status: opts.sessionsStatus })
        return Response.json({ sessions: ROWS, at: 1 })
      }
      if (path.startsWith("/v1/transcript?")) {
        const id = new URLSearchParams(path.split("?")[1]).get("session") ?? ""
        const t = opts.transcripts?.[id]
        if (!t) return Response.json({ error: { code: "no_transcript", message: "no transcript for this session" } }, { status: 404 })
        return Response.json(t)
      }
      const m = /^\/v1\/sessions\/([^/]+)\/(screen|send)$/.exec(path)
      if (m && m[2] === "screen") {
        const id = decodeURIComponent(m[1])
        const answers = screens[id]
        if (!answers) return Response.json({ error: { code: "no_session" } }, { status: 404 })
        const n = reads[id] ?? 0
        reads[id] = n + 1
        return Response.json({ screen: answers[Math.min(n, answers.length - 1)] })
      }
      if (m && m[2] === "send") return Response.json({ ok: true })
      if (/^\/v1\/sessions\/[^/]+\/(key|close)$/.test(path)) return Response.json({ ok: true })
      if (path.startsWith("/v1/snippets?")) return Response.json({ snippets: opts.snippets ?? [] })
      if (path.startsWith("/v1/work/v2/human-interventions/")) return Response.json(opts.notes ?? { ok: true, rows: [], pruned_resolved: 0 })
      return new Response("", { status: 404 })
    },
    async sleep(ms) { sleeps.push(ms) },
    newRequestID: () => "req-1",
  }
  return { deps, calls, sleeps }
}

test("list_sessions sorts Sessions by what they need and says what the busy ones are doing", async () => {
  const { deps, calls } = fakeMachine({}, { transcripts: {
    "owned:acp:AAAA": { entries: [{ role: "user", text: "go" }, { role: "assistant", text: "Rewrote the invoice table; running the page tests." }] },
    "%7": { entries: [{ role: "assistant", text: "Twenty runs pass now. Should I push to main?" }] },
  } })
  deps.now = () => 1_000_600
  const { output } = await runCopilotTool("list_sessions", "{}", deps)
  const out = JSON.parse(output)
  assert.equal(out.summary, "4 個 Session：1 個等你回應、2 個工作中、1 個閒置")
  assert.deepEqual(out.needs_you.map((r: { id: string }) => r.id), ["%7"])
  assert.equal(out.needs_you[0].question, "Push to main?")
  assert.deepEqual(out.needs_you[0].options, ["1. Yes", "2. No"])
  assert.equal(out.needs_you[0].latest, "Twenty runs pass now. Should I push to main?")
  assert.deepEqual(out.working.map((r: { id: string }) => r.id), ["owned:acp:AAAA", "%9"])
  assert.equal(out.working[0].working_for, "10 分鐘")
  assert.equal(out.working[0].latest, "Rewrote the invoice table; running the page tests.")
  assert.equal(out.working[1].latest, undefined, "an unreadable transcript leaves the row without a latest line")
  assert.deepEqual(out.idle, [{ id: "%2", label: "Prepare screenshots", assistant: "codex", delivered: "Captured six feature screenshots; abc123 is on main.", last_active: "5 分鐘前" }])
  const reads = calls.filter((c) => c.path.startsWith("/v1/transcript?"))
  assert.equal(reads.length, 3)
  for (const c of reads) assert.equal((c.init?.headers as Record<string, string>)["X-Clawdline-Unopened-Read"], "1")
})

test("list_sessions says why when the machine refuses", async () => {
  const { deps } = fakeMachine({}, { sessionsStatus: 401 })
  const out = JSON.parse((await runCopilotTool("list_sessions", "", deps)).output)
  assert.match(out.error, /HTTP 401/)
  assert.match(out.error, /sign in again/)
})

test("get_session_screen waits out the first pending read and hands over the words, not the paint", async () => {
  const { deps, calls, sleeps } = fakeMachine({
    "%2": [
      { id: "%2", pending: true, readable: false },
      { id: "%2", pending: false, readable: true, text: PAINTED, lines: 24 },
    ],
  })
  const { output } = await runCopilotTool("get_session_screen", JSON.stringify({ session_id: "%2" }), deps)
  const out = JSON.parse(output)
  assert.deepEqual(calls.map((c) => c.path), ["/v1/sessions", "/v1/sessions/%252/screen", "/v1/sessions/%252/screen"])
  for (const c of calls.slice(1)) assert.equal((c.init?.headers as Record<string, string>)["X-Clawdline-Unopened-Read"], "1")
  assert.equal(sleeps.length, 1)
  assert.equal(out.session_id, "%2")
  assert.equal(out.label, "Prepare screenshots")
  assert.equal(
    out.recent_screen_text,
    "$ go test ./...\nok  \tgithub.com/example/pkg\t0.42s\n--- FAIL: TestLogin (0.01s)\n    login_test.go:12: want 200, got 500\n? for shortcuts",
  )
  assert.doesNotMatch(output, /\\u001b/)
})

test("get_session_screen accepts the row's id as list_sessions gave it", async () => {
  const { deps } = fakeMachine({ "owned:acp:AAAA": [{ pending: false, readable: true, text: "hello\n" }] })
  const out = JSON.parse((await runCopilotTool("get_session_screen", '{"session_id":"owned:acp:AAAA"}', deps)).output)
  assert.equal(out.recent_screen_text, "hello")
  assert.equal(out.assistant, "antigravity")
})

test("get_session_screen returns only the tail of a long screen", async () => {
  const long = Array.from({ length: 100 }, (_, i) => `line ${i}`).join("\n")
  const { deps } = fakeMachine({ "%7": [{ pending: false, readable: true, text: long }] })
  const out = JSON.parse((await runCopilotTool("get_session_screen", '{"session_id":"%7"}', deps)).output)
  const lines = out.recent_screen_text.split("\n")
  assert.equal(lines.length, 40)
  assert.equal(lines.at(-1), "line 99")
})

test("a name that is no id nor whole label is put back with every Session, not guessed", async () => {
  const { deps, calls } = fakeMachine({})
  const out = JSON.parse((await runCopilotTool("get_session_screen", '{"session_id":"claude"}', deps)).output)
  assert.match(out.error, /\u4f9d\u4f7f\u7528\u8005\u7684\u610f\u601d\u6311\u51fa id/)
  assert.ok(out.candidates.some((c: { id: string }) => c.id === "%7") && out.candidates.some((c: { id: string }) => c.id === "%9"))
  assert.deepEqual(calls.map((c) => c.path), ["/v1/sessions"])
})

test("an id finds that one row", async () => {
  const { deps } = fakeMachine({ "%7": [{ pending: false, readable: true, text: "waiting for you" }] })
  const out = JSON.parse((await runCopilotTool("get_session_screen", '{"session_id":"%7"}', deps)).output)
  assert.equal(out.session_id, "%7")
})

test("a screen that never arrives is an error after a bounded wait", async () => {
  const { deps, calls, sleeps } = fakeMachine({ "%2": [{ pending: true, readable: false }] })
  const out = JSON.parse((await runCopilotTool("get_session_screen", '{"session_id":"%2"}', deps)).output)
  assert.match(out.error, /逾時/)
  assert.equal(calls.length, 1 + SCREEN_READ_ATTEMPTS)
  assert.equal(sleeps.length, SCREEN_READ_ATTEMPTS - 1)
})

test("an unreadable screen is said to be unreadable, not empty", async () => {
  const { deps } = fakeMachine({ "%2": [{ pending: false, readable: false }] })
  const out = JSON.parse((await runCopilotTool("get_session_screen", '{"session_id":"%2"}', deps)).output)
  assert.match(out.error, /無法讀取/)
  assert.equal(out.recent_screen_text, undefined)
})

test("send_message_to_session posts the text under an idempotency key", async () => {
  const { deps, calls } = fakeMachine({})
  const out = JSON.parse((await runCopilotTool("send_message_to_session", '{"session_id":"%2","session_label":"Prepare screenshots","message":"run the tests"}', deps)).output)
  assert.equal(out.success, true)
  assert.equal(out.sent_to, "Prepare screenshots")
  const post = calls.at(-1)!
  assert.equal(post.path, "/v1/sessions/%252/send")
  assert.equal(post.init?.method, "POST")
  assert.equal((post.init?.headers as Record<string, string>)["Idempotency-Key"], "req-1")
  assert.deepEqual(JSON.parse(String(post.init?.body)), { text: "run the tests" })
})

test("the session configuration is in the Realtime GA shape and carries every tool", () => {
  const ev = sessionUpdateEvent() as { type: string; session: Record<string, any> }
  assert.equal(ev.type, "session.update")
  assert.equal(ev.session.type, "realtime")
  assert.deepEqual(ev.session.output_modalities, ["audio"])
  assert.equal(ev.session.audio.input.transcription.model, "whisper-1")
  assert.equal(ev.session.audio.output.voice, "marin")
  assert.deepEqual(ev.session.audio.input.turn_detection, { type: "semantic_vad", eagerness: "low", create_response: true, interrupt_response: true },
    "it waits for the end of what the person means, not the first pause")
  for (const beta of ["modalities", "voice", "input_audio_transcription"]) assert.equal(beta in ev.session, false, beta)
  assert.deepEqual(ev.session.tools.map((t: { name: string }) => t.name), COPILOT_TOOLS.map((t) => t.name))
  assert.deepEqual(COPILOT_TOOLS.map((t) => t.name), ["get_status_briefing", "list_sessions", "get_session_recap", "get_session_summary", "get_session_activity", "explain_session_reply", "get_session_screen", "open_session", "answer_session_question", "get_session_notes", "list_snippets", "open_snippets", "run_snippet", "set_speaking_speed", "set_answer_length", "close_session", "list_machines", "list_projects", "start_session", "send_message_to_session"])
})

test("a tool's answer goes back as its call's output and asks for the spoken reply", () => {
  assert.deepEqual(toolReplyEvents("call_1", '{"count":0}'), [
    { type: "conversation.item.create", item: { type: "function_call_output", call_id: "call_1", output: '{"count":0}' } },
    { type: "response.create" },
  ])
})

test("plain text keeps CJK and drops OSC titles", () => {
  assert.equal(plainScreenText("\x1b]0;title\x07測試 \x1b[32m通過\x1b[0m  "), "測試 通過")
})

// Entries in the shape `GET /v1/transcript?session=` answers.
const TRANSCRIPT = {
  id: "%7",
  entries: [
    { at: "1", role: "user", text: "Make the flaky login test pass and push it." },
    { at: "2", role: "assistant", text: "The test fails because the session cookie expires mid-run." },
    { at: "3", role: "tool", tool: "Bash", text: "go test ./internal/auth -run TestLogin -count=20 " + "x".repeat(400) },
    { at: "4", role: "tool", text: "\x1b[32mok\x1b[0m  github.com/example/auth 1.2s" },
    { at: "5", role: "assistant", text: "Twenty runs pass now. Should I push to main?" },
  ],
}

test("get_session_activity explains a Session from its transcript, goal first and newest last", async () => {
  const { deps, calls } = fakeMachine({}, { transcripts: { "%7": TRANSCRIPT } })
  const { output, logs } = await runCopilotTool("get_session_activity", '{"session_id":"%7"}', deps)
  const out = JSON.parse(output)
  assert.deepEqual(calls.map((c) => c.path).slice(0, 2), ["/v1/sessions", "/v1/transcript?session=%257"])
  assert.equal(out.label, "Fix the flaky test")
  assert.equal(out.state, "waiting")
  assert.deepEqual(out.recent_activity.map((e: { role: string }) => e.role), ["user", "assistant", "tool", "tool", "assistant"])
  assert.equal(out.recent_activity[0].text, "Make the flaky login test pass and push it.")
  assert.equal(out.recent_activity.at(-1).text, "Twenty runs pass now. Should I push to main?")
  assert.equal(out.recent_activity[2].tool, "Bash")
  assert.equal(out.last_report, "Twenty runs pass now. Should I push to main?")
  assert.ok(out.recent_activity[2].text.length <= 163, "a command is clipped")
  assert.equal(out.recent_activity[3].text, "ok github.com/example/auth 1.2s")
  assert.ok(logs.some((l) => l.text.includes("5 筆")))
})

test("get_session_activity says why when the machine has no transcript", async () => {
  const { deps } = fakeMachine({})
  const out = JSON.parse((await runCopilotTool("get_session_activity", '{"session_id":"%2"}', deps)).output)
  assert.match(out.error, /HTTP 404/)
  assert.match(out.error, /no transcript for this session/)
})

test("a long transcript keeps the newest turns within budget and still names the goal", () => {
  const entries = [{ role: "user", text: "GOAL" }, ...Array.from({ length: 59 }, (_, i) => ({ role: "assistant", text: `step ${i} ` + "y".repeat(590) }))]
  const kept = activityOf(entries)
  const chars = kept.reduce((n: number, e: { text: string }) => n + e.text.length, 0)
  assert.equal(kept[0].text, "GOAL")
  assert.match(kept.at(-1)?.text ?? "", /^step 58 /)
  assert.ok(chars <= ACTIVITY_TOTAL_CHARS + 10, String(chars))
  assert.ok(kept.length < entries.length)
})

test("a waiting Session's activity carries the question on its screen", async () => {
  const { deps, calls } = fakeMachine(
    { "%7": [{ pending: true }, { pending: false, readable: true, text: "Push to main?\n\x1b[1m❯ 1. Yes\x1b[0m\n  2. No\n" }] },
    { transcripts: { "%7": TRANSCRIPT } },
  )
  const out = JSON.parse((await runCopilotTool("get_session_activity", '{"session_id":"%7"}', deps)).output)
  assert.equal(out.waiting_screen_text, "Push to main?\n❯ 1. Yes\n  2. No")
  assert.equal(calls.filter((c) => c.path.endsWith("/screen")).length, 2)
})

test("a working Session's activity does not read its screen", async () => {
  const { deps, calls } = fakeMachine({}, { transcripts: { "%9": { entries: [{ role: "user", text: "go" }] } } })
  const out = JSON.parse((await runCopilotTool("get_session_activity", '{"session_id":"%9"}', deps)).output)
  assert.equal(out.waiting_screen_text, undefined)
  assert.equal(calls.some((c) => c.path.endsWith("/screen")), false)
})

test("a long report keeps its conclusion", () => {
  const [kept] = activityOf([{ role: "assistant", text: "start " + "z".repeat(2000) + " CONCLUSION" }])
  assert.match(kept.text, /^start /)
  assert.match(kept.text, / CONCLUSION$/)
})

test("a pane id without its sign is that pane, not a position in the list", async () => {
  const { deps } = fakeMachine({ "%2": [{ pending: false, readable: true, text: "two" }], "%7": [{ pending: false, readable: true, text: "seven" }] })
  assert.equal(JSON.parse((await runCopilotTool("get_session_screen", '{"session_id":"7"}', deps)).output).session_id, "%7")
  assert.match(JSON.parse((await runCopilotTool("get_session_screen", '{"session_id":"3"}', deps)).output).error, /找不到/)
})

test("get_session_summary gives the Session's own delivery words and its last reply in one read", async () => {
  const { deps, calls } = fakeMachine({}, { transcripts: { "%2": { entries: [
    { role: "user", text: "screenshots please" },
    { role: "assistant", text: "Done: six screenshots are in docs/shots and pushed." },
    { role: "tool", tool: "Bash", text: "git push" },
  ] } } })
  deps.now = () => 1_000_600
  const out = JSON.parse((await runCopilotTool("get_session_summary", '{"session_id":"%2"}', deps)).output)
  assert.equal(out.label, "Prepare screenshots")
  assert.equal(out.delivered_summary, "Captured six feature screenshots; abc123 is on main.")
  assert.equal(out.delivered_when, "10 分鐘前")
  assert.equal(out.last_reply, "Done: six screenshots are in docs/shots and pushed.")
  assert.deepEqual(calls.map((c) => c.path), ["/v1/sessions", "/v1/transcript?session=%252"])
  assert.equal((calls[1].init?.headers as Record<string, string>)["X-Clawdline-Unopened-Read"], "1")
})

test("a waiting Session's summary says what it asks, and survives an unreadable transcript", async () => {
  const { deps } = fakeMachine({})
  const out = JSON.parse((await runCopilotTool("get_session_summary", '{"session_id":"%7"}', deps)).output)
  assert.equal(out.question, "Push to main?")
  assert.deepEqual(out.options, ["1. Yes", "2. No"])
  assert.equal(out.last_reply, undefined)
  assert.equal(out.delivered_summary, undefined)
  assert.match(out.note, /get_session_screen/)
})

// Two pages of one Session's conversation, newest first as the machine pages them.
const NEWER = { entries: [
  { at: "30", role: "user", text: "Now make the call button float." },
  { at: "31", role: "tool", tool: "Bash", text: 'git push && clawdline session report --summary "The call shrinks to a \\"bubble\\"; a497edf is on main." 2>&1 | tail -1' },
  { at: "32", role: "user", text: "<clawdline-notice>{\"kind\":\"task_finished\"}</clawdline-notice>" },
  { at: "33", role: "assistant", text: "The bubble is live." },
], nextBefore: 500 }
const OLDER = { entries: [
  { at: "10", role: "user", text: "Make the copilot read any Session." },
  { at: "11", role: "tool", tool: "Bash", text: "clawdline session report --summary 'Reads any Session over Cloud; 0fbc999 deployed.'" },
  { at: "12", role: "tool", tool: "Bash", text: "clawdline codegraph query \"session report summary\"" },
] }

function pagedMachine() {
  const paths: string[] = []
  const deps: ToolDeps = {
    async fetch(path) {
      paths.push(path)
      if (path === "/v1/sessions") return Response.json({ sessions: ROWS })
      if (path === "/v1/transcript?session=%257") return Response.json(NEWER)
      if (path === "/v1/transcript?session=%257&before=500") return Response.json(OLDER)
      return new Response("", { status: 404 })
    },
    async sleep() {},
  }
  return { deps, paths }
}

test("a recap reads the Session back to its start and keeps the person's asks and each delivery, oldest first", async () => {
  const { deps, paths } = pagedMachine()
  const read = await readRecap(deps, "%7")
  assert.ok(!("error" in read))
  const r = read as Exclude<typeof read, { error: string }>
  assert.deepEqual(paths, ["/v1/transcript?session=%257", "/v1/transcript?session=%257&before=500"])
  assert.deepEqual(r.requests.map((x: { text: string }) => x.text), ["Make the copilot read any Session.", "Now make the call button float."])
  assert.deepEqual(r.deliveries.map((x: { text: string }) => x.text), ["Reads any Session over Cloud; 0fbc999 deployed.", 'The call shrinks to a "bubble"; a497edf is on main.'])
  assert.equal(r.lastReply, "The bubble is live.")
  assert.equal(r.complete, true)
})

test("get_session_recap hands the model the whole story to tell", async () => {
  const { deps } = pagedMachine()
  const out = JSON.parse((await runCopilotTool("get_session_recap", '{"session_id":"%7"}', deps)).output)
  assert.equal(out.label, "Fix the flaky test")
  assert.equal(out.requests.length, 2)
  assert.equal(out.deliveries.length, 2)
  assert.equal(out.question, "Push to main?")
  assert.equal(out.note, undefined, "the whole conversation was read")
})

test("a recap stops at its page budget and says it did", async () => {
  let n = 0
  const deps: ToolDeps = {
    async fetch(path) {
      if (path === "/v1/sessions") return Response.json({ sessions: ROWS })
      n++
      return Response.json({ entries: [{ role: "user", text: `ask ${n}` }], nextBefore: 1000 - n })
    },
    async sleep() {},
  }
  const out = JSON.parse((await runCopilotTool("get_session_recap", '{"session_id":"%2"}', deps)).output)
  assert.equal(n, 5)
  assert.match(out.note, /只讀了最近的部分/)
  assert.equal(out.requests[0], "ask 5", "the oldest page read comes first")
})

test("the call opens with the Sessions already in its instructions", async () => {
  const { deps } = fakeMachine({}, { transcripts: {} })
  const snapshot = await sessionsSnapshot(deps)
  assert.match(snapshot, /^【通話開始時的 Session 快照】/)
  assert.match(snapshot, /Fix the flaky test/)
  assert.match(snapshot, /Push to main\?/)
  const ev = sessionUpdateEvent("【之前的通話】x", snapshot) as { session: { instructions: string } }
  assert.ok(ev.session.instructions.startsWith(COPILOT_INSTRUCTIONS))
  assert.ok(ev.session.instructions.indexOf(snapshot) < ev.session.instructions.indexOf("【之前的通話】"))
  const refused: ToolDeps = { async fetch() { return new Response("", { status: 503 }) }, async sleep() {} }
  assert.equal(await sessionsSnapshot(refused), "", "a machine that cannot answer leaves the call to its tools")
})

test("the machine's settings and briefing shape the call's configuration", () => {
  const ev = sessionUpdateEvent("【之前的通話】x", "【通話開始時的 Session 快照】y", { eagerness: "high", extra: "叫我 Sean", briefing: "一件事要你處理。" }) as
    { session: { instructions: string; audio: { input: { turn_detection: { eagerness: string } } } } }
  const text = ev.session.instructions
  assert.equal(ev.session.audio.input.turn_detection.eagerness, "high")
  const order = ["【使用者的附加指示】", "【整理好的現況匯報】", "【通話開始時的 Session 快照】", "【之前的通話】"].map((h) => text.lastIndexOf(h))
  assert.ok(order.every((i) => i > 0), JSON.stringify(order))
  assert.deepEqual([...order].sort((a, b) => a - b), order, "extra, briefing, snapshot, then memory")
  const odd = sessionUpdateEvent("", "", { eagerness: "medium" }) as { session: { audio: { input: { turn_detection: { eagerness: string } } } } }
  assert.equal(odd.session.audio.input.turn_detection.eagerness, "low", "an eagerness the API does not take falls back to patient")
})

test("asked for a briefing, the tool waits for the machine's and says when there is none", async () => {
  const { deps } = fakeMachine({})
  deps.briefing = async () => "一件事要你處理：Fix the flaky test 在等你決定。"
  const got = JSON.parse((await runCopilotTool("get_status_briefing", "{}", deps)).output)
  assert.match(got.briefing, /Fix the flaky test/)
  deps.briefing = async () => null
  assert.match(JSON.parse((await runCopilotTool("get_status_briefing", "{}", deps)).output).error, /list_sessions/)
  delete deps.briefing
  assert.match(JSON.parse((await runCopilotTool("get_status_briefing", "{}", deps)).output).error, /沒有機器整理的匯報/)
})

test("a call speaks in the machine's chosen voice, and an unknown one falls back to marin", () => {
  const voiceOf = (voice?: string) => (sessionUpdateEvent("", "", { voice }) as { session: { audio: { output: { voice: string } } } }).session.audio.output.voice
  assert.equal(voiceOf("cedar"), "cedar")
  assert.equal(voiceOf(undefined), "marin")
  assert.equal(voiceOf("fable"), "marin")
})

test("open_session opens the named Session on the page the call runs in", async () => {
  const { deps } = fakeMachine({})
  const asked: string[] = []
  deps.openSession = async (id) => { asked.push(id); return true }
  const out = JSON.parse((await runCopilotTool("open_session", '{"session_id":"%2"}', deps)).output)
  assert.deepEqual(asked, ["%2"])
  assert.equal(out.opened, true)
  assert.equal(out.label, "Prepare screenshots")
  deps.openSession = async () => false
  assert.match(JSON.parse((await runCopilotTool("open_session", '{"session_id":"%2"}', deps)).output).error, /沒有在這個畫面上打開/)
  assert.ok(JSON.parse((await runCopilotTool("open_session", '{"session_id":"claude"}', deps)).output).candidates, "an ambiguous name is put back with its candidates")
})

test("explain_session_reply hands over a long reply whole, with the ask it answers", async () => {
  const diagnosis = "Problem 1: the start path skips the default model.\n" + "x".repeat(1800) + "\nShall I open a fix branch?"
  const { deps } = fakeMachine({}, { transcripts: { "%2": { entries: [
    { role: "user", text: "Why does it still open on Argon?" },
    { role: "user", text: "<clawdline-notice>{}</clawdline-notice>" },
    { role: "tool", tool: "Bash", text: "grep -rn DefaultModel" },
    { role: "assistant", text: diagnosis },
  ] } } })
  const out = JSON.parse((await runCopilotTool("explain_session_reply", '{"session_id":"%2"}', deps)).output)
  assert.equal(out.label, "Prepare screenshots")
  assert.equal(out.asked, "Why does it still open on Argon?")
  assert.equal(out.reply, diagnosis)
  assert.equal(out.reply_truncated, undefined)
})

test("a reply over its budget keeps its beginning and the question it ends on", async () => {
  const diagnosis = "Root cause found.\n" + "y".repeat(REPLY_CHARS * 2) + "\nShall I open a fix branch?"
  const { deps } = fakeMachine({}, { transcripts: { "%2": { entries: [{ role: "assistant", text: diagnosis }] } } })
  const out = JSON.parse((await runCopilotTool("explain_session_reply", '{"session_id":"%2"}', deps)).output)
  assert.equal(out.reply_truncated, true)
  assert.ok(out.reply.startsWith("Root cause found."))
  assert.ok(out.reply.endsWith("Shall I open a fix branch?"))
  assert.ok(out.reply.length <= REPLY_CHARS + 3)
})

test("explain_session_reply says so when a Session has no reply to explain", async () => {
  const { deps } = fakeMachine({})
  const out = JSON.parse((await runCopilotTool("explain_session_reply", '{"session_id":"%7"}', deps)).output)
  assert.match(out.error ?? out.note, /讀不到|無法讀取/)
})


// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { menuFingerprint } from "../session/fingerprint.ts"

test("a chosen option is pressed with the fingerprint of the question it answers", async () => {
  const { deps, calls } = fakeMachine({})
  const out = JSON.parse((await runCopilotTool("answer_session_question", '{"session_id":"%7","session_label":"Fix the flaky test","choice":"1"}', deps)).output)
  assert.equal(out.answered, true)
  assert.equal(out.chose, "1. Yes")
  const press = calls.at(-1)!
  assert.equal(press.path, "/v1/sessions/%257/key")
  const sent = JSON.parse(String(press.init?.body))
  const menu = ROWS.find((r) => r.id === "%7")!.menu
  assert.deepEqual(sent, { key: "1", expect: menuFingerprint(menu as never) })
  assert.equal((press.init?.headers as Record<string, string>)["X-Clawdline-Unopened-Read"], "1")
})

test("a choice that names no single option presses nothing", async () => {
  const { deps, calls } = fakeMachine({})
  const out = JSON.parse((await runCopilotTool("answer_session_question", '{"session_id":"%7","session_label":"Fix the flaky test","choice":"maybe"}', deps)).output)
  assert.match(out.error, /1\. Yes、2\. No/)
  assert.equal(calls.some((c) => c.path.endsWith("/key")), false)
  const idle = JSON.parse((await runCopilotTool("answer_session_question", '{"session_id":"%2","session_label":"Prepare screenshots","choice":"1"}', deps)).output)
  assert.match(idle.error, /沒有在等問題的答案/)
})

test("spoken choices find their option", () => {
  const choices = [{ n: 1, label: "Yes" }, { n: 2, label: "No, and tell Claude what to do differently" }]
  assert.equal((choiceOf(choices, "第一個") as { n: number }).n, 1)
  assert.equal((choiceOf(choices, "2") as { n: number }).n, 2)
  assert.equal((choiceOf(choices, "yes") as { n: number }).n, 1)
  assert.equal((choiceOf(choices, "tell claude") as { n: number }).n, 2)
  assert.ok("error" in choiceOf(choices, "3"))
})

test("a Session's notes are read for the person, without resolved, withdrawn or secret ones", async () => {
  const { deps, calls } = fakeMachine({}, { notes: { ok: true, pruned_resolved: 0, rows: [
    { id: "n1", kind: "answer", source_label: "Fix the flaky test", title: "Choose a date", summary: "One release date needs your choice.",
      action: "Choose a date.", reason: "Only you can choose it.", options: [{ label: "Tuesday", draft: "Tuesday works." }, { label: "Wednesday", draft: "Wednesday works." }],
      resolved_at: null },
    { id: "n2", kind: "read", title: "Old", summary: "", action: "", reason: "", options: [], resolved_at: 1 },
    { id: "n3", kind: "secret", title: "API key", summary: "", action: "", reason: "", options: [], resolved_at: null },
    { id: "n4", kind: "read", title: "Withdrawn", summary: "", action: "", reason: "", options: [], resolved_at: null, withdrawn: true },
  ] } })
  const out = JSON.parse((await runCopilotTool("get_session_notes", "{}", deps)).output)
  assert.deepEqual(calls.at(-1)?.path, "/v1/work/v2/human-interventions/conversation%3Aconversation-of-the-flaky-test")
  assert.equal(out.sessions.length, 1)
  assert.deepEqual(out.sessions[0].notes.map((n: { title: string }) => n.title), ["Choose a date"])
  assert.deepEqual(out.sessions[0].notes[0].replies, ["Tuesday", "Wednesday"])
  const list = JSON.parse((await runCopilotTool("list_sessions", "{}", deps)).output)
  assert.equal(list.needs_you[0].notes, 2)
  assert.deepEqual(list.needs_you[0].options, ["1. Yes", "2. No"])
})

test("a busy Session's row says its goal, its last two deliveries and its newest reply", () => {
  const p = progressOf([
    { role: "user", text: "Make the login test pass." },
    { role: "tool", text: 'clawdline session report --summary "First fix pushed."' },
    { role: "tool", text: 'clawdline session report --summary "Twenty runs pass."' },
    { role: "user", text: "<clawdline-notice>{}</clawdline-notice>" },
    { role: "user", text: "Now push it to main." },
    { role: "tool", text: "clawdline session report --summary 'Pushed to main.'" },
    { role: "assistant", text: "Pushed; watching CI." },
  ])
  assert.deepEqual(p, { latest: "Pushed; watching CI.", goal: "Now push it to main.", recent: ["Twenty runs pass.", "Pushed to main."] })
})

test("a call speaks at the machine's speed, a little slower than OpenAI's unless set", () => {
  const speedOf = (speed?: string) => (sessionUpdateEvent("", "", { speed }) as { session: { audio: { output: { speed: number } } } }).session.audio.output.speed
  assert.equal(speedOf(undefined), 0.9)
  assert.equal(speedOf("0.8"), 0.8)
  assert.equal(speedOf("3"), 0.9)
})

test("short answers are the machine's switch, on unless it says off", () => {
  const text = (brief?: boolean) => (sessionUpdateEvent("", "", { brief }) as { session: { instructions: string } }).session.instructions
  assert.ok(text(undefined).includes(BRIEF_RULE) && !text(undefined).includes(LONG_RULE))
  assert.ok(text(false).includes(LONG_RULE) && !text(false).includes(BRIEF_RULE))
  assert.match(text(false), /不受三句限制/, "the report exception stays either way")
})

test("the copilot speaks the language the console is shown in", () => {
  const english = (sessionUpdateEvent("", "", { language: "en" }) as { session: { instructions: string } }).session.instructions
  assert.match(english, /全程用英文（en）說話/)
  assert.doesNotMatch(english, /全程台灣繁體中文/)
  const greeting = (greetingEvent(false, "en") as { response: { instructions: string } }).response.instructions
  assert.match(greeting, /一句自然的英文問候/)
  // Chinese, and a console whose language is not known, keep the words as they were.
  assert.equal(instructionsFor(true, "zh-Hant"), instructionsFor(true))
  assert.match(instructionsFor(true), /全程台灣繁體中文/)
  assert.match((greetingEvent(true) as { response: { instructions: string } }).response.instructions, /很短、自然的繁體中文打招呼/)
  // The short-answer setting still applies in another language.
  assert.ok(instructionsFor(false, "en").includes(LONG_RULE))
})

test("slower and faster move one step and stop at the ends", () => {
  assert.equal(nextSpeed("0.9", "slower"), "0.8")
  assert.equal(nextSpeed("0.8", "slower"), "0.8")
  assert.equal(nextSpeed("1.0", "faster"), "1.1")
  assert.equal(nextSpeed("1.1", "faster"), "1.1")
  assert.equal(nextSpeed("0.8", "normal"), "1.0")
  assert.equal(nextSpeed("0.9", "1.1"), "1.1")
  assert.equal(nextSpeed("0.9", "2.0"), "0.9")
})

test("asked in the call, the copilot changes its speed and length and keeps them for the next call", async () => {
  const { deps } = fakeMachine({})
  let delivery = { speed: "0.9", brief: true }
  const changes: unknown[] = []
  deps.delivery = () => delivery
  deps.adjust = async (change) => { changes.push(change); delivery = { ...delivery, ...change }; return { saved: true } }
  assert.deepEqual(JSON.parse((await runCopilotTool("set_speaking_speed", '{"speed":"slower"}', deps)).output), { speed: "0.8", changed: true, saved_for_next_calls: true })
  assert.match(JSON.parse((await runCopilotTool("set_speaking_speed", '{"speed":"slower"}', deps)).output).note, /最慢/)
  assert.deepEqual(JSON.parse((await runCopilotTool("set_answer_length", '{"brief":false}', deps)).output), { brief: false, saved_for_next_calls: true })
  assert.deepEqual(changes, [{ speed: "0.8" }, { brief: false }], "a step that changes nothing is not sent")
})

test("an in-call change sends the instructions and speed, never the voice", () => {
  const ev = adjustEvent("", "", { speed: "0.8", brief: false, voice: "cedar" }) as { session: Record<string, any> }
  assert.equal(ev.session.type, "realtime")
  assert.equal(ev.session.audio.output.speed, 0.8)
  assert.equal("voice" in ev.session.audio.output, false)
  assert.ok(ev.session.instructions.includes(LONG_RULE))
  assert.equal("tools" in ev.session, false)
})

test("times are said in words a person uses, never as a count of minutes", () => {
  const zone = "Asia/Taipei"
  const now = Date.UTC(2026, 10, 12, 6, 0, 0) / 1000 // 14:00 in Taipei
  assert.equal(whenPhrase(now - 30, now, zone), "剛剛")
  assert.equal(whenPhrase(now - 12 * 60, now, zone), "12 分鐘前")
  assert.equal(whenPhrase(now - 3 * 3600, now, zone), "3 小時前")
  assert.equal(whenPhrase(Date.UTC(2026, 10, 11, 10, 20) / 1000, now, zone), "昨天 18:20")
  assert.equal(whenPhrase(Date.UTC(2026, 10, 9, 6, 30) / 1000, now, zone), "11/9 14:30")
  assert.equal(durationPhrase(25 * 60), "25 分鐘")
  assert.equal(durationPhrase(80 * 60), "1 小時 20 分")
  assert.equal(durationPhrase(2 * 86400 + 3 * 3600), "2 天 3 小時")
})

test("a finished Session is closed on request, never forced, at the reading that said it was safe", async () => {
  const { deps, calls } = fakeMachine({})
  const out = JSON.parse((await runCopilotTool("close_session", '{"session_id":"%2","session_label":"Prepare screenshots"}', deps)).output)
  assert.deepEqual(out, { closed: true, label: "Prepare screenshots" })
  const close = calls.at(-1)!
  assert.equal(close.path, "/v1/sessions/%252/close")
  assert.deepEqual(JSON.parse(String(close.init?.body)), { force: false, expected_closeability_version: "cl1_abc" })
  assert.equal((close.init?.headers as Record<string, string>)["X-Clawdline-Unopened-Read"], "1")
})

test("a Session that cannot close safely is asked about again, and closed with force only once the person says so", async () => {
  const { deps, calls } = fakeMachine({})
  const out = JSON.parse((await runCopilotTool("close_session", '{"session_id":"%9","session_label":"Fix the login test"}', deps)).output)
  assert.match(out.error, /還不能安全關閉：它還在工作/)
  assert.match(out.error, /還是要關閉嗎/)
  assert.equal(out.needs_confirmation, true)
  assert.equal(calls.some((c) => c.path.endsWith("/close")), false)

  const forced = JSON.parse((await runCopilotTool("close_session", '{"session_id":"%9","session_label":"Fix the login test","force_confirmed":true}', deps)).output)
  assert.deepEqual(forced, { closed: true, label: "Fix the login test", forced: true })
  const close = calls.at(-1)!
  assert.equal(close.path, "/v1/sessions/%259/close")
  assert.deepEqual(JSON.parse(String(close.init?.body)), { force: true, expected_closeability_version: "cl1_def" })
})

test("a Session the machine calls safe is never sent force, even when the copilot says it was confirmed", async () => {
  const { deps, calls } = fakeMachine({})
  await runCopilotTool("close_session", '{"session_id":"%2","session_label":"Prepare screenshots","force_confirmed":true}', deps)
  assert.equal(JSON.parse(String(calls.at(-1)!.init?.body)).force, false)
})

function focused(deps: ToolDeps, id?: string) {
  let focus = id
  deps.focus = { get: () => focus, set: (next) => { focus = next } }
  return () => focus
}

test("a write never goes to a Session named by its assistant or part of its name", async () => {
  const { deps, calls } = fakeMachine({})
  for (const name of ["codex", "flaky", "claude"]) {
    const out = JSON.parse((await runCopilotTool("send_message_to_session", JSON.stringify({ session_id: name, session_label: name, message: "hi" }), deps)).output)
    assert.match(out.error, /不是一個明確的 Session/, name)
    assert.ok(out.candidates.length > 0)
  }
  assert.equal(calls.some((c) => c.path.endsWith("/send")), false)
})

test("a write whose said name is another Session's is refused", async () => {
  const { deps, calls } = fakeMachine({})
  const out = JSON.parse((await runCopilotTool("send_message_to_session", '{"session_id":"%9","session_label":"Fix the flaky test","message":"push it"}', deps)).output)
  assert.match(out.error, /「Fix the flaky test」，但 id 對到的是「Fix the login test」/)
  assert.equal(calls.some((c) => c.path.endsWith("/send")), false)
})

test("a write goes to the Session under discussion unless the person confirmed a switch", async () => {
  const { deps, calls } = fakeMachine({ "%7": [{ pending: false, readable: true, text: "Push to main?" }] })
  const focus = focused(deps)
  await runCopilotTool("get_session_screen", '{"session_id":"%7"}', deps)
  assert.equal(focus(), "%7", "reading a Session puts it under discussion")
  const other = JSON.parse((await runCopilotTool("send_message_to_session", '{"session_id":"%9","session_label":"Fix the login test","message":"push it"}', deps)).output)
  assert.match(other.error, /目前在討論的不是「Fix the login test」/)
  assert.equal(calls.some((c) => c.path.endsWith("/send")), false)
  const same = JSON.parse((await runCopilotTool("send_message_to_session", '{"session_id":"%7","session_label":"Fix the flaky test","message":"push it"}', deps)).output)
  assert.equal(same.sent_to, "Fix the flaky test")
  const switched = JSON.parse((await runCopilotTool("send_message_to_session", '{"session_id":"%9","session_label":"Fix the login test","message":"push it","switch_confirmed":true}', deps)).output)
  assert.equal(switched.sent_to, "Fix the login test")
  assert.equal(focus(), "%9", "the Session a message went to is now the one under discussion")
  assert.deepEqual(calls.filter((c) => c.path.endsWith("/send")).map((c) => c.path), ["/v1/sessions/%257/send", "/v1/sessions/%259/send"])
})

test("a kind with no Session is not said", () => {
  assert.equal(countSummary(4, [[0, "個等你回應"], [2, "個工作中"], [0, "個已完成"], [2, "個閒置"]]), "4 個 Session：2 個工作中、2 個閒置")
  assert.equal(countSummary(0, [[0, "個工作中"]]), "0 個 Session")
})

function startMachine() {
  const calls: { path: string; init?: RequestInit }[] = []
  const deps: ToolDeps = {
    async fetch(path, init) {
      calls.push({ path, init })
      if (path.startsWith("/v1/places?") || path === "/v1/places") return Response.json({
        places: [{ id: "p-clawdline", label: "clawdline" }, { id: "p-fantease", label: "fantease" }],
        assistants: [{ id: "claude", label: "Claude Code" }, { id: "codex", label: "Codex" }, { id: "grok", label: "Grok", installed: false }],
      })
      if (path === "/v1/settings/default-models") return Response.json({ codex_default_model: "gpt-6.1-sol", claude_default_model: null,
        models: { codex: [{ value: "gpt-6.1-sol" }, { value: "gpt-6-luna" }], claude: [{ value: "claude-opus-5-5" }] } })
      if (/^\/v1\/places\/[^/]+\/start\//.test(path)) return Response.json({ id: "%77", backend: "tmux", assistant: "codex" })
      return new Response("", { status: 404 })
    },
    async sleep() {},
    newRequestID: () => "start-1",
  }
  return { deps, calls }
}

test("list_projects offers the machine's projects, its installed assistants and their models", async () => {
  const { deps } = startMachine()
  const out = JSON.parse((await runCopilotTool("list_projects", "{}", deps)).output)
  assert.deepEqual(out.projects, [{ id: "p-clawdline", label: "clawdline" }, { id: "p-fantease", label: "fantease" }])
  assert.deepEqual(out.assistants.map((a: { id: string }) => a.id), ["claude", "codex"], "an assistant not installed is not offered")
  assert.deepEqual(out.models.codex, { default: "gpt-6.1-sol", choices: ["gpt-6.1-sol", "gpt-6-luna"] })
})

test("start_session opens a Session in the confirmed project with the chosen assistant and model", async () => {
  const { deps, calls } = startMachine()
  let focus: string | undefined
  deps.focus = { get: () => focus, set: (id) => { focus = id } }
  const out = JSON.parse((await runCopilotTool("start_session", '{"project_id":"p-clawdline","project_label":"clawdline","assistant":"codex","model":"gpt-6-luna"}', deps)).output)
  assert.deepEqual(out, { started: true, project: "clawdline", assistant: "codex", model: "gpt-6-luna", session_id: "%77" })
  const start = calls.at(-1)!
  assert.equal(start.path, "/v1/places/p-clawdline/start/codex/gpt-6-luna")
  assert.equal((start.init?.headers as Record<string, string>)["Idempotency-Key"], "start-1")
  assert.equal(focus, "%77", "the new Session is the one under discussion")
  const plain = JSON.parse((await runCopilotTool("start_session", '{"project_id":"p-fantease","project_label":"fantease","assistant":"claude"}', deps)).output)
  assert.equal(plain.model, "預設")
  assert.equal(calls.at(-1)!.path, "/v1/places/p-fantease/start/claude")
})

test("start_session opens nothing when the said project is not the id's", async () => {
  const { deps, calls } = startMachine()
  const wrong = JSON.parse((await runCopilotTool("start_session", '{"project_id":"p-fantease","project_label":"clawdline","assistant":"codex"}', deps)).output)
  assert.match(wrong.error, /「clawdline」，但這個 id 是「fantease」/)
  const missing = JSON.parse((await runCopilotTool("start_session", '{"project_id":"p-nope","project_label":"nope","assistant":"codex"}', deps)).output)
  assert.match(missing.error, /沒有 id 為「p-nope」的專案/)
  assert.equal(calls.some((c) => /\/start\//.test(c.path)), false)
})

test("a page that answers no machines leaves the console's own", async () => {
  const { deps } = startMachine()
  const alone = JSON.parse((await runCopilotTool("list_machines", "{}", deps)).output)
  assert.deepEqual(alone.machines, [{ id: "", name: "這台機器", online: true, current: true }])
  assert.deepEqual(alone.current_machine.projects.map((p: { label: string }) => p.label), ["clawdline", "fantease"], "the first question can name real projects")
  deps.machines = async () => [{ id: "mac-a", name: "Studio", online: true, current: true }, { id: "linux-b", name: "Linux", online: true, current: false }]
  assert.equal(JSON.parse((await runCopilotTool("list_machines", "{}", deps)).output).machines.length, 2)
})

test("a just-started Session is waited for before it is opened", async () => {
  let reads = 0
  const deps: ToolDeps = {
    async fetch(path) {
      if (path === "/v1/sessions") { reads++; return Response.json({ sessions: reads < 3 ? ROWS : [...ROWS, { id: "%77", label: "New one", state: "working" }] }) }
      return new Response("", { status: 404 })
    },
    async sleep() {},
    openSession: async () => true,
  }
  const out = JSON.parse((await runCopilotTool("open_session", '{"session_id":"%77"}', deps)).output)
  assert.equal(out.opened, true)
  assert.equal(out.label, "New one")
  assert.equal(reads, 4, "three until it appears, then the status read after opening")
})

test("a copilot-session refusal is read in the Cloud seam's flat spelling and the machine's nested one", () => {
  assert.deepEqual(copilotRefusal({ error: "cloud_machine_unsupported", detail: "copilot-session", route: "/v1/voice/copilot-session" }, 501), { code: "cloud_machine_unsupported", message: "copilot-session" })
  assert.deepEqual(copilotRefusal({ error: "copilot_key_missing", detail: "copilot_key_missing" }, 412), { code: "copilot_key_missing", message: "" })
  assert.deepEqual(copilotRefusal({ error: { code: "copilot_key_missing", message: "no key" } }, 412), { code: "copilot_key_missing", message: "no key" })
  assert.deepEqual(copilotRefusal(null, 501), { code: "HTTP 501", message: "" })
})

test("a Session described in words is the copilot's to pick: it gets every Session, never a guess", () => {
  const row = (id: string, label: string) => ({ id, label, assistant: "claude", state: "idle", work_state: "" })
  const rows = [row("%7", "[Task] Foyer \u6bcf\u65e5 TimeTree \u65e5\u7a0b\u6574\u7406"), row("%2", "Clawdline voice copilot")]
  const byID = resolveSession(rows, "%7")
  assert.ok("row" in byID && byID.row.id === "%7")
  const byName = resolveSession(rows, "clawdline voice copilot")
  assert.ok("row" in byName && byName.row.id === "%2")
  for (const said of ["TimeTree \u76f8\u95dc\u7684", "Time Tree", "copilot"]) {
    const found = resolveSession(rows, said)
    assert.ok("error" in found, said)
    assert.deepEqual(found.candidates?.map((c) => c.id), ["%7", "%2"], said)
  }
  assert.match(COPILOT_INSTRUCTIONS, /用描述的方式說 Session/)
})

test("a send the person already confirmed, or said whole, is not asked about again", () => {
  assert.match(COPILOT_INSTRUCTIONS, /同一件事只確認一次/)
  assert.match(COPILOT_INSTRUCTIONS, /就直接送，不要再問/)
})

test("the copilot keeps its content but drops the opening and the closing pleasantries", () => {
  assert.match(COPILOT_INSTRUCTIONS, /第一句就是內容，最後一個重點講完就停/)
  assert.match(COPILOT_INSTRUCTIONS, /不要收尾/)
  assert.match(COPILOT_INSTRUCTIONS, /絕對不可以講完總數就結束/)
})

test("the copilot speaks as a secretary reporting in person, not as a list or a help desk", () => {
  assert.match(COPILOT_INSTRUCTIONS, /私人秘書/)
  assert.match(COPILOT_INSTRUCTIONS, /不要客服腔/)
})

test("idle and done Sessions are listed with a warning that their content was not read", async () => {
  const { deps } = fakeMachine({})
  const out = JSON.parse((await runCopilotTool("list_sessions", "{}", deps)).output)
  assert.ok(out.idle.length > 0)
  assert.equal(out.done_and_idle, CONTENT_NOT_READ)
})

test("an opened Session comes with what it did and what is left, for the copilot to tell", async () => {
  const { deps } = fakeMachine({}, { transcripts: { "%2": { entries: [
    { role: "user", text: "Take the six feature screenshots." },
    { role: "assistant", text: "Four are taken; the dark-mode pair is left." },
  ] } } })
  deps.openSession = async () => true
  const out = JSON.parse((await runCopilotTool("open_session", '{"session_id":"%2"}', deps)).output)
  assert.equal(out.status.label, "Prepare screenshots")
  assert.deepEqual(out.status.requests, ["Take the six feature screenshots."])
  assert.match(out.status.last_reply, /dark-mode pair is left/)
  assert.match(COPILOT_INSTRUCTIONS, /還有什麼沒完成/)
})

const SNIPPETS = [
  { id: "s1", title: "Run the checks", body: "Run tools/check.sh and report.", scope: "global" },
  { id: "p1", title: "Daily release", body: "Run the release checklist.", scope: "playbook" },
  { id: "c1", title: "Steward only", body: "x", scope: "clawdfather" },
]

test("list_snippets names a Session's Snippets and Playbooks apart, without Clawdfather's own", async () => {
  const { deps, calls } = fakeMachine({}, { snippets: SNIPPETS })
  const out = JSON.parse((await runCopilotTool("list_snippets", '{"session_id":"%2"}', deps)).output)
  assert.deepEqual(out.snippets, [{ name: "Run the checks", preview: "Run tools/check.sh and report." }])
  assert.deepEqual(out.playbooks, [{ name: "Daily release", preview: "Run the release checklist." }])
  assert.equal(calls.at(-1)!.path, "/v1/snippets?session=%252")
})

test("a Snippet runs as a message to the Session under discussion; a Playbook starts through the page's sheet", async () => {
  const { deps, calls } = fakeMachine({}, { snippets: SNIPPETS })
  const asked: unknown[] = []
  deps.snippets = async (id, request) => { asked.push([id, request]); return "started" }
  const snip = JSON.parse((await runCopilotTool("run_snippet", '{"session_id":"%2","session_label":"Prepare screenshots","name":"Run the checks"}', deps)).output)
  assert.equal(snip.ran, true)
  const send = calls.find((c) => c.path.endsWith("/send"))!
  assert.deepEqual(JSON.parse(String(send.init?.body)), { text: "Run tools/check.sh and report." })
  const play = JSON.parse((await runCopilotTool("run_snippet", '{"session_id":"%2","session_label":"Prepare screenshots","name":"daily release"}', deps)).output)
  assert.equal(play.kind, "playbook")
  assert.deepEqual(asked, [["%2", { tab: "playbook", playbook: "p1", start: true }]])
})

test("a Snippet or Playbook named loosely runs nothing and hands back every name", async () => {
  const { deps, calls } = fakeMachine({}, { snippets: SNIPPETS })
  deps.snippets = async () => { throw new Error("must not be asked") }
  const out = JSON.parse((await runCopilotTool("run_snippet", '{"session_id":"%2","session_label":"Prepare screenshots","name":"release"}', deps)).output)
  assert.match(out.error, /沒有執行/)
  assert.deepEqual(out.names.map((n: { name: string }) => n.name), ["Run the checks", "Daily release"])
  assert.equal(calls.some((c) => c.path.endsWith("/send")), false)
})

test("a Playbook the sheet would not start says why", async () => {
  const { deps } = fakeMachine({}, { snippets: SNIPPETS })
  deps.snippets = async () => "already_running"
  const out = JSON.parse((await runCopilotTool("run_snippet", '{"session_id":"%2","session_label":"Prepare screenshots","name":"Daily release"}', deps)).output)
  assert.match(out.error, /已經有一個 Playbook 在跑/)
})
