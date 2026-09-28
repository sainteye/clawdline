import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"

const controls = readFileSync(new URL("./controls.tsx", import.meta.url), "utf8")
const styles = readFileSync(new URL("./window.css", import.meta.url), "utf8")

test("phone settings keep all six tabs in readable two-column cells", () => {
  const start = styles.indexOf("@media (max-width: 460px)")
  const phone = styles.slice(start, styles.indexOf(".sw-gate-mode", start))
  assert.match(phone, /\.sw-strip \{[\s\S]*height: auto;/)
  assert.match(phone, /grid-template-columns: 22px repeat\(2, minmax\(0, 1fr\)\);/)
  assert.match(phone, /\.sw-strip-mark \{[\s\S]*grid-row: 1 \/ span 3;/)
  assert.match(phone, /\.sw-tab \{[\s\S]*min-height: 30px;[\s\S]*white-space: nowrap;/)
})

test("settings tabs retain roving keyboard focus and pointer activation", () => {
  assert.match(controls, /role="tab"/)
  assert.match(controls, /tabIndex=\{i === current \? 0 : -1\}/)
  assert.match(controls, /onClick=\{\(\) => onPick\(i\)\}/)
  assert.match(controls, /e\.key !== "ArrowLeft" && e\.key !== "ArrowRight"/)
  assert.match(controls, /document\.getElementById\(`sw-tab-\$\{next\}`\)\?\.focus\(\)/)
})
