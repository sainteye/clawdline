import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"

const controls = readFileSync(new URL("./controls.tsx", import.meta.url), "utf8")
const styles = readFileSync(new URL("./window.css", import.meta.url), "utf8")

const luminance = (rgb: number[]) => rgb.reduce((sum, channel, index) => {
  const linear = channel / 255 <= 0.04045 ? channel / 255 / 12.92 : ((channel / 255 + 0.055) / 1.055) ** 2.4
  return sum + linear * [0.2126, 0.7152, 0.0722][index]
}, 0)
const contrast = (a: number[], b: number[]) => {
  const [light, dark] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (light + 0.05) / (dark + 0.05)
}

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

test("inactive settings tabs and their focus ring use contrast-safe tokens", () => {
  assert.match(styles, /--sw-tab-muted: rgba\(255, 255, 255, 0\.5\);/)
  assert.match(styles, /\.sw-tab \{[\s\S]*color: var\(--sw-tab-muted\);/)
  assert.match(styles, /\.sw-tab:focus-visible \{[\s\S]*outline: 2px solid var\(--sw-accent\);/)
  const ink = [20, 20, 23]
  const muted = [255, 255, 255].map((channel, index) => channel * 0.5 + ink[index] * 0.5)
  assert.ok(contrast(muted, ink) >= 4.5)
  assert.ok(contrast([217, 119, 87], ink) >= 3)
})
