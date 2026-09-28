import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { DEFAULT_FONT_SCALE, FONT_SCALE_KEY, applyFontScale, parseFontScale, readFontScale, rememberFontScale, stepFontScale } from "./font-scale.ts"

test("the saved text size accepts only values the controls can reach", () => {
  assert.equal(parseFontScale("80"), 80)
  assert.equal(parseFontScale("130"), 130)
  assert.equal(parseFontScale("125"), DEFAULT_FONT_SCALE)
  assert.equal(parseFontScale("large"), DEFAULT_FONT_SCALE)
  assert.equal(parseFontScale(null), DEFAULT_FONT_SCALE)
})

test("minus and plus stop at the advertised limits", () => {
  assert.equal(stepFontScale(100, -1), 90)
  assert.equal(stepFontScale(100, 1), 110)
  assert.equal(stepFontScale(80, -1), 80)
  assert.equal(stepFontScale(150, 1), 150)
})

test("reading and writing keep this browser's percentage", () => {
  const values = new Map<string, string>([[FONT_SCALE_KEY, "130"]])
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
  }
  assert.deepEqual(readFontScale(storage), { percent: 130, storageAvailable: true })
  assert.equal(rememberFontScale(140, storage), true)
  assert.equal(values.get(FONT_SCALE_KEY), "140")
})

test("a browser that refuses storage still gets an in-memory adjustment", () => {
  const storage = {
    getItem: () => {
      throw new Error("blocked")
    },
    setItem: () => {
      throw new Error("blocked")
    },
  }
  assert.deepEqual(readFontScale(storage), { percent: DEFAULT_FONT_SCALE, storageAvailable: false })
  assert.equal(rememberFontScale(120, storage), false)

  let name = ""
  let value = ""
  applyFontScale(120, {
    style: {
      setProperty: (nextName: string, nextValue: string) => {
        name = nextName
        value = nextValue
      },
    },
  })
  assert.equal(name, "--clawdline-font-scale")
  assert.equal(value, "120%")
})
