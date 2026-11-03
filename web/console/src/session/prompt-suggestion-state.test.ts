import { test } from "node:test"
import assert from "node:assert/strict"
import type { PromptSuggestion } from "@clawdline/contract"
// @ts-expect-error -- Node runs the source test with type stripping.
import { PromptSuggestionState } from "./prompt-suggestion-state.ts"

const prompt = (id: string, at: number, extra: Partial<PromptSuggestion> = {}): PromptSuggestion => ({
  id, text: "Run the fixture tests", source: "claude-terminal", conversation_id: "conversation-a",
  state: "available", observed_at: at, ...extra,
})

test("native suggestions stay outside a draft and order snapshots by their source version", () => {
  const state = new PromptSuggestionState("conversation-a")
  state.observe(prompt("new", 20))
  state.observe(prompt("old", 10))
  assert.equal(state.read()?.id, "new")
  state.observe(prompt("other", 30, { conversation_id: "conversation-b" }))
  assert.equal(state.read()?.id, "new")
  state.observe(prompt("none", 21, { state: "none", text: "" }))
  assert.equal(state.read(), null)
  state.observe(prompt("new", 20))
  assert.equal(state.read(), null)
})

test("typing, accepting or dismissing never resurrects the same native suggestion", () => {
  const state = new PromptSuggestionState("conversation-a")
  state.observe(prompt("first", 10))
  state.dismiss()
  state.observe(prompt("first", 10))
  assert.equal(state.read(), null)
  state.observe(prompt("next-appearance", 11))
  assert.equal(state.read()?.id, "next-appearance", "a coalesced native clear must not hide a new identity")
  state.observe(prompt("clear", 12, { state: "none" }))
  state.observe(prompt("next", 13))
  assert.equal(state.read()?.id, "next")
})

test("typing before the initial read arrives withholds its late result", () => {
  const state = new PromptSuggestionState("conversation-a")
  state.dismiss()
  state.observe(prompt("late", 10))
  assert.equal(state.read(), null)
})

test("unknown native states and invalid versions cannot supply a completion", () => {
  for (const value of [prompt("bad", NaN), prompt("bad", -1), prompt("", 10), prompt("bad", 10, { text: "" })]) {
    const state = new PromptSuggestionState("conversation-a")
    state.observe(value)
    assert.equal(state.read(), null)
  }
})

test("a temporary disconnect clears the view while preserving a user dismissal", () => {
  const state = new PromptSuggestionState("conversation-a")
  state.observe(prompt("first", 10))
  state.pause()
  assert.equal(state.read(), null)
  state.observe(prompt("first", 10))
  assert.equal(state.read()?.id, "first")
  state.dismiss(); state.pause(); state.observe(prompt("first", 10))
  assert.equal(state.read(), null)
})
