import { test } from "node:test"
import assert from "node:assert/strict"

// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { MEMORY_MAX_AGE_S, MEMORY_TURNS, MEMORY_TURN_CHARS, memorySection, parseMemory, remember } from "./copilot-memory.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { addUsage, emptyCost, formatUSD, usageUSD, RATES } from "./copilot-cost.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { greetingEvent, sessionUpdateEvent, COPILOT_INSTRUCTIONS } from "./copilot-tools.ts"

const NOW = 2_000_000_000

test("the next call is told what was said before, oldest first, as background", () => {
  let turns = remember([], "user", "雲端配對那個做完了嗎？", NOW - 600)
  turns = remember(turns, "assistant", "做完了，在等你確認裝置頁。", NOW - 590)
  const section = memorySection(turns, NOW)
  assert.match(section, /^【之前的通話】/)
  assert.match(section, /（10 分鐘前）使用者：雲端配對那個做完了嗎？/)
  assert.match(section, /你：做完了，在等你確認裝置頁。/)
  assert.ok(section.indexOf("使用者：") < section.indexOf("你："))
  const ev = sessionUpdateEvent(section) as { session: { instructions: string } }
  assert.ok(ev.session.instructions.startsWith(COPILOT_INSTRUCTIONS))
  assert.ok(ev.session.instructions.endsWith(section))
})

test("with nothing remembered the instructions are unchanged", () => {
  assert.equal(memorySection([], NOW), "")
  assert.equal((sessionUpdateEvent("") as { session: { instructions: string } }).session.instructions, COPILOT_INSTRUCTIONS)
})

test("a remembered call opens without the introduction", () => {
  const first = (greetingEvent(false) as { response: { instructions: string } }).response.instructions
  const again = (greetingEvent(true) as { response: { instructions: string } }).response.instructions
  assert.match(first, /說你可以回報/)
  assert.match(again, /不要自我介紹/)
})

test("memory keeps only the newest turns, each clipped, none too old", () => {
  let turns: ReturnType<typeof remember> = []
  for (let i = 0; i < MEMORY_TURNS + 10; i++) turns = remember(turns, "user", `line ${i}`, NOW - 100 + i)
  assert.equal(turns.length, MEMORY_TURNS)
  assert.equal((turns.at(-1) as { text: string }).text, `line ${MEMORY_TURNS + 9}`)
  const long = remember([], "assistant", "字".repeat(MEMORY_TURN_CHARS + 50), NOW)
  assert.equal((long[0] as { text: string }).text.length, MEMORY_TURN_CHARS + 1)
  const old = remember([{ role: "user", text: "old", at: NOW - MEMORY_MAX_AGE_S - 1 }], "user", "new", NOW)
  assert.deepEqual(old.map((t: { text: string }) => t.text), ["new"])
  assert.deepEqual(remember([], "user", "   ", NOW), [], "silence is not a turn")
})

test("a damaged stored memory is no memory", () => {
  assert.deepEqual(parseMemory("not json"), [])
  assert.deepEqual(parseMemory('{"a":1}'), [])
  assert.deepEqual(parseMemory('[{"role":"user","text":"ok","at":1},{"role":"x","text":"no","at":1},{"role":"user"}]'),
    [{ role: "user", text: "ok", at: 1 }])
})

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
