// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord } from "../../catalog.ts"
import type { Icon } from "@clawdline/contract"
import type { NewSquadSkill } from "./api.js"
import type { SquadPersona, SquadView } from "./model.js"
import type { WireSkill } from "./wire.js"

export const SKILL_BODY_BYTES = 64 * 1024 // squad.body_bytes
export const ACTIVE_SKILL_BYTES = 512 * 1024 // squad.console_active_skill_bytes
const bytes = (text: string) => new TextEncoder().encode(text).byteLength

export interface SkillTransaction {
  skill: NewSquadSkill
  expectedVersion: number
  key: string
  scopeId: string
  personaId: string
  catalogSaved: boolean
}

export function makeSkill(name: string, purpose: string, content: string, icon: Icon): NewSquadSkill {
  const title = name.trim()
  const summary = purpose.trim()
  return {
    skill_id: "user.skill.s" + crypto.randomUUID().replaceAll("-", ""), version: "1",
    source: "user-authored", license: "unspecified",
    name: { en: title, "zh-Hant": title }, purpose: { en: summary, "zh-Hant": summary },
    icon, content: content.trim(),
  }
}

export function skillInputError(name: string, purpose: string, content: string): string {
  if (!name.trim() || !purpose.trim() || !content.trim()) return catalogWord("literal", "ea38578dd6df")
  if (bytes(content.trim()) > SKILL_BODY_BYTES) return catalogWord("literal", "b25dc4eefbd9")
  return ""
}

export function sameSkill(actual: WireSkill, expected: NewSquadSkill): boolean {
  return actual.skill_id === expected.skill_id && actual.version === expected.version &&
    actual.source === expected.source && actual.license === expected.license &&
    actual.name.en === expected.name.en && actual.name["zh-Hant"] === expected.name["zh-Hant"] &&
    actual.purpose.en === expected.purpose.en && actual.purpose["zh-Hant"] === expected.purpose["zh-Hant"] &&
    actual.content === expected.content && !!actual.folder === !!expected.folder && JSON.stringify(actual.icon) === JSON.stringify(expected.icon) &&
    JSON.stringify(actual.files ?? []) === JSON.stringify(expected.files ?? [])
}

/** Leaves room for the definition, handbook, icons, references and JSON envelope. */
export function activeSkillBytes(view: SquadView, persona: SquadPersona, added: { id: string; version: string; body: string; files?: { path: string; content_base64: string }[] } | null): number {
  const size = (body: string, files?: { content_base64: string }[]) => bytes(body) + (files ?? []).reduce((total, file) => total + Math.floor(file.content_base64.length * 3 / 4) - (file.content_base64.endsWith("==") ? 2 : file.content_base64.endsWith("=") ? 1 : 0), 0)
  const bodyByKey = new Map(view.catalogSkills.map((skill) => [`${skill.id}\u0000${skill.version}`, size(skill.body, skill.files)]))
  for (const skill of persona.skills) bodyByKey.set(`${skill.id}\u0000${skill.version}`, size(skill.body, skill.files))
  if (added) bodyByKey.set(`${added.id}\u0000${added.version}`, size(added.body, added.files))
  let total = 0
  for (const choice of persona.skillsSetting.value) {
    if (choice.enabled) total += bodyByKey.get(`${choice.id}\u0000${choice.version}`) ?? 0
  }
  if (added && !persona.skillsSetting.value.some((choice) => choice.id === added.id)) total += size(added.body, added.files)
  return total
}
