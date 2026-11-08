import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner uses the source .ts extension.
import { MomentQueue, SkillLedger, receiptBelongs, skillSummary, type SessionSkillSnapshot } from "./skill-use.ts"
import type { SquadReceipt } from "../pages/squad/model.js"

const snapshot: SessionSkillSnapshot = {
  snapshotId: "snap.a", conversationId: "conversation.a", definitionId: "persona.a", scopeId: "repo.a",
  skills: [{ id: "skill.review", version: "v2", name: "Review" }, { id: "skill.plan", version: "v1", name: "Plan" }],
}
let next = 0
const receipt = (seq: number, change: Partial<SquadReceipt> = {}): SquadReceipt => ({
  seq, receiptId: "r" + seq + "." + next++, snapshotId: "snap.a", conversationId: "conversation.a", definitionId: "persona.a",
  scopeId: "repo.a", skillId: "skill.review", skillVersion: "v2", status: "read", at: seq * 1000, ...change,
})

test("receipts at or before the initial cursor show their state but never play", () => {
  const ledger = new SkillLedger()
  ledger.baseline(5)
  assert.equal(ledger.take(receipt(3, { status: "applied" }), true), null)
  assert.equal(ledger.usesFor(snapshot).get("skill.review")?.status, "applied")
  // A reload starts a new ledger at the same head: the same state, still nothing to play.
  const reloaded = new SkillLedger()
  reloaded.baseline(5)
  assert.equal(reloaded.take(receipt(3, { status: "applied" }), true), null)
  assert.equal(reloaded.usesFor(snapshot).get("skill.review")?.status, "applied")
})

test("a live read and then applied each play, and the Skill's state moves from read to applied in place", () => {
  const ledger = new SkillLedger()
  ledger.baseline(0)
  assert.equal(ledger.take(receipt(1, { status: "read" }), true)?.receipt.status, "read")
  const afterRead = ledger.usesFor(snapshot).get("skill.review")
  assert.equal(afterRead?.status, "read")
  assert.equal(ledger.take(receipt(2, { status: "applied", at: 9000 }), true)?.receipt.status, "applied")
  const afterApplied = ledger.usesFor(snapshot).get("skill.review")
  assert.equal(afterApplied?.status, "applied")
  // One card per Skill: it keeps the first receipt's place and time.
  assert.equal(afterApplied?.firstAt, 1000)
  assert.equal(afterApplied?.firstSeq, 1)
  assert.equal(ledger.usesFor(snapshot).size, 1)
})

test("a failed receipt never plays; it is a still state, and an applied Skill stays applied", () => {
  const ledger = new SkillLedger()
  ledger.baseline(0)
  assert.equal(ledger.take(receipt(1, { skillId: "skill.plan", skillVersion: "v1", status: "failed" }), true), null)
  assert.equal(ledger.usesFor(snapshot).get("skill.plan")?.status, "failed")
  ledger.take(receipt(2, { status: "applied" }), true)
  ledger.take(receipt(3, { status: "failed" }), true)
  assert.equal(ledger.usesFor(snapshot).get("skill.review")?.status, "applied")
})

test("another Session's, another launch's or another version's receipt is not this Session's", () => {
  const ledger = new SkillLedger()
  ledger.baseline(0)
  const foreign = [
    receipt(1, { conversationId: "conversation.b" }), receipt(2, { snapshotId: "snap.old" }), receipt(3, { definitionId: "persona.b" }),
    receipt(4, { scopeId: "repo.b" }), receipt(5, { skillVersion: "v1" }), receipt(6, { skillId: "skill.other" }),
  ]
  for (const entry of foreign) {
    assert.equal(receiptBelongs(entry, snapshot), false)
    ledger.take(entry, true)
  }
  assert.equal(ledger.usesFor(snapshot).size, 0)
  assert.equal(receiptBelongs(receipt(7), snapshot), true)
  assert.equal(receiptBelongs(receipt(8), null), false)
})

test("a catch-up page after a failure, a repeated sequence and a repeated receipt do not play", () => {
  const ledger = new SkillLedger()
  ledger.baseline(0)
  assert.equal(ledger.take(receipt(1), false), null, "the feed is catching up")
  assert.equal(ledger.usesFor(snapshot).get("skill.review")?.status, "read")
  const live = receipt(2, { status: "applied" })
  assert.ok(ledger.take(live, true))
  assert.equal(ledger.take(live, true), null, "same sequence")
  assert.equal(ledger.take({ ...live, seq: 3 }, true), null, "same receipt ID")
})

test("nothing plays before the head has been read", () => {
  const ledger = new SkillLedger()
  assert.equal(ledger.take(receipt(1), true), null)
  assert.equal(ledger.baselined, false)
})

test("the summary splits the snapshot's Skills into used and not used", () => {
  const ledger = new SkillLedger()
  ledger.baseline(0)
  ledger.take(receipt(1, { status: "applied" }), true)
  const { used, unused } = skillSummary(snapshot, ledger.usesFor(snapshot))
  assert.deepEqual(used.map((skill) => skill.id), ["skill.review"])
  assert.deepEqual(unused.map((skill) => skill.id), ["skill.plan"])
  const failedOnly = new SkillLedger()
  failedOnly.baseline(0)
  failedOnly.take(receipt(1, { status: "failed" }), true)
  assert.deepEqual(skillSummary(snapshot, failedOnly.usesFor(snapshot)).used, [])
})

test("moments in a burst are queued and the playing one is cut short", () => {
  const shown: (string | null)[] = []
  const timers: { at: number; run: () => void }[] = []
  let now = 0
  const clock = {
    set: ((run: () => void, ms: number) => { const timer = { at: now + ms, run }; timers.push(timer); return timer }) as unknown as typeof setTimeout,
    clear: ((timer: { at: number }) => { const index = timers.indexOf(timer as never); if (index >= 0) timers.splice(index, 1) }) as unknown as typeof clearTimeout,
  }
  const advance = (ms: number) => {
    now += ms
    for (;;) {
      timers.sort((a, b) => a.at - b.at)
      if (!timers.length || timers[0].at > now) return
      timers.shift()!.run()
    }
  }
  const queue = new MomentQueue<string>((moment) => shown.push(moment), 1600, 450, clock)
  queue.push("read")
  assert.deepEqual(shown, ["read"])
  advance(100)
  queue.push("applied")
  advance(449)
  assert.deepEqual(shown, ["read"])
  advance(1)
  assert.deepEqual(shown, ["read", "applied"])
  advance(1600)
  assert.deepEqual(shown, ["read", "applied", null])
})
