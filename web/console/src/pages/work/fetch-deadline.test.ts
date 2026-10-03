import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strips source TypeScript in the browser-free test.
import { fetchWithDeadline } from "./fetch-deadline.ts"

test("a hosted fetch that ignores abort cannot hold the project page forever", async () => {
  const previous = globalThis.fetch
  globalThis.fetch = () => new Promise<Response>(() => {})
  try {
    await assert.rejects(fetchWithDeadline("/v1/places?machine=one", {}, 1), { name: "AbortError" })
  } finally { globalThis.fetch = previous }
})
