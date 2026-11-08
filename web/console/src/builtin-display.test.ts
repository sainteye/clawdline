import test from "node:test"
import assert from "node:assert/strict"
import { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import path from "node:path"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { builtinDisplay, builtinTeamDisplay } from "./builtin-display.ts"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { activateCatalog, CATALOG_TAGS, resetCatalogForTest } from "./catalog.ts"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { personaName, personaSummary } from "./personas.ts"
import source from "./builtin-display-source.json" with { type: "json" }
import { checkBuiltinDisplay } from "../tools/check-builtin-display.mjs"

const english = JSON.parse(readFileSync(new URL("../public/catalogs/en.json", import.meta.url), "utf8"))
const names = (en: string) => ({ en, "zh-Hant": "wire Chinese" })
const role = {
  source: "clawdline", suggested_kinds: [], teams: ["engineering"],
  id: "backend", name: names(source.en["builtinPersona.backend.name"]),
  summary: names(source.en["builtinPersona.backend.summary"]),
  icon: { accent: "#000", cells: [["#000"]] },
}

test("builtin metadata follows all nine active catalogs without changing wire or snapshot content", () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "document")
  Object.defineProperty(globalThis, "document", { configurable: true, value: { documentElement: { lang: "en", setAttribute() {} } } })
  try {
    for (const tag of CATALOG_TAGS) {
      // Distinct test translations prove the active catalog is used for every
      // language, including secondary entries delivered independently of code.
      const translated = Object.fromEntries(Object.keys(source.en).map(key => [key, `${tag} ${key}`]))
      assert.equal(activateCatalog({ ...english, ...translated, lang: tag }, tag), tag)
      for (const key of Object.keys(source.en).filter(key => key.endsWith(".name"))) {
        const [domain, id] = key.split(".")
        const kind = domain === "builtinPersona" ? "persona" : "skill"
        const field = kind === "persona" ? "summary" : "purpose"
        const en = source.en as Record<string, string>
        const display = builtinDisplay(kind, `clawdline.${kind}.${id}`, names(en[key]), names(en[`${domain}.${id}.${field}`]))
        assert.equal(display.name, `${tag} ${key}`)
        assert.equal(display.detail, `${tag} ${domain}.${id}.${field}`)
      }
      const before = JSON.stringify(role)
      assert.equal(personaName(role), `${tag} builtinPersona.backend.name`)
      assert.equal(personaSummary(role), `${tag} builtinPersona.backend.summary`)
      assert.equal(builtinDisplay("persona", "clawdline.persona.backend", role.name, role.summary).name, personaName(role))
      const snapshot = { id: "clawdline.skill.golang-concurrency", name: names(source.en["builtinSkill.golang-concurrency.name"]),
        purpose: names(source.en["builtinSkill.golang-concurrency.purpose"]), content: "immutable historical instruction body", version: "old" }
      const snapshotBefore = JSON.stringify(snapshot)
      assert.equal(builtinDisplay("skill", snapshot.id, snapshot.name, snapshot.purpose).detail, `${tag} builtinSkill.golang-concurrency.purpose`)
      assert.equal(JSON.stringify(snapshot), snapshotBefore)
      for (const id of ["custom.backend", "unknown", "clawdline.persona.unknown"]) {
        assert.equal(builtinDisplay("persona", id, role.name, role.summary).name, tag === "zh-Hant" ? "wire Chinese" : role.name.en)
      }
      const historical = names("Older summary")
      assert.equal(builtinDisplay("persona", role.id, role.name, historical).name, tag === "zh-Hant" ? "wire Chinese" : role.name.en)
      assert.equal(builtinDisplay("persona", role.id, names("Edited name"), role.summary).detail, tag === "zh-Hant" ? "wire Chinese" : role.summary.en)
      assert.equal(builtinDisplay("skill", "custom.skill.golang-concurrency", snapshot.name, snapshot.purpose).name,
        tag === "zh-Hant" ? "wire Chinese" : snapshot.name.en)
      assert.equal(builtinTeamDisplay("clawdline.team.engineering", names("Custom team")), tag === "zh-Hant" ? "wire Chinese" : "Custom team")
      assert.equal(JSON.stringify(role), before)
    }
  } finally {
    resetCatalogForTest()
    if (previous) Object.defineProperty(globalThis, "document", previous)
    else Reflect.deleteProperty(globalThis, "document")
  }
})

test("builtin source guard rejects missing, changed and newly added metadata", () => {
  const root = path.resolve(import.meta.dirname, "../../..")
  assert.equal(checkBuiltinDisplay(root), 140)
  const fixture = mkdtempSync(path.join(tmpdir(), "builtin-display-"))
  try {
    for (const file of ["internal/domain/persona/catalog", "internal/domain/squad/builtin_skills.go", "web/console/src/builtin-display-source.json",
      "web/console/public/catalogs/en.json", "web/console/public/catalogs/zh-Hant.json"]) {
      cpSync(path.join(root, file), path.join(fixture, file), { recursive: true })
    }
    assert.equal(checkBuiltinDisplay(fixture), 140)
    const file = path.join(fixture, "internal/domain/persona/catalog/backend.md")
    const original = readFileSync(file, "utf8")
    writeFileSync(file, original.replace("name_en: Backend Engineer", "name_en: Changed role"))
    assert.throws(() => checkBuiltinDisplay(fixture), /builtinPersona.backend.name: en builtin source\/manifest mismatch/)
    writeFileSync(file, original)
    writeFileSync(path.join(fixture, "internal/domain/persona/catalog/new.md"), original.replace("id: backend", "id: new-role"))
    assert.throws(() => checkBuiltinDisplay(fixture), /builtinPersona.new-role.name/)
    rmSync(path.join(fixture, "internal/domain/persona/catalog/new.md"))
    const skillsFile = path.join(fixture, "internal/domain/squad/builtin_skills.go")
    const skills = readFileSync(skillsFile, "utf8")
    writeFileSync(skillsFile, skills.replace("Design and review Go concurrency", "Changed skill purpose"))
    assert.throws(() => checkBuiltinDisplay(fixture), /builtinSkill.golang-concurrency.purpose: en builtin source\/manifest mismatch/)
    writeFileSync(skillsFile, skills)
    const manifestFile = path.join(fixture, "web/console/src/builtin-display-source.json")
    const manifest = JSON.parse(readFileSync(manifestFile, "utf8"))
    delete manifest.en["builtinSkill.golang-concurrency.name"]
    writeFileSync(manifestFile, JSON.stringify(manifest))
    assert.throws(() => checkBuiltinDisplay(fixture), /builtinSkill.golang-concurrency.name: en builtin source\/manifest mismatch/)
  } finally { rmSync(fixture, { recursive: true, force: true }) }
})
