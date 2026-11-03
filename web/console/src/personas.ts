// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { builtinDisplay } from "./builtin-display.ts"
// @ts-expect-error -- Node loads the source extension in focused behavior tests.
import { personaDisplayCatalog } from "./cloud/persona-cache.ts"
import type { Persona, PersonaCatalog } from "@clawdline/contract"

/**
 * A machine's persona catalog (docs/personas.md), read once per machine.
 *
 * It used to be read once per page, because a console showed one machine for
 * its whole life. It no longer does: the hosted console repoints its reader at
 * the machine of an opened fleet Session and at a machine chosen in the header
 * (`cloud/CloudGate.tsx` `pointAt`), and a start sheet opened for another
 * machine offers that machine's roles. So the catalog is held per machine, and
 * `forgetPersonas` drops every one of them when the account behind them
 * changes. Creating a role or adopting a package also drops the cached reads:
 * the catalog can now change while the daemon runs.
 *
 * A read that fails for any reason — an older daemon without the route, a
 * machine on Clawdline Cloud that does not list `personas` among its commands,
 * a dropped connection — is an empty catalog: no role chips and no bots, never
 * a sheet that breaks. Nothing here says a persona the machine did not name.
 */

/** The machine's own key; the console's current machine has no name to give. */
const HERE = ""

const catalogs = new Map<string, Persona[]>()
const readings = new Map<string, Promise<Persona[]>>()
const retries = new Map<string, ReturnType<typeof setTimeout>>()
const listeners = new Set<() => void>()
let generation = 0

// A failed Cloud read may be a momentary loss of the machine connection. Keep
// one retry per machine so a list of Sessions cannot multiply the requests.
const RETRY_MS = 5_000

function wellFormed(p: unknown): p is Persona {
  const q = p as Persona | null
  return !!q && typeof q.id === "string" && !!q.id && !!q.name && typeof q.name.en === "string" && !!q.icon
}

/**
 * Read a catalog, once per machine; every later call answers from that read.
 *
 * `machine` names a machine other than the one this console is reading — a
 * start sheet opened for another machine on the account. Without it the read
 * is this page's own machine, as it has always been.
 */
export function loadPersonas(read: typeof fetch = (...args) => fetch(...args),
  machine: string | null = null): Promise<Persona[]> {
  const key = machine || HERE
  const held = readings.get(key)
  if (held) return held
  const started = generation
  const reading = (async () => {
    let retry = false
    let list: Persona[] = []
    try {
      const res = await read("/v1/personas?view=display-v1" + (machine ? "&machine=" + encodeURIComponent(machine) : ""),
        { credentials: "same-origin" })
      if (res.ok) {
        const body = personaDisplayCatalog(await res.json()) as PersonaCatalog | null
        list = Array.isArray(body?.personas) ? body.personas.filter(wellFormed).map(withTeams) : []
      } else {
        retry = res.status === 408 || res.status === 429 || (res.status >= 500 && res.status !== 501)
      }
    } catch {
      retry = true
    }
    if (started !== generation) return catalogs.get(key) ?? []
    catalogs.set(key, list)
    listeners.forEach((fn) => fn())
    if (retry) {
      readings.delete(key)
      if (!retries.has(key)) retries.set(key, setTimeout(() => {
        retries.delete(key)
        void loadPersonas(read, machine)
      }, RETRY_MS))
    } else {
      const timer = retries.get(key)
      if (timer) clearTimeout(timer)
      retries.delete(key)
    }
    return list
  })()
  readings.set(key, reading)
  return reading
}

/** The catalog if it has been read, or null while it has not. */
export function personasNow(machine: string | null = null): Persona[] | null {
  return catalogs.get(machine || HERE) ?? null
}

/**
 * Drop every catalog read so far.
 *
 * The console changed which machine it reads, so "this page's machine" now
 * means a different daemon, and a named machine may be one this browser can no
 * longer read. Nothing is re-read here; the next sheet that needs a catalog
 * asks for it.
 */
export function forgetPersonas(): void {
  generation++
  for (const timer of retries.values()) clearTimeout(timer)
  retries.clear()
  catalogs.clear()
  readings.clear()
  listeners.forEach((fn) => fn())
}

/** Called once the catalog arrives. */
export function onPersonas(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

/** Keep a mounted view reading its machine after a Cloud reader switch clears the catalogs. */
export function watchPersonas(machine: string | null, show: (list: Persona[]) => void,
  read: typeof fetch = (...args) => fetch(...args)): () => void {
  let active = true
  const update = () => {
    if (!active) return
    const current = personasNow(machine)
    show(current ?? [])
    if (current === null) void loadPersonas(read, machine)
  }
  const off = onPersonas(update)
  update()
  return () => { active = false; off() }
}

/** The persona a session row or an assignment names, when the catalog has it. */
export function personaById(list: readonly Persona[] | null, id: string | null | undefined): Persona | null {
  if (!id || !list) return null
  return list.find((p) => p.id === id) ?? null
}

/** Use the catalog's unique default for a Board item kind, without reading its words. */
export function suggestedPersonaForKind(list: readonly Persona[] | null, kind: string): Persona | null {
  const matches = (list ?? []).filter((persona) => (persona.suggested_kinds ?? []).includes(kind))
  return matches.length === 1 ? matches[0] : null
}

/** The persona's display metadata in the active catalog. */
export function personaName(p: Persona): string {
  return builtinDisplay("persona", p.id, p.name, p.summary ?? { en: "" }).name || p.id
}

export function personaSummary(p: Persona): string {
  return builtinDisplay("persona", p.id, p.name, p.summary ?? { en: "" }).detail
}

/** Name and one-sentence summary, for a title attribute or an accessible label. */
export function personaTitle(p: Persona): string {
  const summary = personaSummary(p)
  return summary ? personaName(p) + " — " + summary : personaName(p)
}

/** What the Session detail header says about a row's persona. */
export interface HeadPersona {
  persona: Persona
  name: string
  summary: string
  title: string
}

/**
 * The persona the Session detail header shows, or null for none: a row with no
 * persona, a catalog not read yet or unreadable, and a name the catalog does
 * not have all show nothing. The header never guesses a role from an id.
 */
export function headPersona(list: readonly Persona[] | null, id: string | null | undefined): HeadPersona | null {
  const persona = personaById(list, id)
  if (!persona) return null
  return { persona, name: personaName(persona), summary: personaSummary(persona), title: personaTitle(persona) }
}

/**
 * What a session row shows for the role it was launched as: the bot at the
 * head of its second line, with name plus summary for its title and accessible
 * name. A row with no persona, or one whose id this machine's catalog does not
 * name, draws nothing there.
 */
export function rowPersonaLine(
  list: readonly Persona[] | null,
  id: string | null | undefined,
): { persona: Persona; name: string; title: string } | null {
  const persona = personaById(list, id)
  return persona ? { persona, name: personaName(persona), title: personaTitle(persona) } : null
}

/**
 * The teams a persona can belong to (`teams` in personas.schema.json), in the
 * order the role row's switcher lists them. The console names them
 * (`personaTeam*` in next-strings.ts); the catalog only says which.
 */
export const PERSONA_TEAMS = ["engineering", "marketing", "product", "quality", "operations", "design", "business", "custom"] as const
export type PersonaTeam = (typeof PERSONA_TEAMS)[number]

function knownTeam(team: unknown): team is PersonaTeam {
  return (PERSONA_TEAMS as readonly unknown[]).includes(team)
}

/**
 * A persona's teams, never empty. The daemon sends `teams`; one from before
 * that field sends a single `team` (read as a list of one) or, older still,
 * neither, which is engineering: every persona such a daemon has. Names this
 * console does not know are dropped, and a persona left with none is
 * engineering too.
 */
export function personaTeams(p: Persona): PersonaTeam[] {
  const raw = p as { teams?: unknown; team?: unknown }
  const listed = Array.isArray(raw.teams) ? raw.teams : raw.team !== undefined ? [raw.team] : []
  const teams = PERSONA_TEAMS.filter((t) => listed.includes(t))
  return teams.length ? teams : ["engineering"]
}

/** A catalog entry with `teams` filled in from whatever the daemon sent. */
export function withTeams(p: Persona): Persona {
  return { ...p, teams: personaTeams(p) }
}

function inTeam(p: Persona, team: string): boolean {
  return (personaTeams(p) as string[]).includes(team)
}

/** The teams that have at least one persona, in switcher order. One or none hides the switcher. */
export function teamsOffered(list: readonly Persona[] | null): PersonaTeam[] {
  const present = new Set((list ?? []).flatMap(personaTeams))
  return PERSONA_TEAMS.filter((t) => present.has(t))
}

/** The personas the chips show for one team, in catalog order. */
export function personasOfTeam(list: readonly Persona[] | null, team: PersonaTeam): Persona[] {
  return (list ?? []).filter((p) => inTeam(p, team))
}

/**
 * The team the role row shows. A chosen persona is never hidden behind a team
 * that does not hold it: the team last picked in this browser when it holds
 * the persona, otherwise the persona's first team in switcher order. With no
 * persona chosen, the remembered team when it still has personas; otherwise
 * engineering (or the first team there is).
 */
export function shownTeam(
  list: readonly Persona[] | null,
  chosen: string | null | undefined,
  preferred: string | null | undefined,
): PersonaTeam {
  const persona = personaById(list, chosen)
  if (persona) return preferred && knownTeam(preferred) && inTeam(persona, preferred) ? preferred : personaTeams(persona)[0]
  const offered = teamsOffered(list)
  if (preferred && (offered as string[]).includes(preferred)) return preferred as PersonaTeam
  return offered.includes("engineering") || !offered.length ? "engineering" : offered[0]
}

/**
 * Switching team: the chosen persona stays when the new team holds it too;
 * otherwise the choice becomes no role, so a start never sends a persona the
 * person cannot see.
 */
export function switchTeam(
  list: readonly Persona[] | null,
  chosen: string | null | undefined,
  team: PersonaTeam,
): { team: PersonaTeam; chosen: string } {
  const persona = personaById(list, chosen)
  return { team, chosen: persona && inTeam(persona, team) ? persona.id : "" }
}

const REMEMBERED_TEAM = "clawdline.persona.team"

/** The team last picked in this browser; "" for none. */
export function rememberedTeam(): string {
  try {
    return localStorage.getItem(REMEMBERED_TEAM) || ""
  } catch {
    // refusal-ok: a browser without storage just does not remember.
    return ""
  }
}

export function rememberTeam(team: PersonaTeam): void {
  try {
    localStorage.setItem(REMEMBERED_TEAM, team)
  } catch {
    // refusal-ok: a browser without storage just does not remember.
  }
}
