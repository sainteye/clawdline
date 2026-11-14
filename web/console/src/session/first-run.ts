/**
 * What a first-time person needs to see, and what an experienced one may hide.
 *
 * Every value here is a per-browser display preference, kept beside the font
 * scale (`font-scale.ts`). None of them changes what the machine does: hiding
 * the voice button does not turn voice off, and hiding roles does not remove a
 * role from a Session that already has one. A browser that refuses storage
 * keeps the defaults for this page view.
 */
import { useSyncExternalStore } from "react"

export const VOICE_START_KEY = "clawdline.show-voice-start"
export const SESSION_ROLES_KEY = "clawdline.session-roles"
export const SESSIONS_STARTED_KEY = "clawdline.sessions-started"
export const FIRST_TOUR_KEY = "clawdline.first-session-tour"
/** Successful starts before the role row appears on its own. */
export const ROLES_AFTER_STARTS = 5

export type RolesMode = "auto" | "show" | "hide"
/** Whether this machine has a Project, as far as this page has read. */
export type PlacePresence = "unknown" | "none" | "some"

type Store = Pick<Storage, "getItem" | "setItem">

function storage(): Store | null {
  try {
    return typeof localStorage === "undefined" ? null : localStorage
  } catch {
    return null
  }
}

function read(key: string, store: Store | null = storage()): string | null {
  try {
    return store?.getItem(key) ?? null
  } catch {
    return null
  }
}

function write(key: string, value: string, store: Store | null = storage()): boolean {
  try {
    if (!store) return false
    store.setItem(key, value)
    return true
  } catch {
    return false
  }
}

const listeners = new Set<() => void>()
let version = 0
function changed(): void {
  version++
  for (const listener of listeners) listener()
}
function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** Re-render when any first-run value changes. */
export function useFirstRun(): number {
  return useSyncExternalStore(subscribe, () => version, () => version)
}

export function voiceStartShown(store?: Store | null): boolean {
  return read(VOICE_START_KEY, store) === "1"
}

export function setVoiceStartShown(shown: boolean, store?: Store | null): boolean {
  const saved = write(VOICE_START_KEY, shown ? "1" : "0", store)
  changed()
  return saved
}

export function rolesMode(store?: Store | null): RolesMode {
  const raw = read(SESSION_ROLES_KEY, store)
  return raw === "show" || raw === "hide" ? raw : "auto"
}

export function setRolesMode(mode: RolesMode, store?: Store | null): boolean {
  const saved = write(SESSION_ROLES_KEY, mode, store)
  changed()
  return saved
}

/** Starts counted so far, or null before this browser has decided. */
export function sessionsStarted(store?: Store | null): number | null {
  const raw = read(SESSIONS_STARTED_KEY, store)
  if (raw === null) return null
  const n = Number(raw)
  return Number.isInteger(n) && n >= 0 ? n : null
}

/**
 * The first authoritative Session list decides where an unknown browser
 * starts. A person upgrading with Sessions on the machine already used the
 * console, so roles stay where they were; an empty machine starts from zero.
 */
export function seedSessionsStarted(machineHasSessions: boolean, store?: Store | null): void {
  if (sessionsStarted(store) !== null) return
  write(SESSIONS_STARTED_KEY, String(machineHasSessions ? ROLES_AFTER_STARTS : 0), store)
  changed()
}

export function countSessionStarted(store?: Store | null): void {
  const n = sessionsStarted(store) ?? 0
  if (n >= ROLES_AFTER_STARTS) return
  write(SESSIONS_STARTED_KEY, String(n + 1), store)
  changed()
}

export function sessionRolesShown(store?: Store | null): boolean {
  const mode = rolesMode(store)
  if (mode !== "auto") return mode === "show"
  return (sessionsStarted(store) ?? 0) >= ROLES_AFTER_STARTS
}

export function firstTourDone(store?: Store | null): boolean {
  return read(FIRST_TOUR_KEY, store) === "done"
}

export function finishFirstTour(store?: Store | null): void {
  write(FIRST_TOUR_KEY, "done", store)
  changed()
}

let presence: PlacePresence = "unknown"

export function placePresence(): PlacePresence {
  return presence
}

/** Told by whoever reads the machine's Project list. */
export function notePlaces(count: number | null): void {
  const next: PlacePresence = count === null ? "unknown" : count > 0 ? "some" : "none"
  if (next === presence) return
  presence = next
  changed()
}
