import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node's strip-types test runner needs the source extension.
import { deletePlaybook, createPlaybook, updatePlaybook } from "./snippets-api.ts"
import english from "../../public/catalogs/en.json" with { type: "json" }
import traditionalChinese from "../../public/catalogs/zh-Hant.json" with { type: "json" }

test("deletePlaybook targets clean endpoint without trailing empty machine parameter", async () => {
  const previous = globalThis.fetch
  const calls: { url: string; method: string }[] = []
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ url: String(input), method: init?.method ?? "GET" })
    return new Response(JSON.stringify({ ok: true, deleted: "test-id" }), { status: 200 })
  }) as typeof fetch

  try {
    // 1. Without machine or with empty string
    await deletePlaybook("", "playbook-123")
    assert.equal(calls.length, 1)
    assert.equal(calls[0].url, "/v1/snippets/playbook-123")
    assert.equal(calls[0].method, "DELETE")

    // 2. With undefined / null machine
    await deletePlaybook(undefined, "playbook-456")
    assert.equal(calls.length, 2)
    assert.equal(calls[1].url, "/v1/snippets/playbook-456")
    assert.equal(calls[1].method, "DELETE")

    // 3. With a remote machine
    await deletePlaybook("remote-host", "playbook-789")
    assert.equal(calls.length, 3)
    assert.equal(calls[2].url, "/v1/snippets/playbook-789?machine=remote-host")
    assert.equal(calls[2].method, "DELETE")
  } finally {
    globalThis.fetch = previous
  }
})

test("createPlaybook and updatePlaybook omit empty machine query string", async () => {
  const previous = globalThis.fetch
  const calls: { url: string; method: string; body: string }[] = []
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ url: String(input), method: init?.method ?? "GET", body: String(init?.body ?? "") })
    return new Response(JSON.stringify({ id: "p1", title: "Test", body: "Body", scope: "playbook" }), { status: 200 })
  }) as typeof fetch

  try {
    await createPlaybook("", "project-a", "Title", "Body")
    assert.equal(calls[0].url, "/v1/snippets")
    assert.equal(calls[0].method, "POST")

    await updatePlaybook("", "p1", "New Title", "New Body")
    assert.equal(calls[1].url, "/v1/snippets/p1")
    assert.equal(calls[1].method, "PATCH")
  } finally {
    globalThis.fetch = previous
  }
})

test("in-place delete confirmation labels exist in supported catalogs", () => {
  assert.equal(traditionalChinese["legacy.webSnippetDeleteAsk"], "再按一次就刪除")
  assert.equal(english["legacy.webSnippetDeleteAsk"], "Press again to delete")
})
