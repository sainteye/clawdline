import { test } from "node:test"
import assert from "node:assert/strict"

// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { ACTIVITY_TOTAL_CHARS, COPILOT_TOOLS, SCREEN_READ_ATTEMPTS, activityOf, plainScreenText, runCopilotTool, sessionUpdateEvent, toolReplyEvents, type ToolDeps } from "./copilot-tools.ts"

// Rows in the shape `GET /v1/sessions` answers (the daemon's row and the Cloud
// reader's snapshot carry the same keys).
const ROWS = [
  { id: "owned:acp:AAAA", label: "Refactor the billing page", assistant: "antigravity", state: "working", work_state: "working", cwd: "/x" },
  { id: "%2", label: "Prepare screenshots", assistant: "codex", state: "idle", work_state: "unknown", cwd: "/y" },
  { id: "%7", label: "Fix the flaky test", assistant: "claude", state: "waiting", work_state: "waiting", cwd: "/z" },
  { id: "%9", label: "Fix the login test", assistant: "claude", state: "working", work_state: "working", cwd: "/z" },
]

// A capture as the daemon sends it: SGR colour runs and trailing blank rows.
const PAINTED =
  "\x1b[1m$\x1b[0m go test ./...\n" +
  "ok  \tgithub.com/example/pkg\t0.42s\n" +
  "\x1b[38;2;143;179;239m--- FAIL: TestLogin\x1b[39m (0.01s)\n" +
  "    login_test.go:12: want 200, got 500   \n" +
  "\x1b[1m?\x1b[0m for shortcuts\n\n\n"

interface Call { path: string; init?: RequestInit }

function fakeMachine(screens: Record<string, unknown[]>, opts: { sessionsStatus?: number; transcripts?: Record<string, unknown> } = {}) {
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
      return new Response("", { status: 404 })
    },
    async sleep(ms) { sleeps.push(ms) },
    newRequestID: () => "req-1",
  }
  return { deps, calls, sleeps }
}

test("list_sessions reports every row by name and counts them by state", async () => {
  const { deps, calls } = fakeMachine({})
  const { output, logs } = await runCopilotTool("list_sessions", "{}", deps)
  const out = JSON.parse(output)
  assert.deepEqual(calls.map((c) => c.path), ["/v1/sessions"])
  assert.equal(out.count, 4)
  assert.deepEqual(out.by_state, { working: 2, idle: 1, waiting: 1 })
  assert.deepEqual(out.sessions[1], { id: "%2", label: "Prepare screenshots", assistant: "codex", state: "idle", work_state: "unknown" })
  assert.ok(logs.some((l) => l.level === "info" && l.text.includes("共 4 個")))
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
  const { output } = await runCopilotTool("get_session_screen", JSON.stringify({ session_id: "codex" }), deps)
  const out = JSON.parse(output)
  assert.deepEqual(calls.map((c) => c.path), ["/v1/sessions", "/v1/sessions/%252/screen", "/v1/sessions/%252/screen"])
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

test("an ambiguous name is put back to the person, not guessed", async () => {
  const { deps, calls } = fakeMachine({})
  const out = JSON.parse((await runCopilotTool("get_session_screen", '{"session_id":"claude"}', deps)).output)
  assert.match(out.error, /2 個 Sessions/)
  assert.match(out.error, /Fix the flaky test/)
  assert.match(out.error, /Fix the login test/)
  assert.deepEqual(out.candidates, [{ id: "%7", label: "Fix the flaky test" }, { id: "%9", label: "Fix the login test" }])
  assert.deepEqual(calls.map((c) => c.path), ["/v1/sessions"])
})

test("a name inside a label finds that one row", async () => {
  const { deps } = fakeMachine({ "%7": [{ pending: false, readable: true, text: "waiting for you" }] })
  const out = JSON.parse((await runCopilotTool("get_session_screen", '{"session_id":"flaky"}', deps)).output)
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
  const out = JSON.parse((await runCopilotTool("send_message_to_session", '{"session_id":"codex","message":"run the tests"}', deps)).output)
  assert.equal(out.success, true)
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
  assert.equal(ev.session.audio.output.voice, "alloy")
  for (const beta of ["modalities", "voice", "input_audio_transcription"]) assert.equal(beta in ev.session, false, beta)
  assert.deepEqual(ev.session.tools.map((t: { name: string }) => t.name), COPILOT_TOOLS.map((t) => t.name))
  assert.deepEqual(COPILOT_TOOLS.map((t) => t.name), ["list_sessions", "get_session_activity", "get_session_screen", "send_message_to_session"])
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
  const { output, logs } = await runCopilotTool("get_session_activity", '{"session_id":"flaky"}', deps)
  const out = JSON.parse(output)
  assert.deepEqual(calls.map((c) => c.path).slice(0, 2), ["/v1/sessions", "/v1/transcript?session=%257"])
  assert.equal(out.label, "Fix the flaky test")
  assert.equal(out.state, "waiting")
  assert.deepEqual(out.recent_activity.map((e: { role: string }) => e.role), ["user", "assistant", "tool", "tool", "assistant"])
  assert.equal(out.recent_activity[0].text, "Make the flaky login test pass and push it.")
  assert.equal(out.recent_activity.at(-1).text, "Twenty runs pass now. Should I push to main?")
  assert.equal(out.recent_activity[2].tool, "Bash")
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
