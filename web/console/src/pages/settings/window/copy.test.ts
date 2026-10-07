// The sentence under the voice language picker:
//   node --test web/console/src/pages/settings/window/copy.test.ts
import { afterEach, beforeEach, test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import chinese from "../../../../public/catalogs/zh-Hant.json" with { type: "json" }
// @ts-expect-error -- Node runs the TypeScript source test directly.
import { activateCatalog, resetCatalogForTest } from "../../../catalog.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { voiceLanguageSaid } from "./copy.ts"

const said = (v: Partial<Parameters<typeof voiceLanguageSaid>[0]>) =>
  voiceLanguageSaid({ setting: "auto", code: "", script: "", source: "", tag: "", script_source: "", script_tag: "", ...v })

let oldDocument: typeof globalThis.document
beforeEach(() => {
  oldDocument = globalThis.document
  Object.defineProperty(globalThis, "document", { configurable: true, value: { documentElement: { lang: "en", dir: "ltr", setAttribute() {} } } })
  assert.equal(activateCatalog(chinese, "zh-Hant"), "zh-Hant")
})
afterEach(() => {
  resetCatalogForTest()
  Object.defineProperty(globalThis, "document", { configurable: true, value: oldDocument })
})

// Each of the three machines the daemon's own test is run as says which one it
// was, so a Simplified transcript is never an answer nobody could have seen.
test("auto names the machine, the variable, or the catalog it followed", () => {
  assert.equal(
    said({ code: "zh", script: "Hant", source: "AppleLanguages", tag: "zh-Hant-TW" }),
    "現在是繁體中文——Clawdline 的語言是自動，而這台機器的語言與地區設定是中文（繁體，台灣）。",
  )
  assert.equal(
    said({ code: "zh", script: "Hans", source: "LANG", tag: "zh_CN.UTF-8" }),
    "現在是簡體中文——Clawdline 的語言是自動，而這台機器的 LANG 環境變數是中文（中國）。",
  )
  assert.equal(
    said({ code: "zh", script: "Hant", source: "catalog", tag: "zh-Hant" }),
    "現在是繁體中文——Clawdline 的語言是自動，這台機器也沒說它用什麼語言，所以跟著 Clawdline 介面本身的語言。",
  )
})

test("Clawdline's own language is named as the General tab", () => {
  assert.equal(
    said({ code: "zh", script: "Hans", source: "language", tag: "zh-Hans" }),
    "現在是簡體中文——「一般」分頁的語言是簡體中文。",
  )
})

// Detection says which script Chinese comes back in and who said so — or that
// nobody did, which is the one case whisper's habit is the answer.
test("a language left to whisper still says how Chinese is written", () => {
  assert.equal(
    said({ code: "auto", script: "Hant", source: "language", tag: "en", script_source: "AppleLanguages", script_tag: "zh-Hant-TW" }),
    "語言交給 whisper 自己判斷——「一般」分頁的語言是英文。聽到中文時寫成繁體——Clawdline 的語言是自動，而這台機器的語言與地區設定是中文（繁體，台灣）。",
  )
  assert.match(said({ code: "auto" }), /whisper 習慣寫成簡體/)
})

test("an explicit choice is said as a choice", () => {
  assert.equal(
    said({ setting: "zh-TW", code: "zh", script: "Hant", source: "voice_language", tag: "zh-TW" }),
    "每段錄音都當成繁體中文來讀。",
  )
  assert.equal(said({ setting: "en", code: "en", source: "voice_language", tag: "en" }), "每段錄音都當成英文來讀。")
})

test("Voice composes complete Chinese names and readable sentences in every catalog", () => {
  for (const tag of ["en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"] as const) {
    const catalog = JSON.parse(readFileSync(new URL(`../../../../public/catalogs/${tag}.json`, import.meta.url), "utf8"))
    assert.equal(activateCatalog(catalog, tag), tag)
    const displayNames = new Intl.DisplayNames([tag], { type: "language" })
    const fixed = said({ setting: "zh-Hans", code: "zh", script: "Hans", source: "voice_language", tag: "zh-Hans" })
    const automatic = said({ code: "zh", script: "Hant", source: "AppleLanguages", tag: "zh-Hant-TW" })
    assert.ok(fixed.includes(displayNames.of("zh-Hans")!), `${tag}: ${fixed}`)
    assert.ok(automatic.includes(displayNames.of("zh-Hant")!), `${tag}: ${automatic}`)
    assert.doesNotMatch(fixed + automatic, /\{(?:name|why|where|tag)\}|\(으\)로|です\s+です|。\s*。/u, tag)
  }
})
