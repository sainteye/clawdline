import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { TERMINAL_REFUSAL_WORDS } from "./refusal-table.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { nextWord } from "../../next-strings.ts"

/** The contract's own list, read from the generated file (it cannot be imported under `node --test`). */
function contractCodes(): string[] {
  const generated = readFileSync(new URL("../../../../contract/src/generated.ts", import.meta.url), "utf8")
  const line = generated.match(/export const TerminalRefusalCodeValues[^=]*=\s*\[([^\]]*)\]/)
  assert.ok(line, "TerminalRefusalCodeValues is in the generated contract")
  return [...line[1].matchAll(/"([^"]+)"/g)].map((m) => m[1])
}

function inLanguage<T>(lang: string, fn: () => T): T {
  const had = Object.getOwnPropertyDescriptor(globalThis, "document")
  Object.defineProperty(globalThis, "document", { value: { documentElement: { lang } }, configurable: true })
  try {
    return fn()
  } finally {
    if (had) Object.defineProperty(globalThis, "document", had)
    else delete (globalThis as { document?: unknown }).document
  }
}

test("every terminal refusal code the contract lists has its own sentence in both languages", () => {
  const codes = contractCodes()
  assert.ok(codes.length >= 18, "the contract lists the terminal refusals")
  assert.deepEqual(Object.keys(TERMINAL_REFUSAL_WORDS).sort(), [...codes].sort())
  for (const lang of ["en", "zh-Hant"]) {
    const seen = new Map<string, string>()
    for (const code of codes) {
      const words = inLanguage(lang, () => nextWord((TERMINAL_REFUSAL_WORDS as Record<string, Parameters<typeof nextWord>[0]>)[code]))
      assert.ok(words.trim().length > 8, `${lang} ${code} is a sentence`)
      assert.ok(!/\{\w+\}/.test(words), `${lang} ${code} leaves no hole unfilled`)
      assert.ok(!/\bMac\b/.test(words), `${lang} ${code} does not call the machine a Mac`)
      assert.ok(!words.includes(code), `${lang} ${code} is words, not the code`)
      assert.equal(seen.get(words), undefined, `${lang} ${code} says something ${seen.get(words)} does not`)
      seen.set(words, code)
    }
  }
  const zh = inLanguage("zh-Hant", () => nextWord(TERMINAL_REFUSAL_WORDS.terminal_busy))
  assert.match(zh, /[一-鿿]/, "the zh-Hant sentence is Chinese")
})
