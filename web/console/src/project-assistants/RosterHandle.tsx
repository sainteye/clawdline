import type { KeyboardEvent } from "react"

export type RosterDrag = {kind: "bot" | "project"; key: string}
/** Explicit keyboard controls remain available through the global reorder mode. */
export function RosterHandle({label,hint,onKey}:{label:string;hint:string;onKey:(event:KeyboardEvent<HTMLButtonElement>)=>void}) {
  return <button type="button" className="pa-roster-handle" aria-label={label} title={hint} onClick={event=>event.preventDefault()} onKeyDown={onKey}>
    <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill="currentColor"><circle cx="5" cy="4" r="1"/><circle cx="11" cy="4" r="1"/><circle cx="5" cy="8" r="1"/><circle cx="11" cy="8" r="1"/><circle cx="5" cy="12" r="1"/><circle cx="11" cy="12" r="1"/></svg>
  </button>
}
