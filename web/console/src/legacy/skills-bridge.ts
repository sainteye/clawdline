// The slash menu's logic: `SkillPicker` in `js/input/composer.js`, ported.
//
// `composer.js` is not copied: it binds every listener through `core/dom.js`,
// which looks each id up once at import, before React has drawn the composer.
// Its one pure neighbour is: `js/input/skill-picker-state.js`, beside this
// file byte for byte and imported as it is. The rest — which text opens the
// menu, how a skill ranks against what was typed, the nine rows — is here,
// function for function, and both boxes that offer the menu use it: the
// console's composer (`session/Composer.tsx`) and the input bar
// (`bar/Bar.tsx`, the Swift app's `Controller` skills list, which ranks with
// `ClaudeSkills.matching`, the same four rungs).
//
// Nothing from a SKILL.md is interpreted here. The daemon hands over a name and
// one safe line of description; choosing one only writes the assistant's real
// invocation into the box, and the assistant remains the one that resolves,
// authorises and runs it when the ordinary send happens.
//
// One difference, on purpose: the original holds a session's catalog for as
// long as the page lives. A console window here lives for days, so a catalog is
// asked for again after the five minutes the daemon itself holds it.
import type { AssistantSkill, SkillsReply } from "@clawdline/contract"
import { clampSkillPickerIndex, selectedSkill } from "./js/input/skill-picker-state.js"

export { clampSkillPickerIndex, selectedSkill }

/** The menu draws nine rows at most, as both originals do. */
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
function rank(skill: AssistantSkill, q: string): number | null {
  const name = String(skill.name || "").toLowerCase()
  if (!q) return 0
  if (name.indexOf(q) === 0) return 0
  if (name.split(/[-_:]/).some((part) => part.indexOf(q) === 0)) return 1
  if (name.replace(/[-_:]/g, "").indexOf(q.replace(/[-_:]/g, "")) === 0) return 2
  if (String(skill.description || "").toLowerCase().indexOf(q) >= 0) return 3
  return null
}

/** `filtered()`: the matches, best first and then by name, nine at most. */
export function filterSkills(items: readonly AssistantSkill[] | undefined, q: string): AssistantSkill[] {
  return (items || [])
    .map((skill) => ({ skill, rank: rank(skill, q) }))
    .filter((row): row is { skill: AssistantSkill; rank: number } => row.rank !== null)
    .sort((a, b) => a.rank - b.rank || String(a.skill.name).localeCompare(String(b.skill.name)))
    .slice(0, SKILL_ROWS)
    .map((row) => row.skill)
}

/** How long a fetched catalog is used before it is asked for again: the daemon's own window. */
const FRESH_MS = 5 * 60 * 1000

const catalogs = new Map<string, { at: number; skills: AssistantSkill[] }>()
const loading = new Map<string, Promise<AssistantSkill[]>>()

/** The catalog already held for a session, or undefined when none is fresh. */
export function heldSkills(id: string): AssistantSkill[] | undefined {
  const held = catalogs.get(id)
  return held && Date.now() - held.at < FRESH_MS ? held.skills : undefined
}

/**
 * `api.skills`, asked once per session while it is in flight. Autocomplete is
 * a convenience, never a reason a box should fail: any failure is an empty
 * menu, and an unknown command may still be sent — the assistant gives the
 * authoritative answer.
 */
export function loadSkills(id: string): Promise<AssistantSkill[]> {
  const held = heldSkills(id)
  if (held) return Promise.resolve(held)
  const pending = loading.get(id)
  if (pending) return pending
  const asked = fetch("/v1/sessions/" + encodeURIComponent(id) + "/skills")
    .then((res) => (res.ok ? (res.json() as Promise<SkillsReply>) : null))
    .then((answer) => (answer && Array.isArray(answer.skills) ? answer.skills : []))
    .catch(() => [] as AssistantSkill[])
    .then((skills) => {
      catalogs.set(id, { at: Date.now(), skills })
      loading.delete(id)
      return skills
    })
  loading.set(id, asked)
  return asked
}
