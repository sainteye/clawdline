import type { CharterSummary } from "./charter-api"
import { charterRosterLine, charterWord } from "./charter-words"
import { CharterIcon } from "./CharterPage"

/** The first row of a Project section that has a charter, in the group-room row's shape. */
export function CharterRosterRow({ summary, selected, onSelect }: { summary: CharterSummary; selected: boolean; onSelect: () => void }) {
  const line = charterRosterLine(summary)
  return <button type="button" className="pa-bot-row pa-charter-row" data-charter-project={summary.project.project_id} aria-current={selected || undefined}
    onClick={onSelect} aria-label={`${charterWord("title")}, ${line}`}>
    <span className="pa-charter-mark"><CharterIcon/></span>
    <span className="pa-row-body"><strong>{charterWord("title")}</strong><span className="pa-row-preview" title={line}><span>{line}</span></span></span>
  </button>
}

/** The row in its roster entry, only for a Project section whose charter exists. */
export function CharterRosterEntry({ shown, summary, selected, onOpen, dismiss }: {
  shown: boolean; summary?: CharterSummary; selected: boolean; onOpen?: (summary: CharterSummary) => void; dismiss: () => void
}) {
  if (!shown || !onOpen || !summary || summary.version < 1) return null
  return <div className="pa-roster-entry pa-charter-entry"><CharterRosterRow summary={summary} selected={selected} onSelect={() => { dismiss(); onOpen(summary) }}/></div>
}
