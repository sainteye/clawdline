import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- a .ts path for Node's type-stripping test runner.
import { gateModeText } from "./gate-mode.ts"

test("all four settings combinations explain the captured behavior", () => {
  assert.match(gateModeText(true, true), /Feature 和 Epic.*獨立 checker/)
  assert.match(gateModeText(true, false), /只擷取規劃.*一般驗證紀錄/)
  assert.match(gateModeText(false, true), /Epic 也略過.*獨立 checker/)
  assert.match(gateModeText(false, false), /Epic 也略過.*一般執行/)
})
