import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node's strip-types runner.
import { projectFeatureMeasurementWords } from "../pages/projects/measurement.ts"

test("an unmeasured Feature list is not described as an empty measured list", () => {
  const zh = projectFeatureMeasurementWords(true)
  assert.match(zh.empty, /尚未量測/)
  assert.doesNotMatch(zh.empty, /沒有任何/)
  assert.match(zh.read, /Feature 歸屬：未量測/)
  assert.doesNotMatch(zh.read, /\{feature\}/)
})
