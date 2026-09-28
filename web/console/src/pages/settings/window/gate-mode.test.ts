import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- a .ts path for Node's type-stripping test runner.
import { gateModeOptions, gateModeText } from "./gate-mode.ts"

test("all four settings combinations explain the captured behavior", () => {
	assert.match(gateModeText(true, true), /NEXUS.*Feature 和 Epic.*獨立 checker/)
  assert.match(gateModeText(true, false), /只擷取規劃.*一般驗證紀錄/)
  assert.match(gateModeText(false, true), /Epic 也略過.*獨立 checker/)
  assert.match(gateModeText(false, false), /Epic 也略過.*一般執行/)
})

test("the settings comparison names every mode and marks exactly one current mode", () => {
	const options = gateModeOptions(true, true)
	assert.deepEqual(options.map((option) => option.label), ["NEXUS", "規劃", "獨立驗證", "一般流程"])
	assert.equal(options.filter((option) => option.current).length, 1)
	assert.equal(options.find((option) => option.current)?.label, "NEXUS")
	assert.equal(options.find((option) => option.default)?.label, "規劃")
})
