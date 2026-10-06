import { strict as assert } from "node:assert"
import { test } from "node:test"
import { displayRefusal } from "./http-refusal-display.mjs"

test("command examples remain readable text without HTML-looking brackets", () => {
  const examples = [
    ["http.32a3b0c1ae85abed", 'Send {"version": "<the plan\'s version>"}.'],
    ["http.3ad9765394f45b04", 'Send {"files": {"<schedule-id>.json": "<file contents>"}}.'],
    ["http.b83800283d88b391", 'duplicate of <id>'],
    ["http.bb96b476b96bba73", '/v1/usage/sessions/<conversation>'],
    ["http.dd9496ebcab9dfee", '/v1/orchestrator/sessions/<conversation id>/run'],
    ["http.df99d615a86cecb5", 'conversation:<lowercase UUID>'],
  ]
  for (const [key, example] of examples) {
    const text = displayRefusal(key, example)
    assert.equal(text, example.replace(/<([^<>]+)>/gu, "‹$1›"))
    assert.doesNotMatch(text, /[<>]/u)
  }
  assert.equal(displayRefusal("http.unlisted", "<script>"), "<script>")
  assert.throws(() => displayRefusal("http.32a3b0c1ae85abed", "<id"), /unpaired angle bracket/u)
})
