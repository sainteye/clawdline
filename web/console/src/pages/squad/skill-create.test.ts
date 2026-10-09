import { test } from "node:test"
import assert from "node:assert/strict"
import type { SquadPersona, SquadView } from "./model.ts"
// @ts-expect-error -- Node's strip-types runner uses the source .ts extension.
import { ACTIVE_SKILL_BYTES, activeSkillBytes, sameSkill, skillInputError } from "./skill-create.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { withCatalog } from "../../catalog-testing.ts"

withCatalog("zh-Hant")

test("an ambiguous catalog reply can be reconciled only with identical stored content", () => {
  const icon = { accent: "#123456", cells: [["#123456"]] }
  const skill = { skill_id: "user.skill.s123", version: "1", source: "user-authored", license: "unspecified",
    name: { en: "技能", "zh-Hant": "技能" }, purpose: { en: "整理", "zh-Hant": "整理" }, icon, content: "完整內容" }
  assert.equal(sameSkill(skill, skill), true)
  assert.equal(sameSkill({ ...skill, content: "被其他寫入改掉" }, skill), false)
})

test("the editor checks UTF-8 body size and leaves room for the rest of a launch snapshot", () => {
  assert.match(skillInputError("  ", "用途", "內容"), /請填寫/)
  assert.match(skillInputError("名稱", "用途", "中".repeat(22_000)), /64 KiB/)
  const body = "x".repeat(64 * 1024)
  const choices = Array.from({ length: 8 }, (_, i) => ({ id: `user.skill.s${i}`, version: "1", enabled: true }))
  const persona = { skillsSetting: { value: choices }, skills: choices.map((choice) => ({ ...choice, body })) } as unknown as SquadPersona
  const view = { catalogSkills: [] } as unknown as SquadView
  assert.equal(activeSkillBytes(view, persona, null), ACTIVE_SKILL_BYTES)
  assert.equal(activeSkillBytes(view, persona, { id: "user.skill.s9", version: "1", body }), ACTIVE_SKILL_BYTES + body.length)
})
