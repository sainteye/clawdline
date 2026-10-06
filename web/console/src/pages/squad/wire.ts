import { catalogWord, currentCatalogTag } from "../../catalog.js"
import type { Icon } from "@clawdline/contract"
import type { Effective, SquadPersona, SquadProject, SquadSkill, SquadView } from "./model.js"

export interface WireNames { en: string; "zh-Hant": string }
export interface WireChoice { id: string; version: string; enabled: boolean }
export interface WireField<T> { value: T; source: "default" | "global" | "project"; present: boolean; version: number }
export interface WireDefinition {
  definition_id: string; short_id?: string; version: string; source: string; license: string
  name: WireNames; summary: WireNames; body: string; icon: Icon
  teams: { id: string; version: string }[]; skills: WireChoice[]
}
export interface WireTeam { team_id: string; name: WireNames; personas: { id: string; version: string }[] }
export interface WireSkill {
  skill_id: string; version: string; source: string; license: string
  name: WireNames; purpose: WireNames; icon: Icon; content: string
  folder?: boolean
  files?: { path: string; content_base64: string }[]
}
export interface WireCatalog { catalog_version: number; definitions: WireDefinition[]; teams: WireTeam[]; skills: WireSkill[] }
export interface WireEffectivePersona {
  definition_id: string; settings_version: number; handbook: WireField<string>; global_handbook: WireField<string>
  auto_assign: WireField<boolean>; skills: WireField<WireChoice[]>
}
export interface WireSettings {
  scope_id: string; scope_kind: "global" | "repo" | "place"; place_id?: string
  motion: WireField<boolean>; motion_settings_version: number; personas: WireEffectivePersona[]
}

function name(words: WireNames): string { return currentCatalogTag() === "zh-Hant" ? words["zh-Hant"] || words.en : words.en || words["zh-Hant"] }

function effective<T>(field: WireField<T>, global: T): Effective<T> {
  return { value: field.value, global, source: field.source, present: field.present, version: field.version }
}

/** Maps one authoritative catalog and two server-resolved scopes into a view. */
export function squadView(
  catalog: WireCatalog, global: WireSettings, current: WireSettings, projects: SquadProject[],
  sessions: SquadView["sessions"], canWrite: boolean,
): SquadView {
  const currentByID = new Map(current.personas.map((entry) => [entry.definition_id, entry]))
  const globalByID = new Map(global.personas.map((entry) => [entry.definition_id, entry]))
  const skillByKey = new Map(catalog.skills.map((entry) => [`${entry.skill_id}\u0000${entry.version}`, entry]))
  const personas: SquadPersona[] = catalog.definitions.map((definition) => {
    const entry = currentByID.get(definition.definition_id)
    const globalEntry = globalByID.get(definition.definition_id)
    if (!entry || !globalEntry) throw Object.assign(new Error(catalogWord("literal", "3057a039e94d")), { code: "catalog_inconsistent" })
    const globalChoices = globalEntry.skills.value
    const skills: SquadSkill[] = entry.skills.value.map((choice, index) => {
      const found = skillByKey.get(`${choice.id}\u0000${choice.version}`)
      return {
        id: choice.id, name: found ? name(found.name) : choice.id,
        purpose: found ? name(found.purpose) : catalogWord("literal", "feba2f8d9bcd"),
        body: found?.content ?? "", folder: found?.folder, files: found?.files, source: found?.source ?? catalogWord("literal", "2316613f030f"), version: choice.version,
        license: found?.license ?? "", icon: found?.icon, status: found ? "available" : "unavailable",
        enabled: effective({ ...entry.skills, value: choice.enabled }, globalChoices.find((row) => row.id === choice.id)?.enabled ?? false),
        order: index + 1,
      }
    })
    return {
      id: definition.definition_id, shortId: definition.short_id, name: name(definition.name),
      subtitle: definition.short_id ?? definition.definition_id, summary: name(definition.summary),
      body: definition.body, source: definition.source, version: definition.version, icon: definition.icon,
      teamIds: definition.teams.map((team) => team.id), settingsVersion: entry.settings_version,
      enabled: effective(entry.auto_assign, globalEntry.auto_assign.value),
      handbook: effective(entry.handbook, entry.global_handbook.value),
      skillsSetting: effective(entry.skills, globalChoices), skills,
    }
  })
  return {
    scopeId: current.scope_id,
    project: projects.find((row) => row.id === current.scope_id) ?? null, projects,
    teams: catalog.teams.map((team) => ({ id: team.team_id, name: name(team.name) })), personas,
    catalogSkills: catalog.skills.map((skill) => ({
      id: skill.skill_id, name: name(skill.name), purpose: name(skill.purpose), body: skill.content, folder: skill.folder, files: skill.files,
      source: skill.source, version: skill.version, license: skill.license, icon: skill.icon,
    })), sessions,
    motion: effective(current.motion, global.motion.value),
    motionSettingsVersion: current.motion_settings_version,
    catalogVersion: catalog.catalog_version, canWrite, partial: false,
  }
}
