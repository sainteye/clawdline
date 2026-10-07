import { test } from "node:test"
import assert from "node:assert/strict"
import { readdirSync, readFileSync } from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import chinese from "../public/catalogs/zh-Hant.json" with { type: "json" }
import french from "../public/catalogs/fr.json" with { type: "json" }
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { activateCatalog, catalogWord, resetCatalogForTest } from "./catalog.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogLabel, closingMark, colonMark, fullStop, labelled, listSeparator, openingMark, parenthesized, quotedTitle, wordGap } from "./punctuation.ts"

function inLanguage(catalog: Record<string, string>, tag: string, run: () => void) {
  const oldDocument = globalThis.document
  Object.defineProperty(globalThis, "document", { configurable: true, value: { documentElement: { lang: "en", dir: "ltr", setAttribute() {} } } })
  try {
    assert.equal(activateCatalog(catalog, tag), tag)
    run()
  } finally {
    resetCatalogForTest()
    Object.defineProperty(globalThis, "document", { configurable: true, value: oldDocument })
  }
}

test("an English label keeps a space before its value and closes the quote it opened", () => {
  resetCatalogForTest()
  assert.equal(catalogLabel("inline", "7ed8d242eabd") + "Choose a date.", "What you need to do: Choose a date.")
  assert.equal(catalogLabel("inline", "b9fbb8ede4cb") + "Only you can.", "Reason: Only you can.")
  const received = catalogWord("inline", "e3de4a9b5b50")
  assert.equal(received + "Yes" + closingMark(received), "Answer received: “Yes”")
  assert.equal(labelled("Current mode", "Strict"), "Current mode: Strict")
  assert.equal(["a", "b"].join(listSeparator()), "a, b")
  assert.equal(fullStop() + wordGap(), ". ")
  assert.equal("Squad" + parenthesized("HTTP 500"), "Squad (HTTP 500)")
  assert.equal(quotedTitle("Release"), "“Release”")
  assert.equal(colonMark(), ": ")
})

test("Taiwan Chinese keeps its own full-width marks and adds no space", () => {
  inLanguage(chinese, "zh-Hant", () => {
    assert.equal(catalogLabel("inline", "b9fbb8ede4cb") + "只有你能做", "原因：只有你能做")
    const received = catalogWord("inline", "e3de4a9b5b50")
    assert.equal(received + "是" + closingMark(received), "已收到回答：「是」")
    assert.equal(labelled("目前模式", "嚴格"), "目前模式：嚴格")
    assert.equal(["甲", "乙"].join(listSeparator()), "甲、乙")
    assert.equal(fullStop() + wordGap(), "。")
    assert.equal("角色小隊" + parenthesized("HTTP 500"), "角色小隊（HTTP 500）")
    assert.equal(quotedTitle("發布"), "〈發布〉")
  })
})

test("a closing quote is the partner of the one the translation opened, spacing included", () => {
  inLanguage(french, "fr", () => {
    const sending = catalogWord("inline", "56bf73fe1d04")
    assert.equal(sending + "Oui" + closingMark(sending), "Envoi de « Oui »")
    const offline = catalogWord("inline", "bee4fb5a17bb")
    assert.equal(openingMark(offline) + "Atelier" + offline.slice(0, 2), "« Atelier »")
  })
  assert.equal(closingMark("Vererbt von „"), "“")
  assert.equal(closingMark("상속 출처: ‘"), "’")
  assert.equal(closingMark("No quote here"), "")
  assert.equal(openingMark("lines unchanged)"), "(")
  assert.equal(openingMark("行不變）"), "（")
})

test("Taiwan Chinese copy carries the reviewed words, not English left behind", () => {
  assert.equal(chinese["literal.3aa8e038c09d"], "再送一次")
  assert.equal(chinese["legacy.webLedgerFindings"], "發現事項")
  assert.equal(chinese["legacy.webLedgerVerdicts"], "結論")
  assert.equal(chinese["verify.colStalled"], "停滯")
  assert.doesNotMatch(chinese["next.cloudForgetHonest"], /routing/)
  // A full-width mark already carries its own spacing; a space after it is a typing slip.
  const spaced = Object.entries(chinese as Record<string, string>).filter(([, value]) => /[，。：；、] +\S/u.test(value)).map(([key]) => key)
  assert.deepEqual(spaced, [])
})

test("no shown source puts a Chinese punctuation mark beside a catalog fragment", () => {
  // Each language's marks come from punctuation.ts or the catalog. What stays
  // is an icon glyph or a mark already chosen by language.
  const allowed = new Map([
    ["pages/settings/capacity.ts", /startsWith\("zh"\) \? "，"/],
    ["pages/settings/window/SettingsWindow.tsx", /=== "zh-Hant" \? "、"/],
    ["pages/projects/ProjectUnify.tsx", /aria-hidden="true">\{row\.mark === "same" \? "＝"/],
    ["session/Snippets.tsx", /^\s*＋$/],
  ])
  const root = path.dirname(fileURLToPath(import.meta.url))
  const offenders: string[] = []
  const walk = (directory: string) => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const file = path.join(directory, entry.name)
      const name = path.relative(root, file).split(path.sep).join("/")
      if (entry.isDirectory()) { if (name !== "legacy") walk(file); continue }
      if (!/\.tsx?$/u.test(name) || /\.(test|e2e)\.tsx?$/u.test(name)) continue
      // Per-language tables hold whole sentences in each language.
      if (/(^|\/)(next-strings|words|copy|punctuation)\.ts$/u.test(name)) continue
      readFileSync(file, "utf8").split("\n").forEach((line, index) => {
        if (/^\s*(\*|\/\/|\/\*)/u.test(line)) return
        if (!/[　-〿＀-￯]/u.test(line)) return
        // A line with Han text is a Chinese half of a language pair.
        if (/[一-鿿]/u.test(line)) return
        if (allowed.get(name)?.test(line)) return
        offenders.push(`${name}:${index + 1}`)
      })
    }
  }
  walk(root)
  assert.deepEqual(offenders, [])
})
