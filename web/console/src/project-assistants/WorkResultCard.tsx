import { useEffect, useRef, useState } from "react"
import type { ProjectAssistantWorkLink } from "@clawdline/contract"
import type { WorkV2Item } from "../pages/work/api"
import { ChevronIcon } from "../ui/ChevronIcon.js"
import { onStatusRefresh } from "../status-refresh.js"
import type { SourceClient } from "./client"
import type { Words } from "./words"

function WorkStep({ label, detail, tone, commit }: {
  label: string; detail?: string; tone: "done" | "active" | "pending"; commit?: string
}) {
  return <li data-tone={tone}>
    <span aria-hidden="true">{tone === "done" ? "✓" : tone === "active" ? "●" : "·"}</span>
    <strong>{label}</strong>
    {commit ? <code title={commit}>{commit.slice(0, 8)}</code> : detail && <small>{detail}</small>}
  </li>
}

export function WorkResultCard({ work, review, w, source, onOpen }: {
  work: ProjectAssistantWorkLink
  review?: { text?: string; details?: string }
  w: Words
  source?: SourceClient
  onOpen?: (work: ProjectAssistantWorkLink, view?: "session") => void
}) {
  const node = useRef<HTMLElement>(null)
  const workRef = useRef(work)
  workRef.current = work
  const [visible, setVisible] = useState(false)
  const [reading, setReading] = useState<{ source: SourceClient; id: string; state: string; board?: WorkV2Item }>()
  const boardID = work.board_item_id
  const available = !!boardID && !!source?.workItem && source.current?.() !== false
  useEffect(() => {
    if (!available || !node.current) return
    if (typeof IntersectionObserver === "undefined") { setVisible(true); return }
    const observer = new IntersectionObserver(([entry]) => setVisible(entry.isIntersecting))
    observer.observe(node.current)
    return () => observer.disconnect()
  }, [available])
  useEffect(() => {
    if (!visible || !available || !source) return
    let control: AbortController | undefined
    const refresh = () => {
      if (control || document.visibilityState === "hidden" || source.current?.() === false) return
      const request = new AbortController()
      control = request
      void source.workItem!(workRef.current, request.signal).then(
        board => {
          if (!request.signal.aborted && source.current?.() !== false) setReading({ source, id: boardID, state: work.state, board })
        },
        () => {
          if (!request.signal.aborted) setReading({ source, id: boardID, state: work.state })
        },
      ).finally(() => { if (control === request) control = undefined })
    }
    refresh()
    const unsubscribe = onStatusRefresh(refresh)
    const timer = work.state === "landed" ? undefined : window.setInterval(refresh, 30_000)
    return () => { control?.abort(); unsubscribe(); window.clearInterval(timer) }
  }, [source, available, visible, boardID, work.id, work.state, work.target_project_id])

  const item = available && reading?.source === source && reading?.id === boardID && reading?.state === work.state ? reading?.board : undefined
  const gate = item?.verification
  const compact = gate && ("compact" in gate ? gate.compact : gate)
  const round = compact?.latest_round
  const verifies = item?.verify_gate === true
  // Board evidence describes the item candidate. Display it as its own
  // fact even when the individual worker delivered a smaller commit.
  const passed = round?.state === "complete" && round.verdict === "PASS" &&
    round.criteria_digest === item?.acceptance_digest && compact?.current_authorization?.kind === "pass"
  const running = !!round && ["queued", "dispatching", "running"].includes(round.state)
  const needsChanges = round?.state === "complete" && round.verdict !== "PASS"
  const decision = compact?.escalation?.state === "waiting_user"
  const override = !!compact?.current_authorization && compact.current_authorization.kind !== "pass"
  const completed = work.state === "completed" || work.state === "landed"
  const landed = work.state === "landed"
  const waiting = work.state === "waiting_for_person"
  const unknown = work.state === "unknown"
  const status = landed && work.landing_target ? w.workLandedIn.replace("{branch}", work.landing_target)
    : running ? w.workVerification : waiting || decision ? w.rosterBackgroundNeedsYou : unknown ? w.serverUnknown : w[work.state]
  const hasWorker = !!onOpen && !!work.worker_terminal && !!work.worker_session

  return <article ref={node} className="pa-work-task" data-state={work.state} data-verifying={running || undefined}>
    <header className="pa-work-task-head">
      <span className="pa-work-task-state">{status}</span>
      <strong>{work.title || item?.title || w.sessionTask}</strong>
      {item?.project.label && <small>{item.project.label}</small>}
      {(waiting || unknown) && <p>{w[work.state]}</p>}
      {review?.text&&<p role="status">{review.text}</p>}
    </header>
    {(completed || verifies) && <ol className="pa-work-task-steps">
      {completed && <WorkStep label={work.delivery_commit ? w.workDeliveryCompleted : w.completed} tone="done" commit={work.delivery_commit} />}
      {verifies && <WorkStep label={passed ? w.workVerificationPassed : w.workVerification}
        tone={passed ? "done" : running ? "active" : "pending"}
        commit={passed ? round?.candidate_commit : undefined}
        detail={decision ? w.rosterBackgroundNeedsYou : override ? w.workVerificationOverride :
          needsChanges ? w.workVerificationFailed : running ? w.workVerificationRunning : w.workVerificationPending} />}
      {(landed || verifies && completed) && <WorkStep label={w.workLanding} tone={landed ? "done" : "pending"}
        commit={landed ? work.landing_commit : undefined}
        detail={passed || override ? w.workLandingPending : w.workVerificationPending} />}
    </ol>}
    <details className="pa-work-task-details">
      <summary>{w.workEvidence}<ChevronIcon direction="right" /></summary>
      <div className="pa-work-task-reference">
        {boardID && <small>{boardID}</small>}
        <small>{work.task_id}</small>
        {review?.details&&<small>{review.details}</small>}
        {work.landing_commit && <code>{work.landing_commit}</code>}
        {verifies && round?.candidate_commit && <span>{w.workVerification} <code>{round.candidate_commit}</code></span>}
        {available && reading?.source === source && reading?.id === boardID && !item && <small role="status">{w.workVerificationUnavailable}</small>}
      </div>
    </details>
    {(hasWorker || onOpen && boardID) && <footer className="pa-work-task-actions">
      {onOpen && boardID && <button type="button" className="pa-work-task-open" onClick={() => onOpen(work)}>{verifies ? w.workViewVerification : w.openWork}</button>}
      {hasWorker && <button type="button" onClick={() => onOpen?.(work, "session")}>{w.workOpenSession}</button>}
    </footer>}
  </article>
}
