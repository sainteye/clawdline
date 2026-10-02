import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"

const source = readFileSync(new URL("./UserMessages.tsx", import.meta.url), "utf8")

test("opening My messages does not put the keyboard in the search field", () => {
  assert.doesNotMatch(source, /search\.current\?\.focus\(/)
  // Focus still enters the sheet, so its own Escape handler keeps closing it.
  assert.match(source, /closeButton\.current\?\.focus\(\{ preventScroll: true \}\)/)
  assert.match(source, /id="user-messages-close"[\s\S]*?ref=\{closeButton\}/)
})
