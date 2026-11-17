import type { KeyboardEvent } from "react"

export type RosterDrag = {kind: "bot" | "project"; key: string}
export function rosterTarget(x: number, y: number): RosterDrag | null {
  const target = document.elementFromPoint(x, y)?.closest<HTMLElement>("[data-roster-drop-kind]")
  const kind = target?.dataset.rosterDropKind, key = target?.dataset.rosterDropKey
  return key && (kind === "bot" || kind === "project") ? {kind, key} : null
}

/** Mouse, touch and pen dragging and arrow-key ordering share one action. */
export function RosterHandle({label, hint, source, onStart, onEnd, onDrop, onHover, onKey}: {
  label: string; hint: string; source: RosterDrag; onStart: (source: RosterDrag) => void; onEnd: () => void;
  onDrop: (target: RosterDrag) => void; onHover: (target: RosterDrag | null) => void; onKey: (event: KeyboardEvent<HTMLButtonElement>) => void
}) {
  return <button type="button" className="pa-roster-handle" aria-label={label} title={hint}
    onClick={event => event.preventDefault()} onKeyDown={onKey}
    onPointerDown={event => {if (event.button !== 0) return; event.preventDefault(); event.currentTarget.setPointerCapture(event.pointerId); onStart(source)}}
    onPointerMove={event => {if (event.currentTarget.hasPointerCapture(event.pointerId)) onHover(rosterTarget(event.clientX, event.clientY))}}
    onPointerUp={event => {if (!event.currentTarget.hasPointerCapture(event.pointerId)) return; const target = rosterTarget(event.clientX, event.clientY); event.currentTarget.releasePointerCapture(event.pointerId); if (target) onDrop(target); onEnd()}}
    onPointerCancel={onEnd}>
    <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill="currentColor"><circle cx="5" cy="4" r="1"/><circle cx="11" cy="4" r="1"/><circle cx="5" cy="8" r="1"/><circle cx="11" cy="8" r="1"/><circle cx="5" cy="12" r="1"/><circle cx="11" cy="12" r="1"/></svg>
  </button>
}
