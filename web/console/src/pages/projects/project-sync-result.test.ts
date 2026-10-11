import { test } from "node:test"
import assert from "node:assert/strict"
import {
  describeSyncResult, syncStateWord,
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
} from "./project-sync-result.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { withCatalog } from "../../catalog-testing.ts"

withCatalog("zh-Hant")

// The answer a mirror gives for a repository it does not have. A Go build
// that still sends nil slices spells all three lists `null`.
const missingFromAnOlderMachine = {
  repo: "github.com/acme/shop", state: "missing", revision: "r1",
  written: null, deleted: null, kept: null,
}

test("a mirror that does not have the repository says so, whatever shape its lists arrive in", () => {
  const said = describeSyncResult(missingFromAnOlderMachine)
  assert.equal(said, "這台沒有這個 repo")
  assert.doesNotMatch(said, /null|undefined/)
  assert.equal(describeSyncResult({ repo: "github.com/acme/shop", state: "missing", revision: "r1" }), said)
})

test("an apply counts what it wrote and names what it kept", () => {
  const said = describeSyncResult({
    repo: "github.com/acme/shop", state: "applied", revision: "r2",
    written: [".claude/skills/a/SKILL.md", "CLAUDE.local.md"], deleted: [".claude/commands/old.md"],
    kept: [{ path: ".claude/skills/mine/SKILL.md", reason: "local_edit" }],
  })
  assert.match(said, /已同步/)
  assert.match(said, /2/)
  assert.match(said, /1/)
  assert.match(said, /\.claude\/skills\/mine\/SKILL\.md/)
})

test("a state or a reason this console has no word for is shown as it arrived", () => {
  assert.equal(syncStateWord("something_newer"), "something_newer")
  assert.equal(syncStateWord(undefined), "")
  assert.match(describeSyncResult({ state: "applied", written: [], deleted: [], kept: [{ path: "p", reason: "newer_reason" }] }),
    /newer_reason/)
})

test("an answer that is not an object at all is still a sentence, not a throw", () => {
  assert.equal(describeSyncResult(undefined), "")
  assert.equal(describeSyncResult(null), "")
  assert.equal(describeSyncResult({ state: "applied", written: 3 }), "已同步")
})
