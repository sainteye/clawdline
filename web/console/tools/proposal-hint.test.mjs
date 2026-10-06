import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"

const tags = ["en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"]

test("the proposal hint names its visible details control in every language", () => {
  const source = readFileSync(new URL("../src/pages/work/WorkV2.tsx", import.meta.url), "utf8")
  assert.match(source, /work-fold-hint[^\n]*catalogWord\("inline", "2be2b494050d"\)/)
  assert.match(source, /catalogWordLanguage\("inline", "2be2b494050d"\)/)
  assert.match(source, /catalogWordLanguage\("inline", "3b61e4e93652"\)/)
  assert.match(source, /catalogWordLanguage\("inline", "b3b7ae4f6384"\)/)
  for (const tag of tags) {
    const catalog = JSON.parse(readFileSync(new URL(`../public/catalogs/${tag}.json`, import.meta.url), "utf8"))
    const label = catalog["inline.3b61e4e93652"]
    const hint = catalog["inline.2be2b494050d"]
    assert.ok(label && hint.includes(label), `${tag}: hint must name its visible control`)
    if (tag !== "en") assert.doesNotMatch(hint, /\bExplain\b/u, `${tag}: no untranslated English control name`)
  }
})
