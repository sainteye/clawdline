import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node loads the TypeScript module directly.
import { addUsage, emptyCost, formatUSD, usageUSD, RATES } from "./copilot-cost.ts"

// The second response of our first live run against gpt-realtime-mini.
const USAGE = {
  total_tokens: 1938, input_tokens: 1233, output_tokens: 705,
  input_token_details: { text_tokens: 1233, audio_tokens: 0, image_tokens: 0, cached_tokens: 0,
    cached_tokens_details: { text_tokens: 0, audio_tokens: 0, image_tokens: 0 } },
  output_token_details: { text_tokens: 182, audio_tokens: 523 },
}

test("a response's usage is priced by kind of token", () => {
  const usd = usageUSD(USAGE, RATES["gpt-realtime-mini"])
  // 1233 text in × 0.60 + 182 text out × 2.40 + 523 audio out × 20.00, per 1M tokens.
  assert.equal(usd.toFixed(8), ((1233 * 0.6 + 182 * 2.4 + 523 * 20) / 1e6).toFixed(8))
})

test("cached input is priced at the cached rate, not twice", () => {
  const usage = { input_tokens: 1962, output_tokens: 0,
    input_token_details: { text_tokens: 1439, audio_tokens: 523, cached_tokens: 1920, cached_tokens_details: { text_tokens: 1408, audio_tokens: 512 } },
    output_token_details: {} }
  const r = RATES["gpt-realtime-mini"]
  assert.equal(usageUSD(usage, r).toFixed(10),
    (((1439 - 1408) * r.textIn + 1408 * r.textCached + (523 - 512) * r.audioIn + 512 * r.audioCached) / 1e6).toFixed(10))
})

test("a call's cost adds up its responses and says when a model is unpriced", () => {
  let cost = emptyCost("gpt-realtime-mini")
  cost = addUsage(cost, USAGE)
  cost = addUsage(cost, USAGE)
  assert.equal(cost.responses, 2)
  assert.equal(cost.outputTokens, 1410)
  assert.equal(cost.audioOutTokens, 1046)
  assert.equal(cost.priced, true)
  assert.equal(formatUSD(cost.usd), "US$0.023")
  assert.equal(formatUSD(0.0042), "US$0.0042")
  assert.equal(addUsage(emptyCost("some-new-model"), USAGE).priced, false)
  assert.equal(formatUSD(0.1234), "US$0.123")
})
