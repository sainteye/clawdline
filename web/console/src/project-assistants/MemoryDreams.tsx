import type { ProjectAssistantMemoryDreamHistory } from "@clawdline/contract"
import { currentCatalogTag } from "../catalog"
import type { Words } from "./words"

export function MemoryDreams({ history, w }: { history?: ProjectAssistantMemoryDreamHistory; w: Words }) {
  if (!history) return <p role="status" className="pa-memory-muted">{w.memoryDreamsUnavailable}</p>
  if (!history.runs.length) return <p role="status" className="pa-memory-muted">{w.memoryDreamsEmpty}</p>

  const date = new Intl.DateTimeFormat(currentCatalogTag(), { dateStyle: "medium", timeStyle: "short" })
  const newestFirst = [...history.runs].sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
  return <ol className="pa-memory-dreams" aria-label={w.memoryDreams}>
    {newestFirst.map(run => {
      const timestamp = Date.parse(run.at)
      const groups = [
        { label: w.memoryDreamsAdded, items: run.added },
        { label: w.memoryDreamsArchived, items: run.archived },
        { label: w.memoryDreamsNotes, items: run.notes },
      ]
      return <li key={run.id}>
        <header>
          {Number.isFinite(timestamp) ? <time dateTime={run.at}>{date.format(timestamp)}</time> : w.memoryTimeUnknown}
          {run.undone && <small>{w.memoryDreamsUndone}</small>}
        </header>
        {groups.filter(group => group.items.length).map(group => <section key={group.label}>
          <h4>{group.label}</h4>
          <ul>{group.items.map((item, index) => <li key={index}>{item}</li>)}</ul>
        </section>)}
      </li>
    })}
  </ol>
}
