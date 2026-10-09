import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner uses the source .ts extension.
import { squadView, type WireCatalog, type WireSettings } from "./wire.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { withCatalog } from "../../catalog-testing.ts"

withCatalog("zh-Hant")

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
  assert.equal(view.personas[0].skills[0].body, "Complete skill body")
  assert.equal(view.teams[0].name, "新團隊")
})

test("a different Project inherits global text without the first Project's override", () => {
  const global = settings("global", field("Global handbook", "global", 3))
  const project = settings("project-b", field("Global handbook", "global", 3))
  const view = squadView(catalog, global, project, [{ id: "project-b", kind: "repo", name: "Project B", path: "/work/b" }], [], true)
  assert.equal(view.personas[0].handbook.value, "Global handbook")
  assert.equal(view.personas[0].handbook.source, "global")
})
