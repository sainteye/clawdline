// The outer envelope sequence a browser device signs with (`durableSequence`, copied
// `cloud-boot.js`), shared by every tab of that device through local storage:
// `node --test web/console/src/cloud/viewer-sequence.test.ts`.
//
// On 2026-10-06 (21:01) a reloaded Cloud terminal page typed a dozen keys, then the machine
// dropped every later envelope as `reason=replay` from seq 26112 (a 64-block boundary): another
// app.clawdline.com tab of the same device had reserved that block, and each tab, on running out
// of its own block, reserved the next one counted from its own last number instead of from the
// ceiling the other tab had written.
import { test } from "node:test"
import assert from "node:assert/strict"
import { durableSequence } from "../legacy/js/net/cloud-boot.js"

/** One device's local storage, shared by its tabs. */
function storage() {
  const values = new Map<string, string>()
  return { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) } }
}

/** The machine's per-sender window (`internal/domain/cloud/replay.go`): a number is taken once, and none 1024 below the highest. */
function machine() {
  let highest = -1
  const seen = new Set<number>()
  return (seq: number): boolean => {
    if (seen.has(seq) || (highest >= 0 && seq < highest - 1024)) return false
    seen.add(seq); highest = Math.max(highest, seq)
    return true
  }
}

test("two tabs of one device, one of them reloaded, never sign the same sequence", () => {
  const shared = storage()
  const claim = machine()
  const other = durableSequence(shared, "seq")
  const send = (tab: () => number, what: string) => { const seq = tab(); assert.ok(claim(seq), `${what} seq ${seq} was dropped as a replay`) }
  for (let i = 0; i < 10; i++) send(other, "the other tab")
  let page = durableSequence(shared, "seq")
  for (let i = 0; i < 30; i++) send(page, "the terminal page")
  page = durableSequence(shared, "seq") // reloaded
  for (let round = 0; round < 300; round++) {
    send(page, "the reloaded page")
    if (round % 3 === 0) send(other, "the other tab")
  }
})

test("a tab that sat idle while another sent past the machine's window is not left below it", () => {
  const shared = storage()
  const claim = machine()
  const idle = durableSequence(shared, "seq")
  assert.ok(claim(idle()))
  const busy = durableSequence(shared, "seq")
  for (let i = 0; i < 2000; i++) assert.ok(claim(busy()))
  const seq = idle()
  assert.ok(claim(seq), `the idle tab's seq ${seq} fell off the machine's window`)
})

test("a reload continues above every number the page before it handed out", () => {
  const shared = storage()
  const claim = machine()
  let page = durableSequence(shared, "seq")
  for (let load = 0; load < 5; load++) {
    for (let i = 0; i < 70; i++) assert.ok(claim(page()))
    page = durableSequence(shared, "seq")
  }
})
