// A card a Session created on the person's message says so:
// `node --test --experimental-strip-types web/console/src/pages/work/created-via.test.ts`.
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { clockOf, createdViaLine } from "./words.ts"

const source = readFileSync(new URL("./WorkV2.tsx", import.meta.url), "utf8")
const styles = readFileSync(new URL("./work.css", import.meta.url), "utf8")

const at = Math.floor(new Date(2026, 8, 25, 9, 5).getTime() / 1000)
const via = { run: "0f0f0f0f-0000-4000-8000-000000000001", session_id: "conv", at, excerpt: "Put the release on the Board" }

test("the line names the local time of the person's message in both languages", () => {
  assert.equal(clockOf(at), "09:05")
  assert.equal(createdViaLine(via, "zh-Hant"), "Session 依你 09:05 的訊息建立")
  assert.equal(createdViaLine(via, "en"), "Created by the Session from your message at 09:05")
})

test("a person's own item carries no line", () => {
  assert.equal(createdViaLine(undefined, "zh-Hant"), null)
  assert.equal(createdViaLine(null, "en"), null)
  assert.equal(createdViaLine({ run: "", session_id: "", at: 0 }, "en"), null)
})

test("the card draws the line with the excerpt as its tooltip and quotes it when opened", () => {
  assert.match(source, /function CreatedViaNote/)
  assert.match(source, /<CreatedViaNote item=\{item\} \/>/)
  assert.match(source, /createdViaLine\(item\.created_via\)/)
  assert.match(source, /<summary title=\{excerpt\}>\{line\}<\/summary>/)
  assert.match(source, /<blockquote aria-label=\{workWord\("createdViaQuote"\)\}>\{excerpt\}<\/blockquote>/)
  assert.match(styles, /\.work-created-via blockquote/)
})

test("an item without an excerpt still says who created it, with nothing to open", () => {
  assert.match(source, /if \(!excerpt\) return <p className="work-created-via">\{line\}<\/p>/)
})
