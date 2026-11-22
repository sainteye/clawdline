import type { ProjectAssistantWorkLink } from "@clawdline/contract"
import { WorkIcon } from "../pages/work/WorkIcon"
import { ChevronIcon } from "../ui/ChevronIcon.js"
import type { Words } from "./words"

export function WorkResultCard({ work, w, onOpen }: {
  work: ProjectAssistantWorkLink
  w: Words
  onOpen?: (work: ProjectAssistantWorkLink) => void
}) {
  const reference = work.board_item_id || work.task_id
  const waiting = work.state === "waiting_for_person"
  const unknown = work.state === "unknown"
  const status = waiting ? w.rosterBackgroundNeedsYou : unknown ? w.serverUnknown : w[work.state]

  return <article className="pa-work-task" data-state={work.state}>
    <details>
      <summary>
        <span className="pa-work-task-icon"><WorkIcon name="file" /></span>
        <strong>{w.sessionTask}</strong>
        <span className="pa-work-task-state">{status}</span>
        <ChevronIcon direction="right" />
      </summary>
      <div className="pa-work-task-reference">
        <small>{reference}</small>
        {work.landing_commit && <code>{work.landing_commit}</code>}
      </div>
    </details>
    {onOpen && work.board_item_id && <button type="button" className="pa-work-task-open"
      onClick={() => onOpen(work)} aria-label={w.openWork} title={w.openWork}>
      <WorkIcon name="open" />
    </button>}
    {(waiting || unknown) && <p>{w[work.state]}</p>}
  </article>
}
