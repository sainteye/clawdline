import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner uses the source .ts extension.
import { squadView, type WireCatalog, type WireSettings } from "./wire.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { withCatalog } from "../../catalog-testing.ts"
import builtinSource from "../../builtin-display-source.json" with { type: "json" }

withCatalog("zh-Hant")

test("bundled Squad metadata is translated while bodies and versions stay authoritative", () => {
  const words = (key: keyof typeof builtinSource.en) => ({ en: builtinSource.en[key], "zh-Hant": builtinSource["zh-Hant"][key] })
  const bundled = structuredClone(catalog)
  const definition = bundled.definitions[0]
  definition.definition_id = "clawdline.persona.backend"
  definition.name = words("builtinPersona.backend.name")
  definition.summary = words("builtinPersona.backend.summary")
  const skill = bundled.skills[0]
  skill.skill_id = "clawdline.skill.golang-concurrency"
  skill.name = words("builtinSkill.golang-concurrency.name")
  skill.purpose = words("builtinSkill.golang-concurrency.purpose")
  bundled.teams[0] = { team_id: "clawdline.team.engineering", name: { en: "engineering", "zh-Hant": "engineering" }, personas: [] }
  const global = settings("global", field("Handbook"))
  global.personas[0].definition_id = definition.definition_id
  global.personas[0].skills.value[0].id = skill.skill_id
  const before = JSON.stringify(bundled)
  const view = squadView(bundled, global, global, [], [], true)
  assert.equal(view.personas[0].name, "後端工程師")
  assert.equal(view.personas[0].summary, definition.summary["zh-Hant"])
  assert.equal(view.personas[0].skills[0].name, "Go 並行設計")
  assert.equal(view.catalogSkills[0].purpose, "設計與審查 Go 並行流程")
  assert.notEqual(view.teams[0].name, "engineering")
  assert.equal(view.personas[0].body, definition.body)
  assert.equal(view.personas[0].version, definition.version)
  assert.equal(view.personas[0].skills[0].body, skill.content)
  assert.equal(JSON.stringify(bundled), before)
})

const icon = { accent: "#fff", cells: [["#fff"]] }
const field = <T>(value: T, source: "default" | "global" | "project" = "default", version = 0) =>
  ({ value, source, version, present: source !== "default" })

const catalog: WireCatalog = {
  catalog_version: 8,
  definitions: [{
    definition_id: "community.persona.writer", version: "v2", source: "community", license: "MIT",
    name: { en: "Writer", "zh-Hant": "撰稿角色" }, summary: { en: "Writes", "zh-Hant": "撰寫長篇內容" },
    body: "Complete role body", icon, teams: [{ id: "community.team", version: "v1" }],
    skills: [{ id: "community.skill", version: "v1", enabled: true }],
  }],
  teams: [{ team_id: "community.team", name: { en: "Team", "zh-Hant": "新團隊" }, personas: [{ id: "community.persona.writer", version: "v2" }] }],
  skills: [{ skill_id: "community.skill", version: "v1", source: "community", license: "MIT", name: { en: "Draft", "zh-Hant": "草稿" },
    purpose: { en: "Drafts", "zh-Hant": "整理草稿" }, icon, content: "Complete skill body" }],
}

function settings(scope: string, handbook: ReturnType<typeof field<string>>, skills = field([{ id: "community.skill", version: "v1", enabled: true }])): WireSettings {
  return {
    scope_id: scope, scope_kind: scope === "global" ? "global" : "repo", motion: field(true), motion_settings_version: 4,
    personas: [{
      definition_id: "community.persona.writer", settings_version: 7,
      handbook, global_handbook: field("Global handbook", "global", 3),
      auto_assign: field(true), skills,
    }],
  }
}

test("project handbook keeps global text beside an explicit empty override", () => {
  const global = settings("global", field("Global handbook", "global", 3))
  const project = settings("project-a", field("", "project", 7))
  const projects = [{ id: "project-a", kind: "repo" as const, name: "Project A", path: "/work/a" }]
  const view = squadView(catalog, global, project, projects, [], true)
  assert.equal(view.scopeId, "project-a")
  assert.equal(view.personas[0].handbook.global, "Global handbook")
  assert.equal(view.personas[0].handbook.value, "")
  assert.equal(view.personas[0].handbook.source, "project")
  assert.equal(view.personas[0].settingsVersion, 7)
  assert.equal(view.motionSettingsVersion, 4)
  assert.equal(view.autoDispatchSupported, false)
  assert.equal(view.personas[0].skills[0].body, "Complete skill body")
  assert.equal(view.teams[0].name, "新團隊")
})

test("machine role dispatch stays global while a Project is selected", () => {
  const global = { ...settings("global", field("")), auto_dispatch: field(false, "global", 6), auto_dispatch_version: 6 }
  const view = squadView(catalog, global, settings("project-a", field("")), [], [], true)
  assert.equal(view.autoDispatchSupported, true)
  assert.equal(view.autoDispatch.value, false)
  assert.equal(view.autoDispatchVersion, 6)
})

test("a different Project inherits global text without the first Project's override", () => {
  const global = settings("global", field("Global handbook", "global", 3))
  const project = settings("project-b", field("Global handbook", "global", 3))
  const view = squadView(catalog, global, project, [{ id: "project-b", kind: "repo", name: "Project B", path: "/work/b" }], [], true)
  assert.equal(view.personas[0].handbook.value, "Global handbook")
  assert.equal(view.personas[0].handbook.source, "global")
})

test("a skill shared by two roles is one catalog card, and a role lists its several skills in order", () => {
  const shared = catalog.skills[0]
  const second = { ...shared, skill_id: "community.skill.second", content: "Second skill body" }
  const editor = {
    ...catalog.definitions[0], definition_id: "community.persona.editor",
    skills: [{ id: second.skill_id, version: "v1", enabled: true }, { id: shared.skill_id, version: "v1", enabled: true }],
  }
  const both: WireCatalog = { ...catalog, definitions: [catalog.definitions[0], editor], skills: [shared, second] }
  const global = settings("global", field(""))
  global.personas.push({ ...global.personas[0], definition_id: editor.definition_id, skills: field(editor.skills) })
  const view = squadView(both, global, global, [], [], true)
  assert.equal(view.catalogSkills.length, 2)
  assert.deepEqual(view.catalogSkills.map((skill) => skill.id), [shared.skill_id, second.skill_id])
  assert.deepEqual(view.personas[0].skills.map((skill) => skill.id), [shared.skill_id])
  assert.deepEqual(view.personas[1].skills.map((skill) => [skill.id, skill.order, skill.status]),
    [[second.skill_id, 1, "available"], [shared.skill_id, 2, "available"]])
  assert.equal(view.personas[1].skills[1].body, view.personas[0].skills[0].body)
})
