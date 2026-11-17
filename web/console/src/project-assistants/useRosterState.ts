import { useEffect, useState } from "react"
import { emptyRosterState, getRosterState, rosterStorageKey, saveRosterState, type RosterState } from "./roster-state"

function read(scope: string) {
  try {return {state: getRosterState(scope, window.localStorage), problem: false}} catch {return {state: emptyRosterState(), problem: true}}
}

/** The owning roster is keyed by its authenticated account namespace. */
export function useRosterState(scope: string) {
  const [value, setValue] = useState(() => read(scope))
  useEffect(() => {
    const changed = (event: StorageEvent) => {if (event.key === null || event.key === rosterStorageKey(scope)) setValue(read(scope))}
    window.addEventListener("storage", changed)
    return () => window.removeEventListener("storage", changed)
  }, [scope])
  const update = (state: RosterState) => {
    let saved = false
    try {saved = saveRosterState(scope, state, window.localStorage)} catch { /* Storage may be denied. Keep this view usable. */ }
    setValue({state, problem: !saved})
  }
  return {...value, update}
}
