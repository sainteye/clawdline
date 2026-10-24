import assert from "node:assert/strict"
import { test } from "node:test"
// @ts-expect-error -- Node's strip-types runner loads this source directly.
import { ITermConsent, type ITermConsentTarget } from "./iterm-consent.ts"

function fixture(value: boolean | undefined = false) {
  const controller = new ITermConsent()
  let current = true
  let writes = 0
  const target: ITermConsentTarget = {
    current: () => current,
    read: async () => ({ iterm_integration: value }),
    enable: async () => { writes++; return { iterm_integration: true } },
  }
  return { controller, target, writes: () => writes, leave: () => { current = false } }
}

test("a disabled integration offers consent without writing; cancelling preserves the setting", async () => {
  const f = fixture()
  await f.controller.offer("backend_unsupported", f.target)
  assert.equal(f.controller.snapshot().phase, "asking")
  assert.equal(f.writes(), 0)
  f.controller.close()
  await f.controller.confirm()
  assert.equal(f.writes(), 0)
})

test("enabled, unsupported and unreadable settings do not offer enabling", async () => {
  for (const value of [true, undefined]) {
    const f = fixture()
    f.target.read = async () => ({ iterm_integration: value })
    await f.controller.offer("backend_unsupported", f.target)
    assert.equal(f.controller.snapshot().phase, "closed")
  }
  const f = fixture()
  await f.controller.offer("send_failed", f.target)
  await f.controller.offer("backend_unsupported", { ...f.target, read: async () => { throw Error("offline") } })
  assert.equal(f.controller.snapshot().phase, "closed")
})

test("confirming writes once and waits for the saved setting without resending a message", async () => {
  const f = fixture()
  let finish!: (value: { iterm_integration: boolean }) => void
  let writes = 0
  await f.controller.offer("backend_unsupported", { ...f.target, enable: () => {
    writes++
    return new Promise(resolve => { finish = resolve })
  } })
  const pending = f.controller.confirm()
  await f.controller.confirm()
  assert.equal(writes, 1)
  assert.equal(f.controller.snapshot().phase, "saving")
  finish({ iterm_integration: true })
  await pending
  assert.equal(f.controller.snapshot().phase, "saved")
})

test("a machine switch during reading or before confirmation never enables the new machine", async () => {
  const f = fixture()
  let finish!: (value: { iterm_integration: boolean }) => void
  const pending = f.controller.offer("backend_unsupported", { ...f.target, read: () => new Promise(resolve => { finish = resolve }) })
  f.leave()
  finish({ iterm_integration: false })
  await pending
  assert.equal(f.controller.snapshot().phase, "closed")
  const g = fixture()
  await g.controller.offer("backend_unsupported", g.target)
  g.leave()
  await g.controller.confirm()
  assert.equal(g.writes(), 0)
})

test("a failed settings write remains retryable and never reports success", async () => {
  const f = fixture()
  await f.controller.offer("backend_unsupported", { ...f.target, enable: async () => { throw Error("offline") } })
  await f.controller.confirm()
  assert.equal(f.controller.snapshot().phase, "failed")
})
