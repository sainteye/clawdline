import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { SAID_MS, StatusMessage, holderKey, isRegionKey, pasteRefusal, withCtrl } from "./keys.ts"

const key = (k: string, mods: Partial<{ altKey: boolean; ctrlKey: boolean; metaKey: boolean }> = {}) =>
  ({ key: k, altKey: false, ctrlKey: false, metaKey: false, ...mods })

test("F6 leaves the terminal, and Tab, Escape and F6 with a modifier stay the program's", () => {
  assert.equal(isRegionKey(key("F6")), true)
  for (const k of ["Tab", "Escape", "F5", "F7", "a"]) assert.equal(isRegionKey(key(k)), false, k)
  assert.equal(isRegionKey(key("F6", { ctrlKey: true })), false)
  assert.equal(isRegionKey(key("F6", { altKey: true })), false)
  assert.equal(isRegionKey(key("F6", { metaKey: true })), false)
})

test("Ctrl applies to one letter and is let go", () => {
  assert.deepEqual(withCtrl(true, "c"), { out: "\x03", armed: false })
  assert.deepEqual(withCtrl(true, "["), { out: "\x1b", armed: false })
  assert.deepEqual(withCtrl(false, "c"), { out: "c", armed: false })
})

test("Ctrl is let go by a special key from the row, which goes as it is", () => {
  for (const special of ["\x1b", "\t", "\x1b[A", "\x1bOD"]) {
    assert.deepEqual(withCtrl(true, special), { out: special, armed: false }, JSON.stringify(special))
  }
})

test("Ctrl is let go by what an IME commits at once, which goes as it is", () => {
  assert.deepEqual(withCtrl(true, "中文"), { out: "中文", armed: false })
  assert.deepEqual(withCtrl(true, "ab"), { out: "ab", armed: false })
})

test("a paste this tab may not send says why", () => {
  assert.equal(pasteRefusal({ holding: false, stale: false }), "terminalPasteWatching")
  assert.equal(pasteRefusal(null), "terminalPasteWatching")
  assert.equal(pasteRefusal({ holding: true, stale: true }), "terminalPasteStale")
  assert.equal(pasteRefusal({ holding: true, stale: false }), null)
})

class FakeTimers {
  t = 0
  private timers: { at: number; fn: () => void; id: number }[] = []
  private ids = 0
  setTimeout = (fn: () => void, ms: number) => {
    const id = ++this.ids
    this.timers.push({ at: this.t + ms, fn, id })
    return id
  }
  clearTimeout = (id: unknown) => {
    this.timers = this.timers.filter((x) => x.id !== id)
  }
  advance(ms: number) {
    this.t += ms
    for (const due of this.timers.filter((x) => x.at <= this.t)) {
      this.timers = this.timers.filter((x) => x !== due)
      due.fn()
    }
  }
}

test("the status line's sentence clears by itself after a while", () => {
  const clock = new FakeTimers()
  const shown: string[] = []
  const m = new StatusMessage((w: string) => shown.push(w), clock)
  m.say("Only whoever controls the terminal can close it.")
  clock.advance(SAID_MS - 1)
  assert.equal(m.text, "Only whoever controls the terminal can close it.")
  clock.advance(1)
  assert.equal(m.text, "")
  assert.deepEqual(shown, ["Only whoever controls the terminal can close it.", ""])
})

test("a newer sentence gets its own full time, and clear() ends it at once", () => {
  const clock = new FakeTimers()
  const m = new StatusMessage(() => {}, clock)
  m.say("first")
  clock.advance(SAID_MS - 100)
  m.say("second")
  clock.advance(200)
  assert.equal(m.text, "second", "the first sentence's timer did not take the second away")
  m.clear()
  assert.equal(m.text, "")
})

test("a change of hands is told apart from the same holder", () => {
  const you = { held: true, holder: { same_client: true, same_device: true, name: "" } }
  const tab = { held: true, holder: { same_client: false, same_device: true, name: "" } }
  const phone = { held: true, holder: { same_client: false, same_device: false, name: "Phone" } }
  const nobody = { held: false }
  const keys = [you, tab, phone, nobody].map(holderKey)
  assert.equal(new Set(keys).size, 4)
  assert.equal(holderKey({ ...phone }), holderKey(phone))
})
