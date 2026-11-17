export interface RosterState {
  pinnedBotKeys: string[]
  projectSortOrder: string[]
  botSortOrder: string[]
}
export type RosterStorage = Pick<Storage, "getItem" | "setItem">
export const rosterStorageKey = (scope: string) => JSON.stringify(["project-assistants", "roster", scope])
export const emptyRosterState = (): RosterState => ({pinnedBotKeys: [], projectSortOrder: [], botSortOrder: []})

/** Preferences identify sources and Bots, never grant access to them. */
export function getRosterState(scope: string, storage?: RosterStorage): RosterState {
  if (!scope || !storage) return emptyRosterState()
  const raw = storage.getItem(rosterStorageKey(scope))
  if (!raw) return emptyRosterState()
  const value: unknown = JSON.parse(raw)
  if (!value || typeof value !== "object") throw new Error("Invalid roster preferences")
  const record = value as Record<string, unknown>
  const strings = (key: string): string[] => {
    const list = record[key] ?? []
    if (!Array.isArray(list) || !list.every(id => typeof id === "string")) throw new Error("Invalid roster order")
    return [...new Set(list)]
  }
  return {pinnedBotKeys: strings("pinnedBotKeys"), projectSortOrder: strings("projectSortOrder"), botSortOrder: strings("botSortOrder")}
}

export function saveRosterState(scope: string, state: RosterState, storage?: RosterStorage): boolean {
  if (!scope || !storage) return false
  try {storage.setItem(rosterStorageKey(scope), JSON.stringify(state)); return true} catch {return false}
}

/** Move only known identities. A filtered view cannot remove hidden preferences. */
export function reorderKeys(order: readonly string[], from: string, to: string): string[] {
  if (from === to || !order.includes(from) || !order.includes(to)) return [...order]
  const result = order.filter(key => key !== from)
  result.splice(order.indexOf(to), 0, from)
  return result
}

export function orderedKeys(current: readonly string[], saved: readonly string[]): string[] {
  const present = new Set(current)
  return [...saved.filter(key => present.has(key)), ...current.filter(key => !saved.includes(key))]
}

export function moveRosterBot(state: RosterState, from: string, to: string, pinned: boolean, current: readonly string[]): RosterState {
  if (!current.includes(from) || !current.includes(to)) return state
  const full = [...state.botSortOrder, ...current.filter(key => !state.botSortOrder.includes(key))]
  const next = reorderKeys(full, from, to)
  return {...state, botSortOrder: next,
    pinnedBotKeys: pinned ? [...state.pinnedBotKeys.filter(key => key !== from), from] : state.pinnedBotKeys.filter(key => key !== from)}
}

export function pinRosterBot(state: RosterState, key: string, pinned: boolean): RosterState {
  return {...state, pinnedBotKeys: pinned ? [...new Set([...state.pinnedBotKeys, key])] : state.pinnedBotKeys.filter(id => id !== key)}
}
