import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes the source file directly.
import { decodeToolDraft, encodeToolDraft, filterProjects, localized, repositoryURL, sourceLink, toolMessage, toolTarget, type Project, type ToolAttachment } from "./model.ts"

test("tool attachments preserve a draft across reload and refuse another execution", () => {
  const target = toolTarget({ id: "%1", sessionId: "conversation-a", execution_generation: "generation-a" }, "machine-a")
  const a: ToolAttachment = { id: "selection", target, title: "Example", prompt: "Install, verify, then use the pinned Skill.", revision: "revision", mode: "task" }
  const saved = encodeToolDraft("My existing text\nNew task", a)
  const restored = decodeToolDraft(saved)
  assert.deepEqual(restored, { text: "My existing text\nNew task", attachment: a })
  assert.match(toolMessage(restored.text, restored.attachment, target), /Install, verify/)
  assert.throws(() => toolMessage(restored.text, a, toolTarget({ id: "%1", sessionId: "conversation-a", execution_generation: "generation-b" }, "machine-a")), /stale_tool_target/)
  assert.throws(() => toolMessage(restored.text, a, toolTarget({ id: "%1", sessionId: "conversation-b", execution_generation: "generation-a" }, "machine-a")), /stale_tool_target/)
  assert.throws(() => toolMessage(restored.text, a, toolTarget({ id: "%1", sessionId: "conversation-a", execution_generation: "generation-a" }, "machine-b")), /stale_tool_target/)
  assert.equal(encodeToolDraft("Plain draft", null), "Plain draft")
  assert.deepEqual(decodeToolDraft("CLAWDLINE_TOOL_DRAFT_V1\nnot json"), { text: "CLAWDLINE_TOOL_DRAFT_V1\nnot json", attachment: null })
})

test("search finds actual Skill names and either maintained language", () => {
  const p = { owner: "example", repo: "skills", repo_url: "https://github.com/example/skills", name: { en: "Example", "zh-Hant": "範例" }, summary: { en: "Review" }, category_id: "security", category: {}, skills: [{ native_name: "check-auth", path: "auth/SKILL.md", name: { en: "Check auth" }, purpose: { "zh-Hant": "權限審查" } }] } as unknown as Project
  for (const q of ["check-auth", "權限", "範例", "auth/SKILL"]) assert.equal(filterProjects([p], "security", q, "en").length, 1)
  assert.equal(filterProjects([p], "design", "auth", "en").length, 0)
  assert.equal(localized(p.name, "zh-Hant-TW"), "範例")
  assert.equal(localized(p.name, "fr"), "Example")
})

test("repository entry rejects credential and arbitrary download URLs", () => {
 const userinfo = new URL("https://github.com/a/b"); userinfo.username = "fixture"
  assert.equal(repositoryURL("https://github.com/example/skills.git/")?.url, "https://github.com/example/skills")
  for (const value of [userinfo.href, "http://github.com/a/b", "https://github.com.evil.test/a/b", "https://github.com/a/b/tree/main", "https://github.com/a/b?token=secret"]) assert.equal(repositoryURL(value), null)
})


test("source links cannot carry script schemes or credentials", () => {
 const userinfo = new URL("https://github.com/a/b"); userinfo.username = "fixture"
  for (const url of ["javascript:alert(1)","data:text/html,a",userinfo.href]) assert.equal(sourceLink(url),undefined)
  assert.equal(sourceLink("https://github.com/a/b"),"https://github.com/a/b")
})
