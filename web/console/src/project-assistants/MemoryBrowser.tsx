import { useId, useMemo, useState } from "react"
import type * as W from "@clawdline/contract"
import { currentCatalogTag } from "../catalog"
import type { Words } from "./words"

type OpenMemory = (entry: W.ProjectAssistantMemoryRow, opener: HTMLButtonElement) => void

function title(entry: W.ProjectAssistantMemoryRow) {
  return entry.title || entry.text || entry.id
}

function MemoryConcepts({ entries, onOpen, w }: {
  entries: W.ProjectAssistantMemoryRow[]; onOpen: OpenMemory; w: Words
}) {
  const selectID = useId()
  const [selected, setSelected] = useState("")
  const concept = entries.find(entry => entry.id === selected) ?? entries.find(entry => entry.kind === "note") ?? entries[0]
  const related = useMemo(() => {
    if (!concept) return []
    const sourceIDs = new Set(concept.sources.map(source => source.id).filter(Boolean))
    return entries.filter(entry => entry.id !== concept.id && entry.sources.some(source => sourceIDs.has(source.id)))
  }, [concept, entries])

  if (!concept) return null
  return <section className="pa-memory-concepts">
    <header>
      <h4><label htmlFor={selectID}>{w.memoryConcepts}</label></h4>
      <select id={selectID} value={concept.id} onChange={event => setSelected(event.target.value)}>
        {entries.map(entry => <option key={entry.id} value={entry.id}>{title(entry)}</option>)}
      </select>
    </header>
    <button type="button" className="pa-memory-node pa-memory-node-primary" onClick={event => onOpen(concept, event.currentTarget)}>{title(concept)}</button>
    <h5>{w.memoryRelated}</h5>
    {related.length > 0 ? <ul className="pa-memory-branches">
      {related.map(entry => <li key={entry.id}>
        <button type="button" className="pa-memory-node" onClick={event => onOpen(entry, event.currentTarget)}>{title(entry)}</button>
      </li>)}
    </ul> : <p className="pa-memory-muted">{w.memoryNoRelated}</p>}
  </section>
}

function MemoryRows({ heading, entries, files = false, onOpen, w }: {
  heading: string; entries: W.ProjectAssistantMemoryRow[]; files?: boolean; onOpen: OpenMemory; w: Words
}) {
  if (!entries.length) return null
  const date = new Intl.DateTimeFormat(currentCatalogTag(), { dateStyle: "medium", timeStyle: "short" })
  return <section className="pa-memory-collection">
    <h4>{heading}</h4>
    {entries.map(entry => {
      const name = entry.title || (files ? entry.id : "")
      const timestamp = Date.parse(entry.updated_at)
      const author = entry.author === "human" ? w.memoryAuthorHuman : entry.author === "bot" ? w.memoryAuthorBot : entry.author || w.memoryAuthorUnknown
      return <article key={entry.id} className="pa-memory-entry">
        <button type="button" className="pa-memory-index" aria-label={w.memoryRead + ": " + (name || entry.text || entry.id)} onClick={event => onOpen(entry, event.currentTarget)}>
          {name && <strong>{name}</strong>}{entry.text && <span>{entry.text}</span>}
        </button>
        <small>{!files && <>{author} · </>}{Number.isFinite(timestamp)
          ? <time dateTime={entry.updated_at}>{date.format(timestamp)}</time> : w.memoryTimeUnknown}</small>
      </article>
    })}
  </section>
}

export function MemoryBrowser({ layer, onOpen, w }: {
  layer: W.ProjectAssistantMemoryLayer; onOpen: OpenMemory; w: Words
}) {
  if (!layer.entries.length) return <p>{w.emptyMemory}</p>
  if (layer.layer === "project") return <MemoryRows heading={w.memoryProjectFiles} entries={layer.entries} files onOpen={onOpen} w={w}/>
  const notes = layer.entries.filter(entry => entry.kind === "note")
  const facts = layer.entries.filter(entry => entry.kind !== "note")
  return <>
    <MemoryConcepts entries={layer.entries} onOpen={onOpen} w={w}/>
    <MemoryRows heading={w.memoryTopicNotes} entries={notes} onOpen={onOpen} w={w}/>
    <MemoryRows heading={w.memoryFacts} entries={facts} onOpen={onOpen} w={w}/>
  </>
}
