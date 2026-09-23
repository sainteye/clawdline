import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
// @ts-expect-error -- a `.ts` path, for node's strip-types runner.
import { statusLimitCells } from "./status-limits.ts"

const responsiveStyles = readFileSync(new URL("./git-status.css", import.meta.url), "utf8")

test("a Codex weekly window reaches the Status Line", () => {
  assert.deepEqual(statusLimitCells([{ name: "7d", usedPercent: 84 }], "unknown"), [
    { name: "7d", value: "84%", level: "warn" },
  ])
})

test("phones keep provider limits visible alongside Git status", () => {
  assert.match(responsiveStyles, /@media \(max-width: 520px\)[\s\S]*?footer\.status-line \.limits \{[\s\S]*?display: flex;/)
  assert.doesNotMatch(responsiveStyles, /footer\.status-line \.limits \{\s*display: none;/)
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
