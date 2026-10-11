// The press key: `node --test web/console/src/press-key.test.ts`.
//
// What is asserted is what a Session write depends on, not how the id is
// spelled: two presses are two requests, and every one of them is a header
// value — a key with a space or a newline in it would be dropped by `fetch`
// before the daemon ever saw it, and the write would be refused as unkeyed.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see cloud/relay-writer.test.ts.
import { pressKey } from "./press-key.ts"

test("two presses are two requests, each one a header value", () => {
  const keys = new Set<string>()
  for (let i = 0; i < 1_000; i += 1) {
    const key = pressKey("model")
    assert.match(key, /^[\x21-\x7e]+$/, "a key travels as a header value")
    assert.equal(key.trim(), key)
    keys.add(key)
  }
  assert.equal(keys.size, 1_000, "no press repeats another's request")
})

test("a machine with no randomUUID still mints a request per press", () => {
  const real = globalThis.crypto
  // A console served over plain http on a LAN: `crypto.randomUUID` is absent
  // there, and a press that minted nothing would be refused across Cloud.
  Object.defineProperty(globalThis, "crypto", {
    value: { getRandomValues: real.getRandomValues.bind(real) }, configurable: true,
  })
  try {
    const keys = new Set<string>()
    for (let i = 0; i < 100; i += 1) keys.add(pressKey("bar"))
    assert.equal(keys.size, 100)
    for (const key of keys) assert.match(key, /^bar-\d+-[0-9a-f]+$/)
  } finally {
    Object.defineProperty(globalThis, "crypto", { value: real, configurable: true })
  }
})
