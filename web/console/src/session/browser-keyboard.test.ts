import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node source tests use the source extension.
import { BrowserInputQueue, BROWSER_INPUT_BYTES, BROWSER_INPUT_EDITS, type BrowserEdit } from "./browser-keyboard.ts"
const tick = () => new Promise<void>((resolve) => setImmediate(resolve))
const text = (value: string): BrowserEdit => ({ op: "type", args: { text: value } })

test("native input waits for the clicked field and coalesces text without reordering keys", async () => {
  let admit!: (value: boolean) => void
  const sent: BrowserEdit[] = []
  const q = new BrowserInputQueue(async edit => { sent.push(edit); return { ok: true } }, () => assert.fail("input refused"), () => {}, () => {})
  q.start(new Promise(resolve => { admit = resolve }))
  q.add(text("中")); q.add(text("文")); q.add({ op: "key", args: { key: "Backspace" } }); q.add(text("字"))
  await tick(); assert.deepEqual(sent, [])
  admit(true); await tick()
  assert.deepEqual(sent, [text("中文"), { op: "key", args: { key: "Backspace" } }, text("字")])
})

test("handback fences unsent text and late acknowledgements; a failure is never retried", async () => {
  let finish!: (value: { ok: boolean }) => void
  const sent: BrowserEdit[] = [], refused: string[] = []
  const q = new BrowserInputQueue(edit => { sent.push(edit); return new Promise(resolve => { finish = resolve }) }, reason => refused.push(reason), () => {}, () => {})
  q.start(Promise.resolve(true)); q.add(text("a")); await tick(); q.add(text("b")); q.cancel(); finish({ ok: true }); await tick()
  assert.deepEqual(sent, [text("a")]); assert.deepEqual(refused, [])
  q.start(Promise.resolve(true)); q.add(text("c")); await tick(); q.add(text("d")); finish({ ok: false }); await tick(); await tick()
  assert.deepEqual(sent, [text("a"), text("c")]); assert.deepEqual(refused, ["unconfirmed"])
})

test("a rejected takeover sends nothing and both buffer bounds refuse visibly", async () => {
  const refused: string[] = []
  const q = new BrowserInputQueue(async () => assert.fail("input escaped admission"), r => refused.push(r), () => {}, () => {})
  q.start(Promise.resolve(false)); q.add(text("private")); await tick(); assert.deepEqual(refused, ["unconfirmed"])
  q.start(new Promise(() => {})); assert.equal(q.add(text("x".repeat(BROWSER_INPUT_BYTES + 1))), false)
  q.start(new Promise(() => {})); for (let i = 0; i < BROWSER_INPUT_EDITS; i++) assert.equal(q.add({ op: "key", args: { key: "Enter" } }), true)
  assert.equal(q.add({ op: "key", args: { key: "Enter" } }), false)
  assert.deepEqual(refused, ["unconfirmed", "full", "full"])
})
