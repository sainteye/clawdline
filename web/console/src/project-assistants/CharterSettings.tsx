import { useState } from "react"
import type { CharterProject, CharterSummary } from "./charter-api"
import { charterFormat, charterProblem, charterWord } from "./charter-words"

export interface CharterSettingsRow { project: CharterProject; name: string; bots: string[]; summary?: CharterSummary }

/** The Bots settings "Team charter" tab: one row per Project with its charter state. */
export function CharterSettings({ rows, onOpen, onCreate }: {
  rows: readonly CharterSettingsRow[]; onOpen: (project: CharterProject) => void; onCreate: (row: CharterSettingsRow) => Promise<void>
}) {
  const [creating, setCreating] = useState<string | null>(null)
  const [error, setError] = useState("")
  const create = async (row: CharterSettingsRow) => {
    setCreating(row.project.project_id); setError("")
    try { await onCreate(row) } catch (problem) { setError(charterProblem(problem)) } finally { setCreating(null) }
  }
  return <section className="pa-bs-group" aria-labelledby="pa-bs-charter">
    <h3 id="pa-bs-charter">{charterWord("title")}</h3>
    <p className="pa-bs-note">{charterWord("settingsNote")}</p>
    {rows.length ? <ul className="pa-bs-charters">
      {rows.map(row => {
        const version = row.summary?.version ?? 0
        return <li key={row.project.project_id}>
          <div>
            <strong>{row.name}</strong>
            <small>{version ? charterFormat("settingsVersion", version, row.bots.length) : charterFormat("settingsNone", row.bots.length)}</small>
          </div>
          {version
            ? <button type="button" className="pa-bs-secondary" onClick={() => onOpen(row.project)} aria-label={`${charterWord("open")}: ${row.name}`}>{charterWord("open")}</button>
            : <button type="button" className="pa-bs-primary" disabled={creating !== null} onClick={() => void create(row)} aria-label={`${charterWord("create")}: ${row.name}`}>{creating === row.project.project_id ? charterWord("creating") : charterWord("create")}</button>}
        </li>
      })}
    </ul> : <p className="pa-bs-empty">{charterWord("settingsEmpty")}</p>}
    {error && <p className="pa-bs-error" role="alert">{error}</p>}
  </section>
}
