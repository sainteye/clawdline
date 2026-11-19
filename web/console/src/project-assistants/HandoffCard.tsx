import {useId, useState} from "react"
import type * as W from "@clawdline/contract"
import {handoffWords} from "./handoff-card-words"
import "./handoff-card.css"

export function HandoffCard({card, language, enabled, busy, error, onEdit, onKeep, onHistory, onRefresh, onEditing}: {
  card: W.ProjectAssistantHandoffCard | null
  language: string
  enabled: boolean
  busy: boolean
  error: string
  onEdit: (ref: W.ProjectAssistantHandoffRef, text: string) => Promise<boolean>
  onKeep: (ref: W.ProjectAssistantHandoffRef) => void
  onHistory: (bookmark?: string) => void
  onRefresh: () => void
  onEditing: (editing: boolean) => void
}) {
  const w = handoffWords(language)
  const heading = useId()
  const [open, setOpen] = useState(false)
  const [edit, setEdit] = useState<{ref: W.ProjectAssistantHandoffRef; text: string} | null>(null)
  const pending = card?.state === "draft"
  const conflicting = !!edit && (!pending || edit.ref.id !== card.ref.id || edit.ref.revision !== card.ref.revision)
  if (!card && !error && !edit) return null
  const closeEditor = () => {setEdit(null);onEditing(false)}

  return <section tabIndex={-1} className="pa-handoff-card" aria-labelledby={heading} aria-busy={busy || undefined}>
    <h3 id={heading}>{w.title}</h3>
    {card && <p role="status">{w[card.state]}</p>}
    {error && <p role="alert">{error === "pa_stale_version" || error === "pa_stale_generation" || error === "pa_context_stale" ? w.changed : error === "pa_capacity_full" ? w.limit : w.unavailable}</p>}
    {(edit || pending && open) && <div className="pa-handoff-summary">
      {edit ? <form onSubmit={async event => {
        event.preventDefault()
        if (!enabled || busy || conflicting || error) return
        if (await onEdit(edit.ref, edit.text)) closeEditor()
      }}>
        <label>{w.summary}<textarea value={edit.text} rows={6} disabled={busy} onChange={event => setEdit({...edit, text: event.target.value})} /></label>
        {conflicting && !error && <p role="alert">{w.changed}</p>}
        <div className="pa-handoff-actions">
          <button type="submit" disabled={!enabled || busy || conflicting || !!error}>{w.save}</button>
          <button type="button" disabled={busy} onClick={closeEditor}>{w.cancel}</button>
        </div>
      </form> : <p className="pa-handoff-text">{card?.text || w.empty}</p>}
    </div>}
    <div className="pa-handoff-actions">
      {pending && !edit && <button type="button" aria-expanded={open} onClick={() => setOpen(!open)}>{open ? w.hide : w.open}</button>}
      {pending && !edit && <button type="button" disabled={!enabled || busy || !!error} onClick={() => {
        setEdit({ref: card.ref, text: card.text ?? ""})
        onEditing(true)
        setOpen(true)
      }}>{w.edit}</button>}
      {pending && !edit && <button type="button" disabled={!enabled || busy || !!error} onClick={() => onKeep(card.ref)}>{w.keep}</button>}
      {card && <button type="button" onClick={() => onHistory(card.bookmark_message_id)}>{w.history}</button>}
      {error && <button type="button" disabled={busy} onClick={onRefresh}>{w.load}</button>}
    </div>
  </section>
}
