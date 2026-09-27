import type { Persona, PersonaCatalog } from "@clawdline/contract"

/**
 * The machine's persona catalog (docs/personas.md), read once per page.
 *
 * A console shows one machine for its whole life (`cloud/install.ts`), so one
 * read is one read per machine. The catalog is compiled into the daemon and
 * does not change while it runs.
 *
 * A read that fails for any reason — an older daemon without the route, a
 * machine on Clawdline Cloud that does not list `personas` among its commands,
 * a dropped connection — is an empty catalog: no role chips and no bots, never
 * a sheet that breaks. Nothing here says a persona the machine did not name.
 */

let catalog: Persona[] | null = null
let reading: Promise<Persona[]> | null = null
const listeners = new Set<() => void>()

function wellFormed(p: unknown): p is Persona {
  const q = p as Persona | null
  return !!q && typeof q.id === "string" && !!q.id && !!q.name && typeof q.name.en === "string" && !!q.icon
}

/** Read the catalog, once; every later call answers from the first read. */
export function loadPersonas(read: typeof fetch = (...args) => fetch(...args)): Promise<Persona[]> {
  if (reading) return reading
  reading = (async () => {
    try {
      const res = await read("/v1/personas", { credentials: "same-origin" })
      if (!res.ok) return []
      const body = (await res.json()) as PersonaCatalog | null
      return Array.isArray(body?.personas) ? body.personas.filter(wellFormed) : []
    } catch {
      // refusal-ok: a missing catalog is shown as no roles to pick, not as a failure.
      return []
    }
  })().then((list) => {
    catalog = list
    listeners.forEach((fn) => fn())
    return list
  })
  return reading
}

/** The catalog if it has been read, or null while it has not. */
export function personasNow(): Persona[] | null {
  return catalog
}

/** Called once the catalog arrives. */
export function onPersonas(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

/** The persona a session row or an assignment names, when the catalog has it. */
export function personaById(list: readonly Persona[] | null, id: string | null | undefined): Persona | null {
  if (!id || !list) return null
  return list.find((p) => p.id === id) ?? null
}

function language(): "en" | "zh-Hant" {
  const lang =
    (typeof document !== "undefined" && document.documentElement.lang) ||
    (typeof navigator !== "undefined" && navigator.language) ||
    "en"
  return lang.toLowerCase().startsWith("zh") ? "zh-Hant" : "en"
}

/** The persona's name in the page's language. */
export function personaName(p: Persona): string {
  return p.name[language()] || p.name.en || p.id
}

/** Name and one-sentence summary, for a title attribute or an accessible label. */
export function personaTitle(p: Persona): string {
  const summary = p.summary?.[language()] || p.summary?.en || ""
  return summary ? personaName(p) + " — " + summary : personaName(p)
}

/**
 * The persona a Board item of this kind is offered first: the first catalog
 * entry whose `suggested_kinds` names the kind, and only when exactly one does.
 * A kind two personas suggest (feature: backend and frontend) is the person's
 * to choose, so it gets none.
 */
export function suggestedPersona(list: readonly Persona[] | null, kind: string): Persona | null {
  const matches = (list ?? []).filter((p) => (p.suggested_kinds ?? []).includes(kind))
  return matches.length === 1 ? matches[0] : null
}

const REMEMBERED = "clawdline.start.persona"

/** The start sheet's last choice in this browser; "" for none. */
export function rememberedPersona(): string {
  try {
    return localStorage.getItem(REMEMBERED) || ""
  } catch {
    // refusal-ok: a browser without storage just does not remember.
    return ""
  }
}

export function rememberPersona(id: string): void {
  try {
    if (id) localStorage.setItem(REMEMBERED, id)
    else localStorage.removeItem(REMEMBERED)
  } catch {
    // refusal-ok: a browser without storage just does not remember.
  }
}
