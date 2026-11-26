import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { settingsMachine } from "./settings-machine.ts"

test("Bots settings open on the machine the person chose or the selected Bot runs on", () => {
  assert.equal(settingsMachine(["linux", "mac"], ["mac", "mac"], "linux"), "linux")
})

test("with no choice, Bots settings open on the machine with the most Bots, not the first listed", () => {
  assert.equal(settingsMachine(["linux", "mac"], ["mac", "mac", "linux"], ""), "mac")
})

test("a tie keeps the listed order, and no Bots falls back to the first machine", () => {
  assert.equal(settingsMachine(["linux", "mac"], ["mac", "linux"], undefined), "linux")
  assert.equal(settingsMachine(["linux", "mac"], [], undefined), "linux")
  assert.equal(settingsMachine([], [], undefined), undefined)
})

test("a chosen or Bot machine that is not a source is ignored", () => {
  assert.equal(settingsMachine(["linux", "mac"], ["gone", "gone", "mac"], "gone"), "mac")
})
