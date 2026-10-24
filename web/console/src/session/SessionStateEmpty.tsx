import { nextWord } from "../next-strings.js"
import type { SessionStateFilter } from "./state-filter.js"

/** One compact result, followed by the independent schedule section. */
export function SessionStateEmpty({ filter, incomplete = false, onReset }: {
  filter: Exclude<SessionStateFilter, "all">
  incomplete?: boolean
  onReset: () => void
}) {
  const title = incomplete ? "sessionStateFilterIncomplete" : filter === "attention" ? "sessionStateEmptyAttention" :
    filter === "working" ? "sessionStateEmptyWorking" : "sessionStateEmptyIdle"
  return <section className="session-state-empty" role="status">
    <svg className="session-state-empty-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M4 6h16M7 12h10M10 18h4" />
    </svg>
    <div className="session-state-empty-body">
      <p>{nextWord(title)}</p>
      <button type="button" className="session-state-reset" onClick={onReset}>{nextWord("sessionStateShowAll")}</button>
    </div>
  </section>
}
