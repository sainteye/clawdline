// A card a Session created on the person's message says so:
// `node --test --experimental-strip-types web/console/src/pages/work/created-via.test.ts`.
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { claimedViaLine, clockOf, createdViaLine, epicOwnerLine } from "./words.ts"

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

test("a card its Session claimed on the person's message says so, in both languages", () => {
  assert.equal(claimedViaLine(via, "zh-Hant"), "Session 依你 09:05 的訊息認領")
  assert.equal(claimedViaLine(via, "en"), "Claimed by the Session from your message at 09:05")
  assert.equal(claimedViaLine(undefined, "en"), null)
  assert.equal(claimedViaLine({ run: "", session_id: "", at: 0 }, "zh-Hant"), null)
  assert.match(source, /<ClaimedViaNote item=\{item\} \/>/)
  assert.match(source, /claimedViaLine\(item\.claimed_via, undefined, persona \? personaName\(persona\) : undefined\)/)
})

test("a card a Session assigned to a new Session on the person's message names that and the persona", () => {
  const assigned = { ...via, assigned: true, persona: "security" }
  assert.equal(claimedViaLine(assigned, "en", "Security Engineer"),
    "Assigned by a Session from your message at 09:05; the new Session runs as Security Engineer")
  assert.equal(claimedViaLine(assigned, "zh-Hant", "資安工程師"), "Session 依你 09:05 的訊息指派，新 Session 的角色是資安工程師")
  // The catalog not yet read still names the persona, by its id.
  assert.equal(claimedViaLine(assigned, "en"), "Assigned by a Session from your message at 09:05; the new Session runs as security")
  assert.equal(claimedViaLine({ ...via, assigned: true }, "zh-Hant"), "Session 依你 09:05 的訊息指派")
  assert.match(source, /personaById\(personas, item\.claimed_via\?\.persona\)/)
})

test("an item the Epic's owner Session split out says so in both languages, and the card draws it", () => {
  const split = { run: "", session_id: "owner", at, epic_id: "epic-1" }
  assert.equal(epicOwnerLine(split, "zh-Hant"), "由 Epic 的負責 Session 在 09:05 拆分建立")
  assert.equal(epicOwnerLine(split, "en"), "Split out by the Epic's owner Session at 09:05")
  assert.equal(epicOwnerLine(undefined, "zh-Hant"), "由 Epic 的負責 Session 拆分建立")
  assert.equal(epicOwnerLine(null, "en"), "Split out by the Epic's owner Session")
  // Without a run, the person's-message line still says nothing.
  assert.equal(createdViaLine(split, "en"), null)
  assert.match(source, /workOrigin\(item\.created_by\) === "epic_owner" \? epicOwnerLine\(item\.created_via\) : createdViaLine\(item\.created_via\)/)
})
