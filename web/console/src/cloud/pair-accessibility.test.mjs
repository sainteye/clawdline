import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { test } from "node:test"

const panel = readFileSync(new URL("./PairPanel.tsx", import.meta.url), "utf8")
const gate = readFileSync(new URL("./CloudGate.tsx", import.meta.url), "utf8")
const scanner = readFileSync(new URL("./PairScanner.tsx", import.meta.url), "utf8")
const english = JSON.parse(readFileSync(new URL("../../public/catalogs/en.json", import.meta.url), "utf8"))
const taiwan = JSON.parse(readFileSync(new URL("../../public/catalogs/zh-Hant.json", import.meta.url), "utf8"))

test("pairing dialog names its current title and leads with one way for the device", () => {
  assert.match(gate, /aria-labelledby=\{pairing \? "cloud-pair-title" : undefined\}/)
  assert.match(panel, /<h2[^>]*id="cloud-pair-title"[^>]*ref=\{titleRef\}/)
  // A phone scans the machine's QR; a computer is offered the code instead.
  const steps = panel.indexOf('nextWord("cloudPairOutcome")')
  assert.ok(panel.indexOf("cloudPairStepScanApp") > 0 && panel.indexOf("cloudPairStepCopy") > 0)
  assert.ok(steps > 0 && steps < panel.indexOf("<PairScanner"), "what the person gets is said before the action")
  assert.match(panel, /<PairScanner onScanned=\{onScan\} onUseCode=\{begin\} startRef=\{scanStart\}/)
  assert.match(panel, /nextWord\("cloudPairGenerate"\)/)
  assert.match(scanner, /id="cloud-pair-scan-start"[\s\S]*ref=\{startRef\}/)
  assert.match(scanner, /id="cloud-pair-scan-use-code"/)
  assert.match(scanner, /nextWord\("cloudPairScanRetry"\)/)
  assert.match(scanner, /type="file" accept="image\/\*" capture="environment"/)
  assert.match(scanner, /type="file" accept="image\/\*" onClick=/)
  assert.doesNotMatch(scanner, /fetch\(|sendBeacon\(|\.post\(/, "camera frames and decoded offers stay on this device")
})

test("manual recovery keeps the complete code and separate agent instructions", () => {
  const code = panel.indexOf('id="cloud-pair-code"')
  const copy = panel.indexOf('id="cloud-pair-code-copy"')
  const advanced = panel.indexOf('id="cloud-pair-agent-advanced"')
  assert.ok(code > 0 && code < copy && copy < advanced)
  assert.match(panel, /id="cloud-pair-code"[\s\S]*value=\{state\.fragment\}/)
  assert.match(panel, /id="cloud-pair-code-copy"[\s\S]*copy\(state\.fragment, "code"\)/)
  assert.match(panel, /nextWord\("cloudPairStepPaste"\)/)
  for (const key of ["cloudPairCodeLabel", "cloudPairCodeCopy", "cloudPairStepPaste", "cloudPairAgentAdvanced"]) {
    assert.ok(english[`next.${key}`], `English ${key}`)
    assert.ok(taiwan[`next.${key}`], `Taiwan Traditional Chinese ${key}`)
  }
})

test("Safari and the Home Screen app describe their separate keys and paths", () => {
  for (const catalog of [english, taiwan]) {
    assert.ok(catalog["next.cloudPairContainer"].includes("Safari"))
    assert.ok(catalog["next.cloudPairContainerPwa"].includes("Safari"))
    assert.ok(catalog["next.cloudPairStepScanApp"])
    assert.ok(catalog["next.cloudPairUseCodeButton"])
    assert.ok(catalog["next.cloudPairPhotoTake"])
    assert.ok(catalog["next.cloudPairPhotoChoose"])
  }
  assert.match(panel, /nextWord\("cloudPairContainerPwa"\)/)
  assert.match(gate, /device: pairDevice\(\)/)
})
