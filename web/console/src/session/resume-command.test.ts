import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { resumeCommandQuery, resumeCommandPlace } from "./resume-command.ts"

test("Claude and Codex resume invocations become picker searches", () => {
  for (const assistant of ["claude", "codex"]) {
    assert.equal(resumeCommandQuery(" /resume ", assistant), "")
    assert.equal(resumeCommandQuery("/resume reconnect loop", assistant), "reconnect loop")
    assert.equal(resumeCommandQuery("/resume\tconversation-id", assistant), "conversation-id")
  }
})

test("ordinary prompts, other providers and picture messages keep their send semantics", () => {
  for (const text of ["explain /resume", "/resume-later", "/resum", "/Resume", "/resume\nthen explain"]) {
    assert.equal(resumeCommandQuery(text, "claude"), null, text)
  }
  assert.equal(resumeCommandQuery("/resume", "antigravity"), null)
  assert.equal(resumeCommandQuery("/resume", undefined), null)
  assert.equal(resumeCommandQuery("/resume", "codex", 1), null)
})

test("resume chooses a containing Project without confusing sibling path prefixes", () => {
  const root = { path: "/work" }, project = { path: "/work/api/" }, nested = { path: "/work/api/nested" }
  assert.equal(resumeCommandPlace([root, project, nested], "/work/api/nested/src"), nested)
  assert.equal(resumeCommandPlace([project], "/work/api"), project)
  assert.equal(resumeCommandPlace([project], "/work/api-other"), null)
  assert.equal(resumeCommandPlace([project], undefined), null)
  assert.equal(resumeCommandPlace([{ path: "C:\\work\\api" }], "C:\\work\\api\\src")?.path, "C:\\work\\api")
})
