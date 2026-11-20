import { useState } from "react"
import type { Words } from "../words"
import { FloorError } from "./tower-floors"

/** One Project as the floor panel lists it. */
export interface FloorRow {
  key: string
  machine: string
  project: string
  name: string
  floor: number
  /** The person placed it; otherwise the default rule did. */
  placed: boolean
  /** Its machine answered its floors and this page may change them. */
  ready: boolean
  /** Why it cannot be changed from here, when it cannot. */
  locked?: string
}

/** What a placement that did not finish says, always with the machine's code. */
function floorSentence(error: unknown, w: Words): string {
  const e = error instanceof FloorError ? error : new FloorError("refused", "unavailable")
  const code = e.code || "unavailable"
  if (e.outcome === "refused") return w.towerFloorsRefused.replace("{code}", code)
  const text = e.outcome === "stale" ? w.towerFloorsStale : e.outcome === "unknown" ? w.towerFloorsUnknown : w.towerFloorsUnsupported
  return `${text} (${code})`
}

/** Each Project picks one of the tower's floors. Saving goes to that Project's
 * own machine; a stale or unknown outcome shows the floors read again. */
export function TowerFloorsPanel({ rows, labels, w, onPlace, onClose }: {
  rows: readonly FloorRow[]
  labels: readonly string[]
  w: Words
  onPlace: (row: FloorRow, floor: number) => Promise<void>
  onClose: () => void
}) {
  const [busy, setBusy] = useState<string | null>(null)
  const [note, setNote] = useState<{ key: string, text: string } | null>(null)
  const place = async (row: FloorRow, floor: number) => {
    if (busy || !row.ready || (row.placed && row.floor === floor)) return
    setBusy(row.key); setNote(null)
    try { await onPlace(row, floor) }
    catch (error) {
      setNote({ key: row.key, text: floorSentence(error, w) })
    } finally { setBusy(null) }
  }
  return <div id="pa-tower-floorset" className="pa-tower-waitlist pa-tower-floorset" role="region" aria-label={w.towerFloorsTitle}
    onKeyDown={e => { if (e.key === "Escape") onClose() }}>
    <header><b>{w.towerFloorsTitle}</b><button type="button" aria-label={w.towerFloorsClose} onClick={onClose}>×</button></header>
    <p className="pa-tower-floorset-hint">{w.towerFloorsHint}</p>
    <ol>{rows.map(row => <li key={row.key}>
      <p><strong>{row.name}</strong>{!row.placed && <span>{w.towerFloorAuto}</span>}</p>
      <div role="group" aria-label={w.towerFloorsFor.replace("{project}", row.name)} aria-busy={busy === row.key || undefined}>
        {labels.map((label, i) => <button key={label} type="button" aria-pressed={row.floor === i} disabled={!row.ready || busy !== null}
          aria-label={w.towerFloorsPick.replace("{project}", row.name).replace("{floor}", label)} title={row.locked} onClick={() => void place(row, i)}>{label}</button>)}
      </div>
      {row.locked && <small>{row.locked}</small>}
      {note?.key === row.key && <small role="alert">{note.text}</small>}
    </li>)}</ol>
  </div>
}
