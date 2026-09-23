import assert from "node:assert/strict"
import test from "node:test"
import { completionReportText } from "./completion-report-text.js"

test("legacy completion reports recover literal blank-line separators", () => {
  assert.equal(
    completionReportText("First paragraph.\\n\\nSecond paragraph."),
    "First paragraph.\n\nSecond paragraph.",
  )
})

test("real Markdown and an intentional single escaped newline stay untouched", () => {
  assert.equal(completionReportText("First\n\nSecond"), "First\n\nSecond")
  assert.equal(completionReportText("The token \\n is meaningful."), "The token \\n is meaningful.")
})
