import { test } from "node:test"
import assert from "node:assert/strict"

// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { QUIET, easeLook, frameDue, levelFromSamples, orbLook, orbPixelRatio, settled, smoothLevel } from "./copilot-orb.ts"

const sine = (amplitude: number, n = 512) =>
  Array.from({ length: n }, (_, i) => 128 + amplitude * 127 * Math.sin((i / n) * Math.PI * 16))

test("a louder voice turns the clouds faster and swells the orb while it listens", () => {
  const quiet = orbLook("listening", QUIET)
  const loud = orbLook("listening", { input: 0.9, output: 0 })
  assert.ok(loud.speed > quiet.speed * 2, `${loud.speed} vs ${quiet.speed}`)
  assert.ok(loud.swell > quiet.swell)
  assert.ok(loud.glow > quiet.glow)
})

test("the copilot's own voice drives the orb while it answers, faster than listening", () => {
  const answering = orbLook("speaking", { input: 0, output: 0.8 })
  const listening = orbLook("listening", { input: 0.8, output: 0 })
  assert.ok(answering.speed > listening.speed)
  assert.ok(answering.speed > orbLook("speaking", QUIET).speed)
})

test("thinking gathers the clouds into a whirl and nothing else does", () => {
  assert.ok(orbLook("thinking", QUIET).swirl > 0)
  for (const mode of ["idle", "connecting", "listening", "speaking"] as const) assert.equal(orbLook(mode, QUIET).swirl, 0)
})

test("with reduced motion nothing turns or swells, and only the brightness follows the voice", () => {
  const quiet = orbLook("speaking", QUIET, true)
  const loud = orbLook("speaking", { input: 0, output: 1 }, true)
  assert.equal(loud.speed, 0)
  assert.equal(loud.swell, 1)
  assert.equal(loud.swirl, 0)
  assert.ok(loud.bright > quiet.bright)
})

test("an idle orb settles and stops drawing; a live call keeps moving", () => {
  let look = orbLook("listening", QUIET)
  const target = orbLook("idle", QUIET)
  for (let i = 0; i < 300 && !settled(look, target); i++) look = easeLook(look, target, 1 / 30)
  assert.ok(settled(look, target), "the idle orb never settled")
  assert.equal(settled(orbLook("connecting", QUIET), orbLook("connecting", QUIET)), false)
})

test("silence measures as zero, ordinary speech fills most of the range", () => {
  assert.equal(levelFromSamples(new Array(512).fill(128)), 0)
  assert.equal(levelFromSamples([]), 0)
  const speech = levelFromSamples(sine(0.15))
  assert.ok(speech > 0.4 && speech < 0.9, `${speech}`)
  assert.ok(levelFromSamples(sine(0.9)) > 0.95)
})

test("a level rises with a voice faster than it falls after it", () => {
  const up = smoothLevel(0, 1, 0.05)
  const down = 1 - smoothLevel(1, 0, 0.05)
  assert.ok(up > down * 2, `${up} vs ${down}`)
})

test("a 60 Hz display draws the orb about thirty times a second", () => {
  let drawn = 0
  let last = 0
  for (let frame = 1; frame <= 60; frame++) {
    const now = frame * (1000 / 60)
    if (!last || frameDue(last, now)) { drawn++; last = now }
  }
  assert.ok(drawn >= 29 && drawn <= 31, `${drawn}`)
})

test("a dense screen is drawn at most twice its CSS pixels", () => {
  assert.equal(orbPixelRatio(3), 2)
  assert.equal(orbPixelRatio(1.5), 1.5)
  assert.equal(orbPixelRatio(0), 1)
  assert.equal(orbPixelRatio(Number.NaN), 1)
})
