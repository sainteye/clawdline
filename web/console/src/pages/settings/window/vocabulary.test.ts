// The voice vocabulary as the settings card sends it back:
// `node --test web/console/src/pages/settings/window/vocabulary.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { cleanVocabulary, validTerm, VOICE_VOCABULARY_TERMS } from "./vocabulary.ts"

test("a hand-edited list is cleaned to what the daemon accepts", () => {
  assert.deepEqual(
    cleanVocabulary([" Tailscale ", "tailscale", "PostgreSQL", "", "two\nlines", "字".repeat(65), 7, "台北"]),
    ["Tailscale", "PostgreSQL", "台北"],
  )
  assert.deepEqual(cleanVocabulary("Tailscale"), [])
  assert.equal(cleanVocabulary(Array.from({ length: 250 }, (_, i) => "term" + i)).length, VOICE_VOCABULARY_TERMS)
})

test("a term is one line of at most 64 characters", () => {
  assert.equal(validTerm("字".repeat(64)), true)
  assert.equal(validTerm("字".repeat(65)), false)
  assert.equal(validTerm(" padded"), false)
  assert.equal(validTerm("tab\there"), false)
})
