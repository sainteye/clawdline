import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node's strip-types runner.
import { batchReadingWords, retainedStateWords, totalSessionWords } from "./session-reading.ts"

const source = { observed_at: 1_000, provenance: "iterm", freshness: "unverified" as const }

test("one failed batch keeps each row's last state and age", () => {
  assert.equal(retainedStateWords({ source, state: "working" }, 1_120, true), "2 分鐘前在跑")
  assert.equal(retainedStateWords({ source, state: "waiting" }, 1_120, true), "2 分鐘前在等你回答")
  assert.equal(retainedStateWords({ source: { ...source, freshness: "current" }, state: "working" }, 1_120, true), null)
})

test("the batch owns the failure sentence", () => {
  assert.equal(batchReadingWords(source, 1_120, true), "這次沒有讀完整；其中保留的內容最後在 2 分鐘前讀到。")
  assert.equal(
    batchReadingWords({ observed_at: 0, provenance: "iterm", freshness: "missing" }, 1_120, true),
    "這次沒有讀完整；至少一個來源沒有仍可採用的上次讀數。",
  )
})

test("the title's number is the same set as the list", () => {
  assert.equal(totalSessionWords(6, true), "6 個 session")
  assert.equal(totalSessionWords(1, false), "1 session")
})
