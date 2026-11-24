import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { watchBotViewing, BOT_VIEWING_REPEAT_MS } from "./bot-viewing.ts"

function fakePage(visible: boolean, focused: boolean) {
  const page = { visible, focused, change: () => {}, tick: () => {}, every: 0,
    view: { visible: () => page.visible, focused: () => page.focused,
      listen(c: () => void) { page.change = c; return () => { page.change = () => {} } },
      every(ms: number, t: () => void) { page.every = ms; page.tick = t; return () => { page.tick = () => {} } } } }
  return page
}

test("an open, focused chat is reported on screen, repeated, and reported left when closed", () => {
  const page = fakePage(true, true)
  const reports: string[] = []
  const stop = watchBotViewing("xavia", "chat", page.view, (b: string, c: string, v: boolean) => reports.push(`${b}/${c}:${v}`))
  page.tick()
  assert.equal(page.every, BOT_VIEWING_REPEAT_MS)
  assert.ok(BOT_VIEWING_REPEAT_MS < 45_000, "repeats inside the daemon's window")
  stop()
  assert.deepEqual(reports, ["xavia/chat:true", "xavia/chat:true", "xavia/chat:false"])
})

test("a hidden or unfocused page says it left and stops repeating until it is back", () => {
  const page = fakePage(true, true)
  const reports: boolean[] = []
  const stop = watchBotViewing("xavia", "chat", page.view, (_b: string, _c: string, v: boolean) => reports.push(v))
  page.visible = false
  page.change()
  page.tick()
  page.focused = false
  page.visible = true
  page.change()
  page.focused = true
  page.change()
  stop()
  assert.deepEqual(reports, [true, false, true, false])
})

test("a page that opens in the background reports nothing", () => {
  const page = fakePage(false, false)
  const reports: boolean[] = []
  const stop = watchBotViewing("xavia", "chat", page.view, (_b: string, _c: string, v: boolean) => reports.push(v))
  page.tick()
  stop()
  assert.deepEqual(reports, [])
  assert.deepEqual((() => { const r: boolean[] = []; watchBotViewing("", "chat", page.view, () => r.push(true))(); return r })(), [])
})
