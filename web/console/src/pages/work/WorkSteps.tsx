import { workWord } from "./words.js"
import type { WorkV2Step } from "./api.js"
import { stepState } from "./step-state.js"
import { WorkIcon } from "./WorkIcon.js"

export function WorkSteps({ steps }: { steps?: WorkV2Step[] }) {
  if (!steps?.length) return null
  const done = steps.filter((step) => stepState(step) === "done").length
  const cancelled = steps.filter((step) => stepState(step) === "cancelled").length
  const summary = workWord("stepSummary", { done, cancelled, open: steps.length - done - cancelled })
  return <section className="work-item-steps" aria-label={summary}>
    <p>{summary}</p>
    <ol>{steps.map((step) => {
      const state = stepState(step)
      return <li key={step.id} data-state={state}>
        <WorkIcon name={state === "done" ? "check" : state === "cancelled" ? "close" : "circle"} />
        <span className="work-step-content"><span className="work-step-title">{step.title}</span>
          {state === "cancelled" && <><span className="work-step-cancelled">{workWord("stepCancelled")}</span>
            {step.cancel_reason && <small className="work-step-reason">{step.cancel_reason}</small>}</>}
        </span>
      </li>
    })}</ol>
  </section>
}
