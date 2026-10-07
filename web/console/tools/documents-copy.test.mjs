import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { test } from "node:test"

const catalog = tag => JSON.parse(readFileSync(new URL(`../public/catalogs/${tag}.json`, import.meta.url), "utf8"))

test("the Documents error sentence is complete before the legacy code and ref are added", () => {
  for (const tag of ["en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"]) {
    const values = catalog(tag)
    for (const key of ["invalidLinkError", "copyUnavailableError", "shareUnavailableError"]) {
      assert.doesNotMatch(values[`documents.${key}`], /\{arg[0-9]+\}/u, `${tag} ${key}`)
    }
    assert.match(values["legacy.webFailWithTag"], /\{text\}/u)
    assert.match(values["legacy.webFailWithTag"], /\{tag\}/u)
    assert.equal(values["documents.menu"], undefined, "React owns the Session menu label")
  }
})

test("English, Spanish, Portuguese and French use one-byte sentences at one", () => {
  for (const [tag, singular, plural] of [
    ["en", "byte", "bytes"], ["es", "byte", "bytes"],
    ["pt-BR", "byte", "bytes"], ["fr", "octet", "octets"],
  ]) {
    const values = catalog(tag)
    for (const key of ["listProjectMeta", "listTaskMeta", "projectMeta", "taskMeta"]) {
      assert.match(values[`documents.${key}One`], new RegExp(`\\b${singular}$`, "u"), `${tag} ${key} 1`)
      assert.match(values[`documents.${key}`], new RegExp(`\\b${plural}$`, "u"), `${tag} ${key} 0/many`)
    }
  }
})
