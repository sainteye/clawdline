import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node source tests use the source extension.
import { webAddress } from "./browser-address.ts"

test("a bare host gains https and a web address is kept", () => {
  assert.equal(webAddress("github.com/settings"), "https://github.com/settings")
  assert.equal(webAddress("  example.com  "), "https://example.com/")
  assert.equal(webAddress("http://localhost:3000/x?y=1"), "http://localhost:3000/x?y=1")
  assert.equal(webAddress("localhost:3000"), "https://localhost:3000/")
})

test("other schemes, spaces and empty input are refused", () => {
  for (const raw of ["", "   ", "javascript:alert(1)", "JavaScript:alert(1)", "file:///etc/passwd", "chrome://settings", "data:text/html,hi", "about:blank", "ftp://example.com", "two words"]) {
    assert.equal(webAddress(raw), null, raw)
  }
})
