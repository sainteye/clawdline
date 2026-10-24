import assert from "node:assert/strict"
import test from "node:test"
import type { CloudStatus, CloudViewer } from "../pages/settings/cloud.js"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { completionChange, parseSkipped, readSetup, setupChip, SETUP_FOLD_AFTER_SECONDS, SETUP_ITEMS } from "./progress.ts"

const status = (devices: CloudViewer[], extra: Partial<CloudStatus> = {}): CloudStatus => ({
  enabled: true, commands: false, terminal_direct: true, configured: true, state: "connected",
  connects: 1, reconnects: 0, published: 0, acked: 0, publish_errors: 0, inbound_total: 0,
  answered: 0, refused: 0, queue_refused: 0, queue_unanswered: 0, roster_readable: true,
  pinned_readable: true, devices, ...extra,
} as CloudStatus)

const app: CloudViewer = { id: "d_app", kind: "browser", name: "Clawdline App · macOS", pinned: true }
const chrome: CloudViewer = { id: "d_chrome", kind: "browser", name: "Chrome · macOS", pinned: true }
const phone: CloudViewer = { id: "d_phone", kind: "ios", name: "iPhone", pinned: true }

test("each item is finished by a device this machine pinned, told apart by kind and name", () => {
  const reading = readSetup(status([app, chrome, phone]))
  assert.deepEqual(reading, { kind: "known", cloud: true, done: { app: true, browser: true, phone: true },
    skipped: { app: false, browser: false, phone: false }, items: SETUP_ITEMS, count: 3, next: null })
  const partial = readSetup(status([chrome, { ...phone, kind: "android", name: "Android device" }]))
  assert.equal(partial.kind === "known" && partial.next, "app")
  assert.equal(partial.kind === "known" && partial.count, 2)
})

test("a revoked device, one only in the account's roster, or a pin the roster forgot gives its item back", () => {
  const reading = readSetup(status([app, chrome, { ...phone, revoked: true }, { ...phone, id: "d_other", pinned: false },
    { id: "d_forgotten", pinned: true }]))
  assert.equal(reading.kind === "known" && reading.next, "phone")
  assert.equal(reading.kind === "known" && reading.count, 2)
  const leftovers = readSetup(status([{ id: "d_forgotten", pinned: true }]))
  assert.equal(leftovers.kind === "known" && leftovers.count, 0, "a pin with no roster row is not a paired browser")
})

test("Cloud off counts nothing, and a list this machine cannot read is unknown, not 0/3", () => {
  const off = readSetup(status([app, chrome, phone], { enabled: false }))
  assert.equal(off.kind === "known" && off.count, 0)
  assert.equal(readSetup(status([app], { pinned_readable: false })).kind, "unknown")
  assert.equal(readSetup(null).kind, "unknown")
  assert.deepEqual(setupChip(readSetup(null), "", 0, 0), { kind: "hidden" })
})

test("the header shows progress, a quiet local mark, or done until a day after completion", () => {
  const now = 2_000_000_000
  const partial = readSetup(status([app]))
  assert.deepEqual(setupChip(partial, "", 0, now), { kind: "progress", count: 1, total: 3, next: "browser" })
  assert.deepEqual(setupChip(partial, "local", 0, now), { kind: "local" })
  const all = readSetup(status([app, chrome, phone]))
  assert.deepEqual(setupChip(all, "local", 0, now), { kind: "done" })
  assert.deepEqual(setupChip(all, "", now - SETUP_FOLD_AFTER_SECONDS + 1, now), { kind: "done" })
  assert.deepEqual(setupChip(all, "", now - SETUP_FOLD_AFTER_SECONDS, now), { kind: "hidden" })
})

test("completion is stamped once and cleared when something is unpaired", () => {
  const now = 2_000_000_000
  const all = readSetup(status([app, chrome, phone]))
  const partial = readSetup(status([app, chrome]))
  assert.equal(completionChange(all, 0, now), now)
  assert.equal(completionChange(all, now - 5, now), null)
  assert.equal(completionChange(partial, now - 5, now), 0)
  assert.equal(completionChange(partial, 0, now), null)
  assert.equal(completionChange(readSetup(null), now - 5, now), null, "unknown never clears a recorded completion")
})

test("a machine without the macOS app is set up by a browser and a phone", () => {
  const reading = readSetup(status([chrome]), false)
  assert.equal(reading.kind === "known" && reading.next, "phone")
  assert.deepEqual(setupChip(reading, "", 0, 0), { kind: "progress", count: 1, total: 2, next: "phone" })
  const both = readSetup(status([chrome, phone]), false)
  assert.equal(both.kind === "known" && both.next, null)
})

test("the app is the device recorded from its own pairing, whatever name it registered under", () => {
  const oldApp: CloudViewer = { id: "d_old_app", kind: "browser", name: "Web browser", pinned: true }
  const unknown = readSetup(status([oldApp]))
  assert.equal(unknown.kind === "known" && unknown.done.app, false, "without the record it is a browser")
  const known = readSetup(status([oldApp, chrome]), true, "d_old_app")
  assert.deepEqual(known.kind === "known" && known.done, { app: true, browser: true, phone: false })
})

test("a skipped item counts as settled, so setup can finish without it, and pairing it later still shows as paired", () => {
  const skipped = parseSkipped("browser,phone,bogus")
  assert.deepEqual(skipped, ["browser", "phone"])
  const reading = readSetup(status([app]), true, "", skipped)
  assert.equal(reading.kind === "known" && reading.next, null)
  assert.equal(reading.kind === "known" && reading.count, 3)
  assert.deepEqual(setupChip(reading, "", 0, 0), { kind: "done" })
  const later = readSetup(status([app, phone]), true, "", skipped)
  assert.deepEqual(later.kind === "known" && later.skipped, { app: false, browser: true, phone: false })
  const partial = readSetup(status([]), true, "", ["browser"])
  assert.equal(partial.kind === "known" && partial.next, "app", "skipping one item does not settle the others")
})
