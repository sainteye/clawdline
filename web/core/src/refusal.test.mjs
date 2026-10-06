import assert from "node:assert/strict"
import test from "node:test"

import { makeJSONFetch, RefusalError } from "./refusal.ts"

test("flat and nested refusals expose a key without changing their raw detail", () => {
  const flat = new RefusalError(409, {
    error: "close_not_proven",
    detail: "Read this Session again.",
    detail_key: "http.0123456789abcdef",
  })
  assert.equal(flat.status, 409)
  assert.equal(flat.code, "close_not_proven")
  assert.equal(flat.detail, "Read this Session again.")
  assert.equal(flat.detailKey, "http.0123456789abcdef")

  const nested = new RefusalError(403, {
    error: {
      code: "forbidden",
      message: "This request is forbidden.",
      detail_key: "http.fedcba9876543210",
    },
  })
  assert.equal(nested.code, "forbidden")
  assert.equal(nested.detail, "This request is forbidden.")
  assert.equal(nested.detailKey, "http.fedcba9876543210")

  const dynamic = new RefusalError(503, {
    error: "upstream_unavailable",
    detail: "The upstream did not answer in 12 seconds.",
  })
  assert.equal(dynamic.detailKey, undefined)
})

test("legacy JSON fetch keeps flat and nested wire detail separate from transport errors", async () => {
  const originalFetch = globalThis.fetch
  try {
    const request = makeJSONFetch({ words: { offline: "offline", requestFailed: "failed", notJSON: "not JSON" } })
    for (const body of [
      { error: "forbidden", detail: "The flat request is forbidden.", detail_key: "http.0011223344556677" },
      { error: { code: "forbidden", message: "The nested request is forbidden.", detail_key: "http.aabbccddeeff0011" } },
    ]) {
      globalThis.fetch = async () => new Response(JSON.stringify(body), {
        status: 403, headers: { "Content-Type": "application/json" },
      })
      const detail = typeof body.error === "string" ? body.detail : body.error.message
      const detailKey = typeof body.error === "string" ? body.detail_key : body.error.detail_key
      await assert.rejects(request("/v1/example"), (error) => {
        assert.equal(error.message, detail)
        assert.equal(error.detail, detail)
        assert.equal(error.code, "forbidden")
        assert.equal(error.detailKey, detailKey)
        return true
      })
    }
    globalThis.fetch = async () => { throw new Error("network down") }
    await assert.rejects(request("/v1/example"), (error) => {
      assert.equal(error.code, "offline")
      assert.equal(error.detail, undefined)
      assert.equal(error.detailKey, undefined)
      return true
    })
  } finally {
    globalThis.fetch = originalFetch
  }
})
