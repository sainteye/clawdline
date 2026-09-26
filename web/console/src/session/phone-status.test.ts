// The Status Line keeps its context and cost on a phone:
// `node --test --experimental-strip-types web/console/src/session/phone-status.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"

// Away from the machine this row is the only place the context and the money
// are (legacy/responsive.css says so, and gives the branch the squeeze
// instead). A rule added with the Git viewer on 2026-09-22 hid both under
// 520px, and on every phone the Status Line lost them without a word; the
// daemon was sending them all along. Measured in WebKit on 2026-09-26 without
// that rule: at 390px and 360px `ctx 61%` and `$57.39` draw whole, inside the
// footer, and the branch shrinks to make room.
//
// One exception is deliberate and not a media rule: while a deploy or a local
// run holds the chip, `status-line-deploy.css` hides ctx and the tree at every
// width until it ends (`status-line-deploy.e2e.ts`). The cost stays.
const sheets = ["../legacy/status-line.css", "../legacy/responsive.css", "./git-status.css"]

function mediaBlocks(css: string): string[] {
  const out: string[] = []
  let at = css.indexOf("@media")
  while (at >= 0) {
    const open = css.indexOf("{", at)
    let depth = 0
    let end = open
    for (; end < css.length; end++) {
      if (css[end] === "{") depth++
      else if (css[end] === "}" && --depth === 0) break
    }
    out.push(css.slice(at, end + 1))
    at = css.indexOf("@media", end)
  }
  return out
}

test("no narrow-screen rule hides the Status Line's context or cost", () => {
  let scanned = 0
  for (const sheet of sheets) {
    const css = readFileSync(new URL(sheet, import.meta.url), "utf8")
    for (const block of mediaBlocks(css)) {
      scanned++
      for (const rule of block.split("}")) {
        const [selector, body = ""] = rule.split("{").slice(-2)
        if (!/display\s*:\s*none/.test(body)) continue
        for (const part of (selector ?? "").split(",")) {
          if (/status-line/.test(part) && /\.(context|cost)\b/.test(part)) {
            assert.fail(`${sheet}: "${part.trim()}" is hidden on a narrow screen`)
          }
        }
      }
    }
  }
  // Nothing scanned is the scan broken, not the sheets clean.
  assert.ok(scanned >= 3, `only ${scanned} media blocks were read`)
})
