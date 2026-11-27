// What the owner reads when InstaBox stops a wake: `node --test web/console/src/cloud/instabox-wake.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
// @ts-expect-error -- a `.ts` path, for node; see cloud/forget.test.ts.
import { instaBoxWakeReasonWord } from "./instabox-wake-reason.ts"

const catalog = (locale: string) =>
  JSON.parse(readFileSync(new URL(`../../public/catalogs/${locale}.json`, import.meta.url), "utf8")) as Record<string, string>

test("each key problem the Cloud API names reads differently in English and Chinese", () => {
  const words = ["instabox_key_missing_scope", "instabox_key_expired", "instabox_key_revoked", "instabox_key_refused"]
    .map(instaBoxWakeReasonWord)
  for (const locale of ["en", "zh-Hant"]) {
    const said = words.map((word) => catalog(locale)[`next.${word}`])
    assert.ok(said.every(Boolean), `${locale} has every sentence`)
    assert.equal(new Set(said).size, said.length, `${locale} tells the problems apart`)
  }
})
