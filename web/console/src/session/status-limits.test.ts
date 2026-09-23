import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node's strip-types runner.
import { statusLimitCells } from "./status-limits.ts"

test("a Codex weekly window reaches the Status Line", () => {
  assert.deepEqual(statusLimitCells([{ name: "7d", usedPercent: 84 }], "unknown"), [
    { name: "7d", value: "84%", level: "warn" },
  ])
})

test("Status Line percentages are rounded, bounded and honest about unknown values", () => {
  assert.deepEqual(statusLimitCells([
    { name: "5h", usedPercent: -2 },
    { name: "7d", usedPercent: 100.4 },
    { name: "later", usedPercent: Number.NaN },
  ], "unknown"), [
    { name: "5h", value: "0%", level: "ok" },
    { name: "7d", value: "100%", level: "bad" },
    { name: "later", value: "unknown", level: "" },
  ])
})
