import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import english from "../public/catalogs/en.json" with { type: "json" }
// @ts-expect-error -- Node runs this source test directly.
import { activateCatalog, bootCatalog, browserPreference, catalogFormat, catalogRefusalDetail, chooseTag, currentCatalogTag, missingCatalogKeys, resetCatalogForTest, resolveTag, validCatalog } from "./catalog.ts"

test("a refusal with no translated detail key retains the producer's English words", () => {
  assert.deepEqual(catalogRefusalDetail({ detail: "The operation was refused.", detailKey: "http.0000000000000000" }),
    { text: "The operation was refused.", lang: "en" })
  assert.deepEqual(catalogRefusalDetail({ detail: "A dynamic path is unavailable." }),
    { text: "A dynamic path is unavailable.", lang: "en" })
  assert.deepEqual(catalogRefusalDetail({ error: { code: "closed", message: "The session is closed.", detail_key: "http.0000000000000000" } }),
    { text: "The session is closed.", lang: "en" })
})

test("all numeric sentences keep 0, 1, many and long values in order in nine rendered languages", () => {
  const oldDocument = globalThis.document
  Object.defineProperty(globalThis, "document", { configurable: true, value: { documentElement: { lang: "en", dir: "ltr", setAttribute() {} } } })
  const keys = ["skillFoldersSkipped", "unfinishedWork", "squadSkills", "squadGroups", "squadRoles", "squadItems", "squadSkillSources", "recentInterventions", "prunedInterventions", "pendingInterventions", "localFiles", "unsyncedProjects", "exportRounds"]
  try {
    for (const tag of ["en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"] as const) {
      const catalog = JSON.parse(readFileSync(new URL(`../public/catalogs/${tag}.json`, import.meta.url), "utf8"))
      assert.equal(activateCatalog(catalog, tag), tag)
      for (const key of keys) {
        for (const count of [0, 1, 123456]) {
          const values = [count, count + 1, "A very long source label from a machine / with a path"]
          const output = catalogFormat("count", key, values)
          assert.ok(output.length > String(count).length, `${tag} ${key} rendered only the number`)
          assert.doesNotMatch(output, /\{arg[0-9]+\}/u, `${tag} ${key}`)
          assert.ok(output.includes(String(count)), `${tag} ${key} lost its count`)
          if (key === "unfinishedWork" || key === "squadSkillSources") {
            assert.ok(output.indexOf(String(count)) < output.indexOf(String(count + 1)), `${tag} ${key} reversed counts`)
          }
          if (key === "squadSkillSources") assert.ok(output.includes(String(values[2])), `${tag} lost the original source label`)
        }
      }
    }
  } finally {
    resetCatalogForTest()
    Object.defineProperty(globalThis, "document", { configurable: true, value: oldDocument })
  }
})

test("the local and Cloud resolver uses the same script-first cases", () => {
  for (const [input, expected] of [
    ["en-US", "en"], ["zh-Hant", "zh-Hant"], ["zh-Hant-TW", "zh-Hant"],
    ["zh-TW", "zh-Hant"], ["zh-HK", "zh-Hant"], ["zh-MO", "zh-Hant"], ["zh_MO", "zh-Hant"], ["zh-Hans", "zh-Hans"],
    ["zh-Latn-TW", null], ["zh-Hans-TW", "zh-Hans"], ["zh-Hant-CN", "zh-Hant"],
    ["zh-Hans-CN", "zh-Hans"], ["zh-CN", "zh-Hans"], ["zh-SG", "zh-Hans"],
    ["zh", null], ["ja-JP", "ja"], ["pt", "pt-BR"], ["pt-PT", "pt-BR"],
    ["pt-BR", "pt-BR"], ["es-MX", "es"], ["fr-CA", "fr"], ["de-AT", "de"],
    ["ko-KR", "ko"], ["not-a-real-tag", null],
  ] as const) assert.equal(resolveTag(input), expected, input)
  assert.equal(chooseTag("auto", ["zh", "ja-JP"]), "ja")
  assert.equal(chooseTag("auto", ["zh-Hans", "zh-TW"]), "zh-Hans")
  assert.equal(chooseTag("unknown", ["ja-JP"]), "en")
  assert.equal(chooseTag("", []), "en")
  const oldStorage = globalThis.localStorage
  Object.defineProperty(globalThis, "localStorage", { configurable: true, value: { getItem: () => "zh_MO" } })
  try { assert.equal(browserPreference(), "zh-Hant") }
  finally { Object.defineProperty(globalThis, "localStorage", { configurable: true, value: oldStorage }) }
})

test("damaged, incomplete and incorrectly interpolated catalogs fail as a unit", () => {
  const base = { ...english, lang: "ja" }
  assert.equal(validCatalog(base, "ja"), true)
  assert.equal(validCatalog({ ...base, "next.cloudRetry": "" }, "ja"), false)
  assert.equal(validCatalog({ ...base, "next.cloudRetry": "<script>bad</script>" }, "ja"), false)
  assert.equal(validCatalog({ ...base, "next.cloudRetrying": "Retrying in {wrong} s" }, "ja"), false)
  assert.equal(validCatalog({ ...base, "legacy.closeabilityBlocked": "{n} obligation remains\x1fobligations remain" }, "ja"), false)
  const incomplete = { ...base } as Record<string, string>
  delete incomplete["legacy.webMenu"]
  assert.equal(validCatalog(incomplete, "ja"), false)
  assert.equal(validCatalog({ ...base, lang: "fr", extra: "obsolete" }, "fr"), false)
  assert.equal(validCatalog({ ...base, lang: "en", "legacy.webMenu": "Unexpected replacement" }, "ja"), false)
  assert.equal(validCatalog({ ...base, lang: "fr", dir: "rtl" }, "fr"), false)
  assert.equal(validCatalog({ ...base, lang: "fr", dir: "" }, "fr"), false)
})

test("a future secondary language may omit only a new key and keeps its document language", () => {
  const oldDocument = globalThis.document
  Object.defineProperty(globalThis, "document", { configurable: true, value: { documentElement: { lang: "en", dir: "ltr", setAttribute() {} } } })
  try {
    const candidate = { ...english, lang: "ja" } as Record<string, string>
    delete candidate["ui.language"]
    const priorBaseline = Object.keys(english).filter((key) => key !== "ui.language")
    assert.equal(validCatalog(candidate, "ja", priorBaseline), true)
    assert.equal(validCatalog(candidate, "zh-Hant", priorBaseline), false)
    assert.equal(activateCatalog(candidate, "ja", priorBaseline), "ja")
    assert.equal(document.documentElement.lang, "ja")
    assert.deepEqual(missingCatalogKeys(), ["ui.language"])
  } finally {
    resetCatalogForTest()
    Object.defineProperty(globalThis, "document", { configurable: true, value: oldDocument })
  }
})

test("a failed catalog request leaves the page usable in English", async () => {
  const oldDocument = globalThis.document
  const oldNavigator = globalThis.navigator
  const oldStorage = globalThis.localStorage
  Object.defineProperty(globalThis, "document", { configurable: true, value: { documentElement: { lang: "en", dir: "ltr" } } })
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: { languages: ["ja-JP"], language: "ja-JP" } })
  Object.defineProperty(globalThis, "localStorage", { configurable: true, value: { getItem: () => "auto" } })
  try {
    resetCatalogForTest()
    let requests = 0
    const tag = await bootCatalog(() => "/catalogs/ja.json", async () => {
      requests++
      throw new Error("offline")
    })
    assert.equal(requests, 1)
    assert.equal(tag, "en")
    assert.equal(currentCatalogTag(), "en")
    assert.equal(document.documentElement.lang, "en")
    assert.equal(document.documentElement.dir, "ltr")
    assert.equal(activateCatalog({ ...english, lang: "ja", "legacy.webMenu": "" }, "ja"), "en")
  } finally {
    resetCatalogForTest()
    Object.defineProperty(globalThis, "document", { configurable: true, value: oldDocument })
    Object.defineProperty(globalThis, "navigator", { configurable: true, value: oldNavigator })
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: oldStorage })
  }
})
