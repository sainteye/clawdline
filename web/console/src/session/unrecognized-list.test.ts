import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for Node's type-strip runner.
import { nextWord } from "../next-strings.ts"

test("an unrecognized session uses a short list label and keeps the explanation in detail", () => {
  const original = Object.getOwnPropertyDescriptor(globalThis, "document")
  try {
    for (const [lang, limit] of [["zh-Hant", 6], ["en", 20]] as const) {
      Object.defineProperty(globalThis, "document", { configurable: true, value: { documentElement: { lang } } })
      const list = nextWord("sessionStateUnrecognizedList")
      const detail = nextWord("sessionStateUnrecognizedDetail")
      assert.ok(list.length <= limit, `${lang}: list label is ${list.length} characters: ${list}`)
      assert.ok(detail.length > list.length, `${lang}: detail keeps the explanation`)
    }
  } finally {
    if (original) Object.defineProperty(globalThis, "document", original)
    else Reflect.deleteProperty(globalThis, "document")
  }
})
