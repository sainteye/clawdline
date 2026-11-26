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
