// Shared native command and skill completion for the Session composer and bar.
// Provider reference snapshots are merged with metadata read by the daemon.
// Choosing a row only fills its invocation; sending remains an explicit action.
// File-backed metadata uses the daemon's five-minute freshness window.
// @ts-expect-error -- Node source tests use the source extension.
import { commandCatalog, commandDescription, commandInvocation, type MenuCommand } from "./command-catalog.ts"
export { commandDescription, commandInvocation }

import type { AssistantSkill, SkillsReply } from "@clawdline/contract"
import { clampSkillPickerIndex, selectedSkill } from "./js/input/skill-picker-state.js"

export { clampSkillPickerIndex, selectedSkill }

/** Kept for the compact legacy picker; the React lists scroll through all results. */
export const SKILL_ROWS = 9

/** `skillInvocationPrefix`: Codex spells its own `$`; everything else is `/`. */
export function skillPrefix(assistant: string | undefined): string {
  return assistant === "codex" ? "$" : "/"
}

/**
 * `query()`: the incomplete mention filling the whole box, or null. `/` opens
 * it for both assistants and Codex's native `$` too; a space or a second
 * slash ends it — from there the words are arguments, and the ordinary send
 * owns them.
 */
export function skillQuery(text: string, assistant: string | undefined): string | null {
  const pattern = assistant === "codex" ? /^[/$][^\s/$]*$/ : /^\/[^\s/]*$/
  return pattern.test(text) ? text.slice(1).toLowerCase() : null
}

/**
 * `rank()`: the beginning of a name wins, then a component after `-`, `_` or
 * `:`, then a separator-free spelling, and only then words in the
 * description. Null is no match.
 */
function rank(skill: MenuCommand, q: string): number | null {
  const name = String(skill.name || "").toLowerCase()
  if (!q) return 0
  if (name.indexOf(q) === 0 || skill.aliases?.some(alias => alias.startsWith(q))) return 0
  if (name.split(/[-_:]/).some((part) => part.indexOf(q) === 0)) return 1
  if (name.replace(/[-_:]/g, "").indexOf(q.replace(/[-_:]/g, "")) === 0) return 2
  if ((skill.description + " " + (skill.descriptionZhHant || "")).toLowerCase().indexOf(q) >= 0) return 3
  return null
}

/** Every match remains reachable by scrolling or keyboard navigation. */
export function filterSkills(items: readonly AssistantSkill[] | undefined, q: string, prefix = "/"): AssistantSkill[] {
  return (items || [])
    .filter(skill => prefix !== "$" || !skill.invocation || skill.invocation.startsWith("$"))
    .map((skill) => ({ skill, rank: rank(skill, q) }))
    .filter((row): row is { skill: AssistantSkill; rank: number } => row.rank !== null)
    .sort((a, b) => a.rank - b.rank || String(a.skill.name).localeCompare(String(b.skill.name)))
    .map((row) => row.skill)
}

/** How long a fetched catalog is used before it is asked for again: the daemon's own window. */
const FRESH_MS = 5 * 60 * 1000

const catalogs = new Map<string, { at: number; skills: AssistantSkill[] }>()
const loading = new Map<string, Promise<AssistantSkill[]>>()

/** The catalog already held for a session, or undefined when none is fresh. */
export function heldSkills(id: string, assistant?: string): AssistantSkill[] | undefined {
  const held = catalogs.get((assistant || "") + "\0" + id)
  return held && Date.now() - held.at < FRESH_MS ? held.skills : undefined
}

/**
 * `api.skills`, asked once per session while it is in flight. Autocomplete is
 * a convenience, never a reason a box should fail: failures retain the bundled
 * native reference, and an unknown command may still be sent — the assistant gives the
 * authoritative answer.
 */
export function loadSkills(id: string, reader?: () => Promise<AssistantSkill[]>, assistant?: string): Promise<AssistantSkill[]> {
  const key = (assistant || "") + "\0" + id
  const held = heldSkills(id, assistant)
  if (held) return Promise.resolve(held)
  const pending = loading.get(key)
  if (pending) return pending
  let resolved = false
  const asked = (reader ? reader() : fetch("/v1/sessions/" + encodeURIComponent(id) + "/skills")
    .then((res) => (res.ok ? (res.json() as Promise<SkillsReply>) : null))
    .then((answer) => (answer && Array.isArray(answer.skills) ? answer.skills : [])))
    .then((skills) => { resolved = true; return commandCatalog(assistant, skills) })
    .catch(() => commandCatalog(assistant, []))
    .then((skills) => {
      if (resolved) catalogs.set(key, { at: Date.now(), skills })
      loading.delete(key)
      return skills
    })
  loading.set(key, asked)
  return asked
}
