// `node --test web/console/src/session/place-routes.test.ts`
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { resumePath, startPath } from "./place-routes.ts"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { writeRoute } from "../cloud/relay-writer.ts"

test("a start names its persona only after an assistant", () => {
  assert.equal(startPath("p1"), "/v1/places/p1/start")
  assert.equal(startPath("p1", "codex"), "/v1/places/p1/start/codex")
  assert.equal(startPath("p1", null, undefined, "architect"), "/v1/places/p1/start/claude/as/architect")
  assert.equal(startPath("p1", "codex", "gpt-5", "security"), "/v1/places/p1/start/codex/gpt-5/as/security")
})

test("a resume without a persona is the route it always was", () => {
  assert.equal(resumePath("p1", "abc"), "/v1/places/p1/resume/abc")
  assert.equal(resumePath("p1", "abc", "codex"), "/v1/places/p1/resume/codex/abc")
  assert.equal(resumePath("p1", "abc", null, ""), "/v1/places/p1/resume/abc")
})

test("a resume with a persona names its assistant, defaulting to claude", () => {
  assert.equal(resumePath("p1", "abc", null, "architect"), "/v1/places/p1/resume/claude/abc/as/architect")
  assert.equal(resumePath("p1", "abc", "codex", "code-reviewer"), "/v1/places/p1/resume/codex/abc/as/code-reviewer")
  assert.equal(resumePath("p/1", "a b", "claude", "x/y"), "/v1/places/p%2F1/resume/claude/a%20b/as/x%2Fy")
})

test("Cloud reads every resume path the console builds as the same resume", () => {
  const cases: [string, { assistant: string; past: string; persona: string }][] = [
    [resumePath("p1", "abc"), { assistant: "", past: "abc", persona: "" }],
    [resumePath("p1", "abc", "codex"), { assistant: "codex", past: "abc", persona: "" }],
    [resumePath("p1", "abc", null, "architect"), { assistant: "claude", past: "abc", persona: "architect" }],
    [resumePath("p1", "abc", "codex", "code-reviewer"), { assistant: "codex", past: "abc", persona: "code-reviewer" }],
  ]
  for (const [path, want] of cases) {
    const route = writeRoute("POST", path)
    assert.ok(route && route.op === "resume", path)
    if (route?.op !== "resume") continue
    assert.deepEqual({ place: route.place, assistant: route.assistant, past: route.past, persona: route.persona }, { place: "p1", ...want }, path)
  }
})

test("Cloud reads every start path the console builds with its persona", () => {
  const route = writeRoute("POST", startPath("p1", null, undefined, "architect"))
  assert.ok(route?.op === "start" && route.assistant === "claude" && route.persona === "architect")
  const bare = writeRoute("POST", startPath("p1", "codex"))
  assert.ok(bare?.op === "start" && bare.persona === "")
})
